package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"sunomaker/internal/platform"
)

// ---------------------------------------------------------------------------
// Asking the configured provider a question
//
// One entry point — askAI — and two ways it gets answered, decided by how the
// producer connected the provider:
//
//	api_key   an HTTP call to the provider's chat endpoint with the stored key.
//	account   the provider's signed-in CLI, run headless, billed to the
//	          subscription.
//
// No tools and no MCP on either path. Everything this app asks a model is a
// question about what the producer just said — interview a style, name an
// album, write a prompt — and a turn that could go and read the database is a
// turn that might, mid-sentence, while somebody waits. The MCP server (mcp.go)
// exists for the other direction: an agent asking *this app* questions.
// ---------------------------------------------------------------------------

// aiChatHTTP is how this file reaches a provider. Bounded, and a variable so a
// test can point it at a stub — the same arrangement aiVerifyHTTP uses.
//
// Three minutes rather than the verify client's twenty seconds: this one waits
// on a model writing several paragraphs, and a style breakdown that timed out at
// the two-minute mark would have cost the tokens and produced nothing.
var aiChatHTTP = &http.Client{Timeout: 3 * time.Minute}

// chatMsg is one turn of a conversation. Role is "user" or "assistant".
type chatMsg struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// aiChatURL is a provider's chat endpoint. Anthropic has its own Messages shape;
// OpenAI and Grok share the chat-completions one.
func aiChatURL(id string) string {
	switch id {
	case serviceOpenAI:
		return "https://api.openai.com/v1/chat/completions"
	case serviceGrok:
		return "https://api.x.ai/v1/chat/completions"
	case serviceGemini:
		// The OpenAI-compatibility endpoint, so Gemini rides the same request
		// and response shape OpenAI and Grok do (#26).
		return "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"
	default:
		return "https://api.anthropic.com/v1/messages"
	}
}

// defaultChatModel is the model used when the producer has not named one. The
// "-latest" aliases where a provider offers them, so this does not name a
// specific version that will be retired; a producer who wants a particular model
// sets it in Settings.
func defaultChatModel(id string) string {
	switch id {
	case serviceOpenAI:
		return "gpt-4o"
	case serviceGrok:
		return "grok-2-latest"
	case serviceGemini:
		return "gemini-2.5-flash"
	default:
		return "claude-sonnet-4-5"
	}
}

// aiMaxTokens bounds one answer. Generous, because the longest thing asked for
// here is a full lyric sheet, and an answer cut off mid-verse is worse than no
// answer — the producer cannot tell which it is without reading to the end.
const aiMaxTokens = 8000

// AIReadiness is whether there is anything to hold a conversation with, and why
// not when there is not.
//
// Asked before a dialog offers to start, because every creation flow in this app
// is an interview: a producer whose machine cannot run one must be told that on
// the page, not left pressing a button that fails.
type AIReadiness struct {
	Ready bool `json:"ready"`
	// Service is the provider that would answer, so the dialog can say which.
	Service string `json:"service"`
	Name    string `json:"name"`
	Why     string `json:"why"`
}

// GetAIReadiness reports whether an interview can run here.
func (a *App) GetAIReadiness() AIReadiness {
	connected := a.connectedAIServices()
	if len(connected) == 0 {
		return AIReadiness{Why: "No AI provider is connected — connect one in " +
			"Settings → AI."}
	}
	id := a.defaultAIService()
	return AIReadiness{Ready: true, Service: id, Name: aiServiceName(id)}
}

// askAI answers one turn with the configured provider.
//
// system is the standing brief — who the model is being asked to be. turns is
// the conversation, oldest first, and must end with the producer's message.
func (a *App) askAI(ctx context.Context, system string, turns []chatMsg) (string, error) {
	id := a.defaultAIService()
	conns := a.aiConnections()
	conn := conns[id]

	// A connected service with a key first, whatever the starred one is. The
	// direct path is the one that works the same on every machine; falling
	// through to it when the starred provider is account-mode-only would be
	// wrong (it would bill the wrong account), but falling through when the
	// starred one is not connected at all is exactly right.
	if conn.Mode == modeAPIKey && conn.Key != "" {
		return chatViaAPI(ctx, id, conn.Key, a.aiServiceModel(id), system, turns)
	}
	if conn.Mode == modeAccount {
		return chatViaCLI(ctx, id, a.aiServiceModel(id), system, turns)
	}
	for _, other := range a.connectedAIServices() {
		if other == id {
			continue
		}
		if key := conns[other].Key; key != "" {
			return chatViaAPI(ctx, other, key, a.aiServiceModel(other), system, turns)
		}
	}
	return "", fmt.Errorf(
		"no AI provider is connected — connect Claude, ChatGPT or Grok in Settings → AI")
}

