package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Registering with the AI clients
//
// Settings → AI assistants connects this app to the clients the producer uses,
// by writing an MCP server entry into each one's own config:
//
//	Claude Code     ~/.claude.json                 JSON, "mcpServers"
//	Claude Desktop  %APPDATA%\Claude\…_config.json JSON, "mcpServers"
//	Codex           ~/.codex/config.toml           TOML, [mcp_servers.x]
//	Grok            ~/.grok/mcp.json               JSON, "mcpServers"
//
// All of them get the same stdio entry: this binary, relaunched as
// `suno-maker mcp` (see mcp.go). No token and no port, because the server reads
// the database directly.
//
// These files belong to those applications and hold unrelated configuration the
// producer cares about, so every edit here is strictly surgical:
//
//   - JSON is decoded with json.Number, so re-encoding never rewrites a large or
//     precise number into something else, and only mcpServers.suno-maker is
//     touched.
//   - TOML is not parsed at all. This app has no TOML library, and adding one to
//     round-trip somebody else's config would risk losing comments and ordering
//     on every write. The file is treated as text and only our own
//     [mcp_servers.suno-maker] block is spliced in or cut out; every other byte
//     is left exactly as it was.
//
// A file that cannot be read as its own format is reported and left alone, never
// overwritten.
// ---------------------------------------------------------------------------

// mcpClientID names one client, and is what the frontend sends back.
const (
	clientClaudeCode    = "claude-code"
	clientClaudeDesktop = "claude-desktop"
	clientCodex         = "codex"
	clientGrok          = "grok"
)

// MCPClient is one AI client's connection state, for Settings.
type MCPClient struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Path is the config file this client keeps its MCP servers in.
	Path string `json:"path"`
	// Present says the client looks installed — its config file exists. A client
	// that has never been run has no file, and connecting to one that is not
	// there would write a config nothing reads.
	Present bool `json:"present"`
	// Connected says an entry for this app exists in that config.
	Connected bool `json:"connected"`
	// Current says the entry points at *this* binary. A stale one is how a client
	// ends up launching a copy that was moved or replaced.
	Current bool `json:"current"`
	// Problem is why the file could not be read, when it could not be.
	Problem string `json:"problem"`
}

// MCPStatus is the whole Settings → AI assistants view.
type MCPStatus struct {
	// Command is what the clients are configured to launch.
	Command string `json:"command"`
	// ToolCount is how many tools this server offers.
	ToolCount int         `json:"toolCount"`
	Clients   []MCPClient `json:"clients"`
}

// mcpCommand is the executable a client is told to run: this binary, by absolute
// path.
//
// os.Executable rather than "suno-maker": a client launching by name would need
// this app on its PATH, which it is not on any of the three platforms unless
// somebody put it there. The path is resolved through symlinks so a build run
// from a link records where the program actually is.
func mcpCommand() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return path, nil
}

// mcpConfigHome is where the client config files are looked for.
//
// A variable so tests can point it at a temp directory. Not defensive plumbing:
// the real paths are the producer's own ~/.claude.json and ~/.codex/config.toml,
// which belong to other applications and hold configuration they care about — a
// suite that wrote into those would be doing the exact thing every comment in
// this file says never to do.
var mcpConfigHome = os.UserHomeDir

// clientConfigPath is where one client keeps its servers.
func clientConfigPath(id string) (string, error) {
	home, err := mcpConfigHome()
	if err != nil {
		return "", err
	}
	switch id {
	case clientClaudeCode:
		return filepath.Join(home, ".claude.json"), nil
	case clientClaudeDesktop:
		switch runtime.GOOS {
		case "windows":
			appData := os.Getenv("APPDATA")
			if appData == "" {
				return "", fmt.Errorf("APPDATA is not set")
			}
			return filepath.Join(appData, "Claude", "claude_desktop_config.json"), nil
		case "darwin":
			return filepath.Join(home, "Library", "Application Support", "Claude",
				"claude_desktop_config.json"), nil
		default:
			return filepath.Join(home, ".config", "Claude", "claude_desktop_config.json"), nil
		}
	case clientCodex:
		return filepath.Join(home, ".codex", "config.toml"), nil
	case clientGrok:
		return filepath.Join(home, ".grok", "mcp.json"), nil
	}
	return "", fmt.Errorf("unknown client %q", id)
}

