package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"aimusicmaker/internal/mcpclient"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ---------------------------------------------------------------------------
// MCP servers this app is a client of
//
// The other direction from mcp.go: there the app is the server an assistant
// reaches; here it reaches remote servers itself (internal/mcpclient). Suno is
// the one that matters — its server is how an album is made without the
// browser (sunomcp.go) — and it is preset, so the producer only has to
// authorize it. Others can be added beside it.
//
// The app signs in on its own account. Claude Code may be connected to the same
// server on the same machine, and its tokens are deliberately not read: they
// were issued to Claude Code as a client, under its registration, and a token
// borrowed from another app's store is one that app can rotate out from under
// this one.
// ---------------------------------------------------------------------------

// keyMCPServers holds every server and its authorization as one JSON blob, the
// way keyYouTube holds that connection. A secret, so the MCP setting tools
// refuse it.
const keyMCPServers = "mcp_servers"

// scopeMCPServers is reported after a server's connection or tools change.
const scopeMCPServers = "mcp_servers"

// sunoServerID is the preset Suno server.
const sunoServerID = "suno"

// sunoServerURL is Suno's public MCP server, remote Streamable HTTP.
const sunoServerURL = "https://api.suno.com/mcp"

// MCP server states, as Settings shows them.
const (
	mcpNotConnected       = "not_connected"
	mcpNeedsAuthorization = "needs_authorization"
	mcpConnected          = "connected"
)

// mcpServerRecord is what is stored for one server.
type mcpServerRecord struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
	// Auth is the sign-in; nil until the producer authorizes.
	Auth *mcpclient.Authorization `json:"auth,omitempty"`
	// Open is a server that answered without asking for a token.
	Open bool `json:"open,omitempty"`
	// NeedsAuth is set when the server or its sign-in refused the held grant:
	// the record still says who it was for, but only a new sign-in helps.
	NeedsAuth   bool             `json:"needsAuth,omitempty"`
	Tools       []mcpclient.Tool `json:"tools,omitempty"`
	ToolsAt     string           `json:"toolsAt,omitempty"`
	ServerName  string           `json:"serverName,omitempty"`
	ConnectedAt string           `json:"connectedAt,omitempty"`
	LastError   string           `json:"lastError,omitempty"`
}

func (r mcpServerRecord) state() string {
	switch {
	case r.NeedsAuth:
		return mcpNeedsAuthorization
	case r.Open, r.Auth != nil && r.Auth.Token.Access != "":
		return mcpConnected
	}
	return mcpNotConnected
}

// MCPServerTool is one tool, as Settings lists it.
type MCPServerTool struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Schema is the input schema as JSON text, for whoever wants to read it.
	Schema string `json:"schema"`
}

// MCPServerStatus is a server as the window sees it: never the tokens.
type MCPServerStatus struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
	// Preset is a server the app ships with, which cannot be removed.
	Preset bool `json:"preset"`
	// State is not_connected, needs_authorization or connected.
	State       string          `json:"state"`
	ServerName  string          `json:"serverName"`
	ConnectedAt string          `json:"connectedAt"`
	Scopes      []string        `json:"scopes"`
	Tools       []MCPServerTool `json:"tools"`
	ToolsAt     string          `json:"toolsAt"`
	LastError   string          `json:"lastError"`
}

func describeMCPServer(r mcpServerRecord) MCPServerStatus {
	status := MCPServerStatus{
		ID: r.ID, Name: r.Name, URL: r.URL, Preset: r.ID == sunoServerID,
		State: r.state(), ServerName: r.ServerName, ConnectedAt: r.ConnectedAt,
		Scopes: []string{}, Tools: []MCPServerTool{}, ToolsAt: r.ToolsAt, LastError: r.LastError,
	}
	if r.Auth != nil {
		status.Scopes = append(status.Scopes, r.Auth.Scopes...)
	}
	for _, t := range r.Tools {
		status.Tools = append(status.Tools, MCPServerTool{
			Name: t.Name, Title: t.Title, Description: t.Description, Schema: string(t.InputSchema),
		})
	}
	return status
}

