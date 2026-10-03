package app

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"aimusicmaker/internal/audio"
	"aimusicmaker/internal/platform"
)

// ---------------------------------------------------------------------------
// The files Suno gives back
//
// A generation produces two takes; a producer runs several generations before
// one of them is the track. So a track has a list of files rather than a file,
// and exactly one of them is marked as the one that made the record.
//
// Importing copies rather than references. The alternative — storing the path
// into Downloads and leaving the bytes there — was rejected because Downloads is
// the one folder on any machine that gets emptied without ceremony, and a
// library whose rows point into it is a library that quietly stops playing. The
// copy lands in the OS music folder under Artist/Album (see musicdir.go), which
// is where the producer would have put it by hand.
// ---------------------------------------------------------------------------

// audioExtensions are what this app will file. Suno hands back MP3 and WAV; the
// others are here because a producer who has been through a mastering step is
// still importing the same track.
var audioExtensions = map[string]bool{
	".mp3": true, ".wav": true, ".flac": true, ".m4a": true, ".aac": true, ".ogg": true,
}

// maxTrackFileBytes bounds one import. A Suno track is a few megabytes and a
// lossless master is tens; a gigabyte is not a song, it is a mistake — a folder
// dragged in, or the wrong file picked — and copying it silently would fill a
// disk answering a misclick.
const maxTrackFileBytes = 512 << 20

// ImportDroppedTrackFile stores a take that arrived as a browser drop (#57).
//
// A file dropped onto the window reaches the page as bytes and a name, not a
// path — the webview does not reveal where it lives — so the bytes land in a
// temporary file under the dropped file's own name and then take the same
// road every picked file takes, ImportTrackFile: same validation, same
// numbered copy into the album folder, same label-from-the-name default.
func (a *App) ImportDroppedTrackFile(trackID, name, dataURL, label string) (Track, error) {
	base := filepath.Base(strings.TrimSpace(name))
	if base == "" || base == "." || base == ".." || base != strings.TrimSpace(name) {
		return Track{}, fmt.Errorf("that is not a file name")
	}
	mime, encoded, err := splitDataURL(dataURL)
	if err != nil {
		return Track{}, err
	}
	if ext := strings.ToLower(filepath.Ext(base)); !audioExtensions[ext] {
		// A name without a usable extension is rescued by the MIME the browser
		// read off the file — a drop from a stream-ripper names files oddly.
		mimeExt, known := interviewAttachmentKinds[mime]
		if !known || !audioExtensions[mimeExt] {
			return Track{}, fmt.Errorf("%s is not an audio file this app knows", base)
		}
		base += mimeExt
	}
	// Base64 is four characters per three bytes, so this bounds the decode
	// without doing it.
	if len(encoded)/4*3 > maxTrackFileBytes {
		return Track{}, fmt.Errorf("%s is larger than this app will file (%d MB)",
			base, maxTrackFileBytes>>20)
	}
	payload, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(payload) == 0 {
		return Track{}, fmt.Errorf("%s could not be read", base)
	}
	dir, err := os.MkdirTemp("", "aimusicmaker-drop-")
	if err != nil {
		return Track{}, fmt.Errorf("could not make somewhere to put the drop: %w", err)
	}
	defer os.RemoveAll(dir)
	temp := filepath.Join(dir, base)
	if err := os.WriteFile(temp, payload, 0o600); err != nil {
		return Track{}, fmt.Errorf("could not write %s: %w", base, err)
	}
	return a.ImportTrackFile(trackID, temp, label)
}

// ImportTrackFile copies a finished file into the library and records it.
//
// label is what tells this variation from the others ("v1 take 2"); an empty one
// is filled from the file's own name, which is what Suno's download is called
// and is at least something the producer will recognise.
func (a *App) ImportTrackFile(trackID, sourcePath, label string) (Track, error) {
	return a.importTrackFile(trackID, sourcePath, label, "")
}

