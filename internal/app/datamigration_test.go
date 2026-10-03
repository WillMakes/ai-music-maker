package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDataFolderMovesToTheNewName is #103: the app was SunoMaker and kept
// everything in ~/.sunomaker. Renamed, it moves that folder to ~/.aimusicmaker
// on first launch, so the library, settings and queue come along.
func TestDataFolderMovesToTheNewName(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, legacyDataDirName)
	if err := os.MkdirAll(filepath.Join(legacy, "artwork"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "artwork", "cover.png"), []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}

	dir, err := settleDataDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(home, dataDirName) {
		t.Fatalf("data folder = %s, want the new name", dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "artwork", "cover.png")); err != nil {
		t.Fatalf("the old folder's contents did not come along: %v", err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("the old folder is still there: %v", err)
	}

	// Already moved: a second launch leaves everything where it is.
	again, err := settleDataDir(home)
	if err != nil || again != dir {
		t.Fatalf("second launch: %s, %v", again, err)
	}
}

// TestDataFolderNewInstall: no old folder means a plain new one.
func TestDataFolderNewInstall(t *testing.T) {
	home := t.TempDir()
	dir, err := settleDataDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() || filepath.Base(dir) != dataDirName {
		t.Fatalf("no new folder: %s, %v", dir, err)
	}
}

// TestDataFolderNeverMergesTwo: with both folders present the new one wins and
// the old one is left exactly as it was — two libraries are not merged by
// guesswork.
func TestDataFolderNeverMergesTwo(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, legacyDataDirName)
	current := filepath.Join(home, dataDirName)
	for _, d := range []string{legacy, current} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	dir, err := settleDataDir(home)
	if err != nil || dir != current {
		t.Fatalf("got %s, %v", dir, err)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("the old folder was touched: %v", err)
	}
}

