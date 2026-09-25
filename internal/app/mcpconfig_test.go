package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withMCPConfigHome points the client config paths at a temp directory.
//
// Not optional. These tests write MCP server entries, and the real paths are the
// producer's own ~/.claude.json and ~/.codex/config.toml — files belonging to
// other applications, holding configuration they care about. A suite that edited
// those would be doing the exact thing mcpconfig.go is written to never do.
func withMCPConfigHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	previous := mcpConfigHome
	mcpConfigHome = func() (string, error) { return dir, nil }
	t.Cleanup(func() { mcpConfigHome = previous })
	return dir
}

// clientStatus finds one client in a status answer.
func clientStatus(t *testing.T, status MCPStatus, id string) MCPClient {
	t.Helper()
	for _, c := range status.Clients {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no client %q in %+v", id, status.Clients)
	return MCPClient{}
}

// TestConnectingCodexReportsItselfCurrent is #13.
//
// writeTOMLEntry writes the command with %q, which doubles every backslash in a
// Windows path. describeMCPClient then looked for the raw path inside that text
// with strings.Contains, which cannot match — so Codex read as connected but
// pointing somewhere else, however freshly it had been connected. The JSON
// clients were given jsonEscape for exactly this reason; the TOML branch never
// was.
func TestConnectingCodexReportsItselfCurrent(t *testing.T) {
	app := newTestApp(t)
	home := withMCPConfigHome(t)

	// Codex only gets an entry written where its config already exists — a client
	// that has never been run has no file, and writing one nothing reads would be
	// a connection that appears to work and does nothing.
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatalf("codex dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"),
		[]byte("model = \"o3\"\n"), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	status, err := app.ConnectMCPClient(clientCodex)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	codex := clientStatus(t, status, clientCodex)

	if !codex.Connected {
		t.Fatal("the entry was written and Codex does not report itself connected")
	}
	if !codex.Current {
		t.Error("Codex says the entry points elsewhere, having just written it here")
	}
	if codex.Problem != "" {
		t.Errorf("problem = %q, want none", codex.Problem)
	}
}

// TestConnectingAJSONClientReportsItselfCurrent holds the other branch, which
// was already right — the fix must not trade one for the other.
func TestConnectingAJSONClientReportsItselfCurrent(t *testing.T) {
	app := newTestApp(t)
	home := withMCPConfigHome(t)

	if err := os.WriteFile(filepath.Join(home, ".claude.json"),
		[]byte(`{"numberThatMustSurvive":12345678901234567890}`), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	status, err := app.ConnectMCPClient(clientClaudeCode)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	client := clientStatus(t, status, clientClaudeCode)
	if !client.Connected || !client.Current {
		t.Errorf("connected=%v current=%v, want both true", client.Connected, client.Current)
	}

	// The producer's own configuration is not collateral. json.Number is what
	// keeps a large integer elsewhere in the file from being rewritten through
	// float64 into scientific notation.
	written, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(string(written), "12345678901234567890") {
		t.Errorf("an unrelated number was rewritten:\n%s", written)
	}
}

// TestAnEntryPointingAtAnotherBinaryIsNotCurrent: the state the flag exists to
// report has to still be reportable. Making Current always true would "fix" #13
// and lose the one thing it is for.
func TestAnEntryPointingAtAnotherBinaryIsNotCurrent(t *testing.T) {
	app := newTestApp(t)
	home := withMCPConfigHome(t)

	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatalf("codex dir: %v", err)
	}
	// An entry written by some other copy of this app — the shape GoLand's debug
	// binary leaves behind, quoted the way writeTOMLEntry quotes.
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte(
		"[mcp_servers."+mcpServerName+"]\n"+
			`command = "C:\\Users\\somebody\\tmp\\___1255App__Go_debugger_.exe"`+"\n"+
			"args = [\"mcp\"]\n"), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	codex := clientStatus(t, app.GetMCPStatus(), clientCodex)
	if !codex.Connected {
		t.Error("an entry is there and Codex reports itself unconnected")
	}
	if codex.Current {
		t.Error("an entry pointing at another binary reported itself current")
	}
}

// TestDisconnectingCodexLeavesTheRestOfTheFile: the file belongs to Codex, and
// only this app's own block may be touched.
func TestDisconnectingCodexLeavesTheRestOfTheFile(t *testing.T) {
	app := newTestApp(t)
	home := withMCPConfigHome(t)

	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("codex dir: %v", err)
	}
	original := "model = \"o3\"\n\n[mcp_servers.something-else]\ncommand = \"other\"\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	if _, err := app.ConnectMCPClient(clientCodex); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := app.DisconnectMCPClient(clientCodex); err != nil {
		t.Fatalf("disconnect: %v", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	for _, want := range []string{`model = "o3"`, "[mcp_servers.something-else]", `command = "other"`} {
		if !strings.Contains(string(after), want) {
			t.Errorf("connecting and disconnecting lost %q:\n%s", want, after)
		}
	}
	if strings.Contains(string(after), "[mcp_servers."+mcpServerName+"]") {
		t.Errorf("this app's own block survived a disconnect:\n%s", after)
	}
}

// TestTOMLBlockCommand reads the command back out of a block.
func TestTOMLBlockCommand(t *testing.T) {
	cases := map[string]string{
		`command = "C:\\Users\\willj\\suno-maker.exe"`: `C:\Users\willj\suno-maker.exe`,
		`command = "/usr/local/bin/suno-maker"`:        "/usr/local/bin/suno-maker",
		// Whitespace around the equals is legal TOML and somebody may have
		// reformatted the file by hand.
		`command="/tmp/suno-maker"`:    "/tmp/suno-maker",
		`  command   =   "/tmp/sm"   `: "/tmp/sm",
		`args = ["mcp"]`:               "",
		``:                             "",
		`command = not-quoted`:         "not-quoted",
	}
	for block, want := range cases {
		if got := tomlBlockCommand(block); got != want {
			t.Errorf("tomlBlockCommand(%q) = %q, want %q", block, got, want)
		}
	}
}
