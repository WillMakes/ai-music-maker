package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readAgyAllow is the allow list in an agy settings file, and the whole file
// as a map so a test can check that nothing else was lost.
func readAgyAllow(t *testing.T, path string) ([]string, map[string]any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the settings file is no longer JSON: %v\n%s", err, raw)
	}
	allow := []string{}
	if perms, ok := doc["permissions"].(map[string]any); ok {
		if list, ok := perms["allow"].([]any); ok {
			for _, rule := range list {
				allow = append(allow, rule.(string))
			}
		}
	}
	return allow, doc
}

// TestAgyURLReadingIsAllowed is #100. Music analysis asks agy to research a
// song on the web, and headless -p auto-denies the read_url tool unless a rule
// in agy's settings allows it — so every analysis ended "printed no answer".
// The rule is added to the file agy reads, beside whatever is already there.
func TestAgyURLReadingIsAllowed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	existing := `{
  "trustedWorkspaces": ["C:\\Users\\producer"],
  "permissions": {"allow": ["command(git status)"], "deny": ["command(rm)"]},
  "queuedMessages": "send-immediately"
}`
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := allowAgyURLReading(path); err != nil {
		t.Fatal(err)
	}
	allow, doc := readAgyAllow(t, path)
	if strings.Join(allow, ",") != "command(git status),"+agyReadURLRule {
		t.Errorf("allow list = %v, want the producer's rule and then %s", allow, agyReadURLRule)
	}
	if doc["queuedMessages"] != "send-immediately" || doc["trustedWorkspaces"] == nil {
		t.Errorf("the producer's other settings were lost: %v", doc)
	}
	if deny := doc["permissions"].(map[string]any)["deny"]; deny == nil {
		t.Errorf("the producer's deny rules were lost: %v", doc["permissions"])
	}

	// Every analysis calls this, so the second call must leave one rule, not two.
	if err := allowAgyURLReading(path); err != nil {
		t.Fatal(err)
	}
	if allow, _ := readAgyAllow(t, path); len(allow) != 2 {
		t.Errorf("the rule was added twice: %v", allow)
	}
}

// TestAgyURLReadingWithNoSettingsFile covers a fresh agy install, which has
// no settings file until something is saved.
func TestAgyURLReadingWithNoSettingsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "antigravity-cli", "settings.json")
	if err := allowAgyURLReading(path); err != nil {
		t.Fatal(err)
	}
	if allow, _ := readAgyAllow(t, path); strings.Join(allow, ",") != agyReadURLRule {
		t.Errorf("allow list = %v, want just %s", allow, agyReadURLRule)
	}
}

// TestAgyURLReadingLeavesABrokenFileAlone: agy itself refuses to save over a
// settings file it cannot parse, so the producer can repair it by hand. This
// app must not be the thing that overwrites it.
func TestAgyURLReadingLeavesABrokenFileAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	broken := `{"permissions": {"allow": [` // a hand edit, half done
	if err := os.WriteFile(path, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := allowAgyURLReading(path); err == nil {
		t.Error("an unreadable settings file was accepted")
	}
	if raw, _ := os.ReadFile(path); string(raw) != broken {
		t.Errorf("the broken file was rewritten: %q", raw)
	}
}

// TestAnalysisBriefKeepsAgyToTheWeb: allowing read_url got the run one tool
// further, and then agy reached for a shell command, which headless mode denies
// the same way. The brief has to say which tools the run may use.
func TestAnalysisBriefKeepsAgyToTheWeb(t *testing.T) {
	// Whitespace folded, because the brief is wrapped for reading and a phrase
	// can break across a line.
	brief := strings.Join(strings.Fields(analysisBrief), " ")
	for _, want := range []string{"URL reading", "terminal commands"} {
		if !strings.Contains(brief, want) {
			t.Errorf("the analysis brief does not mention %q", want)
		}
	}
}
