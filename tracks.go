package main

import (
	"database/sql"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Tracks
//
// The unit the browser extension actually reaches for. A track carries the three
// things Suno's create form asks for — a title, a style prompt and lyrics — plus
// the target length that shaped the prompt, and the files that came back once it
// had been made.
//
// The prompt is stored rather than assembled on demand. It is written once
// against the album's combined style and the Track Generation skill (see
// interview.go), and then it is edited by hand: a prompt that regenerated itself
// from the album every time it was read would silently discard the tuning that
// is most of the work.
// ---------------------------------------------------------------------------

// Track is one track, as the frontend sees it.
type Track struct {
	ID      string `json:"id"`
	AlbumID string `json:"albumId"`
	Name    string `json:"name"`
	// Position is the running order, 1-based. Kept as a column rather than
	// inferred from created_at because the order a producer writes tracks in is
	// almost never the order they go on the record.
	Position int `json:"position"`
	// TargetSeconds is the length the track is aimed at. A target, not a
	// measurement — what Suno returns is whatever the file turns out to be.
	TargetSeconds int    `json:"targetSeconds"`
	Prompt        string `json:"prompt"`
	Lyrics        string `json:"lyrics"`
	Notes         string `json:"notes"`
	// Files are the variations Suno gave back. Always non-nil.
	Files []TrackFile `json:"files"`
	// AlbumName and ArtistName are joined for the header and for the extension's
	// picker, which lists tracks by the record they belong to.
	AlbumName  string `json:"albumName"`
	ArtistName string `json:"artistName"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

// TrackFile is one finished file under a track.
type TrackFile struct {
	ID      string `json:"id"`
	TrackID string `json:"trackId"`
	// Label is what distinguishes this variation from the others — Suno hands
	// back two per generation and a producer runs several generations.
	Label string `json:"label"`
	Name  string `json:"name"`
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	// Seconds is the file's real playing time, read off its headers at import
	// (#85) — 0 when the codec is not one this app reads. The track's
	// targetSeconds follows the chosen take's value, so the page stops saying
	// how long the song was aimed to be once it knows how long it is.
	Seconds int `json:"seconds"`
	// Chosen marks the take that is the track. At most one per track, enforced
	// on write: "which of these is the song" is a question with one answer, and
	// a list where three are ticked has not answered it.
	Chosen bool   `json:"chosen"`
	At     string `json:"at"`
}

// ListTracks reports one album's tracks in running order.
func (a *App) ListTracks(albumID string) ([]Track, error) {
	if a.store == nil {
		return []Track{}, nil
	}
	return a.store.listTracks(albumID)
}

// GetTrack reads one track with its files.
func (a *App) GetTrack(id string) (Track, error) {
	if a.store == nil {
		return Track{Files: []TrackFile{}}, nil
	}
	return a.store.track(id)
}

// SaveTrack creates or updates a track and returns the stored record.
//
// A new track with no position given goes on the end. Asking the producer to
// number it is asking them to know how many there already are, which is the
// app's job and not theirs.
func (a *App) SaveTrack(track Track) (Track, error) {
	if a.store == nil {
		return Track{}, errNoStore
	}
	track.Name = strings.TrimSpace(track.Name)
	if track.Name == "" {
		return Track{}, fmt.Errorf("a track needs a name")
	}
	if strings.TrimSpace(track.AlbumID) == "" {
		return Track{}, fmt.Errorf("a track needs an album")
	}
	if _, err := a.store.album(track.AlbumID); err != nil {
		return Track{}, err
	}
	if track.TargetSeconds <= 0 {
		track.TargetSeconds = a.defaultTrackLength()
	}
	saved, err := a.store.saveTrack(track)
	if err != nil {
		return Track{}, err
	}
	a.store.changed(scopeTracks)
	return saved, nil
}

// ReorderTracks writes a new running order from a list of track ids.
//
// The whole order at once rather than "move this one up": the page drags rows
// around and already knows the result, and a per-row API would have it issue
// several writes to express one change, any of which could be the last one to
// land.
func (a *App) ReorderTracks(albumID string, trackIDs []string) ([]Track, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	if err := a.store.reorderTracks(albumID, trackIDs); err != nil {
		return nil, err
	}
	a.store.changed(scopeTracks)
	return a.store.listTracks(albumID)
}

// DeleteTrack removes a track and the rows naming its files. The audio stays on
// disk, for DeleteArtist's reason.
func (a *App) DeleteTrack(id string) error {
	if a.store == nil {
		return errNoStore
	}
	if err := a.store.deleteTrack(id); err != nil {
		return err
	}
	a.store.changed(scopeTracks)
	return nil
}

// defaultTrackLength is the target new tracks start at, in seconds. Three
// minutes when nobody has said otherwise — the length of the thing Suno is
// usually asked for, and a number the producer immediately recognises as a
// default rather than as a decision made for them.
func (a *App) defaultTrackLength() int {
	const fallback = 180
	if a.store == nil {
		return fallback
	}
	raw, err := a.store.setting(keyDefaultTrackLength)
	if err != nil || strings.TrimSpace(raw) == "" {
		return fallback
	}
	var seconds int
	if _, err := fmt.Sscanf(raw, "%d", &seconds); err != nil || seconds <= 0 {
		return fallback
	}
	return seconds
}

// ---------------------------------------------------------------------------
// Storage
// ---------------------------------------------------------------------------

const trackColumns = `t.id, t.album_id, t.name, t.position, t.target_seconds,
	t.prompt, t.lyrics, t.notes, t.created_at, t.updated_at,
	COALESCE(al.name, ''), COALESCE(ar.name, '')`

const trackJoins = `FROM tracks t
	LEFT JOIN albums al ON al.id = t.album_id
	LEFT JOIN artists ar ON ar.id = al.artist_id`

func scanTrack(scan func(...any) error) (Track, error) {
	t := Track{Files: []TrackFile{}}
	err := scan(&t.ID, &t.AlbumID, &t.Name, &t.Position, &t.TargetSeconds,
		&t.Prompt, &t.Lyrics, &t.Notes, &t.CreatedAt, &t.UpdatedAt,
		&t.AlbumName, &t.ArtistName)
	return t, err
}

func (s *Store) listTracks(albumID string) ([]Track, error) {
	out := []Track{}
	if s == nil || s.db == nil {
		return out, nil
	}
	rows, err := s.db.Query(
		`SELECT `+trackColumns+` `+trackJoins+`
		 WHERE t.album_id = ? ORDER BY t.position, t.created_at`, albumID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		t, err := scanTrack(rows.Scan)
		if err != nil {
			return out, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	// Files in one further query rather than one per track: an album is a dozen
	// tracks and this is the read the album page makes on every visit.
	byTrack, err := s.filesForAlbum(albumID)
	if err != nil {
		return out, err
	}
	for i := range out {
		if files, ok := byTrack[out[i].ID]; ok {
			out[i].Files = files
		}
	}
	return out, nil
}

func (s *Store) track(id string) (Track, error) {
	if s == nil || s.db == nil {
		return Track{Files: []TrackFile{}}, nil
	}
	t, err := scanTrack(s.db.QueryRow(
		`SELECT `+trackColumns+` `+trackJoins+` WHERE t.id = ?`, id).Scan)
	if err == sql.ErrNoRows {
		return Track{Files: []TrackFile{}}, fmt.Errorf("no track %s", id)
	}
	if err != nil {
		return Track{Files: []TrackFile{}}, err
	}
	files, err := s.trackFiles(id)
	if err != nil {
		return t, err
	}
	t.Files = files
	return t, nil
}

func (s *Store) saveTrack(track Track) (Track, error) {
	if s == nil || s.db == nil {
		return Track{}, errNoStore
	}
	now := nowStamp()
	if track.ID == "" {
		track.ID = newID("trk")
		track.CreatedAt = now
		if track.Position <= 0 {
			var highest sql.NullInt64
			if err := s.db.QueryRow(
				`SELECT MAX(position) FROM tracks WHERE album_id = ?`, track.AlbumID).
				Scan(&highest); err != nil {
				return Track{}, err
			}
			track.Position = int(highest.Int64) + 1
		}
	}
	track.UpdatedAt = now

	if _, err := s.db.Exec(
		`INSERT INTO tracks (id, album_id, name, position, target_seconds, prompt,
		   lyrics, notes, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   album_id = excluded.album_id, name = excluded.name,
		   position = excluded.position, target_seconds = excluded.target_seconds,
		   prompt = excluded.prompt, lyrics = excluded.lyrics, notes = excluded.notes,
		   updated_at = excluded.updated_at`,
		track.ID, track.AlbumID, track.Name, track.Position, track.TargetSeconds,
		track.Prompt, track.Lyrics, track.Notes, track.CreatedAt, track.UpdatedAt); err != nil {
		return Track{}, err
	}
	return s.track(track.ID)
}

func (s *Store) reorderTracks(albumID string, ids []string) error {
	if s == nil || s.db == nil {
		return errNoStore
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := nowStamp()
	for n, id := range ids {
		// Scoped to the album as well as the id, so a stale list from another
		// page cannot renumber tracks on a record it was never showing.
		if _, err := tx.Exec(
			`UPDATE tracks SET position = ?, updated_at = ? WHERE id = ? AND album_id = ?`,
			n+1, now, id, albumID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) deleteTrack(id string) error {
	if s == nil || s.db == nil {
		return errNoStore
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range []string{
		`DELETE FROM track_files WHERE track_id = ?`,
		`DELETE FROM chats WHERE subject_kind = 'track' AND subject_id = ?`,
		`DELETE FROM tracks WHERE id = ?`,
	} {
		if _, err := tx.Exec(stmt, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