func clientName(id string) string {
	switch id {
	case clientClaudeCode:
		return "Claude Code"
	case clientClaudeDesktop:
		return "Claude Desktop"
	case clientCodex:
		return "Codex"
	case clientGrok:
		return "Grok"
	}
	return id
}

// mcpClientIDs is every client, in the order Settings lists them.
var mcpClientIDs = []string{clientClaudeCode, clientClaudeDesktop, clientCodex, clientGrok}

// GetMCPStatus reports every client.
func (a *App) GetMCPStatus() MCPStatus {
	command, _ := mcpCommand()
	status := MCPStatus{Command: command, ToolCount: mcpToolCount(), Clients: []MCPClient{}}
	for _, id := range mcpClientIDs {
		status.Clients = append(status.Clients, describeMCPClient(id, command))
	}
	return status
}

func describeMCPClient(id, command string) MCPClient {
	client := MCPClient{ID: id, Name: clientName(id)}
	path, err := clientConfigPath(id)
	if err != nil {
		client.Problem = err.Error()
		return client
	}
	client.Path = path

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return client
	}
	if err != nil {
		client.Problem = err.Error()
		return client
	}
	client.Present = true

	if id == clientCodex {
		entry, found := tomlServerBlock(string(data))
		client.Connected = found
		client.Current = found && sameCommand(tomlBlockCommand(entry), command)
		return client
	}

	var config map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&config); err != nil {
		// Somebody else's file that this app cannot read. Reported, never
		// overwritten: the alternative is destroying a config to fix a display.
		client.Problem = "this file is not valid JSON, so it has been left alone"
		return client
	}
	servers := map[string]json.RawMessage{}
	if raw, ok := config["mcpServers"]; ok {
		_ = json.Unmarshal(raw, &servers)
	}
	entry, ok := servers[mcpServerName]
	client.Connected = ok
	if ok {
		// Decoded rather than searched. The entry holds the command JSON-escaped —
		// a Windows path arrives with every backslash doubled — so looking for the
		// raw path inside the text finds nothing, which is the shape of the bug the
		// TOML branch had (#13). Decoding asks the question directly and has no
		// escaping to get wrong.
		var stored struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal(entry, &stored)
		client.Current = sameCommand(stored.Command, command)
	}
	return client
}

// sameCommand says whether a stored command names this binary.
//
// Case-insensitively on Windows, where the filesystem is, and exactly everywhere
// else, where it is not. Both sides come from mcpCommand, so the only differences
// that should ever appear are ones the platform considers meaningless.
func sameCommand(stored, command string) bool {
	if stored == "" || command == "" {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(stored, command)
	}
	return stored == command
}

// ConnectMCPClient writes this app's entry into a client's config.
func (a *App) ConnectMCPClient(id string) (MCPStatus, error) {
	command, err := mcpCommand()
	if err != nil {
		return MCPStatus{}, err
	}
	path, err := clientConfigPath(id)
	if err != nil {
		return MCPStatus{}, err
	}
	if id == clientCodex {
		if err := writeTOMLEntry(path, command); err != nil {
			return MCPStatus{}, err
		}
		return a.GetMCPStatus(), nil
	}
	if err := writeJSONEntry(path, command); err != nil {
		return MCPStatus{}, err
	}
	return a.GetMCPStatus(), nil
}

// DisconnectMCPClient removes it again.
func (a *App) DisconnectMCPClient(id string) (MCPStatus, error) {
	path, err := clientConfigPath(id)
	if err != nil {
		return MCPStatus{}, err
	}
	if id == clientCodex {
		if err := removeTOMLEntry(path); err != nil {
			return MCPStatus{}, err
		}
		return a.GetMCPStatus(), nil
	}
	if err := removeJSONEntry(path); err != nil {
		return MCPStatus{}, err
	}
	return a.GetMCPStatus(), nil
}

// ---------------------------------------------------------------------------
// JSON clients
// ---------------------------------------------------------------------------

