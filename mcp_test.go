package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// initialize runs one initialize request through dispatch and returns the
// result as a map.
func initialize(t *testing.T, app *App, params string) map[string]any {
	t.Helper()
	result, rpcErr := dispatchMCP(app, jsonRPCRequest{
		JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "initialize",
		Params: json.RawMessage(params),
	})
	if rpcErr != nil {
		t.Fatalf("initialize failed: %v", rpcErr.Message)
	}
	out, _ := result.(map[string]any)
	return out
}

// TestInitializeOrientsTheClient is #89. A producer asked Codex for SunoMaker
// "at a glance" and was told the app had no such view: the server was
// connected, but nothing in the handshake said where to start, and the
// overview tool's description did not contain the words the model searched
// for. The handshake now carries instructions that name get_documentation,
// and get_documentation says what it is.
func TestInitializeOrientsTheClient(t *testing.T) {
	app := newTestApp(t)
	result := initialize(t, app, `{"protocolVersion":"2025-06-18"}`)

	instructions, _ := result["instructions"].(string)
	if !strings.Contains(instructions, "get_documentation") {
		t.Errorf("the instructions do not say where to start: %q", instructions)
	}
	info, _ := result["serverInfo"].(map[string]any)
	if info["title"] != "SunoMaker" || info["name"] != mcpServerName {
		t.Errorf("serverInfo does not name the app: %v", info)
	}

	var overview *mcpTool
	for _, tool := range mcpTools() {
		if tool.name == "get_documentation" {
			t := tool
			overview = &t
		}
	}
	if overview == nil {
		t.Fatal("no get_documentation tool")
	}
	lower := strings.ToLower(overview.description)
	if !strings.Contains(lower, "at a glance") || !strings.Contains(lower, "overview") {
		t.Errorf("get_documentation does not describe itself as the overview: %q", overview.description)
	}
}

// TestInitializeEchoesASupportedRevision: the protocol says a server answers
// with the revision the client proposed when it supports it, and with one it
// does support otherwise. Always answering the oldest was neither.
func TestInitializeEchoesASupportedRevision(t *testing.T) {
	app := newTestApp(t)
	for proposed, want := range map[string]string{
		"2024-11-05": "2024-11-05",
		"2025-03-26": "2025-03-26",
		"2025-06-18": "2025-06-18",
		"1999-01-01": mcpProtocolLatest,
		"":           mcpProtocolLatest,
	} {
		result := initialize(t, app, `{"protocolVersion":"`+proposed+`"}`)
		if result["protocolVersion"] != want {
			t.Errorf("proposed %q: want %q, got %v", proposed, want, result["protocolVersion"])
		}
	}
	// No params at all is what the first tools/list smoke test sends.
	if result := initialize(t, app, `{}`); result["protocolVersion"] != mcpProtocolLatest {
		t.Errorf("no proposal: got %v", result["protocolVersion"])
	}
}

// TestListingsAnswerEmptyRatherThanUnsupported: Codex lists resources and
// prompts whether or not a server declared them, and logged a failure for
// this server on every connection. There are none, and that is an answer.
func TestListingsAnswerEmptyRatherThanUnsupported(t *testing.T) {
	app := newTestApp(t)
	for method, key := range map[string]string{
		"resources/list":           "resources",
		"resources/templates/list": "resourceTemplates",
		"prompts/list":             "prompts",
	} {
		result, rpcErr := dispatchMCP(app, jsonRPCRequest{
			JSONRPC: "2.0", ID: json.RawMessage("1"), Method: method,
		})
		if rpcErr != nil {
			t.Errorf("%s answered an error: %v", method, rpcErr.Message)
			continue
		}
		out, _ := result.(map[string]any)
		list, ok := out[key].([]any)
		if !ok || len(list) != 0 {
			t.Errorf("%s: want an empty %s list, got %v", method, key, out)
		}
	}
	// Unknown methods are still unknown.
	if _, rpcErr := dispatchMCP(app, jsonRPCRequest{
		JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "resources/read",
	}); rpcErr == nil || rpcErr.Code != -32601 {
		t.Errorf("resources/read should be unsupported, got %v", rpcErr)
	}
}
