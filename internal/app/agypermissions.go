package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ---------------------------------------------------------------------------
// Letting agy read the web (#100)
//
// Music analysis is research: agy has to open pages about the song. Its
// headless -p mode cannot ask the producer for permission, so a tool with no
// standing rule is auto-denied, and a denied tool ends the run with nothing on
// stdout. The URL-reading tool was such a tool, and every analysis failed.
//
// agy reads standing rules from permissions.allow in its own settings files and
// nowhere else that a single run can reach: there is no per-tool flag, and a
// settings file in the working directory is ignored. The one flag that would
// work, --dangerously-skip-permissions, approves everything — shell commands
// and file writes included, on a run whose input is whatever a web page says.
// So the rule goes into agy's CLI settings, which is also what agy's own error
// message tells the producer to do.
//
// A wildcard rather than a list of domains, because the research goes wherever
// the song is written about and nobody can list that in advance. The rule only
// lets agy read pages; anything that runs or writes still needs a rule of its
// own, which is why the analysis brief also tells agy not to try.
// ---------------------------------------------------------------------------

// agyReadURLRule is the allow rule added to agy's settings.
const agyReadURLRule = "read_url(*)"

// agySettingsPath is the CLI settings file agy reads its permissions from. A
// variable so tests point it at a temp folder; the real one belongs to the
// producer's own agy install.
var agySettingsPath = func() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".gemini", "antigravity-cli", "settings.json"), nil
}

// allowAgyURLReading adds agyReadURLRule to the settings file at path, keeping
// every other setting and rule as it was. It does nothing when the rule is
// already there, and refuses to touch a file it cannot parse: agy leaves such a
// file alone so the producer can repair it, and this app does the same.
func allowAgyURLReading(path string) error {
	// RawMessage throughout, so settings this app has never heard of round-trip
	// byte for byte rather than through a Go type that would drop them.
	doc := map[string]json.RawMessage{}
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &doc); err != nil {
			return fmt.Errorf("agy's settings file %s is not valid JSON, so it was left alone: %w", path, err)
		}
		if doc == nil {
			doc = map[string]json.RawMessage{}
		}
	case os.IsNotExist(err):
		// A fresh agy install has no settings file until something is saved.
	default:
		return err
	}

	perms := map[string]json.RawMessage{}
	if section, ok := doc["permissions"]; ok {
		if err := json.Unmarshal(section, &perms); err != nil || perms == nil {
			return fmt.Errorf("agy's permissions in %s are not in the shape this app expects, so they were left alone", path)
		}
	}
	allow := []string{}
	if list, ok := perms["allow"]; ok {
		if err := json.Unmarshal(list, &allow); err != nil || allow == nil {
			return fmt.Errorf("agy's allow list in %s is not a list of rules, so it was left alone", path)
		}
	}
	for _, rule := range allow {
		if rule == agyReadURLRule {
			return nil
		}
	}
	allow = append(allow, agyReadURLRule)

	if perms["allow"], err = marshalAgy(allow); err != nil {
		return err
	}
	if doc["permissions"], err = marshalAgy(perms); err != nil {
		return err
	}
	out, err := marshalAgy(doc)
	if err != nil {
		return err
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, out, "", "  "); err != nil {
		return err
	}
	pretty.WriteByte('\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// The file may hold settings the producer wrote by hand, so it keeps the
	// mode it had; a new one is private, like the rest of agy's folder.
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	return os.WriteFile(path, pretty.Bytes(), mode)
}

// marshalAgy encodes without HTML escaping. Go escapes < and > by default,
// and agy's rules are written with them — read_url(<target>) in its own docs —
// so the default would turn a producer's hand-written rule into < soup.
func marshalAgy(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return json.RawMessage(bytes.TrimRight(buf.Bytes(), "\n")), nil
}
