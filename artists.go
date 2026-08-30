package main

import (
	"database/sql"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Artists
//
// The top of the model. An artist is a name, a base technical style, and the
// references that style was decided from — which is the whole of what an album
// under them inherits (see albums.go, combinedStyle).
//
// "Base technical style" is deliberately not a genre. Suno reads a dense
// comma-led breakdown far better than it reads "indie rock": instrumentation,
// production, tempo range, vocal treatment, mix character. The interview writes
// one (see interview.go) and this file only stores it.
// ---------------------------------------------------------------------------

// Artist is one artist, as the frontend sees it.
type Artist struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
	// Summary is the one-line "who is this", for the card and the header.
	Summary string `json:"summary"`
	// BaseStyle is the technical breakdown every album under this artist is
	// combined with.
	BaseStyle string `json:"baseStyle"`
	// Inspirations are the YouTube / YouTube Music references. Always non-nil:
	// the bindings marshal a nil slice as null and every view would need to
	// guard for it.
	Inspirations []Inspiration `json:"inspirations"`
	// AlbumCount is a joined count rather than the albums themselves. The list
	// page wants the number and nothing else, and loading every album of every
	// artist to count them is the read that would get slower with every record.
	AlbumCount int    `json:"albumCount"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

// Inspiration is one reference behind an artist's sound.
type Inspiration struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	Title string `json:"title"`
	// The song's identity, captured when it is analyzed (#51). Display-only —
	// the URL stays the reference — but a list of bare URLs a year later says
	// nothing about what any of them is.
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	ArtworkURL string `json:"artworkUrl"`
	// Note is what this reference contributes: the full markdown style
	// breakdown once the song has been analyzed, or the producer's own line.
	Note     string `json:"note"`
	Position int    `json:"position"`
}

// ListArtists reports every artist, newest first.
func (a *App) ListArtists() ([]Artist, error) {
	if a.store == nil {
		return []Artist{}, nil
	}
	return a.store.listArtists()
}

// GetArtist reads one artist, with its inspirations.
func (a *App) GetArtist(id string) (Artist, error) {
	if a.store == nil {
		return Artist{Inspirations: []Inspiration{}}, nil
	}
	return a.store.artist(id)
}

// SaveArtist creates or updates an artist and returns the stored record, so the
// caller never has to re-fetch to learn what it made — the slug in particular is
// decided here and the caller could not have known it.
//
// The inspirations on the record passed in replace the ones stored: they are
// edited as a list in one panel, and a diff against what is on disk would be
// this file working out which of the producer's rows is which when the panel
// already knows.
func (a *App) SaveArtist(artist Artist) (Artist, error) {
	if a.store == nil {
		return Artist{}, errNoStore
	}
	artist.Name = strings.TrimSpace(artist.Name)
	if artist.Name == "" {
		return Artist{}, fmt.Errorf("an artist needs a name")
	}
	saved, err := a.store.saveArtist(artist)
	if err != nil {
		return Artist{}, err
	}
	a.store.changed(scopeArtists)
	return saved, nil
}

// DeleteArtist removes an artist and everything under it.
//
// Everything, in one transaction: albums, their tracks, and the rows naming
// those tracks' files. The audio itself is left on disk — it is in the
// producer's music library, other applications may be pointing at it, and this
// app deleting a record is not a licence to delete the music that came of it.
func (a *App) DeleteArtist(id string) error {
	if a.store == nil {
		return errNoStore
	}
	if err := a.store.deleteArtist(id); err != nil {
		return err
	}
	a.store.changed(scopeArtists)
	a.store.changed(scopeAlbums)
	a.store.changed(scopeTracks)
	return nil
}

// ---------------------------------------------------------------------------
// Storage
// ---------------------------------------------------------------------------

func (s *Store) listArtists() ([]Artist, error) {
	out := []Artist{}
	if s == nil || s.db == nil {
		return out, nil
	}
	rows, err := s.db.Query(
		`SELECT a.id, a.name, a.slug, a.summary, a.base_style, a.created_at, a.updated_at,
		        (SELECT COUNT(*) FROM albums WHERE albums.artist_id = a.id)
		 FROM artists a ORDER BY a.created_at DESC`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var a Artist
		if err := rows.Scan(&a.ID, &a.Name, &a.Slug, &a.Summary, &a.BaseStyle,
			&a.CreatedAt, &a.UpdatedAt, &a.AlbumCount); err != nil {
			return out, err
		}
		// The list page draws a card per artist and does not show references, so
		// they are not loaded here — one query for the list rather than one per
		// row. GetArtist fills them for the page that does.
		a.Inspirations = []Inspiration{}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) artist(id string) (Artist, error) {
	out := Artist{Inspirations: []Inspiration{}}
	if s == nil || s.db == nil {
		return out, nil
	}
	err := s.db.QueryRow(
		`SELECT id, name, slug, summary, base_style, created_at, updated_at,
		        (SELECT COUNT(*) FROM albums WHERE albums.artist_id = artists.id)
		 FROM artists WHERE id = ?`, id).
		Scan(&out.ID, &out.Name, &out.Slug, &out.Summary, &out.BaseStyle,
			&out.CreatedAt, &out.UpdatedAt, &out.AlbumCount)
	if err == sql.ErrNoRows {
		return out, fmt.Errorf("no artist %s", id)
	}
	if err != nil {
		return out, err
	}
	inspirations, err := s.inspirations(id)
	if err != nil {
		return out, err
	}
	out.Inspirations = inspirations
	return out, nil
}

func (s *Store) inspirations(artistID string) ([]Inspiration, error) {
	out := []Inspiration{}
	rows, err := s.db.Query(
		`SELECT id, url, title, artist, album, artwork_url, note, position
		 FROM artist_inspirations
		 WHERE artist_id = ? ORDER BY position, at`, artistID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var i Inspiration
		if err := rows.Scan(&i.ID, &i.URL, &i.Title, &i.Artist, &i.Album,
			&i.ArtworkURL, &i.Note, &i.Position); err != nil {
			return out, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Store) saveArtist(artist Artist) (Artist, error) {
	if s == nil || s.db == nil {
		return Artist{}, errNoStore
	}
	now := nowStamp()
	if artist.ID == "" {
		artist.ID = newID("art")
		artist.CreatedAt = now
	}
	// The slug follows the name on every save, not only on the first. An artist
	// renamed before their first album should not keep a folder named after the
	// working title; one renamed afterwards keeps their old files where they are
	// (the paths are absolute) and files new ones under the new name, which is
	// the same thing every music library does with a rename.
	base := slugify(artist.Name)
	slug, err := s.uniqueSlug("artists", base, artist.ID)
	if err != nil {
		return Artist{}, err
	}
	artist.Slug = slug
	artist.UpdatedAt = now

	tx, err := s.db.Begin()
	if err != nil {
		return Artist{}, err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`INSERT INTO artists (id, name, slug, summary, base_style, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   name = excluded.name, slug = excluded.slug, summary = excluded.summary,
		   base_style = excluded.base_style, updated_at = excluded.updated_at`,
		artist.ID, artist.Name, artist.Slug, artist.Summary, artist.BaseStyle,
		artist.CreatedAt, artist.UpdatedAt); err != nil {
		return Artist{}, err
	}

	// Replaced whole rather than diffed — see SaveArtist.
	if _, err := tx.Exec(`DELETE FROM artist_inspirations WHERE artist_id = ?`, artist.ID); err != nil {
		return Artist{}, err
	}
	for n, insp := range artist.Inspirations {
		if strings.TrimSpace(insp.URL) == "" {
			// An empty row is the one the producer started and abandoned. Saving
			// it would put a reference to nothing on the record.
			continue
		}
		if insp.ID == "" {
			insp.ID = newID("insp")
		}
		if _, err := tx.Exec(
			`INSERT INTO artist_inspirations
			 (id, artist_id, url, title, artist, album, artwork_url, note, position, at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			insp.ID, artist.ID, strings.TrimSpace(insp.URL), strings.TrimSpace(insp.Title),
			strings.TrimSpace(insp.Artist), strings.TrimSpace(insp.Album),
			strings.TrimSpace(insp.ArtworkURL),
			strings.TrimSpace(insp.Note), n, now); err != nil {
			return Artist{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Artist{}, err
	}
	return s.artist(artist.ID)
}

func (s *Store) deleteArtist(id string) error {
	if s == nil || s.db == nil {
		return errNoStore
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Bottom up, so no statement here is ever looking for a parent that has
	// already gone. SQLite has no cascading delete on these tables by design:
	// declaring foreign keys would mean turning the pragma on for every
	// connection including the MCP server's, and getting that wrong in one place
	// silently disables the constraint everywhere.
	for _, stmt := range []string{
		`DELETE FROM track_files WHERE track_id IN (
			SELECT t.id FROM tracks t JOIN albums al ON al.id = t.album_id WHERE al.artist_id = ?)`,
		`DELETE FROM chats WHERE subject_kind = 'track' AND subject_id IN (
			SELECT t.id FROM tracks t JOIN albums al ON al.id = t.album_id WHERE al.artist_id = ?)`,
		`DELETE FROM tracks WHERE album_id IN (SELECT id FROM albums WHERE artist_id = ?)`,
		`DELETE FROM chats WHERE subject_kind = 'album' AND subject_id IN (
			SELECT id FROM albums WHERE artist_id = ?)`,
		`DELETE FROM albums WHERE artist_id = ?`,
		`DELETE FROM artist_inspirations WHERE artist_id = ?`,
		`DELETE FROM chats WHERE subject_kind = 'artist' AND subject_id = ?`,
		`DELETE FROM artists WHERE id = ?`,
	} {
		if _, err := tx.Exec(stmt, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
