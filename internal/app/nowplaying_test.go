package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// stubNowPlayingClock fixes the time the snapshot is read at, so a playing
// position can be checked to the second.
func stubNowPlayingClock(t *testing.T, at time.Time) *time.Time {
	t.Helper()
	now := at
	original := nowPlayingClock
	nowPlayingClock = func() time.Time { return now }
	t.Cleanup(func() { nowPlayingClock = original })
	return &now
}

// TestNowPlayingFollowsThePlayer is #111: the player lives in the window, and
// an API answering "what is playing" has to be told. The window reports each
// change — track, play, pause, seek, stop — and the app answers with the
// position moved on by the time since, so the window need not report every
// second for the position to be right.
func TestNowPlayingFollowsThePlayer(t *testing.T) {
	a := newTestApp(t)
	clock := stubNowPlayingClock(t, time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC))

	if got := a.NowPlaying(); got.State != nowPlayingStopped || got.Title != "" {
		t.Fatalf("before anything plays: %+v", got)
	}

	artist, err := a.SaveArtist(Artist{Name: "Primlocked", Inspirations: []Inspiration{}})
	if err != nil {
		t.Fatal(err)
	}
	album, err := a.SaveAlbum(Album{ArtistID: artist.ID, Name: "The Long Quiet"})
	if err != nil {
		t.Fatal(err)
	}
	report := NowPlayingReport{
		State:           nowPlayingPlaying,
		TrackID:         "trk_1",
		AlbumID:         album.ID,
		Title:           "Threshold",
		ArtistName:      "Primlocked",
		AlbumName:       "The Long Quiet",
		PositionSeconds: 30,
		DurationSeconds: 200,
	}
	a.ReportNowPlaying(report)

	*clock = clock.Add(12 * time.Second)
	got := a.NowPlaying()
	if got.State != nowPlayingPlaying || got.Title != "Threshold" || got.ArtistName != "Primlocked" ||
		got.AlbumName != "The Long Quiet" || got.DurationSeconds != 200 {
		t.Fatalf("snapshot: %+v", got)
	}
	if got.PositionSeconds != 42 {
		t.Fatalf("position = %v, want 30 + 12 seconds of playing", got.PositionSeconds)
	}
	// No cover on the album, so no cover URL.
	if got.ArtworkURL != "" {
		t.Fatalf("artwork URL with no cover: %q", got.ArtworkURL)
	}

	// Never past the end, however long since the last report.
	*clock = clock.Add(time.Hour)
	if got := a.NowPlaying(); got.PositionSeconds != 200 {
		t.Fatalf("position ran past the end: %v", got.PositionSeconds)
	}

	// Paused holds still.
	report.State = nowPlayingPaused
	report.PositionSeconds = 50
	a.ReportNowPlaying(report)
	*clock = clock.Add(time.Minute)
	if got := a.NowPlaying(); got.State != nowPlayingPaused || got.PositionSeconds != 50 {
		t.Fatalf("paused: %+v", got)
	}

	// Stopped clears the track.
	a.ReportNowPlaying(NowPlayingReport{State: nowPlayingStopped})
	if got := a.NowPlaying(); got.State != nowPlayingStopped || got.Title != "" || got.PositionSeconds != 0 {
		t.Fatalf("stopped: %+v", got)
	}

	// An unknown state is read as stopped rather than repeated to callers.
	a.ReportNowPlaying(NowPlayingReport{State: "buffering", Title: "x"})
	if got := a.NowPlaying(); got.State != nowPlayingStopped {
		t.Fatalf("unknown state passed through: %+v", got)
	}
}

// TestNowPlayingEndpointIsOpen: the producer chose an endpoint any program on
// the machine can read without a token (#111), cover included. The JSON and
// the cover are served with no credential at all.
func TestNowPlayingEndpointIsOpen(t *testing.T) {
	a := newTestApp(t)
	stubNowPlayingClock(t, time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC))

	artist, err := a.SaveArtist(Artist{Name: "Primlocked", Inspirations: []Inspiration{}})
	if err != nil {
		t.Fatal(err)
	}
	album, err := a.SaveAlbum(Album{ArtistID: artist.ID, Name: "The Long Quiet"})
	if err != nil {
		t.Fatal(err)
	}
	// A cover on disk, recorded on the album.
	dir, err := artworkDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cover-1.png"), []byte("png bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.db.Exec(`UPDATE albums SET artwork_file = 'cover-1.png' WHERE id = ?`, album.ID); err != nil {
		t.Fatal(err)
	}
	a.ReportNowPlaying(NowPlayingReport{
		State: nowPlayingPaused, TrackID: "trk_1", AlbumID: album.ID,
		Title: "Threshold", ArtistName: "Primlocked", AlbumName: "The Long Quiet",
		PositionSeconds: 5, DurationSeconds: 100,
	})

	rec := httptest.NewRecorder()
	a.bridgeNowPlaying(rec, httptest.NewRequest(http.MethodGet, "/now-playing", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d without a token", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("no CORS header, so a browser overlay could not read it")
	}
	var got NowPlaying
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Title != "Threshold" || got.State != nowPlayingPaused {
		t.Fatalf("body: %+v", got)
	}
	if got.ArtworkURL != nowPlayingArtworkURL {
		t.Fatalf("artwork URL = %q, want the open one", got.ArtworkURL)
	}

	rec = httptest.NewRecorder()
	a.bridgeNowPlaying(rec, httptest.NewRequest(http.MethodGet, "/now-playing/artwork", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "png bytes" {
		t.Fatalf("cover: %d %q", rec.Code, rec.Body.String())
	}

	// With nothing playing there is no cover to serve.
	a.ReportNowPlaying(NowPlayingReport{State: nowPlayingStopped})
	rec = httptest.NewRecorder()
	a.bridgeNowPlaying(rec, httptest.NewRequest(http.MethodGet, "/now-playing/artwork", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cover with nothing playing: %d", rec.Code)
	}
}

// TestNowPlayingToolReadsTheRunningApp: the MCP server is a process of its
// own, with no player, so its now_playing answer comes from the window's
// endpoint — and with no app to ask, the answer is that nothing is playing.
func TestNowPlayingToolReadsTheRunningApp(t *testing.T) {
	window := newTestApp(t)
	stubNowPlayingClock(t, time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC))
	window.ReportNowPlaying(NowPlayingReport{State: nowPlayingPaused, Title: "Threshold", PositionSeconds: 7})
	server := httptest.NewServer(http.HandlerFunc(window.bridgeNowPlaying))
	t.Cleanup(server.Close)

	original := nowPlayingEndpoint
	t.Cleanup(func() { nowPlayingEndpoint = original })

	nowPlayingEndpoint = server.URL + "/now-playing"
	mcp := newTestApp(t)
	if got := mcp.nowPlayingFromApp(); got.Title != "Threshold" || got.PositionSeconds != 7 {
		t.Fatalf("from the running app: %+v", got)
	}

	server.Close()
	if got := mcp.nowPlayingFromApp(); got.State != nowPlayingStopped {
		t.Fatalf("with the app closed: %+v", got)
	}
}
