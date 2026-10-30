// Package mcpclient talks to a remote MCP server over Streamable HTTP: the
// initialize handshake, tools/list and tools/call, with answers that come back
// either as one JSON body or as a server-sent event stream. auth.go is the
// other half — how a server that answers 401 is signed in to.
//
// Written against the protocol rather than an SDK: the client side of
// Streamable HTTP is four requests and an SSE reader, and the Go SDKs bring a
// transport abstraction and their own OAuth machinery that the app's
// settings-stored token would have to be bent into — the reason internal/youtube
// is written the same way.
//
// Nothing here knows about the app. It is handed a token source and hands back
// what the server said.
package mcpclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ProtocolVersion is the revision proposed at initialize. The server answers
// with the one it will speak, which is what later requests carry.
const ProtocolVersion = "2025-06-18"

// ErrUnauthorized is a 401 the token source could not get past: the grant is
// gone, or the server no longer accepts it, and only signing in again helps.
var ErrUnauthorized = errors.New("the server needs authorization")

// TokenSource answers the bearer token to send. forceRefresh is true after the
// server refused the last one, so a token that looked valid by its expiry but
// was revoked gets one refresh before the caller is told to sign in again.
// An empty token sends no Authorization header.
type TokenSource func(ctx context.Context, forceRefresh bool) (string, error)

// Tool is one entry of tools/list.
type Tool struct {
	Name        string          `json:"name"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

// Content is one block of a tool result.
type Content struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	MimeType string          `json:"mimeType,omitempty"`
	Resource json.RawMessage `json:"resource,omitempty"`
}

// ToolResult is what tools/call answers.
type ToolResult struct {
	Content           []Content       `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError,omitempty"`
}

