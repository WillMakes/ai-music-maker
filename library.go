package main

// ---------------------------------------------------------------------------
// The library tree (#34)
//
// The redesign's sidebar is the library itself: every artist with their albums
// nested under them, searchable, always visible. That is a different read from
// ListArtists + ListAlbums-per-artist — the rail redraws on every change scope
// and would otherwise issue a query per artist — so the whole tree comes back
// from one call, shaped for the rail and nothing else.
// ---------------------------------------------------------------------------

// LibraryAlbum is one album row in the rail.
type LibraryAlbum struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// TrackCount is the rail's badge — how far along the record is.
	TrackCount int `json:"trackCount"`
	// UpdatedAt orders the Home view's resume cards: the record touched last is
	// the one to pick back up.
	UpdatedAt string `json:"updatedAt"`
}

// LibraryArtist is one artist and their records.
type LibraryArtist struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Albums []LibraryAlbum `json:"albums"`
}

// LibraryTree reports every artist with their albums, in the artists' own
// listing order.
func (a *App) LibraryTree() ([]LibraryArtist, error) {
	out := []LibraryArtist{}
	if a.store == nil {
		return out, nil
	}

	rows, err := a.store.db.Query(
		`SELECT ar.id, ar.name, COALESCE(al.id, ''), COALESCE(al.name, ''),
		        COALESCE(al.updated_at, ''),
		        (SELECT COUNT(*) FROM tracks t WHERE t.album_id = al.id)
		 FROM artists ar
		 LEFT JOIN albums al ON al.artist_id = ar.id
		 ORDER BY ar.created_at DESC, al.created_at`)
	if err != nil {
		return out, err
	}
	defer rows.Close()

	// Assembled in row order: the LEFT JOIN keeps an artist with no albums as
	// one row with empty album columns, so a brand-new artist still shows in
	// the rail rather than appearing only once they have a record.
	index := map[string]int{}
	for rows.Next() {
		var artistID, artistName, albumID, albumName, updatedAt string
		var trackCount int
		if err := rows.Scan(&artistID, &artistName, &albumID, &albumName,
			&updatedAt, &trackCount); err != nil {
			return out, err
		}
		at, known := index[artistID]
		if !known {
			out = append(out, LibraryArtist{ID: artistID, Name: artistName,
				Albums: []LibraryAlbum{}})
			at = len(out) - 1
			index[artistID] = at
		}
		if albumID != "" {
			out[at].Albums = append(out[at].Albums, LibraryAlbum{
				ID: albumID, Name: albumName, TrackCount: trackCount,
				UpdatedAt: updatedAt,
			})
		}
	}
	return out, rows.Err()
}
