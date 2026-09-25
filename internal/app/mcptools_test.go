package app

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// mcpCall runs one tool against a test app and decodes the JSON it answered.
// The result's text is JSON by construction (see mcpResult), so a test reads
// the record back rather than grepping a string for it.
func mcpCall(t *testing.T, app *App, name string, args any, out any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	result := callMCPTool(app, name, encoded)
	if out == nil {
		return result
	}
	if result["isError"] == true {
		t.Fatalf("%s refused: %v", name, mcpText(result))
	}
	if err := json.Unmarshal([]byte(mcpText(result)), out); err != nil {
		t.Fatalf("%s did not answer JSON: %v\n%s", name, err, mcpText(result))
	}
	return result
}

// mcpText is the text of a result's first content block.
func mcpText(result map[string]any) string {
	content, _ := result["content"].([]map[string]any)
	if len(content) == 0 {
		return ""
	}
	text, _ := content[0]["text"].(string)
	return text
}

// TestEveryBindingHasATool is #87: the MCP server offers the whole binding
// surface, not the slice of it that was convenient when the server was
// written. Reflection over App is the list of what the window can do; each
// tool names the binding it reaches; the difference must be exactly the
// bindings that cannot work without a window, and those are named too.
//
// Both directions are checked. A binding with no tool is the bug the report
// is about; a tool naming a binding that does not exist is a tool that will
// stop reaching anything the next time that binding is renamed.
func TestEveryBindingHasATool(t *testing.T) {
	bindings := map[string]bool{}
	appType := reflect.TypeOf(&App{})
	for i := 0; i < appType.NumMethod(); i++ {
		bindings[appType.Method(i).Name] = true
	}

	reached := map[string]string{}
	for _, tool := range mcpTools() {
		if strings.TrimSpace(tool.binding) == "" {
			t.Errorf("tool %s does not say which binding it reaches", tool.name)
			continue
		}
		for _, name := range strings.Fields(tool.binding) {
			if !bindings[name] {
				t.Errorf("tool %s reaches %s, which is not a binding on App", tool.name, name)
			}
			reached[name] = tool.name
		}
	}

	for name, reason := range mcpNotOffered {
		if !bindings[name] {
			t.Errorf("%s is listed as not offered, but is not a binding on App", name)
		}
		if tool, ok := reached[name]; ok {
			t.Errorf("%s is listed as not offered, yet %s reaches it", name, tool)
		}
		if strings.TrimSpace(reason) == "" {
			t.Errorf("%s is not offered and no reason is given", name)
		}
	}

	var missing []string
	for name := range bindings {
		if reached[name] != "" {
			continue
		}
		if _, listed := mcpNotOffered[name]; !listed {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Errorf("bindings with no MCP tool and no reason given: %s", strings.Join(missing, ", "))
	}
}

// TestToolDefinitionsAreWellFormed is what a client's tools/list validator
// would say: unique names, a description, and a schema whose required keys
// are all declared properties — a required argument with no property is a
// tool a client can list and no model can call.
func TestToolDefinitionsAreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, tool := range mcpTools() {
		if seen[tool.name] {
			t.Errorf("two tools are called %s", tool.name)
		}
		seen[tool.name] = true
		if strings.TrimSpace(tool.description) == "" {
			t.Errorf("%s has no description", tool.name)
		}
		if tool.schema["type"] != "object" {
			t.Errorf("%s: schema is not an object", tool.name)
		}
		properties, _ := tool.schema["properties"].(map[string]any)
		required, _ := tool.schema["required"].([]string)
		for _, key := range required {
			if _, ok := properties[key]; !ok {
				t.Errorf("%s requires %q, which it does not declare", tool.name, key)
			}
		}
		if tool.run == nil {
			t.Errorf("%s has no implementation", tool.name)
		}
	}
	if mcpToolCount() != len(seen) {
		t.Errorf("mcpToolCount says %d, there are %d", mcpToolCount(), len(seen))
	}
}

