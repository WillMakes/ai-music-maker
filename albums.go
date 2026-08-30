package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// Albums
//
// An album belongs to one artist and carries the half of the sound that is this
// record rather than this artist: the artist's base style says what they always
// sound like, the album's technical style says what is different about this one.
// The two are joined at prompt time (see combinedStyle) rather than being copied
// together at save time, so editing the artist's base style corrects every album
// under them instead of only the ones made afterwards.
//
// A name is the only thing required. Artwork and the three release links are
// filled in when there is something to fill them in with — the links, in
// particular, only exist once the record is out, which is the last step of the
// workflow rather than the first.
// ---------------------------------------------------------------------------

// Album is one album, as the frontend sees it.
type Album struct {
	ID       string `json:"id"`
	ArtistID string `json:"artistId"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	// ArtworkFile is the cover's file name under the artwork folder, and
	// ArtworkURL the address the frontend draws it from (see artwork.go). The
	// file name is what is stored; the URL is assembled on read, because it
	// carries a port that is only known while the app is running.
	ArtworkFile string `json:"artworkFile"`
	ArtworkURL  string `json:"artworkUrl"`
	// ArtworkPrompt is the brief the cover was generated from, kept so "make it
	// bluer" has something to revise rather than starting again.
	ArtworkPrompt string `json:"artworkPrompt"`
	// TechnicalStyle is this record's own breakdown, combined with the artist's
	// base style when a track prompt is written.
	TechnicalStyle string `json:"technicalStyle"`
	AppleMusicURL  string `json:"appleMusicUrl"`
	SpotifyURL     string `json:"spotifyUrl"`
	YouTubeURL     string `json:"youtubeUrl"`
	// TrackCount is joined for the card, for listArtists' reason.
	TrackCount int `json:"trackCount"`
	// WrittenCount and MadeCount are the card's progress (#43): tracks with a
	// style prompt, and tracks with at least one take. Joined like TrackCount —
	// the pipeline reads them far more often than anything writes them.
	WrittenCount int `json:"writtenCount"`
	MadeCount    int `json:"madeCount"`
	// ArtistName is joined at read time rather than copied onto the row, so an
	// artist renamed today is renamed on every album of theirs.
	ArtistName string `json:"artistName"`
	// ArtistBaseStyle is joined the same way, for the Record tab's read-only
	// "inherited" pane (#40) — the producer sees what combinedStyle will put
	// before this record's own style without leaving the page.
	ArtistBaseStyle string `json:"artistBaseStyle"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

// combinedStyle is what a track prompt is actually written against: the
// artist's base style and the album's own, in that order.
//
// Order matters and is not arbitrary. The artist's breakdown establishes the
// instrument palette and production character; the album's narrows it. A model
// reading a comma-led style list weights the later terms as refinements of the
// earlier ones, so putting the album first would read as an artist who sounds
// like this record rather than a record by this artist.
func combinedStyle(artist Artist, album Album) string {
	parts := []string{}
	if s := strings.TrimSpace(artist.BaseStyle); s != "" {
		parts = append(parts, s)
	}
	if s := strings.TrimSpace(album.TechnicalStyle); s != "" {
		parts = append(parts, s)
	}
	return strings.Join(parts, "\n\n")
}

// ListAlbums reports one artist's albums, oldest first — a discography reads
// forwards, unlike a list of records being worked on.
func (a *App) ListAlbums(artistID string) ([]Album, error) {
	if a.store == nil {
		return []Album{}, nil
	}
	albums, err := a.store.listAlbums(artistID)
	if err != nil {
		return nil, err
	}
	for i := range albums {
		albums[i].ArtworkURL = a.artworkURL(albums[i].ArtworkFile)
	}
	return albums, nil
}

// GetAlbum reads one album.
func (a *App) GetAlbum(id string) (Album, error) {
	if a.store == nil {
		return Album{}, nil
	}
	album, err := a.store.album(id)
	if err != nil {
		return Album{}, err
	}
	album.ArtworkURL = a.artworkURL(album.ArtworkFile)
	return album, nil
}

// SaveAlbum creates or updates an album and returns the stored record.
func (a *App) SaveAlbum(album Album) (Album, error) {
	if a.store == nil {
		return Album{}, errNoStore
	}
	album.Name = strings.TrimSpace(album.Name)
	if album.Name == "" {
		return Album{}, fmt.Errorf("an album needs a name")
	}
	if strings.TrimSpace(album.ArtistID) == "" {
		return Album{}, fmt.Errorf("an album needs an artist")
	}
	// The artist is read rather than trusted, so an album can never be filed
	// against an id that names nothing — which would leave it invisible on every
	// page (they all list by artist) and undeletable from the UI.
	if _, err := a.store.artist(album.ArtistID); err != nil {
		return Album{}, err
	}
	saved, err := a.store.saveAlbum(album)
	if err != nil {
		return Album{}, err
	}
	saved.ArtworkURL = a.artworkURL(saved.ArtworkFile)
	a.store.changed(scopeAlbums)
	return saved, nil
}

// DeleteAlbum removes an album, its tracks and the rows naming their files. The
// audio and the cover stay on disk, for DeleteArtist's reason.
func (a *App) DeleteAlbum(id string) error {
	if a.store == nil {
		return errNoStore
	}
	if err := a.store.deleteAlbum(id); err != nil {
		return err
	}
	a.store.changed(scopeAlbums)
	a.store.changed(scopeTracks)
	return nil
}

// ---------------------------------------------------------------------------
// Storage
// ---------------------------------------------------------------------------

const albumColumns = `al.id, al.artist_id, al.name, al.slug, al.artwork_file,
	al.artwork_prompt, al.technical_style, al.apple_music_url, al.spotify_url,
	al.youtube_url, al.created_at, al.updated_at,
	(SELECT COUNT(*) FROM tracks WHERE tracks.album_id = al.id),
	(SELECT COUNT(*) FROM tracks WHERE tracks.album_id = al.id
	   AND TRIM(tracks.prompt) <> ''),
	(SELECT COUNT(*) FROM tracks WHERE tracks.album_id = al.id
	   AND EXISTS (SELECT 1 FROM track_files WHERE track_files.track_id = tracks.id)),
	COALESCE(ar.name, ''), COALESCE(ar.base_style, '')`

func scanAlbum(scan func(...any) error) (Album, error) {
	var al Album
	err := scan(&al.ID, &al.ArtistID, &al.Name, &al.Slug, &al.ArtworkFile,
		&al.ArtworkPrompt, &al.TechnicalStyle, &al.AppleMusicURL, &al.SpotifyURL,
		&al.YouTubeURL, &al.CreatedAt, &al.UpdatedAt, &al.TrackCount,
		&al.WrittenCount, &al.MadeCount, &al.ArtistName, &al.ArtistBaseStyle)
	return al, err
}

func (s *Store) listAlbums(artistID string) ([]Album, error) {
	out := []Album{}
	if s == nil || s.db == nil {
		return out, nil
	}
	rows, err := s.db.Query(
		`SELECT `+albumColumns+`
		 FROM albums al LEFT JOIN artists ar ON ar.id = al.artist_id
		 WHERE al.artist_id = ? ORDER BY al.created_at`, artistID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		al, err := scanAlbum(rows.Scan)
		if err != nil {
			return out, err
		}
		out = append(out, al)
	}
	return out, rows.Err()
}

func (s *Store) album(id string) (Album, error) {
	if s == nil || s.db == nil {
		return Album{}, nil
	}
	al, err := scanAlbum(s.db.QueryRow(
		`SELECT `+albumColumns+`
		 FROM albums al LEFT JOIN artists ar ON ar.id = al.artist_id
		 WHERE al.id = ?`, id).Scan)
	if err == sql.ErrNoRows {
		return Album{}, fmt.Errorf("no album %s", id)
	}
	return al, err
}

func (s *Store) saveAlbum(album Album) (Album, error) {
	if s == nil || s.db == nil {
		return Album{}, errNoStore
	}
	now := nowStamp()
	if album.ID == "" {
		album.ID = newID("alb")
		album.CreatedAt = now
	}
	// Album slugs are unique across the whole table rather than per artist, even
	// though the folder they name is already under the artist's own. It costs
	// nothing and it means a slug identifies one album everywhere it appears —
	// in a log line, in an MCP tool's answer — without an artist beside it.
	slug, err := s.uniqueSlug("albums", slugify(album.Name), album.ID)
	if err != nil {
		return Album{}, err
	}
	album.Slug = slug
	album.UpdatedAt = now

	_, err = s.db.Exec(
		`INSERT INTO albums (id, artist_id, name, slug, artwork_file, artwork_prompt,
		   technical_style, apple_music_url, spotify_url, youtube_url, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   artist_id = excluded.artist_id, name = excluded.name, slug = excluded.slug,
		   artwork_file = excluded.artwork_file, artwork_prompt = excluded.artwork_prompt,
		   technical_style = excluded.technical_style,
		   apple_music_url = excluded.apple_music_url, spotify_url = excluded.spotify_url,
		   youtube_url = excluded.youtube_url, updated_at = excluded.updated_at`,
		album.ID, album.ArtistID, album.Name, album.Slug, album.ArtworkFile,
		album.ArtworkPrompt, album.TechnicalStyle, strings.TrimSpace(album.AppleMusicURL),
		strings.TrimSpace(album.SpotifyURL), strings.TrimSpace(album.YouTubeURL),
		album.CreatedAt, album.UpdatedAt)
	if err != nil {
		return Album{}, err
	}
	return s.album(album.ID)
}

func (s *Store) deleteAlbum(id string) error {
	if s == nil || s.db == nil {
		return errNoStore
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range []string{
		`DELETE FROM track_files WHERE track_id IN (SELECT id FROM tracks WHERE album_id = ?)`,
		`DELETE FROM chats WHERE subject_kind = 'track' AND subject_id IN (
			SELECT id FROM tracks WHERE album_id = ?)`,
		`DELETE FROM tracks WHERE album_id = ?`,
		`DELETE FROM chats WHERE subject_kind = 'album' AND subject_id = ?`,
		`DELETE FROM albums WHERE id = ?`,
	} {
		if _, err := tx.Exec(stmt, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// setAlbumArtwork records a cover, removing the one it replaces.
//
// The old file is deleted rather than kept: a cover is regenerated freely while
// somebody is deciding, and an album with forty abandoned attempts beside it is
// a folder nobody can read. What was asked for is kept instead — ArtworkPrompt
// survives, so the decision is reproducible even though the image is not.
func (s *Store) setAlbumArtwork(id, file, prompt string) error {
	if s == nil || s.db == nil {
		return errNoStore
	}
	var previous string
	_ = s.db.QueryRow(`SELECT artwork_file FROM albums WHERE id = ?`, id).Scan(&previous)
	if _, err := s.db.Exec(
		`UPDATE albums SET artwork_file = ?, artwork_prompt = ?, updated_at = ? WHERE id = ?`,
		file, prompt, nowStamp(), id); err != nil {
		return err
	}
	if previous != "" && previous != file {
		if dir, err := artworkDir(); err == nil {
			// Best effort: a cover that could not be removed is clutter, and
			// failing the save that replaced it would be worse than clutter.
			_ = os.Remove(filepath.Join(dir, previous))
		}
	}
	s.changed(scopeAlbums)
	return nil
}