// normalizeTurns makes a turn list every provider will accept: no empty text, a
// user turn first, and roles strictly alternating.
//
// Anthropic rejects a history that starts with the assistant or repeats a role,
// and a transcript can do both — a conversation resumed mid-thread, or two of
// the producer's messages with a failed one dropped between them — so
// consecutive same-role turns are joined rather than sent as-is. Any role that
// is not "assistant" reads as "user".
func normalizeTurns(msgs []chatMsg) []chatMsg {
	out := []chatMsg{}
	for _, m := range msgs {
		if strings.TrimSpace(m.Text) == "" {
			continue
		}
		role := "user"
		if m.Role == "assistant" {
			role = "assistant"
		}
		if len(out) == 0 && role == "assistant" {
			continue // a conversation the model completes must open with the user
		}
		if n := len(out); n > 0 && out[n-1].Role == role {
			out[n-1].Text += "\n\n" + m.Text
			continue
		}
		out = append(out, chatMsg{Role: role, Text: m.Text})
	}
	return out
}

// ---------------------------------------------------------------------------
// The direct path
// ---------------------------------------------------------------------------

func chatViaAPI(ctx context.Context, id, key, model, system string, turns []chatMsg) (string, error) {
	turns = normalizeTurns(turns)
	if len(turns) == 0 {
		return "", fmt.Errorf("there is nothing to send")
	}
	if model == "" {
		model = defaultChatModel(id)
	}

	var body []byte
	var err error
	if id == serviceAnthropic {
		messages := make([]map[string]string, 0, len(turns))
		for _, t := range turns {
			messages = append(messages, map[string]string{"role": t.Role, "content": t.Text})
		}
		body, err = json.Marshal(map[string]any{
			"model": model, "max_tokens": aiMaxTokens,
			"system": system, "messages": messages,
		})
	} else {
		// The OpenAI shape carries the brief as a first message rather than as
		// its own field, which is the only structural difference between the two
		// requests this app makes.
		messages := []map[string]string{{"role": "system", "content": system}}
		for _, t := range turns {
			messages = append(messages, map[string]string{"role": t.Role, "content": t.Text})
		}
		body, err = json.Marshal(map[string]any{
			"model": model, "max_tokens": aiMaxTokens, "messages": messages,
		})
	}
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, aiChatURL(id), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if id == serviceAnthropic {
		req.Header.Set("x-api-key", key)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := aiChatHTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not reach %s: %w", aiServiceName(id), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		// The body carries the provider's own explanation — a model that does
		// not exist, a key without credit — and dropping it leaves the producer
		// with a status code and nothing to act on.
		var reason bytes.Buffer
		_, _ = reason.ReadFrom(resp.Body)
		return "", fmt.Errorf("%s answered %s: %s",
			aiServiceName(id), resp.Status, strings.TrimSpace(reason.String()))
	}

	if id == serviceAnthropic {
		var answer struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
			return "", err
		}
		var text strings.Builder
		for _, block := range answer.Content {
			if block.Type == "text" {
				text.WriteString(block.Text)
			}
		}
		return strings.TrimSpace(text.String()), nil
	}

	var answer struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		return "", err
	}
	if len(answer.Choices) == 0 {
		return "", fmt.Errorf("%s returned no answer", aiServiceName(id))
	}
	return strings.TrimSpace(answer.Choices[0].Message.Content), nil
}

// ---------------------------------------------------------------------------
// The account path
// ---------------------------------------------------------------------------

// cliChatTimeout bounds a headless CLI run. Longer than the HTTP client's, since
// a CLI has a cold start and a sign-in check in front of the same model call.
const cliChatTimeout = 5 * time.Minute

