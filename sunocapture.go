package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Takes filed as they are heard on Suno (#91)
//
// A session in Suno ends with the producer listening back to what it made, and
// every take worth keeping then had to be downloaded and imported by hand. The
// extension already sits on those pages, so it watches Suno's player instead:
// when a song has been heard to the end, it tells the app which Suno clip that
// was, and the app files it onto the track of the same name without being
// asked.
//
// "Record" in the report is done by downloading, not by capturing the tab's
// sound. Suno serves every finished clip as an MP3 on its CDN; that file is
// the take, bit for bit, where a recording of the tab would be a lossy
// re-encode of it that also caught any other sound the browser made, and would
// need Chrome's tab-capture permission and a prompt on every page load.
//
// The app fetches the file itself rather than the extension relaying bytes:
// the browser would be moving several megabytes as base64 through a service
// worker for no gain, and only the app needs to hold it.
// ---------------------------------------------------------------------------

// keySunoCapture turns the capture off when it holds "off". On unless turned
// off, because it is only ever reached by an extension the producer installed
// and it files nothing that does not match a track by name.
const keySunoCapture = "suno_capture"

// sunoClipPattern is what a Suno clip id looks like: a lowercase UUID. The id
// is the one part of the CDN address that comes from the page, so it is
// checked to the character before it goes into one.
var sunoClipPattern = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// SunoPlay is what the extension reports when a song finished playing.
type SunoPlay struct {
	// ClipID is Suno's id for the song, read from the player's audio address.
	ClipID string `json:"clipId"`
	// Title is the song's title as Suno shows it, which is the name it is
	// matched to a track by.
	Title string `json:"title"`
	// AudioURL is the address the page was playing. Tried after the CDN's
	// canonical address, never instead of it, and only on Suno's own hosts.
	AudioURL string `json:"audioUrl"`
	// TrackID is set when the extension itself filled Suno's form from this
	// track under this title — that is certain where a name match is a guess.
	TrackID string `json:"trackId"`
}

// SunoCapture is the answer: what happened to the play, in words the
// extension can show as they are.
type SunoCapture struct {
	// Outcome is "filed", "already" (this clip was filed before), "unmatched"
	// (no track answers to the title) or "off".
	Outcome   string `json:"outcome"`
	Message   string `json:"message"`
	TrackID   string `json:"trackId"`
	TrackName string `json:"trackName"`
	AlbumName string `json:"albumName"`
}

// sunoCaptureMu keeps two reports of one clip — two Suno tabs, or a replay
// racing the first download — from both passing the already-filed check.
var sunoCaptureMu sync.Mutex

// fetchSunoAudio downloads one clip to dest. A variable so tests can file a
// take without reaching Suno.
var fetchSunoAudio = func(address, dest string) error {
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Get(address)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Suno answered %s", resp.Status)
	}
	// A clip that is still being generated answers with a page or an empty
	// stream rather than audio; filing that would be a take that never plays.
	kind, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if kind != "" && !strings.HasPrefix(kind, "audio/") && kind != "application/octet-stream" {
		return fmt.Errorf("Suno answered with %s, not audio", kind)
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	// One byte past the cap is how an oversized download is told apart from
	// one that is exactly the cap.
	written, copyErr := io.Copy(out, io.LimitReader(resp.Body, maxTrackFileBytes+1))
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written == 0 {
		return fmt.Errorf("Suno sent an empty file")
	}
	if written > maxTrackFileBytes {
		return fmt.Errorf("the file is larger than this app will file (%d MB)", maxTrackFileBytes>>20)
	}
	return nil
}

// sunoAudioAddresses are where a clip's file is looked for, in order: the
// CDN's own address for the finished MP3 first, then whatever the page was
// playing, if that is on Suno. The page's address is sometimes a stream of a
// song still being made, which is why it is second.
func sunoAudioAddresses(play SunoPlay) []string {
	out := []string{"https://cdn1.suno.ai/" + play.ClipID + ".mp3"}
	if u, err := url.Parse(strings.TrimSpace(play.AudioURL)); err == nil &&
		u.Scheme == "https" && isSunoHost(u.Hostname()) && u.String() != out[0] {
		out = append(out, u.String())
	}
	return out
}

// isSunoHost limits where the app will download from. The bridge is gated by
// a token, but an app that fetched any address it was handed would be a
// fetcher for whoever held one.
func isSunoHost(host string) bool {
	host = strings.ToLower(host)
	for _, domain := range []string{"suno.ai", "suno.com"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

// captureSunoPlay files a song heard to the end onto its track.
func (a *App) captureSunoPlay(play SunoPlay) (SunoCapture, error) {
	if a.store == nil {
		return SunoCapture{}, errNoStore
	}
	if setting, _ := a.store.setting(keySunoCapture); setting == "off" {
		return SunoCapture{Outcome: "off",
			Message: "Filing takes as they play is turned off in Settings."}, nil
	}
	play.ClipID = strings.ToLower(strings.TrimSpace(play.ClipID))
	play.Title = strings.TrimSpace(play.Title)
	if !sunoClipPattern.MatchString(play.ClipID) {
		return SunoCapture{}, fmt.Errorf("%q is not a Suno clip id", play.ClipID)
	}

	sunoCaptureMu.Lock()
	defer sunoCaptureMu.Unlock()

	if trackID, err := a.store.trackWithSunoClip(play.ClipID); err != nil {
		return SunoCapture{}, err
	} else if trackID != "" {
		out, err := a.describeCapture("already", trackID)
		out.Message = fmt.Sprintf("%s already has this take.", out.TrackName)
		return out, err
	}

	track, found, err := a.trackForSunoPlay(play)
	if err != nil {
		return SunoCapture{}, err
	}
	if !found {
		// Not an error: most songs heard on Suno — the feed, other people's
		// work, an idea with no track yet — have nowhere to go, and that is
		// not something to complain about.
		return SunoCapture{Outcome: "unmatched",
			Message: fmt.Sprintf("No track is called %q, so it was not filed.", play.Title)}, nil
	}

	dir, err := os.MkdirTemp("", "sunomaker-suno-")
	if err != nil {
		return SunoCapture{}, fmt.Errorf("could not make somewhere to put the download: %w", err)
	}
	defer os.RemoveAll(dir)
	name := slugify(track.Name)
	if name == "" {
		name = "take"
	}
	temp := filepath.Join(dir, name+".mp3")
	var fetchErr error
	for _, address := range sunoAudioAddresses(play) {
		if fetchErr = fetchSunoAudio(address, temp); fetchErr == nil {
			break
		}
	}
	if fetchErr != nil {
		return SunoCapture{}, fmt.Errorf("could not download %q from Suno: %w", play.Title, fetchErr)
	}

	// The clip id's first block tells two takes of one track apart in the
	// list, and is the part a producer can find again in Suno's address bar.
	label := "Suno " + play.ClipID[:8]
	if _, err := a.importTrackFile(track.ID, temp, label, play.ClipID); err != nil {
		return SunoCapture{}, err
	}
	out, err := a.describeCapture("filed", track.ID)
	out.Message = fmt.Sprintf("Filed onto %s — %s.", out.TrackName, out.AlbumName)
	return out, err
}

// trackForSunoPlay finds the track a finished song belongs to.
//
// The track the extension filled Suno's form from wins, when it still exists.
// Otherwise the title has to match exactly one track's name — the whole
// library, since Suno does not know which album a song is for. Several tracks
// of the same name are settled by the newest album, the one being made now
// (#79); a tie on that album, or containment rather than equality, is not
// guessed at — a take filed onto the wrong song is worse than one not filed.
func (a *App) trackForSunoPlay(play SunoPlay) (Track, bool, error) {
	if id := strings.TrimSpace(play.TrackID); id != "" {
		if track, err := a.store.track(id); err == nil {
			return track, true, nil
		}
	}
	want := normalizeTitle(play.Title)
	if want == "" {
		return Track{}, false, nil
	}
	artists, err := a.store.listArtists()
	if err != nil {
		return Track{}, false, err
	}
	var matches []Track
	newest := ""
	for _, artist := range artists {
		albums, err := a.store.listAlbums(artist.ID)
		if err != nil {
			return Track{}, false, err
		}
		for _, album := range albums {
			tracks, err := a.store.listTracks(album.ID)
			if err != nil {
				return Track{}, false, err
			}
			for _, t := range tracks {
				if normalizeTitle(t.Name) != want {
					continue
				}
				if album.CreatedAt > newest {
					newest, matches = album.CreatedAt, nil
				}
				if album.CreatedAt == newest {
					matches = append(matches, t)
				}
			}
		}
	}
	if len(matches) != 1 {
		return Track{}, false, nil
	}
	return matches[0], true, nil
}

// describeCapture fills in the names the extension shows.
func (a *App) describeCapture(outcome, trackID string) (SunoCapture, error) {
	out := SunoCapture{Outcome: outcome, TrackID: trackID}
	track, err := a.store.track(trackID)
	if err != nil {
		return out, err
	}
	out.TrackName = track.Name
	if album, err := a.store.album(track.AlbumID); err == nil {
		out.AlbumName = album.Name
	}
	return out, nil
}

// trackWithSunoClip answers the track a clip was already filed onto, or "".
func (s *Store) trackWithSunoClip(clip string) (string, error) {
	if s == nil || s.db == nil {
		return "", errNoStore
	}
	var trackID string
	err := s.db.QueryRow(
		`SELECT track_id FROM track_files WHERE suno_clip = ? LIMIT 1`, clip).Scan(&trackID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return trackID, err
}

// bridgeCapture is the extension's report that a song finished playing.
//
// Answered with the outcome even when nothing was filed, so the extension
// can say what happened; an HTTP error is kept for a request that could not
// be acted on at all.
func (a *App) bridgeCapture(w http.ResponseWriter, r *http.Request) {
	bridgeCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !a.bridgeAuthed(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var play SunoPlay
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&play); err != nil {
		bridgeJSON(w, http.StatusBadRequest, map[string]any{"error": "could not read the report"})
		return
	}
	out, err := a.captureSunoPlay(play)
	if err != nil {
		bridgeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error()})
		return
	}
	bridgeJSON(w, http.StatusOK, out)
}