// writeJSONEntry adds or replaces mcpServers.suno-maker, leaving everything else
// as it was.
//
// UseNumber is what keeps "everything else as it was" true of numbers: without
// it, a large integer elsewhere in the file round-trips through float64 and is
// written back in scientific notation.
func writeJSONEntry(path, command string) error {
	config := map[string]json.RawMessage{}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&config); err != nil {
			return fmt.Errorf("%s is not valid JSON, so it has been left alone", path)
		}
	case !os.IsNotExist(err):
		return err
	}

	servers := map[string]json.RawMessage{}
	if raw, ok := config["mcpServers"]; ok {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return fmt.Errorf("%s has an mcpServers entry this app cannot read", path)
		}
	}
	entry, err := json.Marshal(map[string]any{
		"command": command,
		"args":    []string{"mcp"},
	})
	if err != nil {
		return err
	}
	servers[mcpServerName] = entry

	encodedServers, err := json.Marshal(servers)
	if err != nil {
		return err
	}
	config["mcpServers"] = encodedServers

	out, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o600)
}

func removeJSONEntry(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		// Nothing to remove is the end state asked for, not a failure.
		return nil
	}
	if err != nil {
		return err
	}
	config := map[string]json.RawMessage{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&config); err != nil {
		return fmt.Errorf("%s is not valid JSON, so it has been left alone", path)
	}
	servers := map[string]json.RawMessage{}
	if raw, ok := config["mcpServers"]; ok {
		_ = json.Unmarshal(raw, &servers)
	}
	delete(servers, mcpServerName)
	encoded, err := json.Marshal(servers)
	if err != nil {
		return err
	}
	config["mcpServers"] = encoded
	out, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o600)
}

// ---------------------------------------------------------------------------
// Codex's TOML
// ---------------------------------------------------------------------------

// tomlHeader is the section this app owns. Everything here keys off it as text —
// see the note at the top of the file for why nothing is parsed.
const tomlHeader = "[mcp_servers." + mcpServerName + "]"

// tomlServerBlock finds this app's block, returning it and whether it was there.
//
// A block runs from its header to the next line that starts a section, or to the
// end of the file. That is enough of TOML to find one section and not nearly
// enough to rewrite the rest, which is exactly the intended split.
func tomlServerBlock(content string) (string, bool) {
	start := strings.Index(content, tomlHeader)
	if start < 0 {
		return "", false
	}
	rest := content[start+len(tomlHeader):]
	end := len(rest)
	for offset := 0; ; {
		next := strings.Index(rest[offset:], "\n[")
		if next < 0 {
			break
		}
		end = offset + next + 1
		break
	}
	return content[start : start+len(tomlHeader)+end], true
}

// tomlBlockCommand reads the command out of this app's block, unquoted.
//
// writeTOMLEntry writes it with %q, so a Windows path is stored with every
// backslash doubled. Reading it back with strconv.Unquote undoes exactly that,
// which is what makes the comparison in describeMCPClient about the path rather
// than about how it happened to be quoted (#13).
//
// A line scan rather than a TOML parse, for the reason at the top of this file:
// this app has no TOML library, and adding one to read one string would be a
// dependency taken on to answer a question two lines of text handling can.
// Anything it cannot read answers "", which reports "points elsewhere" — the same
// thing an entry that genuinely could not be verified should report.
func tomlBlockCommand(block string) string {
	for _, line := range strings.Split(block, "\n") {
		rest, found := strings.CutPrefix(strings.TrimSpace(line), "command")
		if !found {
			continue
		}
		rest, found = strings.CutPrefix(strings.TrimSpace(rest), "=")
		if !found {
			continue
		}
		rest = strings.TrimSpace(rest)
		if unquoted, err := strconv.Unquote(rest); err == nil {
			return unquoted
		}
		// Not a quoted string — somebody hand-edited it. Answer what is there
		// rather than nothing, so a config a person wrote still compares.
		return rest
	}
	return ""
}

func writeTOMLEntry(path, command string) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	content := string(data)
	block := fmt.Sprintf("%s\ncommand = %q\nargs = [\"mcp\"]\n", tomlHeader, command)

	if existing, found := tomlServerBlock(content); found {
		content = strings.Replace(content, existing, block, 1)
	} else {
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		if content != "" {
			content += "\n"
		}
		content += block
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

func removeTOMLEntry(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	content := string(data)
	existing, found := tomlServerBlock(content)
	if !found {
		return nil
	}
	content = strings.Replace(content, existing, "", 1)
	return os.WriteFile(path, []byte(strings.TrimLeft(content, "\n")), 0o600)
}