// mcpServersMu serialises read-modify-write of the blob within this process:
// a refresh landing while the producer presses Disconnect must not write the
// token back over the disconnect.
var mcpServersMu sync.Mutex

// mcpServerRecords reads the blob with the presets merged in, in order: Suno
// first, then the producer's own.
func (a *App) mcpServerRecords() []mcpServerRecord {
	var stored []mcpServerRecord
	if raw := a.aiSetting(keyMCPServers); strings.TrimSpace(raw) != "" {
		// A blob this app cannot decode is one it wrote wrong, and "not
		// connected" lets the producer connect again.
		_ = json.Unmarshal([]byte(raw), &stored)
	}
	out := []mcpServerRecord{}
	var suno *mcpServerRecord
	for i := range stored {
		if stored[i].ID == sunoServerID {
			suno = &stored[i]
		}
	}
	if suno == nil {
		out = append(out, mcpServerRecord{ID: sunoServerID, Name: "Suno", URL: sunoServerURL})
	} else {
		// The preset's address is the build's, so a later build that moves it
		// moves it for everybody.
		suno.URL, suno.Name = sunoServerURL, "Suno"
		out = append(out, *suno)
	}
	for _, r := range stored {
		if r.ID != sunoServerID {
			out = append(out, r)
		}
	}
	return out
}

func (a *App) mcpServerRecord(id string) (mcpServerRecord, error) {
	for _, r := range a.mcpServerRecords() {
		if r.ID == id {
			return r, nil
		}
	}
	return mcpServerRecord{}, fmt.Errorf("no MCP server %q", id)
}

func (a *App) saveMCPServerRecords(records []mcpServerRecord) error {
	if a.store == nil {
		return errNoStore
	}
	encoded, err := json.Marshal(records)
	if err != nil {
		return err
	}
	if err := a.store.setSetting(keyMCPServers, string(encoded)); err != nil {
		return err
	}
	a.store.changed(scopeMCPServers)
	return nil
}

// updateMCPServer changes one record under the lock and stores the lot.
func (a *App) updateMCPServer(id string, change func(*mcpServerRecord)) (mcpServerRecord, error) {
	mcpServersMu.Lock()
	defer mcpServersMu.Unlock()
	records := a.mcpServerRecords()
	for i := range records {
		if records[i].ID == id {
			change(&records[i])
			if err := a.saveMCPServerRecords(records); err != nil {
				return mcpServerRecord{}, err
			}
			return records[i], nil
		}
	}
	return mcpServerRecord{}, fmt.Errorf("no MCP server %q", id)
}

// GetMCPServers lists every server, Suno first.
func (a *App) GetMCPServers() []MCPServerStatus {
	out := []MCPServerStatus{}
	for _, r := range a.mcpServerRecords() {
		out = append(out, describeMCPServer(r))
	}
	return out
}

// GetMCPServer reports one.
func (a *App) GetMCPServer(id string) (MCPServerStatus, error) {
	r, err := a.mcpServerRecord(id)
	if err != nil {
		return MCPServerStatus{}, err
	}
	return describeMCPServer(r), nil
}

// AddMCPServer adds a remote server by its address. Nothing is reached yet:
// whether it wants a sign-in is learned when it is connected.
func (a *App) AddMCPServer(name, address string) (MCPServerStatus, error) {
	if a.store == nil {
		return MCPServerStatus{}, errNoStore
	}
	name, address = strings.TrimSpace(name), strings.TrimSpace(address)
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return MCPServerStatus{}, errors.New("enter the server's full address, starting https://")
	}
	if u.Scheme == "http" && !isLoopbackHost(u.Hostname()) {
		// A token sent in the clear is a token anyone on the network has.
		return MCPServerStatus{}, errors.New("a remote server must be reached over https")
	}
	if name == "" {
		name = u.Host
	}
	mcpServersMu.Lock()
	defer mcpServersMu.Unlock()
	records := a.mcpServerRecords()
	for _, r := range records {
		if strings.EqualFold(strings.TrimSuffix(r.URL, "/"), strings.TrimSuffix(address, "/")) {
			return MCPServerStatus{}, fmt.Errorf("%s is already listed as %s", address, r.Name)
		}
	}
	record := mcpServerRecord{ID: newID("mcp"), Name: name, URL: address}
	records = append(records, record)
	if err := a.saveMCPServerRecords(records); err != nil {
		return MCPServerStatus{}, err
	}
	return describeMCPServer(record), nil
}

func isLoopbackHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// RemoveMCPServer forgets a server the producer added. The preset is
// disconnected, never removed.
func (a *App) RemoveMCPServer(id string) ([]MCPServerStatus, error) {
	if id == sunoServerID {
		return nil, errors.New("Suno is built in — disconnect it instead")
	}
	if _, err := a.DisconnectMCPServer(id); err != nil {
		return nil, err
	}
	mcpServersMu.Lock()
	records := a.mcpServerRecords()
	kept := []mcpServerRecord{}
	for _, r := range records {
		if r.ID != id {
			kept = append(kept, r)
		}
	}
	err := a.saveMCPServerRecords(kept)
	mcpServersMu.Unlock()
	if err != nil {
		return nil, err
	}
	return a.GetMCPServers(), nil
}

// authorizeMCP is mcpclient.Authorize, a variable so a test can sign in
// without a browser.
var authorizeMCP = mcpclient.Authorize

// AuthorizeMCPServer signs the app in to a server through the browser, then
// reads its tools to prove the sign-in works. Needs the window: the sign-in
// opens in the producer's browser, and the MCP server process has none.
func (a *App) AuthorizeMCPServer(id string) (MCPServerStatus, error) {
	if a.store == nil {
		return MCPServerStatus{}, errNoStore
	}
	if !hasWindow(a) {
		return MCPServerStatus{}, errors.New("authorize MCP servers from the app's window — the sign-in opens in your browser")
	}
	record, err := a.mcpServerRecord(id)
	if err != nil {
		return MCPServerStatus{}, err
	}
	auth, err := authorizeMCP(context.Background(), mcpclient.AuthorizeOptions{
		ServerURL:  record.URL,
		ClientName: "AIMusicMaker",
		Previous:   record.Auth,
		Open: func(link string) {
			if a.ctx != nil {
				wruntime.BrowserOpenURL(a.ctx, link)
			}
		},
	})
	open := errors.Is(err, mcpclient.ErrNoAuthorizationNeeded)
	if err != nil && !open {
		_, _ = a.updateMCPServer(id, func(r *mcpServerRecord) { r.LastError = err.Error() })
		return MCPServerStatus{}, err
	}
	if _, err := a.updateMCPServer(id, func(r *mcpServerRecord) {
		if open {
			r.Auth, r.Open = nil, true
		} else {
			r.Auth, r.Open = &auth, false
		}
		r.NeedsAuth, r.LastError, r.ConnectedAt = false, "", nowStamp()
	}); err != nil {
		return MCPServerStatus{}, err
	}
	return a.RefreshMCPServerTools(id)
}

// DisconnectMCPServer forgets the sign-in and tells the server to. Told
// best-effort: a disconnect that could not reach it has still forgotten the
// token here. The registration goes too — a client id with no grant behind it
// is nothing worth keeping.
func (a *App) DisconnectMCPServer(id string) (MCPServerStatus, error) {
	if a.store == nil {
		return MCPServerStatus{}, errNoStore
	}
	record, err := a.mcpServerRecord(id)
	if err != nil {
		return MCPServerStatus{}, err
	}
	if record.Auth != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = mcpclient.Revoke(ctx, nil, *record.Auth)
		cancel()
	}
	a.dropMCPClient(id)
	updated, err := a.updateMCPServer(id, func(r *mcpServerRecord) {
		*r = mcpServerRecord{ID: r.ID, Name: r.Name, URL: r.URL}
	})
	if err != nil {
		return MCPServerStatus{}, err
	}
	return describeMCPServer(updated), nil
}