// chatViaCLI runs the provider's signed-in CLI headlessly and returns what it
// printed.
//
// The whole conversation is flattened into one prompt rather than a session
// being resumed. Both CLIs can hold a session, and using one would mean this app
// owning session ids, their expiry and their cleanup for a conversation that is
// minutes long and usually ends in a record or in nothing. Re-sending a short
// transcript costs a little and keeps this file to one process per turn.
func chatViaCLI(ctx context.Context, id, model, system string, turns []chatMsg) (string, error) {
	cli := aiCLIName(id)
	path, found := lookUpCommand(cli)
	if !found {
		return "", fmt.Errorf(
			"%s was not found on this machine — reconnect %s in Settings → AI",
			cli, aiServiceName(id))
	}

	ctx, cancel := context.WithTimeout(ctx, cliChatTimeout)
	defer cancel()

	prompt := flattenForCLI(system, turns)

	var cmd *exec.Cmd
	switch id {
	case serviceAnthropic:
		// -p is print mode: one turn, answer to stdout, no interactive session.
		// text rather than stream-json because nothing here streams — the dialog
		// shows a working line and then the whole reply.
		args := []string{"-p", "--output-format", "text"}
		if model != "" {
			args = append(args, "--model", model)
		}
		cmd = exec.CommandContext(ctx, path, args...)
		cmd.Stdin = strings.NewReader(prompt)
	case serviceOpenAI:
		// codex exec is Codex's headless mode. --skip-git-repo-check because the
		// working directory below is a temp folder and not a checkout; without it
		// the CLI refuses to run at all.
		args := []string{"exec", "--skip-git-repo-check"}
		if model != "" {
			args = append(args, "--model", model)
		}
		args = append(args, prompt)
		cmd = exec.CommandContext(ctx, path, args...)
	case serviceGemini, serviceAntigravity:
		// -p is the non-interactive mode both Google CLIs share — one prompt
		// in, the answer on stdout, no session (#26, #27). agy has had releases
		// where -p dropped stdout under a pipe; that surfaces here as its
		// "returned nothing" error rather than a hang, which is the version of
		// that failure a producer can act on.
		args := []string{"-p", prompt}
		if model != "" {
			args = append(args, "--model", model)
		}
		cmd = exec.CommandContext(ctx, path, args...)
	default:
		return "", fmt.Errorf("%s has no CLI to run", aiServiceName(id))
	}

	// A neutral working directory: without one the CLI picks up whatever project
	// it happens to be launched inside and starts answering about that repository
	// instead of about the question.
	cmd.Dir = os.TempDir()
	// An account-mode run must not inherit an API key — the CLIs prefer one over
	// the subscription login, and would quietly bill the wrong thing.
	cmd.Env = envWithout("ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY")
	platform.HideWindow(cmd)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("%s could not answer: %s", cli, detail)
	}
	answer := strings.TrimSpace(stdout.String())
	if answer == "" {
		// A clean exit with nothing on stdout. The reason, when there is one,
		// is on stderr: agy's engine explains itself there — "a tool required
		// the command permission that headless mode cannot prompt for" was
		// exactly the line this app used to swallow (#67). Surfaced, the
		// producer can act on it; swallowed, it was "returned nothing".
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return "", fmt.Errorf("%s printed no answer: %s", cli, detail)
		}
		return "", fmt.Errorf(
			"%s ran but its answer never reached this app — run `%s -p \"hello\"` in a terminal to see whether answers print there",
			cli, cli)
	}
	return answer, nil
}

// flattenForCLI renders a brief and a transcript as one prompt.
//
// Labelled turns rather than a bare concatenation: without the labels a model
// reading its own previous answer treats it as further instruction from the
// producer, and the interview starts agreeing with itself.
func flattenForCLI(system string, turns []chatMsg) string {
	var b strings.Builder
	b.WriteString(system)
	b.WriteString("\n\n---\n\n")
	turns = normalizeTurns(turns)
	for i, t := range turns {
		who := "Producer"
		if t.Role == "assistant" {
			who = "You"
		}
		if i == len(turns)-1 && t.Role == "user" {
			// The last turn is the one being answered, and saying so is what
			// stops the model summarising the transcript instead of replying to
			// it.
			b.WriteString("Producer (answer this): ")
			b.WriteString(t.Text)
			b.WriteString("\n")
			continue
		}
		b.WriteString(who)
		b.WriteString(": ")
		b.WriteString(t.Text)
		b.WriteString("\n\n")
	}
	return b.String()
}
