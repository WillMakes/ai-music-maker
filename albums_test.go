package main

import "testing"

// TestAlbumCarriesTheArtistsBaseStyle is #40's join: the Record tab shows the
// inherited half of combinedStyle read-only, and it must follow the artist —
// a base style edited today is what every album of theirs reports tomorrow.
func TestAlbumCarriesTheArtistsBaseStyle(t *testing.T) {
	app := newTestApp(t)
	artist, err := app.SaveArtist(Artist{
		Name: "Cassette Motel", BaseStyle: "synthwave, analog warmth",
		Inspirations: []Inspiration{},
	})
	if err != nil {
		t.Fatalf("artist: %v", err)
	}
	album, err := app.SaveAlbum(Album{ArtistID: artist.ID, Name: "Night Drive"})
	if err != nil {
		t.Fatalf("album: %v", err)
	}

	got, err := app.GetAlbum(album.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ArtistBaseStyle != "synthwave, analog warmth" {
		t.Fatalf("the inherited style is wrong: %q", got.ArtistBaseStyle)
	}

	artist.BaseStyle = "coldwave, tape hiss"
	if _, err := app.SaveArtist(artist); err != nil {
		t.Fatalf("re-save: %v", err)
	}
	got, err = app.GetAlbum(album.ID)
	if err != nil {
		t.Fatalf("re-get: %v", err)
	}
	if got.ArtistBaseStyle != "coldwave, tape hiss" {
		t.Fatalf("the join copied instead of following: %q", got.ArtistBaseStyle)
	}
}