// RefreshMCPServerTools reads the server's tools now. It is also the check
// that the connection still works: a refused grant moves the server to
// needs-authorization rather than answering an error the panel has to
// interpret.
func (a *App) RefreshMCPServerTools(id string) (MCPServerStatus, error) {
	record, err := a.mcpServerRecord(id)
	if err != nil {
		return MCPServerStatus{}, err
	}
	if record.state() == mcpNotConnected {
		return describeMCPServer(record), nil
	}
	client := a.mcpClient(record)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	tools, err := client.ListTools(ctx)
	if err != nil {
		updated, saveErr := a.updateMCPServer(id, func(r *mcpServerRecord) {
			r.LastError = err.Error()
			if needsSignIn(err) {
				r.NeedsAuth = true
			}
		})
		if saveErr != nil || !needsSignIn(err) {
			return MCPServerStatus{}, err
		}
		return describeMCPServer(updated), nil
	}
	info, _ := client.Info(ctx)
	updated, err := a.updateMCPServer(id, func(r *mcpServerRecord) {
		r.Tools, r.ToolsAt, r.LastError = tools, nowStamp(), ""
		if info.Name != "" {
			r.ServerName = strings.TrimSpace(info.Name + " " + info.Version)
		}
	})
	if err != nil {
		return MCPServerStatus{}, err
	}
	return describeMCPServer(updated), nil
}

// needsSignIn is an error only a new sign-in gets past.
func needsSignIn(err error) bool {
	return errors.Is(err, mcpclient.ErrUnauthorized) || errors.Is(err, mcpclient.ErrGrantRevoked)
}

// mcpClients holds one client per server for the process, so calls share a
// session instead of opening one each.
var (
	mcpClientsMu sync.Mutex
	mcpClients   = map[string]*mcpclient.Client{}
)

// mcpClient is the server's client, its token read from the store on each
// request and refreshed (and stored) when it runs out or is refused.
func (a *App) mcpClient(record mcpServerRecord) *mcpclient.Client {
	mcpClientsMu.Lock()
	defer mcpClientsMu.Unlock()
	if c, ok := mcpClients[record.ID]; ok && c.URL == record.URL {
		return c
	}
	id := record.ID
	var refreshMu sync.Mutex
	c := &mcpclient.Client{
		URL: record.URL, Name: "AIMusicMaker", Version: appVersion,
		Token: func(ctx context.Context, force bool) (string, error) {
			// One refresh at a time: a rotating refresh token used twice is a
			// grant the server revokes.
			refreshMu.Lock()
			defer refreshMu.Unlock()
			current, err := a.mcpServerRecord(id)
			if err != nil {
				return "", err
			}
			if current.Open {
				return "", nil
			}
			if current.Auth == nil || current.NeedsAuth {
				return "", mcpclient.ErrUnauthorized
			}
			if !force && current.Auth.Token.Valid() {
				return current.Auth.Token.Access, nil
			}
			token, err := mcpclient.Refresh(ctx, nil, *current.Auth)
			if err != nil {
				if errors.Is(err, mcpclient.ErrGrantRevoked) {
					_, _ = a.updateMCPServer(id, func(r *mcpServerRecord) {
						r.NeedsAuth, r.LastError = true, err.Error()
					})
				}
				return "", err
			}
			if _, err := a.updateMCPServer(id, func(r *mcpServerRecord) {
				if r.Auth != nil {
					r.Auth.Token = token
				}
			}); err != nil {
				return "", fmt.Errorf("saving the refreshed token: %w", err)
			}
			return token.Access, nil
		},
	}
	mcpClients[id] = c
	return c
}

func (a *App) dropMCPClient(id string) {
	mcpClientsMu.Lock()
	c := mcpClients[id]
	delete(mcpClients, id)
	mcpClientsMu.Unlock()
	if c != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		c.Close(ctx)
		cancel()
	}
}
