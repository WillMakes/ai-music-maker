package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// The browser-extension bridge
//
// The tracks live in this app; Suno's create form lives in Chrome. The extension
// puts a button on Suno's Lyrics, Style and Title fields, and pressing one picks
// a track from this app and fills the field with it — so the round trip that was
// "switch window, find the track, copy, switch back, paste" is one click.
//
// A browser page cannot reach Wails IPC, so this is the one place the app answers
// over HTTP: a tiny server bound to 127.0.0.1 alone, so nothing off the machine
// can see it, and gated by a token the app generates and writes into the exported
// extension. Loopback stops the network; the token stops other local pages. The
// token travels in the Authorization header, never a cookie, so there is no
// ambient credential to forge — a page without the token cannot act, whatever
// CORS says.
//
// Five routes: health, so the extension can tell whether the app is up; library,
// the artists/albums/tracks it picks from; track, one track's fields; open, which
// raises the window; and artwork, which is not for the extension at all — it is
// how the app's own webview loads an album cover out of ~/.sunomaker (see
// artwork.go).
// ---------------------------------------------------------------------------

// extensionBridgePort is where the bridge listens. Fixed rather than chosen at
// random, because the exported extension has to know it ahead of time and an
// extension is exported once — a port that moved between launches would leave it
// pointing at nothing. 127.0.0.1 only, so the fixed port is a port on this
// machine's loopback and not an opening to the network.
const extensionBridgePort = 8799

// keyExtensionToken is the bridge token, generated once and kept so the same
// exported extension keeps working across launches.
const keyExtensionToken = "extension_bridge_token"

//go:embed chromeextension
var extensionFiles embed.FS

// extensionToken returns the bridge token, minting and storing one the first
// time. Its own token rather than a provider credential: it authorises a browser
// extension to read this app's library, which is not something any other
// credential should also unlock.
func (a *App) extensionToken() (string, error) {
	if a.store == nil {
		return "", errNoStore
	}
	if existing, err := a.store.setting(keyExtensionToken); err == nil && existing != "" {
		return existing, nil
	}
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("could not generate a token: %w", err)
	}
	token := "smext_" + hex.EncodeToString(b[:])
	if err := a.store.setSetting(keyExtensionToken, token); err != nil {
		return "", err
	}
	return token, nil
}