// importTrackFile is ImportTrackFile with the Suno clip the file came from,
// which only the capture from Suno's player knows (#91).
func (a *App) importTrackFile(trackID, sourcePath, label, sunoClip string) (Track, error) {
	if a.store == nil {
		return Track{}, errNoStore
	}
	track, err := a.store.track(trackID)
	if err != nil {
		return Track{}, err
	}
	album, err := a.store.album(track.AlbumID)
	if err != nil {
		return Track{}, err
	}
	artist, err := a.store.artist(album.ArtistID)
	if err != nil {
		return Track{}, err
	}

	sourcePath = strings.TrimSpace(sourcePath)
	info, err := os.Stat(sourcePath)
	if err != nil {
		return Track{}, fmt.Errorf("could not read %s: %w", sourcePath, err)
	}
	if info.IsDir() {
		return Track{}, fmt.Errorf("%s is a folder, not an audio file", sourcePath)
	}
	if info.Size() > maxTrackFileBytes {
		return Track{}, fmt.Errorf("%s is larger than this app will file (%d MB)",
			filepath.Base(sourcePath), maxTrackFileBytes>>20)
	}
	ext := strings.ToLower(filepath.Ext(sourcePath))
	if !audioExtensions[ext] {
		return Track{}, fmt.Errorf("%s is not an audio file this app knows", filepath.Base(sourcePath))
	}

	dir, err := a.albumFolder(artist, album)
	if err != nil {
		return Track{}, err
	}
	// Numbered by running order and named from the track, so the folder sorts
	// and reads as an album rather than as a download history. The variation
	// suffix keeps two takes of the same track apart without either overwriting
	// the other.
	base := fmt.Sprintf("%02d %s", track.Position, slugify(track.Name))
	destination, err := fileTake(sourcePath, dir, base, ext)
	if err != nil {
		return Track{}, err
	}
	// The size of what was kept, which for an encoded WAV is not the size of
	// what was picked.
	stored, err := os.Stat(destination)
	if err != nil {
		return Track{}, err
	}

	if strings.TrimSpace(label) == "" {
		label = strings.TrimSuffix(filepath.Base(sourcePath), ext)
	}
	file := TrackFile{
		ID:       newID("file"),
		TrackID:  trackID,
		Label:    strings.TrimSpace(label),
		Name:     filepath.Base(destination),
		Path:     destination,
		Bytes:    stored.Size(),
		Seconds:  audio.DurationSeconds(destination),
		At:       nowStamp(),
		SunoClip: sunoClip,
	}
	// The first file imported is the chosen one. A track with takes and none
	// picked is a track the extension and the release step cannot act on, and
	// "the only one" is not a choice worth making somebody click.
	file.Chosen = len(track.Files) == 0

	if err := a.store.addTrackFile(file); err != nil {
		return Track{}, err
	}
	// The track's duration follows the chosen take (#85): once a real file is
	// the song, targetSeconds stops being an aim and becomes its length. An
	// unchosen take keeps its reading to itself.
	if file.Chosen && file.Seconds > 0 {
		_ = a.store.setTrackSeconds(trackID, file.Seconds)
	}
	a.store.changed(scopeTracks)
	return a.store.track(trackID)
}

// fileTake puts a take into the album folder and answers where it went. A
// WAV is encoded to MP3 on the way in (#93); anything else, and a WAV the
// encoder could not take, is copied as it is.
func fileTake(sourcePath, dir, base, ext string) (string, error) {
	if ext == ".wav" {
		destination, err := uniquePath(dir, base, ".mp3")
		if err != nil {
			return "", err
		}
		if err := audio.EncodeMP3(sourcePath, destination); err == nil {
			return destination, nil
		}
		// Falls through to the copy: the WAV is the take, and keeping it is
		// what an import did before there was an encoder to ask.
	}
	destination, err := uniquePath(dir, base, ext)
	if err != nil {
		return "", err
	}
	if err := copyFile(sourcePath, destination); err != nil {
		return "", err
	}
	return destination, nil
}

