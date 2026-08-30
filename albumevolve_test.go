package main

import "testing"

// TestEvolveAlbumRefusals is #42's plumbing: a record with no styles anywhere
// has nothing to evolve from, and once there is material, no connected
// provider fails with the message rather than a half-made successor.
func TestEvolveAlbumRefusals(t *testing.T) {
	app := newTestApp(t)
	artist, err := app.SaveArtist(Artist{Name: "Cassette Motel", Inspirations: []Inspiration{}})
	if err != nil {
		t.Fatalf("artist: %v", err)
	}
	album, err := app.SaveAlbum(Album{ArtistID: artist.ID, Name: "Night Drive"})
	if err != nil {
		t.Fatalf("album: %v", err)
	}

	if _, err := app.EvolveAlbum(album.ID); err == nil {
		t.Fatal("a styleless record was evolved")
	}

	album.TechnicalStyle = "synthwave, 110 BPM, analog warmth"
	if album, err = app.SaveAlbum(album); err != nil {
		t.Fatalf("re-save: %v", err)
	}
	if _, err := app.EvolveAlbum(album.ID); err == nil {
		t.Fatal("a successor was made with no provider connected")
	}

	// The failed attempts must not have littered the discography.
	albums, err := app.ListAlbums(artist.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(albums) != 1 {
		t.Fatalf("a refusal still created an album: %d", len(albums))
	}
}
