package app

import (
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// Playback (#45)
//
// The player lives in the frontend — one <audio> element, the queue, the
// Media Session hookup that hands the OS its controls. What the backend owns
// is the two things the webview cannot do alone: streaming the files out of
// the music library, and turning "play this album / artist / everything" into
// an ordered queue of playable tracks.
//
// Audio rides the extension bridge the way artwork does: loopback only, the
// bridge token as a query parameter (an <audio src> cannot send a header),
// and files addressed by their track_files id rather than by path, so the
// URL can never name a file outside the library's own rows.
// ---------------------------------------------------------------------------

// PlayerTrack is one entry of the play queue, as the frontend plays it.
type PlayerTrack struct {
	TrackID    string `json:"trackId"`
	Name       string `json:"name"`
	ArtistID   string `json:"artistId"`
	ArtistName string `json:"artistName"`
	AlbumID    string `json:"albumId"`
	AlbumName  string `json:"albumName"`
	ArtworkURL string `json:"artworkUrl"`
	AudioURL   string `json:"audioUrl"`
	// FileID is the take that plays, which the window's player loads into
	// this process's speaker (#125). AudioURL is kept for the browser
	// extension and the MCP playlist, which play over the loopback bridge.
	FileID string `json:"fileId"`
}

// Playlist builds the queue for one press of a play button. kind is "track",
// "album", "artist", "playlist" (#107) or "all"; id names the subject (ignored
// for "all").
//
// Order is the order the music was made to play in: tracks by running order,
// an artist's albums oldest first, artists by name for "all". A track with no
// takes is skipped rather than erroring — a half-written record should still
// play the songs it has.
//
// So is a track whose takes' files are all gone (#105). But a queue with
// nothing in it because every file is missing is an error, naming the cause:
// "nothing to play" would send the producer looking for a bug in the player
// when the answer is in their music folder.
func (a *App) Playlist(kind, id string) ([]PlayerTrack, error) {
	missing := 0
	out, err := a.playlist(kind, id, &missing)
	if err == nil && len(out) == 0 && missing > 0 {
		return out, fmt.Errorf("nothing here can be played: the files of its takes have been moved or deleted from the music folder since they were filed")
	}
	return out, err
}

// playlist builds the queue, counting into missing the tracks skipped only
// because their files are gone.
func (a *App) playlist(kind, id string, missing *int) ([]PlayerTrack, error) {
	out := []PlayerTrack{}
	if a.store == nil {
		return out, nil
	}
	switch kind {
	case "track":
		// One track plays that track and nothing else (#106). #55 had it
		// queue the whole record from the pressed track, so that next and
		// previous had somewhere to go — but then there was no way to hear one
		// song on its own, which is what a track's own button is for. The
		// album's Play is how the whole record is heard.
		track, err := a.GetTrack(id)
		if err != nil {
			return nil, err
		}
		album, err := a.GetAlbum(track.AlbumID)
		if err != nil {
			return nil, err
		}
		return a.appendPlayable(out, album, []Track{track}, missing), nil
	case "album":
		album, err := a.GetAlbum(id)
		if err != nil {
			return nil, err
		}
		tracks, err := a.ListTracks(album.ID)
		if err != nil {
			return nil, err
		}
		return a.appendPlayable(out, album, tracks, missing), nil
	case "artist":
		return a.appendArtistPlayable(out, id, missing)
	case "playlist":
		return a.appendPlaylistPlayable(out, id, missing)
	case "all":
		artists, err := a.ListArtists()
		if err != nil {
			return nil, err
		}
		for _, artist := range artists {
			out, err = a.appendArtistPlayable(out, artist.ID, missing)
			if err != nil {
				return nil, err
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("nothing called %q can be played", kind)
}

// appendPlaylistPlayable queues a saved playlist in its own order (#107),
// each song carrying its own record's names and cover — a playlist crosses
// albums, and the player bar should say which one each song is from.
func (a *App) appendPlaylistPlayable(out []PlayerTrack, playlistID string, missing *int) ([]PlayerTrack, error) {
	if _, err := a.store.playlist(playlistID); err != nil {
		return nil, err
	}
	entries, err := a.store.playlistEntries(playlistID)
	if err != nil {
		return nil, err
	}
	albums := map[string]Album{}
	for _, entry := range entries {
		track, err := a.GetTrack(entry.TrackID)
		if err != nil {
			return nil, err
		}
		album, known := albums[track.AlbumID]
		if !known {
			if album, err = a.GetAlbum(track.AlbumID); err != nil {
				return nil, err
			}
			albums[track.AlbumID] = album
		}
		out = a.appendPlayable(out, album, []Track{track}, missing)
	}
	return out, nil
}

func (a *App) appendArtistPlayable(out []PlayerTrack, artistID string, missing *int) ([]PlayerTrack, error) {
	albums, err := a.ListAlbums(artistID)
	if err != nil {
		return nil, err
	}
	for _, album := range albums {
		tracks, err := a.ListTracks(album.ID)
		if err != nil {
			return nil, err
		}
		out = a.appendPlayable(out, album, tracks, missing)
	}
	return out, nil
}

// appendPlayable turns tracks into queue entries. The chosen take is the file
// that plays; a track whose takes are all unchosen falls back to the first,
// because "not decided yet" should not silence the song.
//
// Only takes whose file is still there count (#105). A chosen take that has
// gone falls back the same way an unchosen set does, so the song still plays
// from the take that exists; a track with no file left at all is skipped and
// counted into missing.
func (a *App) appendPlayable(out []PlayerTrack, album Album, tracks []Track, missing *int) []PlayerTrack {
	for _, track := range tracks {
		var file *TrackFile
		for i := range track.Files {
			if track.Files[i].Chosen && !track.Files[i].Missing {
				file = &track.Files[i]
				break
			}
		}
		for i := range track.Files {
			if file != nil {
				break
			}
			if !track.Files[i].Missing {
				file = &track.Files[i]
			}
		}
		if file == nil {
			if len(track.Files) > 0 {
				*missing++
			}
			continue
		}
		audio := a.audioURL(file.ID)
		if audio == "" {
			continue
		}
		out = append(out, PlayerTrack{
			TrackID:    track.ID,
			Name:       track.Name,
			ArtistID:   album.ArtistID,
			ArtistName: album.ArtistName,
			AlbumID:    album.ID,
			AlbumName:  album.Name,
			ArtworkURL: album.ArtworkURL,
			AudioURL:   audio,
			FileID:     file.ID,
		})
	}
	return out
}

// audioURL is where the window streams a take from — the bridge, addressed by
// file id, with the bridge token as a query parameter for artworkURL's reason.
func (a *App) audioURL(fileID string) string {
	if fileID == "" {
		return ""
	}
	token, err := a.extensionToken()
	if err != nil {
		return ""
	}
	return fmt.Sprintf("http://127.0.0.1:%d/audio/%s?t=%s",
		extensionBridgePort, url.PathEscape(fileID), url.QueryEscape(token))
}

// bridgeAudio streams one take. http.ServeFile handles Range requests, which
// is what makes seeking work — the audio element asks for byte ranges, and a
// server that ignores them plays from the top on every seek.
func (a *App) bridgeAudio(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !a.bridgeAuthed(w, r) {
		return
	}
	fileID := strings.TrimPrefix(r.URL.Path, "/audio/")
	if fileID == "" || strings.ContainsAny(fileID, "/\\") {
		http.NotFound(w, r)
		return
	}
	path, err := a.store.trackFilePath(fileID)
	if err != nil || path == "" {
		http.NotFound(w, r)
		return
	}
	// The file may be replaced by a re-import under the same id, so unlike the
	// artwork it is not immutable — but a day of caching costs nothing.
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, filepath.Clean(path))
}

// trackFilePath reads where one take lives, by its id alone — the audio route
// has no track in hand and needs none.
func (s *Store) trackFilePath(fileID string) (string, error) {
	if s == nil || s.db == nil {
		return "", errNoStore
	}
	var path string
	err := s.db.QueryRow(
		`SELECT path FROM track_files WHERE id = ?`, fileID).Scan(&path)
	return path, err
}
