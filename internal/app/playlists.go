package app

import (
	"database/sql"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Playlists (#107)
//
// A playlist is a name and an ordered list of tracks from anywhere in the
// library, listed in the rail under the artists. Its rows name tracks rather
// than copying them, so a rename, a newly chosen take or a deleted song shows
// through on the next read.
//
// SavedPlaylist rather than Playlist: App.Playlist is the binding that turns a
// play button into a queue (playback.go), and has been since #45.
// ---------------------------------------------------------------------------

// SavedPlaylist is one playlist. Entries is filled by GetPlaylist and the
// writes that return the stored record; ListPlaylists leaves it empty, since
// the rail draws only names and counts.
type SavedPlaylist struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	TrackCount int             `json:"trackCount"`
	Entries    []PlaylistEntry `json:"entries"`
	CreatedAt  string          `json:"createdAt"`
	UpdatedAt  string          `json:"updatedAt"`
}

// PlaylistEntry is one track's place in a playlist. ID is the entry's own,
// which is what removing names: the track id would be ambiguous the day a
// playlist may hold a song twice.
type PlaylistEntry struct {
	ID            string `json:"id"`
	TrackID       string `json:"trackId"`
	TrackName     string `json:"trackName"`
	TargetSeconds int    `json:"targetSeconds"`
	AlbumID       string `json:"albumId"`
	AlbumName     string `json:"albumName"`
	ArtistID      string `json:"artistId"`
	ArtistName    string `json:"artistName"`
	ArtworkURL    string `json:"artworkUrl"`
	// Playable is whether the track has a take whose file is still there
	// (#105) — the page offers play only where something will sound.
	Playable bool `json:"playable"`
}

// ListPlaylists reports every playlist by name, with how many tracks each
// holds.
func (a *App) ListPlaylists() ([]SavedPlaylist, error) {
	if a.store == nil {
		return []SavedPlaylist{}, nil
	}
	return a.store.listPlaylists()
}

// GetPlaylist reads one playlist with its entries in order.
func (a *App) GetPlaylist(id string) (SavedPlaylist, error) {
	if a.store == nil {
		return SavedPlaylist{Entries: []PlaylistEntry{}}, nil
	}
	return a.playlistWithEntries(id)
}

// SavePlaylist creates a playlist, or renames one, and returns the stored
// record. The entries on the argument are ignored: tracks go in and out
// through AddToPlaylist and RemoveFromPlaylist, so a rename racing an add
// cannot write back a list from before the add.
func (a *App) SavePlaylist(p SavedPlaylist) (SavedPlaylist, error) {
	if a.store == nil {
		return SavedPlaylist{}, errNoStore
	}
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return SavedPlaylist{}, fmt.Errorf("a playlist needs a name")
	}
	id, err := a.store.savePlaylist(p)
	if err != nil {
		return SavedPlaylist{}, err
	}
	a.store.changed(scopePlaylists)
	return a.playlistWithEntries(id)
}

// DeletePlaylist removes a playlist. The tracks in it are untouched.
func (a *App) DeletePlaylist(id string) error {
	if a.store == nil {
		return errNoStore
	}
	if err := a.store.deletePlaylist(id); err != nil {
		return err
	}
	a.store.changed(scopePlaylists)
	return nil
}

// AddToPlaylist puts a track at the end of a playlist and returns the
// playlist. A track already in it stays where it is: the button is pressed
// from a track's own page, where a producer cannot see whether they added it
// last week, and a doubled song is the result nobody pressing it wanted.
func (a *App) AddToPlaylist(playlistID, trackID string) (SavedPlaylist, error) {
	if a.store == nil {
		return SavedPlaylist{}, errNoStore
	}
	// Both read rather than trusted, so an entry never names a playlist or a
	// track that does not exist.
	if _, err := a.store.playlist(playlistID); err != nil {
		return SavedPlaylist{}, err
	}
	if _, err := a.store.track(trackID); err != nil {
		return SavedPlaylist{}, err
	}
	if err := a.store.addToPlaylist(playlistID, trackID); err != nil {
		return SavedPlaylist{}, err
	}
	a.store.changed(scopePlaylists)
	return a.playlistWithEntries(playlistID)
}

// RemoveFromPlaylist takes one entry out of a playlist and returns the
// playlist.
func (a *App) RemoveFromPlaylist(playlistID, entryID string) (SavedPlaylist, error) {
	if a.store == nil {
		return SavedPlaylist{}, errNoStore
	}
	if err := a.store.removeFromPlaylist(playlistID, entryID); err != nil {
		return SavedPlaylist{}, err
	}
	a.store.changed(scopePlaylists)
	return a.playlistWithEntries(playlistID)
}

// playlistWithEntries reads a playlist and fills in its entries, working out
// after the entry rows are closed which ones can play and where their covers
// are — both read further rows, and the store holds a single connection.
func (a *App) playlistWithEntries(id string) (SavedPlaylist, error) {
	p, err := a.store.playlist(id)
	if err != nil {
		return SavedPlaylist{}, err
	}
	entries, err := a.store.playlistEntries(id)
	if err != nil {
		return SavedPlaylist{}, err
	}
	for i := range entries {
		entries[i].ArtworkURL = a.artworkURL(entries[i].ArtworkURL)
		if track, err := a.store.track(entries[i].TrackID); err == nil {
			entries[i].Playable = hasPlayableFile(track)
		}
	}
	p.Entries = entries
	p.TrackCount = len(entries)
	return p, nil
}

