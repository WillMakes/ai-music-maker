package mcpclient

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeServer is an MCP server behind OAuth, laid out the way Suno's is: the
// resource at /mcp, its metadata path-inserted under /.well-known, and the
// authorization server under /oauth on the same host.
type fakeServer struct {
	t   *testing.T
	srv *httptest.Server

	mu         sync.Mutex
	clients    map[string]string // client id → registered redirect uri
	codes      map[string]codeGrant
	access     map[string]bool
	refresh    map[string]bool
	sessions   map[string]bool
	registered int
	refreshed  int
	inits      int
	// forget, when set, makes the next request with a session answer 404, as
	// a server that dropped the session does.
	forget bool
	// expiresIn is the lifetime of issued access tokens.
	expiresIn int
	// lastResource is the resource the last token request named.
	lastResource string
}

type codeGrant struct {
	client, redirect, challenge, resource string
}

func newFakeServer(t *testing.T) *fakeServer {
	f := &fakeServer{
		t: t, clients: map[string]string{}, codes: map[string]codeGrant{},
		access: map[string]bool{}, refresh: map[string]bool{}, sessions: map[string]bool{},
		expiresIn: 3600,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"resource":              f.srv.URL + "/mcp",
			"authorization_servers": []string{f.srv.URL + "/oauth"},
			"scopes_supported":      []string{"songs:read", "songs:write", "offline_access"},
		})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server/oauth", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"issuer":                                f.srv.URL + "/oauth",
			"authorization_endpoint":                f.srv.URL + "/oauth/authorize",
			"token_endpoint":                        f.srv.URL + "/oauth/token",
			"registration_endpoint":                 f.srv.URL + "/oauth/register",
			"revocation_endpoint":                   f.srv.URL + "/oauth/revoke",
			"code_challenge_methods_supported":      []string{"S256"},
			"token_endpoint_auth_methods_supported": []string{"none"},
		})
	})
	mux.HandleFunc("/oauth/register", f.register)
	mux.HandleFunc("/oauth/authorize", f.authorize)
	mux.HandleFunc("/oauth/token", f.token)
	mux.HandleFunc("/oauth/revoke", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		delete(f.refresh, r.Form.Get("token"))
		f.mu.Unlock()
	})
	mux.HandleFunc("/mcp", f.mcp)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeServer) register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RedirectURIs []string `json:"redirect_uris"`
		AuthMethod   string   `json:"token_endpoint_auth_method"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.RedirectURIs) != 1 {
		http.Error(w, "bad registration", http.StatusBadRequest)
		return
	}
	if req.AuthMethod != "none" {
		f.t.Errorf("registered as %q, want a public client", req.AuthMethod)
	}
	f.mu.Lock()
	f.registered++
	id := fmt.Sprintf("client-%d", f.registered)
	f.clients[id] = req.RedirectURIs[0]
	f.mu.Unlock()
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]any{"client_id": id, "token_endpoint_auth_method": "none"})
}

// authorize stands in for the producer approving the sign-in: it checks the
// request and redirects straight back with a code.
func (f *fakeServer) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f.mu.Lock()
	registered := f.clients[q.Get("client_id")]
	f.mu.Unlock()
	switch {
	case registered == "":
		http.Error(w, "unknown client", http.StatusBadRequest)
		return
	case registered != q.Get("redirect_uri"):
		http.Error(w, "redirect_uri does not match the registration", http.StatusBadRequest)
		return
	case q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "":
		http.Error(w, "PKCE required", http.StatusBadRequest)
		return
	case q.Get("resource") != f.srv.URL+"/mcp":
		http.Error(w, "resource missing", http.StatusBadRequest)
		return
	}
	code := randomString(16)
	f.mu.Lock()
	f.codes[code] = codeGrant{q.Get("client_id"), q.Get("redirect_uri"), q.Get("code_challenge"), q.Get("resource")}
	f.mu.Unlock()
	back, _ := url.Parse(q.Get("redirect_uri"))
	back.RawQuery = url.Values{"code": {code}, "state": {q.Get("state")}}.Encode()
	http.Redirect(w, r, back.String(), http.StatusFound)
}

func (f *fakeServer) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastResource = r.Form.Get("resource")
	fail := func(code string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
	}
	switch r.Form.Get("grant_type") {
	case "authorization_code":
		grant, ok := f.codes[r.Form.Get("code")]
		delete(f.codes, r.Form.Get("code"))
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		switch {
		case !ok:
			fail("invalid_grant")
			return
		case base64.RawURLEncoding.EncodeToString(sum[:]) != grant.challenge:
			fail("invalid_grant") // the verifier does not match the challenge
			return
		case grant.client != r.Form.Get("client_id"), grant.redirect != r.Form.Get("redirect_uri"):
			fail("invalid_grant")
			return
		}
	case "refresh_token":
		if !f.refresh[r.Form.Get("refresh_token")] {
			fail("invalid_grant")
			return
		}
		delete(f.refresh, r.Form.Get("refresh_token"))
		f.refreshed++
	default:
		fail("unsupported_grant_type")
		return
	}
	access, refresh := "at-"+randomString(12), "rt-"+randomString(12)
	f.access[access] = true
	f.refresh[refresh] = true
	writeJSON(w, map[string]any{
		"access_token": access, "refresh_token": refresh,
		"token_type": "Bearer", "expires_in": f.expiresIn,
	})
}

// expireAll revokes every access token, as the server does when they run out.
func (f *fakeServer) expireAll() {
	f.mu.Lock()
	f.access = map[string]bool{}
	f.mu.Unlock()
}

func (f *fakeServer) mcp(w http.ResponseWriter, r *http.Request) {
	auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	f.mu.Lock()
	ok := f.access[auth]
	f.mu.Unlock()
	if !ok {
		w.Header().Set("WWW-Authenticate",
			`Bearer resource_metadata="`+f.srv.URL+`/.well-known/oauth-protected-resource/mcp"`)
		http.Error(w, `{"error":"sign in"}`, http.StatusUnauthorized)
		return
	}
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	sid := r.Header.Get("Mcp-Session-Id")
	if req.Method != "initialize" {
		f.mu.Lock()
		known := f.sessions[sid]
		if f.forget {
			f.forget = false
			delete(f.sessions, sid)
			known = false
		}
		f.mu.Unlock()
		if !known {
			http.Error(w, "unknown session", http.StatusNotFound)
			return
		}
		if r.Header.Get("MCP-Protocol-Version") != "2025-06-18" {
			http.Error(w, "missing protocol version", http.StatusBadRequest)
			return
		}
	}
	reply := func(result any) map[string]any {
		return map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}
	}
	switch req.Method {
	case "initialize":
		id := "session-" + randomString(8)
		f.mu.Lock()
		f.sessions[id] = true
		f.inits++
		f.mu.Unlock()
		w.Header().Set("Mcp-Session-Id", id)
		writeJSON(w, reply(map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "fake-songs", "version": "1.0"},
		}))
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
		var p struct {
			Cursor string `json:"cursor"`
		}
		_ = json.Unmarshal(req.Params, &p)
		// Two pages, so the cursor is followed.
		if p.Cursor == "" {
			writeJSON(w, reply(map[string]any{
				"tools":      []map[string]any{{"name": "song-create", "description": "Make a song", "inputSchema": map[string]any{"type": "object"}}},
				"nextCursor": "page2",
			}))
			return
		}
		writeJSON(w, reply(map[string]any{
			"tools": []map[string]any{{"name": "song-get", "inputSchema": map[string]any{"type": "object"}}},
		}))
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.Unmarshal(req.Params, &p)
		if p.Name == "song-fail" {
			writeJSON(w, reply(map[string]any{
				"isError": true,
				"content": []map[string]any{{"type": "text", "text": "402 insufficient_credits"}},
			}))
			return
		}
		// The answer as an event stream, behind a progress notification and a
		// comment, the way a long-running tool answers.
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		fmt.Fprint(w, ": keep-alive\n\n")
		fmt.Fprint(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{\"progress\":1}}\n\n")
		flusher.Flush()
		result, _ := json.MarshalIndent(reply(map[string]any{
			"content":           []map[string]any{{"type": "text", "text": `{"id":"clip-1","status":"submitted"}`}},
			"structuredContent": map[string]any{"id": "clip-1", "status": "submitted", "title": p.Arguments["title"]},
		}), "", " ")
		// One data line per line of the JSON, which the reader must join.
		fmt.Fprint(w, "event: message\nid: 7\n")
		for _, line := range strings.Split(string(result), "\n") {
			fmt.Fprintf(w, "data: %s\n", line)
		}
		fmt.Fprint(w, "\n")
	default:
		writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": req.ID,
			"error": map[string]any{"code": -32601, "message": "method not found"}})
	}
}

// browser follows the sign-in link as the producer's browser would, through
// the authorize redirect to the loopback callback.
func browser(t *testing.T) func(string) {
	return func(link string) {
		go func() {
			resp, err := http.Get(link)
			if err != nil {
				t.Errorf("the browser could not follow the sign-in: %v", err)
				return
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("the callback page answered %d: %s", resp.StatusCode, body)
			}
		}()
	}
}

func signIn(t *testing.T, f *fakeServer, previous *Authorization) Authorization {
	t.Helper()
	auth, err := Authorize(context.Background(), AuthorizeOptions{
		ServerURL: f.srv.URL + "/mcp", ClientName: "Test App",
		Open: browser(t), Previous: previous, Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	return auth
}

func TestAuthorizeDiscoversRegistersAndUsesPKCE(t *testing.T) {
	f := newFakeServer(t)
	auth := signIn(t, f, nil)

	if auth.Server.TokenEndpoint != f.srv.URL+"/oauth/token" {
		t.Errorf("token endpoint = %q, discovery did not reach the authorization server", auth.Server.TokenEndpoint)
	}
	if auth.Resource != f.srv.URL+"/mcp" {
		t.Errorf("resource = %q", auth.Resource)
	}
	if f.lastResource != f.srv.URL+"/mcp" {
		t.Errorf("the token request named resource %q, want the server", f.lastResource)
	}
	if auth.Client.ClientID != "client-1" || !strings.HasPrefix(auth.Client.RedirectURI, "http://127.0.0.1:") {
		t.Errorf("registration = %+v", auth.Client)
	}
	if !auth.Token.Valid() || auth.Token.Refresh == "" {
		t.Errorf("token = %+v", auth.Token)
	}
	if strings.Join(auth.Scopes, " ") != "songs:read songs:write offline_access" {
		t.Errorf("scopes = %v, want the resource's own", auth.Scopes)
	}

	// Signing in again reuses the registration when its port is free.
	again := signIn(t, f, &auth)
	if again.Client.ClientID != auth.Client.ClientID || f.registered != 1 {
		t.Errorf("a second sign-in registered again (%d registrations, client %q)", f.registered, again.Client.ClientID)
	}
}

func TestAuthorizeRejectsAForgedState(t *testing.T) {
	f := newFakeServer(t)
	_, err := Authorize(context.Background(), AuthorizeOptions{
		ServerURL: f.srv.URL + "/mcp", Timeout: 10 * time.Second,
		Open: func(link string) {
			u, _ := url.Parse(link)
			go func() {
				// Something else on the machine hitting the callback with its
				// own code: no state that matches.
				resp, err := http.Get(u.Query().Get("redirect_uri") + "?code=stolen&state=wrong")
				if err == nil {
					resp.Body.Close()
				}
			}()
		},
	})
	if err == nil || !strings.Contains(err.Error(), "different request") {
		t.Fatalf("err = %v, want the state mismatch", err)
	}
}

func TestProbeReadsTheChallenge(t *testing.T) {
	f := newFakeServer(t)
	c, err := Probe(context.Background(), nil, f.srv.URL+"/mcp")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Required || c.ResourceMetadata != f.srv.URL+"/.well-known/oauth-protected-resource/mcp" {
		t.Errorf("challenge = %+v", c)
	}
}

func TestBearerParams(t *testing.T) {
	got := bearerParams([]string{`Bearer realm="x", resource_metadata="https://a/b", scope="one two", error=invalid_token`})
	if got["resource_metadata"] != "https://a/b" || got["scope"] != "one two" || got["error"] != "invalid_token" {
		t.Errorf("params = %v", got)
	}
	if len(bearerParams([]string{`Basic realm="x"`})) != 0 {
		t.Error("a Basic challenge was read as Bearer")
	}
}

// client is a Client whose token source refreshes the way the app's does:
// the held token while it is valid, a refresh when it is not or when the
// server refused it.
func client(t *testing.T, f *fakeServer, auth *Authorization) *Client {
	var mu sync.Mutex
	return &Client{
		URL: f.srv.URL + "/mcp", Name: "test", Version: "1",
		Token: func(ctx context.Context, force bool) (string, error) {
			mu.Lock()
			defer mu.Unlock()
			if !force && auth.Token.Valid() {
				return auth.Token.Access, nil
			}
			token, err := Refresh(ctx, nil, *auth)
			if err != nil {
				return "", err
			}
			auth.Token = token
			return token.Access, nil
		},
	}
}

func TestToolsListAndCallOverSSE(t *testing.T) {
	f := newFakeServer(t)
	auth := signIn(t, f, nil)
	c := client(t, f, &auth)
	ctx := context.Background()

	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 || tools[0].Name != "song-create" || tools[1].Name != "song-get" {
		t.Fatalf("tools = %+v, want both pages", tools)
	}
	info, _ := c.Info(ctx)
	if info.Name != "fake-songs" || info.ProtocolVersion != "2025-06-18" {
		t.Errorf("info = %+v", info)
	}

	res, err := c.CallTool(ctx, "song-create", map[string]any{"title": "Static"})
	if err != nil {
		t.Fatal(err)
	}
	var clip struct{ ID, Status, Title string }
	if err := res.Decode(&clip); err != nil {
		t.Fatal(err)
	}
	if clip.ID != "clip-1" || clip.Title != "Static" {
		t.Errorf("clip = %+v, the SSE answer was not read past the notification", clip)
	}
	if f.inits != 1 {
		t.Errorf("%d initializes, want one session for every call", f.inits)
	}

	_, err = c.CallTool(ctx, "song-fail", nil)
	var toolErr *ToolError
	if !errors.As(err, &toolErr) || !strings.Contains(toolErr.Text, "insufficient_credits") {
		t.Errorf("err = %v, want the tool's own words", err)
	}
}

func TestRefreshOnExpiryAndOnRefusal(t *testing.T) {
	f := newFakeServer(t)
	f.expiresIn = 30 // under Valid's minute, so the first call refreshes
	auth := signIn(t, f, nil)
	f.expiresIn = 3600
	c := client(t, f, &auth)
	if _, err := c.ListTools(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.refreshed != 1 {
		t.Errorf("refreshed %d times, want once for the expiring token", f.refreshed)
	}

	// A token the server stopped accepting before its expiry: the 401 gets
	// one refresh and the call goes through.
	auth.Token.Expiry = time.Now().Add(time.Hour)
	f.expireAll()
	if _, err := c.CallTool(context.Background(), "song-create", nil); err != nil {
		t.Fatalf("call after the server dropped the token: %v", err)
	}
	if f.refreshed != 2 {
		t.Errorf("refreshed %d times, want a second on the 401", f.refreshed)
	}
}

func TestRevokedGrantAsksForSignIn(t *testing.T) {
	f := newFakeServer(t)
	auth := signIn(t, f, nil)
	if err := Revoke(context.Background(), nil, auth); err != nil {
		t.Fatal(err)
	}
	if _, err := Refresh(context.Background(), nil, auth); !errors.Is(err, ErrGrantRevoked) {
		t.Errorf("refresh after revoke = %v, want ErrGrantRevoked", err)
	}

	// A client with no way to a token sees ErrUnauthorized, not a 401 body.
	c := &Client{URL: f.srv.URL + "/mcp", Token: func(context.Context, bool) (string, error) { return "nope", nil }}
	if _, err := c.ListTools(context.Background()); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("err = %v, want ErrUnauthorized", err)
	}
}

func TestForgottenSessionIsOpenedAgain(t *testing.T) {
	f := newFakeServer(t)
	auth := signIn(t, f, nil)
	c := client(t, f, &auth)
	if _, err := c.ListTools(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.forget = true
	if _, err := c.ListTools(context.Background()); err != nil {
		t.Fatalf("after the server dropped the session: %v", err)
	}
	if f.inits != 2 {
		t.Errorf("%d initializes, want a second after the 404", f.inits)
	}
}

func TestAuthorizeWithoutChallenge(t *testing.T) {
	open := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": 0, "result": map[string]any{}})
	}))
	defer open.Close()
	_, err := Authorize(context.Background(), AuthorizeOptions{ServerURL: open.URL})
	if !errors.Is(err, ErrNoAuthorizationNeeded) {
		t.Errorf("err = %v, want ErrNoAuthorizationNeeded", err)
	}
}