// ChooseTrackFile marks one take as the track, unmarking the others.
func (a *App) ChooseTrackFile(trackID, fileID string) (Track, error) {
	if a.store == nil {
		return Track{}, errNoStore
	}
	if err := a.store.chooseTrackFile(trackID, fileID); err != nil {
		return Track{}, err
	}
	// The duration follows the choice (#85), same as at import.
	_ = a.store.syncTrackSeconds(trackID)
	a.store.changed(scopeTracks)
	return a.store.track(trackID)
}

// RemoveTrackFile forgets a take.
//
// deleteFile says whether to remove the audio as well. Off by default from the
// UI: forgetting a row is undoable by importing it again, and deleting somebody
// else's music is not.
func (a *App) RemoveTrackFile(trackID, fileID string, deleteFile bool) (Track, error) {
	if a.store == nil {
		return Track{}, errNoStore
	}
	path, err := a.store.removeTrackFile(trackID, fileID)
	if err != nil {
		return Track{}, err
	}
	if deleteFile && path != "" {
		// Best effort. The row is already gone, and failing here would leave the
		// producer looking at an error about a file they can no longer see.
		_ = os.Remove(path)
	}
	// Removing the chosen take promotes another (see removeTrackFile), and the
	// duration follows the choice (#85).
	_ = a.store.syncTrackSeconds(trackID)
	a.store.changed(scopeTracks)
	return a.store.track(trackID)
}

// RevealTrackFile opens the file's folder in the desktop's own file manager, so
// "where did that go" is one click rather than a path to retype.
func (a *App) RevealTrackFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("no file to show")
	}
	return platform.RevealInFileManager(path)
}