// hasPlayableFile reports whether any of a track's takes is still on disk.
func hasPlayableFile(track Track) bool {
	for _, file := range track.Files {
		if !file.Missing {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

// playlistEntriesQuery joins through tracks, albums and artists, which is
// also what keeps an entry whose track was deleted out of sight: deleting an
// album or an artist removes its tracks in bulk without knowing playlists
// exist, and the inner join means it does not have to.
const playlistEntriesQuery = `
	SELECT pt.id, pt.track_id, t.name, t.target_seconds,
	       al.id, al.name, al.artwork_file, ar.id, ar.name
	FROM playlist_tracks pt
	JOIN tracks t ON t.id = pt.track_id
	JOIN albums al ON al.id = t.album_id
	JOIN artists ar ON ar.id = al.artist_id
	WHERE pt.playlist_id = ?
	ORDER BY pt.position, pt.at`

func (s *Store) listPlaylists() ([]SavedPlaylist, error) {
	out := []SavedPlaylist{}
	if s == nil || s.db == nil {
		return out, nil
	}
	// By name, case aside: the rail has to hold a lot of them (#107), and a
	// long list is searched by eye alphabetically.
	rows, err := s.db.Query(`
		SELECT p.id, p.name, p.created_at, p.updated_at,
		       (SELECT COUNT(*) FROM playlist_tracks pt
		        JOIN tracks t ON t.id = pt.track_id
		        WHERE pt.playlist_id = p.id)
		FROM playlists p
		ORDER BY p.name COLLATE NOCASE, p.created_at`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		p := SavedPlaylist{Entries: []PlaylistEntry{}}
		if err := rows.Scan(&p.ID, &p.Name, &p.CreatedAt, &p.UpdatedAt, &p.TrackCount); err != nil {
			return out, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) playlist(id string) (SavedPlaylist, error) {
	if s == nil || s.db == nil {
		return SavedPlaylist{Entries: []PlaylistEntry{}}, nil
	}
	p := SavedPlaylist{Entries: []PlaylistEntry{}}
	err := s.db.QueryRow(
		`SELECT id, name, created_at, updated_at FROM playlists WHERE id = ?`, id).
		Scan(&p.ID, &p.Name, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return SavedPlaylist{}, fmt.Errorf("no playlist %s", id)
	}
	return p, err
}

func (s *Store) playlistEntries(id string) ([]PlaylistEntry, error) {
	out := []PlaylistEntry{}
	rows, err := s.db.Query(playlistEntriesQuery, id)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var e PlaylistEntry
		// The artwork file rides in ArtworkURL until the caller turns it
		// into an address, once these rows are closed.
		if err := rows.Scan(&e.ID, &e.TrackID, &e.TrackName, &e.TargetSeconds,
			&e.AlbumID, &e.AlbumName, &e.ArtworkURL, &e.ArtistID, &e.ArtistName); err != nil {
			return out, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// savePlaylist inserts or renames, returning the playlist's id.
func (s *Store) savePlaylist(p SavedPlaylist) (string, error) {
	if s == nil || s.db == nil {
		return "", errNoStore
	}
	now := nowStamp()
	if p.ID == "" {
		p.ID = newID("pl")
		_, err := s.db.Exec(
			`INSERT INTO playlists (id, name, created_at, updated_at) VALUES (?, ?, ?, ?)`,
			p.ID, p.Name, now, now)
		return p.ID, err
	}
	res, err := s.db.Exec(`UPDATE playlists SET name = ?, updated_at = ? WHERE id = ?`,
		p.Name, now, p.ID)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", fmt.Errorf("no playlist %s", p.ID)
	}
	return p.ID, nil
}

func (s *Store) deletePlaylist(id string) error {
	if s == nil || s.db == nil {
		return errNoStore
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range []string{
		`DELETE FROM playlist_tracks WHERE playlist_id = ?`,
		`DELETE FROM playlists WHERE id = ?`,
	} {
		if _, err := tx.Exec(stmt, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) addToPlaylist(playlistID, trackID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var already int
	if err := tx.QueryRow(
		`SELECT COUNT(*) FROM playlist_tracks WHERE playlist_id = ? AND track_id = ?`,
		playlistID, trackID).Scan(&already); err != nil {
		return err
	}
	if already > 0 {
		return nil
	}
	now := nowStamp()
	if _, err := tx.Exec(`
		INSERT INTO playlist_tracks (id, playlist_id, track_id, position, at)
		VALUES (?, ?, ?, (SELECT COALESCE(MAX(position), 0) + 1
		                  FROM playlist_tracks WHERE playlist_id = ?), ?)`,
		newID("ple"), playlistID, trackID, playlistID, now); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE playlists SET updated_at = ? WHERE id = ?`, now, playlistID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) removeFromPlaylist(playlistID, entryID string) error {
	res, err := s.db.Exec(`DELETE FROM playlist_tracks WHERE id = ? AND playlist_id = ?`,
		entryID, playlistID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("that track is not in this playlist")
	}
	_, err = s.db.Exec(`UPDATE playlists SET updated_at = ? WHERE id = ?`, nowStamp(), playlistID)
	return err
}