// superviseExtensionBridge runs the loopback server until the app's context is
// cancelled.
//
// A port already taken is not fatal. Nothing on screen depends on the bridge
// except album covers, and the one useful thing to do about a taken port is
// carry on without it — a window that refused to open because another program
// held 8799 would be a far worse failure than one with no thumbnails.
func (a *App) superviseExtensionBridge(ctx context.Context) {
	if a.store == nil {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", a.bridgeHealth)
	mux.HandleFunc("/library", a.bridgeLibrary)
	mux.HandleFunc("/track/", a.bridgeTrack)
	mux.HandleFunc("/open", a.bridgeOpen)
	mux.HandleFunc("/artwork/", a.bridgeArtwork)

	srv := &http.Server{
		Addr:              "127.0.0.1:" + strconv.Itoa(extensionBridgePort),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return
	}
}

// bridgeCORS lets the extension read the answer.
//
// Not the security boundary — the token is — so it is permissive on purpose: an
// extension's origin is chrome-extension://<id>, unknown until it is installed,
// and reflecting * is safe when the credential is a header the caller has to
// already hold.
func bridgeCORS(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Headers", "authorization, content-type")
	h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
}

// bridgeAuthed checks the token. The one gate on everything that reads.
//
// The query parameter is accepted as well as the header for the artwork route's
// sake: an <img src> cannot send a header (see artworkURL). Both are compared
// the same way, and the header is preferred where both are present.
func (a *App) bridgeAuthed(w http.ResponseWriter, r *http.Request) bool {
	token, err := a.extensionToken()
	got := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if got == "" {
		got = strings.TrimSpace(r.URL.Query().Get("t"))
	}
	if err != nil || token == "" || got != token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

func bridgeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// bridgeHealth says the app is up. Unauthenticated on purpose: it answers a
// question the extension must be able to ask before it has done anything, and
// the answer is a fact the producer's own browser could work out by trying to
// connect to the port.
func (a *App) bridgeHealth(w http.ResponseWriter, r *http.Request) {
	bridgeCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	bridgeJSON(w, http.StatusOK, map[string]any{"ok": true, "app": "SunoMaker"})
}

// BridgeArtist is one artist in the extension's picker, with its albums and
// their tracks nested.
//
// The whole library in one answer rather than a call per level. It is a few
// hundred rows at most, the picker draws all three levels at once, and three
// round trips through a service worker to fill one dropdown is latency the
// producer feels on every insert.
type BridgeArtist struct {
	ID     string        `json:"id"`
	Name   string        `json:"name"`
	Albums []BridgeAlbum `json:"albums"`
}

type BridgeAlbum struct {
	ID     string        `json:"id"`
	Name   string        `json:"name"`
	Tracks []BridgeTrack `json:"tracks"`
}

// BridgeTrack is what the extension can put into Suno's fields: the title, the
// style prompt and the lyrics, which are exactly the three inputs it decorates.
type BridgeTrack struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Position int    `json:"position"`
	Prompt   string `json:"prompt"`
	Lyrics   string `json:"lyrics"`
	// TargetSeconds rides along so the picker can show how long the track is
	// meant to be — the one field that says whether this is the right take to be
	// generating right now.
	TargetSeconds int `json:"targetSeconds"`
}

func (a *App) bridgeLibrary(w http.ResponseWriter, r *http.Request) {
	bridgeCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !a.bridgeAuthed(w, r) {
		return
	}
	library, err := a.BridgeLibrary()
	if err != nil {
		bridgeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	bridgeJSON(w, http.StatusOK, library)
}

// BridgeLibrary is the picker's whole answer. Exported as a binding too, so the
// Settings panel can show the producer exactly what the extension will see.
func (a *App) BridgeLibrary() ([]BridgeArtist, error) {
	out := []BridgeArtist{}
	if a.store == nil {
		return out, nil
	}
	artists, err := a.store.listArtists()
	if err != nil {
		return out, err
	}
	for _, artist := range artists {
		entry := BridgeArtist{ID: artist.ID, Name: artist.Name, Albums: []BridgeAlbum{}}
		albums, err := a.store.listAlbums(artist.ID)
		if err != nil {
			return out, err
		}
		for _, album := range albums {
			albumEntry := BridgeAlbum{ID: album.ID, Name: album.Name, Tracks: []BridgeTrack{}}
			tracks, err := a.store.listTracks(album.ID)
			if err != nil {
				return out, err
			}
			for _, t := range tracks {
				albumEntry.Tracks = append(albumEntry.Tracks, BridgeTrack{
					ID: t.ID, Name: t.Name, Position: t.Position,
					Prompt: t.Prompt, Lyrics: t.Lyrics, TargetSeconds: t.TargetSeconds,
				})
			}
			entry.Albums = append(entry.Albums, albumEntry)
		}
		out = append(out, entry)
	}
	return out, nil
}

// bridgeTrack answers one track, for a picker that has been open long enough
// that the library it was drawn from may have moved.
func (a *App) bridgeTrack(w http.ResponseWriter, r *http.Request) {
	bridgeCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !a.bridgeAuthed(w, r) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/track/")
	if id == "" {
		http.Error(w, "no track", http.StatusBadRequest)
		return
	}
	track, err := a.store.track(id)
	if err != nil {
		bridgeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	bridgeJSON(w, http.StatusOK, BridgeTrack{
		ID: track.ID, Name: track.Name, Position: track.Position,
		Prompt: track.Prompt, Lyrics: track.Lyrics, TargetSeconds: track.TargetSeconds,
	})
}

// bridgeOpen raises the window, so a producer who found something in Suno can
// get back to the record it belongs to without hunting for it in the taskbar.
func (a *App) bridgeOpen(w http.ResponseWriter, r *http.Request) {
	bridgeCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !a.bridgeAuthed(w, r) {
		return
	}
	a.ShowWindow()
	bridgeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// bridgeArtwork serves an album cover to the app's own webview.
//
// The one route here that is not for the extension. filepath.Base is the whole
// of the path handling on purpose: the name comes from a database column this
// app wrote, but a column is still input, and a name that reached the join with
// "../" in it would read a file outside the artwork folder.
func (a *App) bridgeArtwork(w http.ResponseWriter, r *http.Request) {
	bridgeCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !a.bridgeAuthed(w, r) {
		return
	}
	name := filepath.Base(strings.TrimPrefix(r.URL.Path, "/artwork/"))
	if name == "" || name == "." || name == string(filepath.Separator) {
		http.NotFound(w, r)
		return
	}
	dir, err := artworkDir()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// Immutable: the file name carries a timestamp and never names two different
	// images (see writeArtwork), so the webview may keep it for as long as it
	// likes and a replaced cover arrives under a new name.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFile(w, r, filepath.Join(dir, name))
}

// ---------------------------------------------------------------------------
// Exporting the extension
// ---------------------------------------------------------------------------

// ExtensionStatus is the Settings panel's whole view of the extension.
type ExtensionStatus struct {
	// Dir is where the unpacked extension was written, "" if it has not been.
	Dir string `json:"dir"`
	// Exported says the folder is there, so the panel offers "show me" rather
	// than "install" a second time.
	Exported bool `json:"exported"`
	// Port and MatchURL are what the panel tells the producer it will do, so
	// installing it is not an act of faith.
	Port     int    `json:"port"`
	MatchURL string `json:"matchUrl"`
}

// extensionMatch is where the content script runs. Suno's create page is under
// suno.com; the app has no business on any other site, and a broader match would
// be an extension that reads pages it has nothing to say about.
const extensionMatch = "https://suno.com/*"

// GetExtensionStatus reports whether the extension has been exported.
func (a *App) GetExtensionStatus() ExtensionStatus {
	status := ExtensionStatus{Port: extensionBridgePort, MatchURL: extensionMatch}
	dir, err := extensionExportDir()
	if err != nil {
		return status
	}
	status.Dir = dir
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err == nil {
		status.Exported = true
	}
	return status
}

// extensionExportDir is where the unpacked extension is written. Under the app's
// own directory rather than somewhere the producer chooses: Chrome loads an
// unpacked extension from a path it remembers, and a folder that can be moved is
// an extension that stops working when it is.
func extensionExportDir() (string, error) {
	base, err := dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "chrome-extension"), nil
}

// ExportExtension writes the extension to disk with this app's bridge token in
// it, and returns where it went.
//
// Re-exportable at any time, and re-exporting is how a producer whose token was
// regenerated fixes an extension that has stopped being able to read the
// library. The whole folder is rewritten rather than patched: it is four small
// files, and a partial update is the state that would be hard to diagnose.
func (a *App) ExportExtension() (ExtensionStatus, error) {
	if a.store == nil {
		return ExtensionStatus{}, errNoStore
	}
	token, err := a.extensionToken()
	if err != nil {
		return ExtensionStatus{}, err
	}
	dir, err := extensionExportDir()
	if err != nil {
		return ExtensionStatus{}, err
	}
	// 0755 rather than 0700: Chrome reads this folder as the producer, which is
	// the same user — but an extension folder is meant to be openable from a file
	// manager, and 0700 on a directory somebody is about to be told to browse to
	// is an obstacle for no gain.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ExtensionStatus{}, err
	}

	entries, err := fs.ReadDir(extensionFiles, "chromeextension")
	if err != nil {
		return ExtensionStatus{}, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := extensionFiles.ReadFile("chromeextension/" + entry.Name())
		if err != nil {
			return ExtensionStatus{}, err
		}
		// config.js is committed as a template with placeholders in it, so the
		// repository never holds a real token and the export is the only place
		// one is written.
		if entry.Name() == "config.js" {
			replaced := strings.ReplaceAll(string(data), "__SM_BRIDGE_TOKEN__", token)
			replaced = strings.ReplaceAll(replaced, "__SM_BRIDGE_PORT__",
				strconv.Itoa(extensionBridgePort))
			data = []byte(replaced)
		}
		if err := os.WriteFile(filepath.Join(dir, entry.Name()), data, 0o644); err != nil {
			return ExtensionStatus{}, err
		}
	}
	return a.GetExtensionStatus(), nil
}

// RevealExtension opens the exported folder, so "Load unpacked" has somewhere to
// be pointed at.
func (a *App) RevealExtension() error {
	status := a.GetExtensionStatus()
	if !status.Exported {
		return fmt.Errorf("the extension has not been exported yet")
	}
	// A file inside it rather than the folder: revealInFileManager takes a file
	// and shows its container, which on Windows is the difference between
	// selecting the folder in its parent and opening it.
	return revealInFileManager(filepath.Join(status.Dir, "manifest.json"))
}