// uniquePath finds a free name in dir: base.ext, then base (2).ext, and so on.
// The parenthesised counter rather than a hyphen, because that is what every
// desktop on all three platforms does with a duplicate download and it reads as
// a copy rather than as part of the title.
func uniquePath(dir, base, ext string) (string, error) {
	for n := 1; n < 1000; n++ {
		name := base + ext
		if n > 1 {
			name = fmt.Sprintf("%s (%d)%s", base, n, ext)
		}
		candidate := filepath.Join(dir, name)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("too many files named %s in %s", base, dir)
}

// copyFile copies bytes, streaming rather than reading whole: these are audio
// files and a lossless master read into memory is tens of megabytes held for no
// reason.
func copyFile(from, to string) error {
	source, err := os.Open(from)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(destination, source); err != nil {
		destination.Close()
		// A half-written file is worse than no file: it is in the library, it
		// plays for ten seconds and stops, and nothing says why.
		_ = os.Remove(to)
		return err
	}
	return destination.Close()
}

// ---------------------------------------------------------------------------
// Storage
// ---------------------------------------------------------------------------

func (s *Store) trackFiles(trackID string) ([]TrackFile, error) {
	out := []TrackFile{}
	if s == nil || s.db == nil {
		return out, nil
	}
	rows, err := s.db.Query(
		`SELECT id, track_id, label, name, path, bytes, seconds, chosen, at, suno_clip
		 FROM track_files WHERE track_id = ? ORDER BY at`, trackID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var f TrackFile
		if err := rows.Scan(&f.ID, &f.TrackID, &f.Label, &f.Name, &f.Path,
			&f.Bytes, &f.Seconds, &f.Chosen, &f.At, &f.SunoClip); err != nil {
			return out, err
		}
		f.Missing = !fileExists(f.Path)
		out = append(out, f)
	}
	return out, rows.Err()
}

// filesForAlbum reads every file under an album in one query, keyed by track —
// see listTracks for why it is not one query per track.
func (s *Store) filesForAlbum(albumID string) (map[string][]TrackFile, error) {
	out := map[string][]TrackFile{}
	if s == nil || s.db == nil {
		return out, nil
	}
	rows, err := s.db.Query(
		`SELECT f.id, f.track_id, f.label, f.name, f.path, f.bytes, f.seconds, f.chosen, f.at, f.suno_clip
		 FROM track_files f JOIN tracks t ON t.id = f.track_id
		 WHERE t.album_id = ? ORDER BY f.at`, albumID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var f TrackFile
		if err := rows.Scan(&f.ID, &f.TrackID, &f.Label, &f.Name, &f.Path,
			&f.Bytes, &f.Seconds, &f.Chosen, &f.At, &f.SunoClip); err != nil {
			return out, err
		}
		f.Missing = !fileExists(f.Path)
		out[f.TrackID] = append(out[f.TrackID], f)
	}
	return out, rows.Err()
}

func (s *Store) addTrackFile(f TrackFile) error {
	if s == nil || s.db == nil {
		return errNoStore
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if f.Chosen {
		if _, err := tx.Exec(`UPDATE track_files SET chosen = 0 WHERE track_id = ?`, f.TrackID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(
		`INSERT INTO track_files (id, track_id, label, name, path, bytes, seconds, chosen, at, suno_clip)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.TrackID, f.Label, f.Name, f.Path, f.Bytes, f.Seconds, f.Chosen, f.At, f.SunoClip); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) chooseTrackFile(trackID, fileID string) error {
	if s == nil || s.db == nil {
		return errNoStore
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE track_files SET chosen = 0 WHERE track_id = ?`, trackID); err != nil {
		return err
	}
	result, err := tx.Exec(
		`UPDATE track_files SET chosen = 1 WHERE id = ? AND track_id = ?`, fileID, trackID)
	if err != nil {
		return err
	}
	// Checked rather than assumed: without this, choosing a file that belongs to
	// another track would leave the track with nothing chosen and report success.
	if n, err := result.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("no file %s on this track", fileID)
	}
	return tx.Commit()
}

// removeTrackFile deletes the row and answers where the audio was, so the caller
// can decide whether to remove it too.
func (s *Store) removeTrackFile(trackID, fileID string) (string, error) {
	if s == nil || s.db == nil {
		return "", errNoStore
	}
	var path string
	var chosen bool
	if err := s.db.QueryRow(
		`SELECT path, chosen FROM track_files WHERE id = ? AND track_id = ?`,
		fileID, trackID).Scan(&path, &chosen); err != nil {
		return "", err
	}
	if _, err := s.db.Exec(`DELETE FROM track_files WHERE id = ?`, fileID); err != nil {
		return "", err
	}
	if chosen {
		// The chosen take was removed, so the track has no answer to "which one
		// is the song" any more. The oldest remaining take takes it — an
		// arbitrary rule, but a track with takes and none chosen is a state the
		// rest of the app has to keep guarding for.
		_, _ = s.db.Exec(
			`UPDATE track_files SET chosen = 1 WHERE id = (
				SELECT id FROM track_files WHERE track_id = ? ORDER BY at LIMIT 1)`, trackID)
	}
	return path, nil
}

// setTrackSeconds writes a track's duration without touching the rest of the
// row — the caller may be mid-import, with the producer's own edits in flight.
func (s *Store) setTrackSeconds(trackID string, seconds int) error {
	if s == nil || s.db == nil {
		return errNoStore
	}
	_, err := s.db.Exec(`UPDATE tracks SET target_seconds = ? WHERE id = ?`,
		seconds, trackID)
	return err
}

// syncTrackSeconds re-derives a track's duration from its chosen take (#85),
// for the paths where the choice moves rather than a file arriving. A chosen
// take with no reading changes nothing — a wrong number is worse than a stale
// one.
func (s *Store) syncTrackSeconds(trackID string) error {
	if s == nil || s.db == nil {
		return errNoStore
	}
	var seconds int
	err := s.db.QueryRow(
		`SELECT seconds FROM track_files WHERE track_id = ? AND chosen = 1`,
		trackID).Scan(&seconds)
	if err != nil || seconds <= 0 {
		return err
	}
	return s.setTrackSeconds(trackID, seconds)
}
