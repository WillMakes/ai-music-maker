package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// ---------------------------------------------------------------------------
// The MCP server
//
// `suno-maker mcp`: the same binary, short-circuited before the window is
// opened, speaking JSON-RPC on stdio. It is what Settings → AI assistants
// registers with Claude Code, Claude Desktop and Codex (see mcpconfig.go), so an
// agent working in this repository can read and write the library the app is
// showing.
//
// Nothing may be written to stdout but responses. A stray Println here does not
// produce a bad log line, it produces a client that cannot parse the stream and
// disconnects — which is why every diagnostic in this file goes to stderr and
// why main returns before anything else has had a chance to print.
//
// The server reads the database directly rather than talking to a running
// window. That is what makes it work when the app is closed, and it is why
// openStoreAt sets busy_timeout: two processes share this file.
// ---------------------------------------------------------------------------

// mcpServerName is what this app calls itself in a client's config, and so the
// key its entry sits under.
const mcpServerName = "suno-maker"

// mcpProtocolVersion is the revision this server implements. Answered verbatim
// in the initialize result: a client that asked for a different one is told what
// it actually got rather than being agreed with.
const mcpProtocolVersion = "2024-11-05"

// runMCPServer handles a `mcp` invocation, reporting handled=false for anything
// else so main falls through and opens the window as usual.
func runMCPServer(args []string, in io.Reader, out io.Writer) (code int, handled bool) {
	if len(args) == 0 || args[0] != "mcp" {
		return 0, false
	}
	store, err := openStore()
	if err != nil {
		// stderr, not stdout — see the note at the top of this file.
		fmt.Fprintf(os.Stderr, "suno-maker mcp: %v\n", err)
		return 1, true
	}
	defer store.Close()

	// A write here has to reach an open window, which is a different process
	// (see notifyRunningWindow). Without it an agent adding a track through MCP
	// leaves the page showing the old list.
	store.onChange = notifyRunningWindow(store)

	app := &App{store: store, toJax: fileReportInJax}
	return serveMCP(app, in, out), true
}

// jsonRPCRequest is one message from the client. ID is json.RawMessage because
// the specification allows a string or a number and this server's only use for
// it is to echo it back — decoding it into either would be inventing a rule the
// protocol does not have.
type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

// serveMCP runs the message loop until stdin closes.
//
// Line-delimited JSON: one message per line, which is the framing every MCP
// stdio client uses. The scanner's buffer is raised because a tool result can be
// a whole library listing and bufio's default 64k line limit would truncate one
// into invalid JSON.
func serveMCP(app *App, in io.Reader, out io.Writer) int {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	encoder := json.NewEncoder(out)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req jsonRPCRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			// -32700 is the protocol's own parse-error code. Answered with a null
			// id, because the id is exactly what could not be read.
			_ = encoder.Encode(jsonRPCResponse{
				JSONRPC: "2.0",
				Error:   &jsonRPCError{Code: -32700, Message: "could not parse the request"},
			})
			continue
		}
		// A notification has no id and takes no answer. `notifications/initialized`
		// is the one every client sends, and replying to it is a protocol error
		// rather than a harmless extra line.
		if len(req.ID) == 0 {
			continue
		}
		result, rpcErr := dispatchMCP(app, req)
		_ = encoder.Encode(jsonRPCResponse{
			JSONRPC: "2.0", ID: req.ID, Result: result, Error: rpcErr,
		})
	}
	return 0
}

func dispatchMCP(app *App, req jsonRPCRequest) (any, *jsonRPCError) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": mcpProtocolVersion,
			// Only tools. This server has no resources and no prompts, and
			// declaring capabilities it does not implement would have clients
			// call methods that answer -32601.
			"capabilities": map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]any{
				"name":    mcpServerName,
				"version": appVersion,
			},
		}, nil

	case "ping":
		return map[string]any{}, nil

	case "tools/list":
		return map[string]any{"tools": mcpToolDefinitions()}, nil

	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, &jsonRPCError{Code: -32602, Message: "bad arguments"}
		}
		return callMCPTool(app, params.Name, params.Arguments), nil
	}
	return nil, &jsonRPCError{
		Code: -32601, Message: fmt.Sprintf("no method %q", req.Method)}
}

// mcpResult wraps a value as a tool result.
//
// Everything is returned as one text block holding JSON. MCP's content types are
// for things a model reads as prose or looks at as an image; every answer here is
// a record, and a model handed JSON can act on a field of it — where the same
// record rendered as a sentence has to be parsed back out of English first.
func mcpResult(value any) map[string]any {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return mcpFailure(err)
	}
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(encoded)}},
	}
}

// mcpFailure reports a tool that could not do what was asked.
//
// isError rather than a JSON-RPC error: the protocol distinguishes "the call was
// malformed" from "the call was fine and the tool said no", and a tool refusing
// is something the model should read and respond to rather than a transport
// fault it cannot see.
func mcpFailure(err error) map[string]any {
	return map[string]any{
		"isError": true,
		"content": []map[string]any{{"type": "text", "text": err.Error()}},
	}
}
