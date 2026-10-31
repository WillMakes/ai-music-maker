package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"aimusicmaker/internal/mcpclient"
)

// sunoCreateSchema is audio-create's input schema as Suno listed it on
// 2026-10-30, descriptions left out (docs/suno-mcp.md has them).
const sunoCreateSchema = `{"additionalProperties":false,"type":"object","properties":{
	"controls":{"type":"object"},"description":{"type":"string"},"duration":{"type":"integer"},
	"duration_guidance":{"type":"boolean"},"duration_strict":{"type":"boolean"},
	"idempotency_key":{"type":"string"},"instrumental":{"type":"boolean"},"lyrics":{"type":"string"},
	"model":{"type":"string"},"negative_style":{"type":"string"},"style":{"type":"string"},
	"title":{"type":"string"},"vocal_gender":{"type":"string"},"voice_id":{"type":"string"},
	"workspace_id":{"type":"string"}}}`

// fakeSuno answers the tools the album run calls, in the shapes Suno's server
// answers them (docs/suno-mcp.md): account-usage, audio-create making two
// songs per call and refusing any argument its schema does not declare,
// audio-wait pending once and then complete, and audio-download that wants a
// credit.
type fakeSuno struct {
	mu        sync.Mutex
	files     *httptest.Server
	credits   int
	downloads int
	calls     []string
	confirmed int
	created   []map[string]any
	waits     map[string]int
	// outOfCredits makes audio-create refuse, as a 402 does.
	outOfCredits bool
	// failClips end in error.
	failClips []string
	// unauthorized makes every call refused as a dead sign-in is.
	unauthorized bool
	nextClip     int
	// schema is what audio-create declares, and what tools/list answers;
	// listed is what the fake lists instead, when it differs (a stale list).
	schema string
	listed string
	// keys maps each idempotency key to the clips it made, as Suno replays them.
	keys map[string][]string
}

// sunoListingFake is fakeSuno with tools/list, as the real client has.
type sunoListingFake struct{ *fakeSuno }

func (f sunoListingFake) ListTools(context.Context) ([]mcpclient.Tool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "tools/list")
	listed := f.listed
	if listed == "" {
		listed = f.schema
	}
	return []mcpclient.Tool{{Name: "audio-create", InputSchema: json.RawMessage(listed)}}, nil
}

func newFakeSuno(t *testing.T) *fakeSuno {
	f := &fakeSuno{credits: 100, downloads: 10, waits: map[string]int{}, schema: sunoCreateSchema, keys: map[string][]string{}}
	f.files = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		fmt.Fprint(w, "ID3-not-really-an-mp3-"+r.URL.Path)
	}))
	t.Cleanup(f.files.Close)
	return f
}

func result(v any) mcpclient.ToolResult {
	raw, _ := json.Marshal(v)
	return mcpclient.ToolResult{Content: []mcpclient.Content{{Type: "text", Text: string(raw)}}}
}

func (f *fakeSuno) CallTool(_ context.Context, name string, args any) (mcpclient.ToolResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name)
	if f.unauthorized {
		return mcpclient.ToolResult{}, mcpclient.ErrUnauthorized
	}
	in, _ := args.(map[string]any)
	switch name {
	case "account-usage":
		return result(map[string]any{
			"allowed_models": []string{"v6", "v6-mini"},
			"suno_account": map[string]any{
				"credits_remaining": f.credits, "download_credits_remaining": f.downloads,
				"monthly_limit": 2500, "plan": map[string]any{"name": "Pro Plan"},
			},
		}), nil
	case "audio-create":
		if f.outOfCredits {
			return mcpclient.ToolResult{}, &mcpclient.ToolError{Tool: name,
				Text: `402 {"error":"insufficient_credits","code":"insufficient_credits"}`}
		}
		declared := declaredInputs(json.RawMessage(f.schema))
		names := []string{}
		for name := range declared {
			names = append(names, name)
		}
		sort.Strings(names)
		for name := range in {
			if !declared[name] {
				// Suno's own words when it refused clip_count.
				return mcpclient.ToolResult{}, &mcpclient.ToolError{Tool: "audio-create", Text: fmt.Sprintf(
					"unknown argument %q: audio-create takes only %s. Pass them at the top level of the arguments, not nested.",
					name, strings.Join(names, ", "))}
			}
		}
		key, _ := in["idempotency_key"].(string)
		ids, replay := f.keys[key]
		if !replay {
			f.created = append(f.created, in)
			// Two songs a call, as suno.com's Create makes: there is no
			// count to ask for fewer since 2026-10-30.
			for i := 0; i < 2; i++ {
				f.nextClip++
				ids = append(ids, fmt.Sprintf("%08d-0000-4000-8000-000000000000", f.nextClip))
			}
			f.keys[key] = ids
			f.credits -= 10 * len(ids)
		}
		return result(map[string]any{"id": ids[0], "clip_ids": ids, "status": "submitted"}), nil
	case "audio-wait":
		ids, _ := in["ids"].([]string)
		clips := []map[string]any{}
		for _, id := range ids {
			f.waits[id]++
			status := "streaming"
			if f.waits[id] > 1 {
				status = "complete"
			}
			var clipErr any
			if slices.Contains(f.failClips, id) {
				status, clipErr = "error", "content policy"
			}
			clips = append(clips, map[string]any{"id": id, "status": status, "error": clipErr,
				"song_url": "https://suno.com/song/" + id, "metadata": map[string]any{"duration": 61.5}})
		}
		answer := map[string]any{}
		for k, v := range clips[0] {
			answer[k] = v
		}
		if len(clips) > 1 {
			answer["clips"] = clips
		}
		return result(answer), nil
	case "audio-download":
		id, _ := in["id"].(string)
		if in["confirm"] != true {
			return result(map[string]any{"status": "requires_credit", "format": "mp3",
				"credits_remaining": f.downloads, "message": "Ask them first."}), nil
		}
		f.confirmed++
		f.downloads--
		return result(map[string]any{"status": "ready", "url": f.files.URL + "/" + id + ".mp3"}), nil
	}
	return mcpclient.ToolResult{}, fmt.Errorf("unexpected tool %s", name)
}