// Text is every text block joined — what a tool that answers in prose said.
func (r ToolResult) Text() string {
	var parts []string
	for _, c := range r.Content {
		if c.Type == "text" && c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// Decode reads the result as JSON into out: the structured content when the
// server sent one, otherwise the first text block that parses — servers that
// predate structuredContent answer JSON as text.
func (r ToolResult) Decode(out any) error {
	if len(r.StructuredContent) > 0 && string(r.StructuredContent) != "null" {
		return json.Unmarshal(r.StructuredContent, out)
	}
	var lastErr error = errors.New("the tool answered nothing that reads as JSON")
	for _, c := range r.Content {
		if c.Type != "text" {
			continue
		}
		if err := json.Unmarshal([]byte(c.Text), out); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	return lastErr
}

// ToolError is a tool that ran and reported failure (isError) — distinct from
// a protocol error, because the text is the server's own words about the work
// and is what the producer should read.
type ToolError struct {
	Tool string
	Text string
}

func (e *ToolError) Error() string {
	if e.Text == "" {
		return e.Tool + " failed"
	}
	return e.Text
}

// RPCError is a JSON-RPC error answer.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("%s (%d)", e.Message, e.Code) }

// HTTPError is a refusal that was neither a 401 nor a JSON-RPC answer.
type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	body := strings.TrimSpace(e.Body)
	if len(body) > 300 {
		body = body[:300] + "…"
	}
	if body == "" {
		return fmt.Sprintf("the server answered %d", e.Status)
	}
	return fmt.Sprintf("the server answered %d: %s", e.Status, body)
}

// ServerInfo is what initialize said about the server.
type ServerInfo struct {
	Name            string `json:"name"`
	Version         string `json:"version"`
	ProtocolVersion string `json:"-"`
	Instructions    string `json:"-"`
}

// Client is one server. Safe for concurrent use: calls share the session,
// which is opened on the first call and opened again if the server forgets it.
type Client struct {
	URL   string
	Token TokenSource
	// HTTP is the transport; nil is a client with a generous timeout, since a
	// tool call on a generation server can take a while to answer.
	HTTP *http.Client
	// Name and Version are what this client calls itself at initialize.
	Name, Version string

	mu          sync.Mutex
	sessionID   string
	protocol    string
	initialized bool
	info        ServerInfo
	nextID      int64
}

var defaultHTTP = &http.Client{Timeout: 3 * time.Minute}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return defaultHTTP
}

// Info is the server's own description, opening the session if need be.
func (c *Client) Info(ctx context.Context) (ServerInfo, error) {
	if err := c.ensureSession(ctx); err != nil {
		return ServerInfo{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.info, nil
}

// ListTools reads every tool, following the pagination cursor.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	out := []Tool{}
	cursor := ""
	for page := 0; page < 50; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var res struct {
			Tools      []Tool `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := c.call(ctx, "tools/list", params, &res); err != nil {
			return nil, err
		}
		out = append(out, res.Tools...)
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	return out, nil
}

// CallTool runs a tool. A result the server marked isError comes back as a
// *ToolError with the result alongside, so a caller can read what it said.
func (c *Client) CallTool(ctx context.Context, name string, args any) (ToolResult, error) {
	if args == nil {
		args = map[string]any{}
	}
	var res ToolResult
	if err := c.call(ctx, "tools/call", map[string]any{"name": name, "arguments": args}, &res); err != nil {
		return ToolResult{}, err
	}
	if res.IsError {
		return res, &ToolError{Tool: name, Text: res.Text()}
	}
	return res, nil
}

// Close ends the session on the server, best effort: a server that does not
// support DELETE answers 405 and forgets it on its own.
func (c *Client) Close(ctx context.Context) {
	c.mu.Lock()
	id := c.sessionID
	c.sessionID, c.initialized = "", false
	c.mu.Unlock()
	if id == "" {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.URL, nil)
	if err != nil {
		return
	}
	req.Header.Set("Mcp-Session-Id", id)
	if token, err := c.Token(ctx, false); err == nil && token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if resp, err := c.http().Do(req); err == nil {
		resp.Body.Close()
	}
}

// call is one request in the open session. A 404 for a session the server has
// forgotten opens a new one and tries again once — the spec's answer for an
// expired session.
func (c *Client) call(ctx context.Context, method string, params any, out any) error {
	if err := c.ensureSession(ctx); err != nil {
		return err
	}
	err := c.request(ctx, method, params, out)
	var httpErr *HTTPError
	if errors.As(err, &httpErr) && httpErr.Status == http.StatusNotFound && c.hasSession() {
		c.mu.Lock()
		c.sessionID, c.initialized = "", false
		c.mu.Unlock()
		if err := c.ensureSession(ctx); err != nil {
			return err
		}
		return c.request(ctx, method, params, out)
	}
	return err
}

func (c *Client) hasSession() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionID != ""
}

// ensureSession runs initialize and notifications/initialized once.
func (c *Client) ensureSession(ctx context.Context) error {
	c.mu.Lock()
	ready := c.initialized
	c.mu.Unlock()
	if ready {
		return nil
	}
	name, version := c.Name, c.Version
	if name == "" {
		name = "mcpclient"
	}
	if version == "" {
		version = "dev"
	}
	var res struct {
		ProtocolVersion string     `json:"protocolVersion"`
		ServerInfo      ServerInfo `json:"serverInfo"`
		Instructions    string     `json:"instructions"`
	}
	if err := c.request(ctx, "initialize", map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": name, "version": version},
	}, &res); err != nil {
		return err
	}
	c.mu.Lock()
	c.protocol = res.ProtocolVersion
	if c.protocol == "" {
		c.protocol = ProtocolVersion
	}
	c.info = res.ServerInfo
	c.info.ProtocolVersion = c.protocol
	c.info.Instructions = res.Instructions
	c.mu.Unlock()
	if err := c.notify(ctx, "notifications/initialized"); err != nil {
		return err
	}
	c.mu.Lock()
	c.initialized = true
	c.mu.Unlock()
	return nil
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int64 `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Result  json.RawMessage `json:"result"`
	Error   *RPCError       `json:"error"`
}

func (c *Client) notify(ctx context.Context, method string) error {
	body, _ := json.Marshal(rpcRequest{JSONRPC: "2.0", Method: method})
	resp, err := c.post(ctx, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	// 202 is the answer; 200 with nothing is tolerated from a lax server.
	if resp.StatusCode/100 != 2 {
		return &HTTPError{Status: resp.StatusCode}
	}
	return nil
}

// request posts one JSON-RPC call and decodes its answer, whichever of the
// two shapes it comes back in.
func (c *Client) request(ctx context.Context, method string, params any, out any) error {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.mu.Unlock()
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: params})
	if err != nil {
		return err
	}
	resp, err := c.post(ctx, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		// A JSON-RPC error can ride on a 4xx; it says more than the status.
		var answer rpcResponse
		if json.Unmarshal(raw, &answer) == nil && answer.Error != nil {
			return answer.Error
		}
		return &HTTPError{Status: resp.StatusCode, Body: string(raw)}
	}
	if method == "initialize" {
		if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
			c.mu.Lock()
			c.sessionID = sid
			c.mu.Unlock()
		}
	}

	var answer *rpcResponse
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	switch mediaType {
	case "text/event-stream":
		answer, err = readSSEAnswer(resp.Body, id)
	default:
		answer = &rpcResponse{}
		err = json.NewDecoder(resp.Body).Decode(answer)
	}
	if err != nil {
		return fmt.Errorf("reading the answer to %s: %w", method, err)
	}
	if answer.Error != nil {
		return answer.Error
	}
	if out == nil || len(answer.Result) == 0 {
		return nil
	}
	return json.Unmarshal(answer.Result, out)
}

// post sends a body with the session's headers and the bearer token, trying
// once more with a refreshed token when the first is refused.
func (c *Client) post(ctx context.Context, body []byte) (*http.Response, error) {
	for attempt := 0; attempt < 2; attempt++ {
		token := ""
		if c.Token != nil {
			var err error
			token, err = c.Token(ctx, attempt > 0)
			if err != nil {
				return nil, err
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		c.mu.Lock()
		if c.sessionID != "" {
			req.Header.Set("Mcp-Session-Id", c.sessionID)
		}
		if c.protocol != "" {
			req.Header.Set("MCP-Protocol-Version", c.protocol)
		}
		c.mu.Unlock()
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := c.http().Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusUnauthorized {
			return resp, nil
		}
		resp.Body.Close()
		if c.Token == nil {
			break
		}
	}
	return nil, ErrUnauthorized
}

// readSSEAnswer reads events until the response to id arrives. Requests the
// server makes of the client and progress notifications ride the same stream
// and are skipped: this client offers no sampling or elicitation, and its
// callers wait on the answer, not on the progress.
func readSSEAnswer(body io.Reader, id int64) (*rpcResponse, error) {
	reader := bufio.NewReaderSize(body, 64<<10)
	var data strings.Builder
	want := fmt.Sprint(id)
	flush := func() (*rpcResponse, bool) {
		defer data.Reset()
		if data.Len() == 0 {
			return nil, false
		}
		var answer rpcResponse
		if err := json.Unmarshal([]byte(data.String()), &answer); err != nil {
			return nil, false
		}
		if answer.Method != "" || strings.Trim(string(answer.ID), `"`) != want {
			return nil, false
		}
		return &answer, true
	}
	for {
		line, err := reader.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		switch {
		case line == "":
			if answer, ok := flush(); ok {
				return answer, nil
			}
		case strings.HasPrefix(line, "data:"):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
		// event:, id:, retry: and comments carry nothing this client uses.
		if err != nil {
			if answer, ok := flush(); ok {
				return answer, nil
			}
			if err == io.EOF {
				return nil, errors.New("the stream ended before the answer came")
			}
			return nil, err
		}
	}
}
