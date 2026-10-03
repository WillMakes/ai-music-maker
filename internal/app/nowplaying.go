package app

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Now playing (#111)
//
// What the player is doing, for anything outside the window that wants to
// know — a stream overlay, a status bar, an AI assistant. The player lives in
// the window (see PlayerProvider), so the window reports each change here and
// the app answers from that report.
//
// Reports come on change, not on a timer: a new track, play, pause, a seek,
// the end of the queue. A reader asking mid-song gets the reported position
// moved on by the time since, which is right to the second for as long as the
// song is playing and costs the window nothing while it is.
//
// The endpoint is open — no token — because the producer asked for it to be
// (#111): an overlay or a script should be able to read it with no setup. The
// rest of the bridge keeps its token. What is open is only this: the title,
// artist, album, timing and state of the song playing, and its cover. The
// cover is served from its own fixed URL rather than the token-bearing one the
// window uses, so the token never appears in an open answer.
// ---------------------------------------------------------------------------

const (
	nowPlayingPlaying = "playing"
	nowPlayingPaused  = "paused"
	nowPlayingStopped = "stopped"
)

// nowPlayingArtworkURL is the open cover URL: always the current album's.
var nowPlayingArtworkURL = fmt.Sprintf("http://127.0.0.1:%d/now-playing/artwork", extensionBridgePort)

// nowPlayingEndpoint is where another process — the MCP server — reads the
// running app's answer. A variable so a test points it at its own server.
var nowPlayingEndpoint = fmt.Sprintf("http://127.0.0.1:%d/now-playing", extensionBridgePort)

// nowPlayingClock is what "since the last report" is measured against. A
// variable so a test can move time on.
var nowPlayingClock = time.Now

// NowPlayingReport is what the window sends on every change.
type NowPlayingReport struct {
	State           string  `json:"state"`
	TrackID         string  `json:"trackId"`
	AlbumID         string  `json:"albumId"`
	Title           string  `json:"title"`
	ArtistName      string  `json:"artistName"`
	AlbumName       string  `json:"albumName"`
	PositionSeconds float64 `json:"positionSeconds"`
	DurationSeconds float64 `json:"durationSeconds"`
}

// NowPlaying is the answer: what is playing, where it is, and its cover.
type NowPlaying struct {
	// State is "playing", "paused" or "stopped". Stopped means nothing is
	// queued, and every other field is empty.
	State           string  `json:"state"`
	TrackID         string  `json:"trackId"`
	AlbumID         string  `json:"albumId"`
	Title           string  `json:"title"`
	ArtistName      string  `json:"artistName"`
	AlbumName       string  `json:"albumName"`
	PositionSeconds float64 `json:"positionSeconds"`
	DurationSeconds float64 `json:"durationSeconds"`
	// ArtworkURL is the open cover URL, "" when the album has no cover.
	ArtworkURL string `json:"artworkUrl"`
	// UpdatedAt is when the window last reported, RFC 3339; "" before it has.
	UpdatedAt string `json:"updatedAt"`
}

// nowPlayingState is the last report and when it came.
type nowPlayingState struct {
	mu     sync.Mutex
	report NowPlayingReport
	at     time.Time
	// artwork is the cover file of the reported album, looked up once per
	// report rather than on every read of an endpoint an overlay polls.
	artwork string
}

// ReportNowPlaying records the player's state. Called by the window on every
// change; an unknown state is recorded as stopped, so a reader is never handed
// a word the API does not document.
func (a *App) ReportNowPlaying(report NowPlayingReport) {
	switch report.State {
	case nowPlayingPlaying, nowPlayingPaused:
	default:
		report = NowPlayingReport{State: nowPlayingStopped}
	}
	artwork := ""
	if report.AlbumID != "" && a.store != nil {
		if album, err := a.store.album(report.AlbumID); err == nil {
			artwork = album.ArtworkFile
		}
	}
	a.nowPlaying.mu.Lock()
	defer a.nowPlaying.mu.Unlock()
	a.nowPlaying.report = report
	a.nowPlaying.at = nowPlayingClock()
	a.nowPlaying.artwork = artwork
}

// NowPlaying answers what this process's player is doing.
func (a *App) NowPlaying() NowPlaying {
	a.nowPlaying.mu.Lock()
	defer a.nowPlaying.mu.Unlock()
	r := a.nowPlaying.report
	if r.State == "" {
		r.State = nowPlayingStopped
	}
	out := NowPlaying{
		State:           r.State,
		TrackID:         r.TrackID,
		AlbumID:         r.AlbumID,
		Title:           r.Title,
		ArtistName:      r.ArtistName,
		AlbumName:       r.AlbumName,
		PositionSeconds: r.PositionSeconds,
		DurationSeconds: r.DurationSeconds,
	}
	if !a.nowPlaying.at.IsZero() {
		out.UpdatedAt = a.nowPlaying.at.UTC().Format(time.RFC3339)
	}
	if r.State == nowPlayingPlaying {
		out.PositionSeconds += nowPlayingClock().Sub(a.nowPlaying.at).Seconds()
		if r.DurationSeconds > 0 {
			out.PositionSeconds = math.Min(out.PositionSeconds, r.DurationSeconds)
		}
	}
	// Whole tenths: a reader polling for a progress bar has no use for the
	// nanoseconds the subtraction leaves behind.
	out.PositionSeconds = math.Round(out.PositionSeconds*10) / 10
	if r.State != nowPlayingStopped && a.nowPlaying.artwork != "" {
		out.ArtworkURL = nowPlayingArtworkURL
	}
	return out
}

// nowPlayingFromApp is the answer for a process with no player of its own —
// the MCP server — read from the running app's endpoint. With the app closed,
// nothing is playing, and that is the answer rather than an error.
func (a *App) nowPlayingFromApp() NowPlaying {
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(nowPlayingEndpoint)
	if err != nil {
		return NowPlaying{State: nowPlayingStopped}
	}
	defer resp.Body.Close()
	var out NowPlaying
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&out) != nil {
		return NowPlaying{State: nowPlayingStopped}
	}
	return out
}

// bridgeNowPlaying serves /now-playing and /now-playing/artwork, with no token
// (see the note at the top of this file).
func (a *App) bridgeNowPlaying(w http.ResponseWriter, r *http.Request) {
	bridgeCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// The answer changes as the song plays; nothing may hold on to it.
	w.Header().Set("Cache-Control", "no-store")

	if strings.TrimSuffix(r.URL.Path, "/") == "/now-playing/artwork" {
		a.nowPlaying.mu.Lock()
		file := ""
		if a.nowPlaying.report.State == nowPlayingPlaying || a.nowPlaying.report.State == nowPlayingPaused {
			file = a.nowPlaying.artwork
		}
		a.nowPlaying.mu.Unlock()
		dir, err := artworkDir()
		if file == "" || err != nil {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(dir, filepath.Base(file)))
		return
	}
	bridgeJSON(w, http.StatusOK, a.NowPlaying())
}
