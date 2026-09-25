package app

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// Takes recorded as they are heard on Suno (#91, #92)
//
// A session in Suno ends with the producer listening back to what it made, and
// every take worth keeping then had to be downloaded and imported by hand. The
// extension already sits on those pages, so it listens instead: with recording
// switched on from its toolbar button, it captures the Suno tab's own audio
// output, and a song heard from its first second to its last is sent here as a
// WAV and filed onto the track with the same title.
//
// Recorded, not downloaded (#92). The first version fetched Suno's MP3 for the
// clip, which the producer did not ask for: what they want kept is what they
// listened to, and only a song they sat through all of. The capture is taken as
// raw samples and sent as WAV, so the one lossy step between Suno's player and
// the library is the MP3 it is filed as (#93, see the audio package).
//
// This file owns the decision — which track, whether it was already filed,
// whether filing is switched off — and the extension owns the listening; see
// chromeextension/offscreen.js for the recording and content.js for what
// counts as heard in full.
// ---------------------------------------------------------------------------

// keySunoCapture turns the capture off when it holds "off". On unless turned
// off, because nothing is recorded until the producer presses the toolbar
// button, and nothing is filed that does not match a track by name.
const keySunoCapture = "suno_capture"

// sunoClipPattern is what a Suno clip id looks like: a lowercase UUID. It is
// what a recording is remembered by, so it is checked to the character.
var sunoClipPattern = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// SunoPlay is what the extension reports alongside a finished recording.
type SunoPlay struct {
	// ClipID is Suno's id for the song, read from the player's audio address.
	ClipID string `json:"clipId"`
	// Title is the song's title as Suno shows it, which is the name it is
	// matched to a track by.
	Title string `json:"title"`
	// TrackID is set when the extension itself filled Suno's form from this
	// track under this title — that is certain where a name match is a guess.
	TrackID string `json:"trackId"`
}

// SunoCapture is the answer: what happened to the recording, in words the
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
// finishing while the first is still being written — from both passing the
// already-filed check.
var sunoCaptureMu sync.Mutex

// captureSunoPlay files a song heard to the end onto its track.
//
// write puts the recording at the path it is given. It is called only once the
// recording is known to be wanted, so a song that has no track, or was filed
// already, costs a check rather than tens of megabytes on disk.
func (a *App) captureSunoPlay(play SunoPlay, write func(dest string) error) (SunoCapture, error) {
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
		return SunoCapture{}, fmt.Errorf("could not make somewhere to put the recording: %w", err)
	}
	defer os.RemoveAll(dir)
	name := slugify(track.Name)
	if name == "" {
		name = "take"
	}
	temp := filepath.Join(dir, name+".wav")
	if err := write(temp); err != nil {
		return SunoCapture{}, err
	}

	// The clip id's first block tells two takes of one track apart in the
	// list, and is the part a producer can find again in Suno's address bar.
	label := "Recorded on Suno " + play.ClipID[:8]
	if _, err := a.importTrackFile(track.ID, temp, label, play.ClipID); err != nil {
		return SunoCapture{}, err
	}
	out, err := a.describeCapture("filed", track.ID)
	out.Message = fmt.Sprintf("Recorded onto %s — %s.", out.TrackName, out.AlbumName)
	return out, err
}

// writeWAV is the bridge's writer: the request body, bounded, and refused
// unless it is a WAV. The extension only ever sends one, so anything else is
// a request this route was not built for, not a format to be guessed at.
func writeWAV(body io.Reader) func(dest string) error {
	return func(dest string) error {
		head := make([]byte, 12)
		if _, err := io.ReadFull(body, head); err != nil ||
			!bytes.Equal(head[:4], []byte("RIFF")) || !bytes.Equal(head[8:], []byte("WAVE")) {
			return fmt.Errorf("the recording is not a WAV file")
		}
		out, err := os.Create(dest)
		if err != nil {
			return err
		}
		// One byte past the cap is how an oversized recording is told apart
		// from one that is exactly the cap.
		written, copyErr := io.Copy(out, io.MultiReader(bytes.NewReader(head),
			io.LimitReader(body, maxTrackFileBytes+1-int64(len(head)))))
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if written > maxTrackFileBytes {
			return fmt.Errorf("the recording is larger than this app will file (%d MB)",
				maxTrackFileBytes>>20)
		}
		return nil
	}
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

// bridgeCapture receives a recording: the WAV as the body, and which song it
// is in the query — a multipart form would be an encoder in the extension and
// a parser here for three short strings.
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
	q := r.URL.Query()
	play := SunoPlay{ClipID: q.Get("clip"), Title: q.Get("title"), TrackID: q.Get("track")}
	out, err := a.captureSunoPlay(play, writeWAV(r.Body))
	if err != nil {
		bridgeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error()})
		return
	}
	bridgeJSON(w, http.StatusOK, out)
}
