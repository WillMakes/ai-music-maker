package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Filing into Jax
//
// Every report in this queue wants a counterpart in Jax. The work happens in
// this repository, but the producer watches the queue from there, so a report
// that never reaches Jax is work nobody can see.
//
// Jax has no HTTP API of its own; what it has is its MCP server, a loopback
// JSON-RPC endpoint, and save_debug_report is the tool for this. Calling the
// same tool the same way is also why this needs no agreement from Jax to keep
// working.
//
// Reaching it takes two facts, and they come from two different files because
// Jax keeps them apart:
//
//	~/.jax/mcp.json      the endpoint. Written when Jax starts and removed when
//	                     it stops, so its absence is how "Jax is not running" is
//	                     known without dialling anything.
//	an MCP client config the bearer token, from the entry Jax registered with
//	                     Claude Code or Claude Desktop.
//
// The split matters: the token is persisted in Jax's settings and survives
// restarts, while the port is picked afresh every launch. That is what makes a
// config file written weeks ago a good source for one and a useless source for
// the other.
//
// Nothing here may fail a filing. Jax is a separate application and is very
// often simply closed; a bug report refused because the tracker was not running
// is a bug report lost, which is strictly worse than an unlinked one. A push
// that does not happen leaves jaxReportId at 0 — which `bugs list --unlinked`
// exists to find and catchUpJaxFilings sweeps on the next launch.
// ---------------------------------------------------------------------------

const (
	// jaxServerName is what Jax calls itself in an MCP client config, and so the
	// key its entry sits under.
	jaxServerName = "jax"
	// jaxTokenEnv carries the token to Jax's stdio proxy, which is the shape the
	// Claude Code and Claude Desktop entries take. The direct HTTP entries Jax
	// writes for its own headless runs carry it as an Authorization header
	// instead, so both are read.
	jaxTokenEnv = "JAX_MCP_TOKEN"

	jaxSaveTool    = "save_debug_report"
	jaxClaimTool   = "check_out_debug_report"
	jaxResolveTool = "delete_debug_report"
)

// errJaxUnavailable is "there is nowhere to file this": Jax is closed, its token
// cannot be found, or this checkout has no repository slug to name a queue with.
// An ordinary state rather than a failure — every one of those is a thing that
// is simply true sometimes — so callers report it to nobody.
var errJaxUnavailable = errors.New("no Jax to file into")

// jaxClient is bounded: Jax answers on loopback in milliseconds when it is there
// at all, and a filing must never sit waiting on a tracker.
var jaxClient = &http.Client{Timeout: 10 * time.Second}

// jaxTarget is a filing destination: a running Jax, and which of its queues the
// report belongs in.
type jaxTarget struct {
	url   string
	token string
}

// fileReportInJax gives a report a counterpart in Jax and returns the id it was
// given there. errJaxUnavailable means there was nowhere to file it.
//
// A free function rather than a method so App.toJax can hold it as a field and a
// test can substitute one — filing reaches out to another application on
// loopback, and a test suite must never depend on whether Jax is open.
//
// app is the repository the report was filed against, which is how Jax names
// the app. It is handed in rather than looked up here because looking it up
// here meant reading the working directory, which named whichever project the
// process was started in (#94).
func fileReportInJax(report BugReport, app string) (int, error) {
	if app == "" {
		return 0, errJaxUnavailable
	}
	target, err := findJax()
	if err != nil {
		return 0, err
	}
	result, err := target.call(jaxSaveTool, map[string]any{
		"app":         app,
		"description": report.Description,
		"route":       report.Route,
		"global":      report.Global,
	})
	if err != nil {
		return 0, err
	}
	return readJaxReportID(result), nil
}

// findJaxTarget locates the running Jax. A variable so a test can point the
// whole file at a stub — every call here goes through it, so one variable
// redirects all of them, and a suite must never depend on whether the producer
// happens to have Jax open.
var findJaxTarget = findJax

// findJax locates the running Jax and the queue to file into.
//
// The endpoint file first, because a Jax on this machine is the ordinary case
// and its file is written afresh at every launch — the only source that cannot
// be out of date. Then a configured one, which is how a Jax running elsewhere on
// the network is reached at all: the url and the token are taken from the same
// entry, because another machine's Jax has another machine's token and pairing a
// remote endpoint with the local token would be a request refused for a reason
// nobody could see.
func findJax() (jaxTarget, error) {
	url, err := jaxRuntimeURL()
	token := jaxToken()
	if err != nil {
		url, token = jaxEndpoint()
	}
	if url == "" || token == "" {
		return jaxTarget{}, errJaxUnavailable
	}
	return jaxTarget{url: url, token: token}, nil
}