// TestToolsListOverStdio drives the server the way a client does: initialize,
// then tools/list, one JSON line each, and checks every tool is offered.
func TestToolsListOverStdio(t *testing.T) {
	app := newTestApp(t)
	var in bytes.Buffer
	in.WriteString(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n")
	in.WriteString(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n")
	in.WriteString(`{"jsonrpc":"2.0","id":"two","method":"tools/list"}` + "\n")
	var out bytes.Buffer
	if code := serveMCP(app, &in, &out); code != 0 {
		t.Fatalf("serveMCP exited %d", code)
	}

	scanner := bufio.NewScanner(&out)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var lines []map[string]any
	for scanner.Scan() {
		var line map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatalf("not a JSON line: %s", scanner.Text())
		}
		lines = append(lines, line)
	}
	// Two answers: the notification takes none.
	if len(lines) != 2 {
		t.Fatalf("want 2 responses, got %d", len(lines))
	}
	result, _ := lines[1]["result"].(map[string]any)
	tools, _ := result["tools"].([]any)
	if len(tools) != mcpToolCount() {
		t.Fatalf("tools/list offered %d tools, the server has %d", len(tools), mcpToolCount())
	}
}

// seedTrack makes an artist, an album and one track, for the tools that need
// something to act on.
func seedTrack(t *testing.T, app *App, name string) Track {
	t.Helper()
	artist, err := app.SaveArtist(Artist{Name: "Cassette Motel", Inspirations: []Inspiration{}})
	if err != nil {
		t.Fatal(err)
	}
	album, err := app.SaveAlbum(Album{ArtistID: artist.ID, Name: "Season 3"})
	if err != nil {
		t.Fatal(err)
	}
	track, err := app.SaveTrack(Track{AlbumID: album.ID, Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return track
}

func TestLibraryToolsReachTheStore(t *testing.T) {
	app := newTestApp(t)
	first := seedTrack(t, app, "Cold Open")
	second, err := app.SaveTrack(Track{AlbumID: first.AlbumID, Name: "Static"})
	if err != nil {
		t.Fatal(err)
	}

	var tree []LibraryArtist
	mcpCall(t, app, "library_tree", map[string]any{}, &tree)
	if len(tree) != 1 || len(tree[0].Albums) != 1 || tree[0].Albums[0].TrackCount != 2 {
		t.Fatalf("the tree does not show the record: %+v", tree)
	}

	var reordered []Track
	mcpCall(t, app, "reorder_tracks", map[string]any{
		"albumId": first.AlbumID, "trackIds": []string{second.ID, first.ID},
	}, &reordered)
	if len(reordered) != 2 || reordered[0].ID != second.ID || reordered[0].Position != 1 {
		t.Fatalf("the order did not move: %+v", reordered)
	}

	var ok map[string]any
	mcpCall(t, app, "delete_track", map[string]any{"id": first.ID}, &ok)
	if ok["ok"] != true {
		t.Fatalf("delete_track did not say it was done: %v", ok)
	}
	remaining, err := app.ListTracks(first.AlbumID)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].ID != second.ID {
		t.Fatalf("the track is still there: %+v", remaining)
	}
}

func TestTrackFileToolsImportAndChoose(t *testing.T) {
	app := newTestApp(t)
	track := seedTrack(t, app, "Cold Open")

	var withOne Track
	mcpCall(t, app, "import_track_file", map[string]any{
		"trackId": track.ID, "sourcePath": makeWAV(t, 3, 176400), "label": "take 1",
	}, &withOne)
	if len(withOne.Files) != 1 || !withOne.Files[0].Chosen {
		t.Fatalf("the first take is not the chosen one: %+v", withOne.Files)
	}

	// A second take arrives as bytes, the shape a drop has.
	wav, err := os.ReadFile(makeWAV(t, 4, 176400))
	if err != nil {
		t.Fatal(err)
	}
	var withTwo Track
	mcpCall(t, app, "import_track_file_data", map[string]any{
		"trackId": track.ID, "name": "take two.wav",
		"dataUrl": "data:audio/wav;base64," + base64.StdEncoding.EncodeToString(wav),
	}, &withTwo)
	if len(withTwo.Files) != 2 {
		t.Fatalf("want two takes, got %+v", withTwo.Files)
	}

	var chosen Track
	mcpCall(t, app, "choose_track_file", map[string]any{
		"trackId": track.ID, "fileId": withTwo.Files[1].ID,
	}, &chosen)
	if !chosen.Files[1].Chosen || chosen.Files[0].Chosen {
		t.Fatalf("the choice did not move: %+v", chosen.Files)
	}
	if chosen.TargetSeconds != 4 {
		t.Fatalf("the duration did not follow the choice: %d", chosen.TargetSeconds)
	}

	var removed Track
	mcpCall(t, app, "remove_track_file", map[string]any{
		"trackId": track.ID, "fileId": chosen.Files[1].ID, "deleteFile": true,
	}, &removed)
	if len(removed.Files) != 1 {
		t.Fatalf("the take was not removed: %+v", removed.Files)
	}
	if _, err := os.Stat(chosen.Files[1].Path); !os.IsNotExist(err) {
		t.Fatalf("deleteFile did not delete the audio: %v", err)
	}
}

func TestBugToolsReadTheQueue(t *testing.T) {
	app := newTestApp(t)
	filed, err := app.FileBugReport(BugReport{Description: "the cover is upside down", Route: "album"})
	if err != nil {
		t.Fatal(err)
	}

	var counts BugCounts
	mcpCall(t, app, "get_bug_counts", map[string]any{}, &counts)
	if counts.Open != 1 {
		t.Fatalf("want one open report, got %+v", counts)
	}

	var one BugReport
	mcpCall(t, app, "get_bug_report", map[string]any{"id": filed.ID}, &one)
	if one.Description != filed.Description {
		t.Fatalf("get_bug_report answered %+v", one)
	}

	var found []BugReport
	mcpCall(t, app, "search_bug_reports", map[string]any{"query": "upside"}, &found)
	if len(found) != 1 {
		t.Fatalf("search found %d reports", len(found))
	}

	// An attachment goes on as a data URL and comes back as an image block —
	// a model given the screenshot can look at it, where a path is only a
	// string.
	png := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("png-bytes"))
	var path map[string]string
	mcpCall(t, app, "attach_bug_media", map[string]any{"reportId": filed.ID, "dataUrl": png}, &path)
	if path["path"] == "" || filepath.Ext(path["path"]) != ".png" {
		t.Fatalf("attach_bug_media answered %v", path)
	}
	result := mcpCall(t, app, "read_bug_attachment", map[string]any{"path": path["path"]}, nil)
	if result["isError"] == true {
		t.Fatalf("read_bug_attachment refused: %s", mcpText(result))
	}
	content, _ := result["content"].([]map[string]any)
	if len(content) != 1 || content[0]["type"] != "image" || content[0]["mimeType"] != "image/png" {
		t.Fatalf("the attachment did not come back as an image: %v", content)
	}
	if data, _ := content[0]["data"].(string); data != base64.StdEncoding.EncodeToString([]byte("png-bytes")) {
		t.Fatalf("the image bytes are not the attachment's: %q", data)
	}
}

// TestSettingsToolsKeepTheKeysIn: the raw settings door is open for the keys
// the window uses it for, and closed on the one that holds API keys — those
// are read through list_ai_services masked, and nothing else may hand them out.
func TestSettingsToolsKeepTheKeysIn(t *testing.T) {
	app := newTestApp(t)
	var ok map[string]any
	mcpCall(t, app, "set_setting", map[string]any{"key": "default_track_length", "value": "240"}, &ok)
	var read map[string]string
	mcpCall(t, app, "get_setting", map[string]any{"key": "default_track_length"}, &read)
	if read["value"] != "240" {
		t.Fatalf("the setting did not round-trip: %v", read)
	}

	refused := mcpCall(t, app, "get_setting", map[string]any{"key": keyAIServices}, nil)
	if refused["isError"] != true {
		t.Fatalf("get_setting handed out the AI connections: %s", mcpText(refused))
	}
	refused = mcpCall(t, app, "set_setting", map[string]any{"key": keyAIServices, "value": "{}"}, nil)
	if refused["isError"] != true {
		t.Fatal("set_setting overwrote the AI connections")
	}
}

// TestListMCPToolsIsTheServersOwnList is #90: the Settings panel's tool list
// comes from the same list the server dispatches through, every tool sits in
// a named group, and a tool's arguments are read off its schema with the
// required ones marked.
func TestListMCPToolsIsTheServersOwnList(t *testing.T) {
	app := newTestApp(t)
	listed := app.ListMCPTools()
	if len(listed) != mcpToolCount() {
		t.Fatalf("listed %d tools, the server has %d", len(listed), mcpToolCount())
	}
	groups := map[string]int{}
	byName := map[string]MCPToolInfo{}
	for _, tool := range listed {
		if tool.Group == "" || tool.Description == "" {
			t.Errorf("%s is listed without a group or a description", tool.Name)
		}
		groups[tool.Group]++
		byName[tool.Name] = tool
	}
	if len(groups) < 5 {
		t.Errorf("want the five domains, got %v", groups)
	}
	save, ok := byName["save_track"]
	if !ok {
		t.Fatal("save_track is not listed")
	}
	required := map[string]bool{}
	for _, arg := range save.Arguments {
		required[arg.Name] = arg.Required
		if arg.Description == "" {
			t.Errorf("save_track's %s has no description", arg.Name)
		}
	}
	if !required["albumId"] || !required["name"] || required["lyrics"] {
		t.Errorf("save_track's required arguments are wrong: %v", required)
	}
	if len(byName["list_artists"].Arguments) != 0 {
		t.Errorf("list_artists takes no arguments, listed %v", byName["list_artists"].Arguments)
	}
}

func TestUnknownToolAndBadArgumentsAreToolErrors(t *testing.T) {
	app := newTestApp(t)
	if result := callMCPTool(app, "no_such_tool", nil); result["isError"] != true {
		t.Fatalf("an unknown tool was not refused: %v", result)
	}
	// A wrongly typed argument is the model's mistake to read and correct, so
	// it comes back as a tool error rather than a transport fault.
	if result := callMCPTool(app, "get_artist", json.RawMessage(`{"id": 7}`)); result["isError"] != true {
		t.Fatalf("bad arguments were not refused: %v", result)
	}
	if got := fmt.Sprint(mcpText(callMCPTool(app, "get_bug_repo", nil))); !strings.Contains(got, "dir") {
		t.Fatalf("get_bug_repo with no arguments did not answer: %s", got)
	}
}
