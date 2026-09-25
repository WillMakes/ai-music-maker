package app

import (
	"strings"
	"testing"
)

// Gemini joins the roster the way Grok did (#26): named, listed, keyed to the
// OpenAI-compatible surface, with the `gemini` CLI for account mode.

func TestGeminiIsAFullProvider(t *testing.T) {
	found := false
	for _, id := range aiServiceIDs {
		if id == serviceGemini {
			found = true
		}
	}
	if !found {
		t.Fatal("gemini is not in the provider list")
	}
	if aiServiceName(serviceGemini) != "Gemini" {
		t.Fatalf("wrong name: %q", aiServiceName(serviceGemini))
	}
	if aiCLIName(serviceGemini) != "gemini" {
		t.Fatalf("wrong CLI: %q", aiCLIName(serviceGemini))
	}
	if aiKeysURL(serviceGemini) == "" || aiInstallURL(serviceGemini) == "" {
		t.Fatal("gemini is missing its keys or install link")
	}

	// Both the verification and the chat go to the OpenAI-compatibility
	// surface, which is what lets Gemini ride the existing request shape.
	req, err := modelsRequest(serviceGemini, "k")
	if err != nil {
		t.Fatalf("modelsRequest: %v", err)
	}
	if !strings.Contains(req.URL.String(), "generativelanguage.googleapis.com") ||
		!strings.Contains(req.URL.String(), "/openai/") {
		t.Fatalf("models URL is not the compatibility surface: %s", req.URL)
	}
	if req.Header.Get("Authorization") == "" {
		t.Fatal("the key is not sent as a bearer token")
	}
	if !strings.Contains(aiChatURL(serviceGemini), "/openai/chat/completions") {
		t.Fatalf("chat URL is not the compatibility surface: %s", aiChatURL(serviceGemini))
	}
	if defaultChatModel(serviceGemini) == "" {
		t.Fatal("gemini has no default model")
	}
}

func TestGoogleAssistantsAreRegistrable(t *testing.T) {
	app := newTestApp(t)
	withMCPConfigHome(t)

	status := app.GetMCPStatus()
	names := map[string]bool{}
	for _, client := range status.Clients {
		names[client.ID] = true
	}
	for _, want := range []string{clientGeminiCLI, clientAntigravity} {
		if !names[want] {
			t.Fatalf("%s is not in the assistants list: %v", want, names)
		}
	}

	// Both keep JSON configs, so both go through the surgical JSON writer the
	// other JSON clients use — distinct paths under the one ~/.gemini home.
	geminiPath, err := clientConfigPath(clientGeminiCLI)
	if err != nil {
		t.Fatalf("gemini-cli path: %v", err)
	}
	antigravityPath, err := clientConfigPath(clientAntigravity)
	if err != nil {
		t.Fatalf("antigravity path: %v", err)
	}
	if geminiPath == antigravityPath {
		t.Fatal("the two Google clients share one config file")
	}
	if !strings.Contains(geminiPath, ".gemini") || !strings.Contains(antigravityPath, ".gemini") {
		t.Fatalf("unexpected homes: %s / %s", geminiPath, antigravityPath)
	}
}

// Antigravity is Grok's inverse (#27): a provider with a CLI and no API, so
// account mode is the whole of how it connects and a pasted key is refused
// with a message rather than stored.
func TestAntigravityIsCLIOnly(t *testing.T) {
	app := newTestApp(t)

	found := false
	for _, id := range aiServiceIDs {
		if id == serviceAntigravity {
			found = true
		}
	}
	if !found {
		t.Fatal("antigravity is not in the provider list")
	}
	if aiCLIName(serviceAntigravity) != "agy" {
		t.Fatalf("wrong CLI: %q", aiCLIName(serviceAntigravity))
	}
	if aiKeysURL(serviceAntigravity) != "" {
		t.Fatal("antigravity claims to have somewhere to make a key")
	}

	// A key is refused before anything is stored.
	if _, err := app.ConnectAIKey(serviceAntigravity, "not-a-real-key"); err == nil {
		t.Fatal("a key was accepted for a provider with no API")
	}

	// The CLI path is the one that works.
	withCLIs(t, "agy")
	status, err := app.ConnectAIAccount(serviceAntigravity)
	if err != nil {
		t.Fatalf("connect by CLI: %v", err)
	}
	if !status.Connected || status.Mode != modeAccount {
		t.Fatalf("not connected in account mode: %+v", status)
	}
}