// jaxRuntimeURL reads ~/.jax/mcp.json, which Jax writes at launch and removes on
// exit.
func jaxRuntimeURL() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(home, ".jax", "mcp.json"))
	if err != nil {
		return "", err
	}
	var runtime struct {
		URL  string `json:"url"`
		Port int    `json:"port"`
	}
	if err := json.Unmarshal(data, &runtime); err != nil {
		return "", err
	}
	if runtime.URL != "" {
		return runtime.URL, nil
	}
	if runtime.Port == 0 {
		return "", errJaxUnavailable
	}
	return fmt.Sprintf("http://127.0.0.1:%d/mcp", runtime.Port), nil
}

// mcpClientConfigs are the files an MCP client keeps its server entries in, in
// the order they are searched. Read-only here: this file never writes them.
func mcpClientConfigs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	paths := []string{filepath.Join(home, ".claude.json")}
	if appData := os.Getenv("APPDATA"); appData != "" {
		paths = append(paths, filepath.Join(appData, "Claude", "claude_desktop_config.json"))
	}
	paths = append(paths,
		filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json"),
		filepath.Join(home, ".config", "Claude", "claude_desktop_config.json"))
	return paths
}

// mcpServerEntry is one server's entry in a client config, in the two shapes Jax
// writes: a stdio proxy carrying the token in env, or a direct HTTP entry
// carrying it as a header.
type mcpServerEntry struct {
	URL     string            `json:"url"`
	Env     map[string]string `json:"env"`
	Headers map[string]string `json:"headers"`
}

// jaxToken finds the bearer token in whichever client config has Jax's entry.
func jaxToken() string {
	url, token := jaxEntry()
	_ = url
	return token
}

// jaxEndpoint is the url and token of a configured Jax, for the case where none
// is running here. Both from the same entry — see findJax.
func jaxEndpoint() (string, string) {
	return jaxEntry()
}

func jaxEntry() (string, string) {
	for _, path := range mcpClientConfigs() {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var config struct {
			MCPServers map[string]mcpServerEntry `json:"mcpServers"`
		}
		// A file this app cannot parse belongs to another application and is
		// none of its business — skipped rather than reported.
		if err := json.Unmarshal(data, &config); err != nil {
			continue
		}
		entry, ok := config.MCPServers[jaxServerName]
		if !ok {
			continue
		}
		token := entry.Env[jaxTokenEnv]
		if token == "" {
			for name, value := range entry.Headers {
				if strings.EqualFold(name, "Authorization") {
					token = strings.TrimSpace(strings.TrimPrefix(value, "Bearer "))
				}
			}
		}
		if token != "" {
			return entry.URL, token
		}
	}
	return "", ""
}

// call invokes one MCP tool over Jax's loopback JSON-RPC endpoint.
func (t jaxTarget) call(tool string, args map[string]any) (string, error) {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  map[string]any{"name": tool, "arguments": args},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, t.url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	// Both are sent: the streamable-HTTP transport requires the event-stream
	// accept even for a call whose answer is a single JSON object.
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+t.token)

	resp, err := jaxClient.Do(req)
	if err != nil {
		return "", errJaxUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", errJaxUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// readJaxReportID digs the new report's id out of the tool's answer.
//
// The answer is an MCP tool result: JSON-RPC around a content block whose text
// is itself JSON. Rather than model all three layers for one integer, this looks
// for the id in the text — the shape of a tool result is Jax's to change, and a
// strict decode here would break on a change that does not affect this at all.
//
// Answering 0 is not a failure. It means the pairing is unknown, which is the
// same state as "Jax was closed", and the sweep that fixes one fixes the other.
func readJaxReportID(raw string) int {
	var envelope struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return 0
	}
	for _, block := range envelope.Result.Content {
		var report struct {
			ID       int `json:"id"`
			ReportID int `json:"reportId"`
		}
		if err := json.Unmarshal([]byte(block.Text), &report); err != nil {
			continue
		}
		if report.ID != 0 {
			return report.ID
		}
		if report.ReportID != 0 {
			return report.ReportID
		}
	}
	return 0
}