// TestDatabaseFileTakesTheNewName: the database inside the folder is renamed
// with its write-ahead log, which holds writes not yet folded into it — a
// database renamed without its log is a database missing its latest changes.
func TestDatabaseFileTakesTheNewName(t *testing.T) {
	dir := t.TempDir()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.WriteFile(filepath.Join(dir, legacyDBName+suffix), []byte(suffix), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	path := settleStorePath(dir)
	if path != filepath.Join(dir, dbName) {
		t.Fatalf("store path = %s", path)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		got, err := os.ReadFile(path + suffix)
		if err != nil || string(got) != suffix {
			t.Fatalf("%s did not move: %q, %v", dbName+suffix, got, err)
		}
		if _, err := os.Stat(filepath.Join(dir, legacyDBName+suffix)); !os.IsNotExist(err) {
			t.Fatalf("%s is still there", legacyDBName+suffix)
		}
	}
}

// TestStoredPathsFollowTheFolder: bug-report attachments and interview files
// are recorded as absolute paths into the data folder. After the move those
// paths name a folder that is gone, so they are rewritten — in their JSON form,
// where a Windows backslash is doubled.
func TestStoredPathsFollowTheFolder(t *testing.T) {
	a := newTestApp(t)
	home := t.TempDir()
	legacy := filepath.Join(home, legacyDataDirName)
	current := filepath.Join(home, dataDirName)
	oldFile := filepath.Join(legacy, "bug-attachments", "bug_1", "shot.png")
	oldChatFile := filepath.Join(legacy, "interview-attachments", "album-new", "take.mp3")

	attachments, _ := json.Marshal([]string{oldFile})
	if _, err := a.store.db.Exec(
		`INSERT INTO bug_reports (id, description, attachments) VALUES ('bug_1', 'x', ?)`,
		string(attachments)); err != nil {
		t.Fatal(err)
	}
	messages, _ := json.Marshal([]map[string]any{{"role": "user", "attachments": []string{oldChatFile}}})
	if _, err := a.store.db.Exec(
		`INSERT INTO chats (subject_kind, subject_id, messages) VALUES ('album', '', ?)`,
		string(messages)); err != nil {
		t.Fatal(err)
	}

	if err := a.store.rewriteLegacyPaths(legacy, current); err != nil {
		t.Fatal(err)
	}

	var gotAttachments, gotMessages string
	if err := a.store.db.QueryRow(`SELECT attachments FROM bug_reports WHERE id = 'bug_1'`).Scan(&gotAttachments); err != nil {
		t.Fatal(err)
	}
	if err := a.store.db.QueryRow(`SELECT messages FROM chats WHERE subject_kind = 'album'`).Scan(&gotMessages); err != nil {
		t.Fatal(err)
	}
	var paths []string
	if err := json.Unmarshal([]byte(gotAttachments), &paths); err != nil || len(paths) != 1 {
		t.Fatalf("attachments no longer JSON: %q", gotAttachments)
	}
	want := filepath.Join(current, "bug-attachments", "bug_1", "shot.png")
	if paths[0] != want {
		t.Fatalf("attachment = %s, want %s", paths[0], want)
	}
	if strings.Contains(gotMessages, legacyDataDirName) || !strings.Contains(gotMessages, dataDirName) {
		t.Fatalf("interview file path not rewritten: %s", gotMessages)
	}
}

// TestMusicLibraryKeepsItsOldFolder: the music library is not moved by the
// rename. A producer with ~/Music/SunoMaker keeps filing into it; a new install
// gets the new name.
func TestMusicLibraryKeepsItsOldFolder(t *testing.T) {
	a := newTestApp(t)
	base, err := osMusicDir()
	if err != nil {
		t.Fatal(err)
	}

	fresh, err := a.MusicRoot()
	if err != nil || fresh != filepath.Join(base, musicFolderName) {
		t.Fatalf("new install: %s, %v", fresh, err)
	}
	if err := os.Remove(fresh); err != nil {
		t.Fatal(err)
	}

	legacy := filepath.Join(base, legacyMusicFolderName)
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := a.MusicRoot()
	if err != nil || got != legacy {
		t.Fatalf("existing library: %s, %v — want it kept at %s", got, err, legacy)
	}
}

// TestMCPConnectReplacesTheOldEntry: a client registered before the rename has
// a suno-maker entry launching a binary that is no longer built. Connecting
// again leaves one entry under the new name, and the producer's own servers
// untouched.
func TestMCPConnectReplacesTheOldEntry(t *testing.T) {
	dir := t.TempDir()

	jsonPath := filepath.Join(dir, "claude.json")
	old := `{"mcpServers": {"suno-maker": {"command": "old.exe", "args": ["mcp"]}, "theirs": {"command": "x"}}}`
	if err := os.WriteFile(jsonPath, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONEntry(jsonPath, "new.exe"); err != nil {
		t.Fatal(err)
	}
	var config struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	data, _ := os.ReadFile(jsonPath)
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if _, ok := config.MCPServers[legacyMCPServerName]; ok {
		t.Errorf("the old entry is still there: %s", data)
	}
	if _, ok := config.MCPServers[mcpServerName]; !ok {
		t.Errorf("no entry under the new name: %s", data)
	}
	if _, ok := config.MCPServers["theirs"]; !ok {
		t.Errorf("the producer's own server was lost: %s", data)
	}

	tomlPath := filepath.Join(dir, "config.toml")
	oldTOML := "model = \"x\"\n\n[mcp_servers.suno-maker]\ncommand = \"old.exe\"\nargs = [\"mcp\"]\n\n[mcp_servers.theirs]\ncommand = \"y\"\n"
	if err := os.WriteFile(tomlPath, []byte(oldTOML), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeTOMLEntry(tomlPath, "new.exe"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(tomlPath)
	text := string(got)
	if strings.Contains(text, legacyTOMLHeader) || strings.Contains(text, "old.exe") {
		t.Errorf("the old block is still there:\n%s", text)
	}
	if !strings.Contains(text, tomlHeader) || !strings.Contains(text, "[mcp_servers.theirs]") || !strings.Contains(text, `model = "x"`) {
		t.Errorf("the new block or the producer's settings are missing:\n%s", text)
	}

	// Disconnecting removes both, and leaves the rest.
	if err := removeTOMLEntry(tomlPath); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(tomlPath)
	if strings.Contains(string(got), "[mcp_servers.aimusicmaker]") || !strings.Contains(string(got), "[mcp_servers.theirs]") {
		t.Errorf("disconnect left:\n%s", got)
	}
}