func (f *fakeSuno) count(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == name {
			n++
		}
	}
	return n
}

func withFakeSuno(t *testing.T, f toolCaller) {
	previous := sunoClient
	sunoClient = func(*App) (toolCaller, error) { return f, nil }
	t.Cleanup(func() { sunoClient = previous })
}

func sunoAlbum(t *testing.T, app *App, names ...string) (Album, []Track) {
	t.Helper()
	artist, err := app.SaveArtist(Artist{Name: "Cassette Motel", Inspirations: []Inspiration{}})
	if err != nil {
		t.Fatal(err)
	}
	album, err := app.SaveAlbum(Album{ArtistID: artist.ID, Name: "Season 3"})
	if err != nil {
		t.Fatal(err)
	}
	var tracks []Track
	for _, name := range names {
		track, err := app.SaveTrack(Track{AlbumID: album.ID, Name: name, Prompt: "lo-fi tape hiss, 80 BPM",
			Lyrics: "[Verse]\nstatic on the line", TargetSeconds: 90})
		if err != nil {
			t.Fatal(err)
		}
		tracks = append(tracks, track)
	}
	return album, tracks
}

func waitForRun(t *testing.T, app *App, albumID string) SunoAlbumRun {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		run := app.GetSunoAlbumRun(albumID)
		if run.State != "running" {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the run did not finish")
	return SunoAlbumRun{}
}

func TestSunoRunFilesTakesLikeTheExtension(t *testing.T) {
	app := newTestApp(t)
	suno := newFakeSuno(t)
	withFakeSuno(t, suno)
	album, tracks := sunoAlbum(t, app, "Static", "Dial Tone")

	run, err := app.StartSunoAlbumRun(album.ID, SunoRunOptions{Download: true})
	if err != nil {
		t.Fatal(err)
	}
	if run.Before.CreditsRemaining != 100 || !run.Before.Reported {
		t.Errorf("before = %+v, want the balance read first", run.Before)
	}
	run = waitForRun(t, app, album.ID)
	if run.State != "done" || run.Error != "" {
		t.Fatalf("run = %s %q", run.State, run.Error)
	}
	if run.After.CreditsRemaining != 60 || run.After.DownloadCreditsRemaining != 6 {
		t.Errorf("after = %+v, want what was spent to show", run.After)
	}
	if suno.confirmed != 4 {
		t.Errorf("confirmed %d downloads, want one per take", suno.confirmed)
	}

	// What was sent is what the extension types into Suno's form.
	first := suno.created[0]
	if first["title"] != "Static" || first["style"] != "lo-fi tape hiss, 80 BPM" ||
		first["lyrics"] != "[Verse]\nstatic on the line" || first["duration"] != 90 {
		t.Errorf("audio-create got %v", first)
	}
	// One call a track, keyed by the run and the track; Suno makes two songs
	// from it and no count is sent, since audio-create refuses one now.
	if len(suno.created) != 2 {
		t.Fatalf("audio-create called %d times, want once per track", len(suno.created))
	}
	for i, track := range tracks {
		if got := suno.created[i]["idempotency_key"]; got != run.ID+":"+track.ID {
			t.Errorf("call %d key = %v", i+1, got)
		}
		if _, ok := suno.created[i]["clip_count"]; ok {
			t.Errorf("call %d sent clip_count", i+1)
		}
	}

	for _, track := range tracks {
		got, err := app.GetTrack(track.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Files) != 2 {
			t.Fatalf("%s has %d takes, want 2", track.Name, len(got.Files))
		}
		f := got.Files[0]
		if f.SunoClip == "" || !strings.HasPrefix(f.Label, "Made through Suno MCP ") {
			t.Errorf("take = %+v, want it filed with its clip like a recorded one", f)
		}
		if !strings.Contains(filepath.ToSlash(f.Path), "/Cassette Motel/Season 3/") &&
			!strings.Contains(strings.ToLower(filepath.ToSlash(f.Path)), "cassette") {
			t.Errorf("take filed at %s, want the album's folder", f.Path)
		}
		if _, err := os.Stat(f.Path); err != nil {
			t.Errorf("the take is not on disk: %v", err)
		}
	}
	for _, tr := range run.Tracks {
		if tr.State != sunoTrackFiled || len(tr.Clips) != 2 || tr.Clips[0].FileID == "" {
			t.Errorf("track run = %+v", tr)
		}
	}

	// A second run over the same songs files nothing twice: the clip id is
	// how a take is known, whichever way it arrived.
	tr := run.Tracks[0]
	if _, err := app.fileSunoClip(context.Background(), suno, tracks[0], tr.Clips[0].ID); err != nil {
		t.Fatal(err)
	}
	got, _ := app.GetTrack(tracks[0].ID)
	if len(got.Files) != 2 {
		t.Errorf("filing a clip again made %d takes", len(got.Files))
	}
}

func TestSunoRunWithoutDownloadApprovalSpendsNoDownloadCredits(t *testing.T) {
	app := newTestApp(t)
	suno := newFakeSuno(t)
	withFakeSuno(t, suno)
	album, tracks := sunoAlbum(t, app, "Static")

	if _, err := app.StartSunoAlbumRun(album.ID, SunoRunOptions{}); err != nil {
		t.Fatal(err)
	}
	run := waitForRun(t, app, album.ID)
	if run.State != "done" || run.Tracks[0].State != sunoTrackInLibrary {
		t.Fatalf("run = %+v", run)
	}
	if n := suno.count("audio-download"); n != 0 {
		t.Errorf("audio-download called %d times without approval", n)
	}
	got, _ := app.GetTrack(tracks[0].ID)
	if len(got.Files) != 0 {
		t.Errorf("%d takes filed without a download", len(got.Files))
	}
}

func TestSunoRunStopsWhenOutOfCredits(t *testing.T) {
	app := newTestApp(t)
	suno := newFakeSuno(t)
	suno.outOfCredits = true
	withFakeSuno(t, suno)
	album, _ := sunoAlbum(t, app, "Static", "Dial Tone")

	if _, err := app.StartSunoAlbumRun(album.ID, SunoRunOptions{Download: true}); err != nil {
		t.Fatal(err)
	}
	run := waitForRun(t, app, album.ID)
	if run.State != "failed" || !strings.Contains(run.Error, "out of credits") {
		t.Errorf("run = %s %q", run.State, run.Error)
	}
	if run.Tracks[0].State != sunoTrackFailed || run.Tracks[1].State != sunoTrackSkipped {
		t.Errorf("tracks = %s, %s; want the first failed and the rest not sent",
			run.Tracks[0].State, run.Tracks[1].State)
	}
	if n := suno.count("audio-create"); n != 1 {
		t.Errorf("audio-create called %d times after running out", n)
	}
}

func TestSunoRunReportsAFailedGeneration(t *testing.T) {
	app := newTestApp(t)
	suno := newFakeSuno(t)
	suno.failClips = []string{"00000001-0000-4000-8000-000000000000", "00000002-0000-4000-8000-000000000000"}
	withFakeSuno(t, suno)
	album, _ := sunoAlbum(t, app, "Static", "Dial Tone")

	if _, err := app.StartSunoAlbumRun(album.ID, SunoRunOptions{Download: true}); err != nil {
		t.Fatal(err)
	}
	run := waitForRun(t, app, album.ID)
	if run.State != "done" || !strings.Contains(run.Error, "1 of 2") {
		t.Errorf("run = %s %q", run.State, run.Error)
	}
	if tr := run.Tracks[0]; tr.State != sunoTrackFailed || !strings.Contains(tr.Error, "content policy") {
		t.Errorf("failed track = %+v, want Suno's reason", tr)
	}
	if run.Tracks[1].State != sunoTrackFiled {
		t.Errorf("the other track = %s, want it filed regardless", run.Tracks[1].State)
	}
}

func TestSunoRunMarksAnExpiredSignIn(t *testing.T) {
	app := newTestApp(t)
	suno := newFakeSuno(t)
	withFakeSuno(t, suno)
	album, _ := sunoAlbum(t, app, "Static")
	// A signed-in record to be marked.
	if _, err := app.updateMCPServer(sunoServerID, func(r *mcpServerRecord) {
		r.Auth = &mcpclient.Authorization{Token: mcpclient.Token{Access: "at", Refresh: "rt"}}
	}); err != nil {
		t.Fatal(err)
	}
	suno.unauthorized = true

	if _, err := app.StartSunoAlbumRun(album.ID, SunoRunOptions{Download: true}); err != nil {
		t.Fatal(err)
	}
	run := waitForRun(t, app, album.ID)
	if run.State != "failed" || run.Error != errSunoSignIn.Error() {
		t.Errorf("run = %s %q", run.State, run.Error)
	}
	status, _ := app.GetMCPServer(sunoServerID)
	if status.State != mcpNeedsAuthorization {
		t.Errorf("Suno is %s, want needs_authorization so the button hides", status.State)
	}
}

func TestSunoRunSendsOnlyWhatAudioCreateDeclares(t *testing.T) {
	app := newTestApp(t)
	suno := newFakeSuno(t)
	// Suno has since dropped duration_strict and model, and refuses them.
	var schema map[string]any
	_ = json.Unmarshal([]byte(sunoCreateSchema), &schema)
	delete(schema["properties"].(map[string]any), "duration_strict")
	delete(schema["properties"].(map[string]any), "model")
	raw, _ := json.Marshal(schema)
	suno.schema = string(raw)
	withFakeSuno(t, sunoListingFake{suno})
	album, _ := sunoAlbum(t, app, "Static")

	if _, err := app.StartSunoAlbumRun(album.ID, SunoRunOptions{Model: "v6"}); err != nil {
		t.Fatal(err)
	}
	run := waitForRun(t, app, album.ID)
	if run.State != "done" || run.Tracks[0].State != sunoTrackInLibrary {
		t.Fatalf("run = %s %q, track %+v; want a dropped field to cost only that field",
			run.State, run.Error, run.Tracks[0])
	}
	if n := suno.count("audio-create"); n != 1 {
		t.Errorf("audio-create called %d times, want the undeclared fields dropped before the first", n)
	}
	sent := suno.created[0]
	if _, ok := sent["duration_strict"]; ok {
		t.Errorf("sent %v, want duration_strict left out", sent)
	}
	if _, ok := sent["model"]; ok {
		t.Errorf("sent %v, want model left out", sent)
	}
	if sent["duration"] != 90 || sent["idempotency_key"] == nil || sent["lyrics"] == nil {
		t.Errorf("sent %v, want everything still declared", sent)
	}
	// The list read for the run is kept, so Settings shows today's tools.
	status, _ := app.GetMCPServer(sunoServerID)
	if len(status.Tools) != 1 || strings.Contains(status.Tools[0].Schema, "duration_strict") {
		t.Errorf("stored tools = %+v, want the list the run read", status.Tools)
	}
}

func TestSunoRunGetsPastAStaleToolList(t *testing.T) {
	app := newTestApp(t)
	suno := newFakeSuno(t)
	// The list still declares a field the tool itself has dropped.
	suno.listed = sunoCreateSchema
	suno.schema = strings.Replace(sunoCreateSchema, `"duration_strict":{"type":"boolean"},`, "", 1)
	withFakeSuno(t, sunoListingFake{suno})
	album, _ := sunoAlbum(t, app, "Static")

	if _, err := app.StartSunoAlbumRun(album.ID, SunoRunOptions{}); err != nil {
		t.Fatal(err)
	}
	run := waitForRun(t, app, album.ID)
	if run.State != "done" || run.Tracks[0].State != sunoTrackInLibrary {
		t.Fatalf("run = %s %q, track %+v", run.State, run.Error, run.Tracks[0])
	}
	if n := suno.count("audio-create"); n != 2 {
		t.Errorf("audio-create called %d times, want once refused and once without the field", n)
	}
	if _, ok := suno.created[0]["duration_strict"]; ok {
		t.Errorf("sent %v", suno.created[0])
	}
}

func TestSunoCreateArgs(t *testing.T) {
	args := sunoCreateArgs(Track{Name: "Hum", Prompt: "drone", TargetSeconds: 600}, SunoRunOptions{Model: "v6"}, "k")
	if args["instrumental"] != true {
		t.Error("a track with no lyrics was not sent as an instrumental")
	}
	if args["duration"] != sunoMaxSeconds || args["duration_strict"] != false {
		t.Errorf("duration = %v strict %v, want clamped to Suno's limit and soft", args["duration"], args["duration_strict"])
	}
	if args["model"] != "v6" || args["idempotency_key"] != "k" {
		t.Errorf("args = %v", args)
	}
	// Suno refuses clip_count since 2026-10-30.
	if _, ok := args["clip_count"]; ok {
		t.Error("clip_count was sent")
	}
	declared := declaredInputs(json.RawMessage(sunoCreateSchema))
	for name := range args {
		if !declared[name] {
			t.Errorf("%s is not in audio-create's schema", name)
		}
	}
	long := sunoCreateArgs(Track{Name: "x", Lyrics: strings.Repeat("ä", 6000)}, SunoRunOptions{}, "k")
	if n := len([]rune(long["lyrics"].(string))); n != sunoLyricsLimit {
		t.Errorf("lyrics cut to %d runes, want %d", n, sunoLyricsLimit)
	}
	if _, ok := long["duration"]; ok {
		t.Error("a track with no target length sent one")
	}
}

func TestSunoRunRefusals(t *testing.T) {
	app := newTestApp(t)
	album, tracks := sunoAlbum(t, app, "Static")

	// Not connected: the button is hidden, and the binding says why anyway.
	if _, err := app.StartSunoAlbumRun(album.ID, SunoRunOptions{}); !errors.Is(err, errSunoNotConnected) {
		t.Errorf("err = %v, want not connected", err)
	}
	suno := newFakeSuno(t)
	withFakeSuno(t, suno)
	empty, _ := app.SaveTrack(Track{AlbumID: album.ID, Name: "Blank"})
	if _, err := app.StartSunoAlbumRun(album.ID, SunoRunOptions{TrackIDs: []string{empty.ID}}); err == nil ||
		!strings.Contains(err.Error(), "neither") {
		t.Errorf("err = %v, want the empty track named", err)
	}
	_ = tracks
}

func TestMCPServersPresetSunoAndKeepTokensSecret(t *testing.T) {
	app := newTestApp(t)
	servers := app.GetMCPServers()
	if len(servers) != 1 || servers[0].ID != sunoServerID || servers[0].URL != sunoServerURL ||
		servers[0].State != mcpNotConnected || !servers[0].Preset {
		t.Fatalf("servers = %+v, want Suno preset and not connected", servers)
	}
	if _, err := app.updateMCPServer(sunoServerID, func(r *mcpServerRecord) {
		r.Auth = &mcpclient.Authorization{Scopes: []string{"download"}, Token: mcpclient.Token{Access: "secret-at", Refresh: "secret-rt"}}
		r.Tools = []mcpclient.Tool{{Name: "audio-create", InputSchema: json.RawMessage(`{"type":"object"}`)}}
	}); err != nil {
		t.Fatal(err)
	}
	status, _ := app.GetMCPServer(sunoServerID)
	if status.State != mcpConnected || len(status.Tools) != 1 || status.Tools[0].Schema != `{"type":"object"}` {
		t.Errorf("status = %+v", status)
	}
	encoded, _ := json.Marshal(app.GetMCPServers())
	if strings.Contains(string(encoded), "secret-") {
		t.Error("the window was handed a token")
	}
	if _, ok := secretSettings[keyMCPServers]; !ok {
		t.Error("the raw setting tools would hand an agent the tokens")
	}

	added, err := app.AddMCPServer("", "https://example.com/mcp")
	if err != nil || added.Name != "example.com" || added.Preset {
		t.Fatalf("added = %+v, %v", added, err)
	}
	if _, err := app.AddMCPServer("x", "http://example.com/mcp"); err == nil {
		t.Error("a remote server over plain http was accepted")
	}
	if _, err := app.RemoveMCPServer(sunoServerID); err == nil {
		t.Error("the preset was removed")
	}
	left, err := app.RemoveMCPServer(added.ID)
	if err != nil || len(left) != 1 {
		t.Errorf("after removing: %+v, %v", left, err)
	}
	if status, _ := app.DisconnectMCPServer(sunoServerID); status.State != mcpNotConnected || len(status.Tools) != 0 {
		t.Errorf("after disconnect: %+v", status)
	}
}