// ---------------------------------------------------------------------------
// The app's side
// ---------------------------------------------------------------------------

// linkToJax gives a report a counterpart and writes the pairing down.
//
// The pairing is stored on the report rather than remembered, so which reports
// are already in Jax survives one process stopping and another starting.
func (a *App) linkToJax(report BugReport) BugReport {
	if a.toJax == nil || report.JaxReportID != 0 {
		return report
	}
	id, err := a.toJax(report, a.bugRepo().Slug)
	if err != nil || id == 0 {
		return report
	}
	report.JaxReportID = id
	if a.store != nil {
		_ = a.store.saveBugReport(report)
	}
	return report
}

// jaxRefusal reads a tool-level refusal out of an MCP result, answering "" for a
// call that was accepted (#15).
//
// A refused call is not a failed request. MCP answers HTTP 200 and puts the
// refusal in the body — {"result":{"isError":true,"content":[…]}} — which is the
// same shape a success takes with one flag flipped. So checking the transport
// alone reads a refusal as a success, which is how this app came to believe it
// had told Jax things it never told it.
//
// An answer that cannot be read answers "" rather than guessing. The shape of a
// tool result is Jax's to change, and a strict decode here would put a permanent
// warning on every report the moment it did.
func jaxRefusal(raw string) string {
	var envelope struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return ""
	}
	if !envelope.Result.IsError {
		return ""
	}
	for _, block := range envelope.Result.Content {
		if text := strings.TrimSpace(block.Text); text != "" {
			return text
		}
	}
	// Refused with nothing to quote. It still has to say something: an empty
	// note is indistinguishable from no refusal at all, which is the bug.
	return "Jax refused the call without saying why"
}

// claimInJax tells Jax the report is being worked, and reports back whatever Jax
// said if it declined to record it.
//
// The returned note is not an error and does not stop the claim. Jax refusing —
// because the report is not the oldest workable one in its own queue, or because
// somebody else holds it — is a fact about Jax's queue, and a claim abandoned on
// the strength of it would be this queue failing at its job to keep a second one
// tidy (see the top of this file). What must not happen is the refusal going
// unmentioned: the two queues then disagree and nothing says so.
func (a *App) claimInJax(report BugReport) string {
	if report.JaxReportID == 0 {
		return ""
	}
	target, err := findJaxTarget()
	if err != nil {
		// Jax is closed. An ordinary state, not a refusal.
		return ""
	}
	raw, err := target.call(jaxClaimTool, map[string]any{"id": report.JaxReportID})
	if err != nil {
		return ""
	}
	return jaxRefusal(raw)
}

// resolveInJax clears the report there, and reports a refusal for claimInJax's
// reason: the fix is on main either way, and the queues disagreeing in silence
// is the thing being fixed.
func (a *App) resolveInJax(report BugReport) string {
	if report.JaxReportID == 0 {
		return ""
	}
	target, err := findJaxTarget()
	if err != nil {
		return ""
	}
	raw, err := target.call(jaxResolveTool, map[string]any{"id": report.JaxReportID})
	if err != nil {
		return ""
	}
	return jaxRefusal(raw)
}

// catchUpJaxFilings files the reports that have no counterpart yet.
//
// Reports filed while the app was closed — by an agent through the CLI, or by
// the MCP server — have none, and launch is the only moment this process can
// notice. Once, on startup, rather than on a timer: a report that Jax keeps
// refusing should not be retried every minute for the life of the window.
func (a *App) catchUpJaxFilings(ctx context.Context) {
	if a.store == nil || a.toJax == nil {
		return
	}
	// A pause before the first attempt. Jax and this app are often started
	// together, and dialling a tracker that is three seconds from listening
	// would leave every report unlinked for a reason that has already stopped
	// being true.
	select {
	case <-ctx.Done():
		return
	case <-time.After(10 * time.Second):
	}

	reports, err := a.store.bugReports()
	if err != nil {
		return
	}
	filed := false
	for _, report := range reports {
		if ctx.Err() != nil {
			return
		}
		if report.JaxReportID != 0 {
			continue
		}
		if linked := a.linkToJax(report); linked.JaxReportID != 0 {
			filed = true
		}
	}
	if filed {
		a.store.changed(scopeBugReports)
	}
}
