package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAlbumCarriesTheArtistsBaseStyle is #40's join: the album page shows the
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

// TestAlbumProgressCounts is the card's pipeline maths (#43): written is the
// tracks with a style prompt, made the tracks with at least one take.
func TestAlbumProgressCounts(t *testing.T) {
	app := newTestApp(t)
	artist, err := app.SaveArtist(Artist{Name: "Cassette Motel", Inspirations: []Inspiration{}})
	if err != nil {
		t.Fatalf("artist: %v", err)
	}
	album, err := app.SaveAlbum(Album{ArtistID: artist.ID, Name: "Night Drive"})
	if err != nil {
		t.Fatalf("album: %v", err)
	}

	written, err := app.SaveTrack(Track{
		AlbumID: album.ID, Name: "Neon Rain", Prompt: "synthwave, 110 BPM",
	})
	if err != nil {
		t.Fatalf("track: %v", err)
	}
	if _, err := app.SaveTrack(Track{AlbumID: album.ID, Name: "Untitled"}); err != nil {
		t.Fatalf("track: %v", err)
	}

	file := filepath.Join(t.TempDir(), "take.mp3")
	if err := os.WriteFile(file, []byte("bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ImportTrackFile(written.ID, file, ""); err != nil {
		t.Fatalf("import: %v", err)
	}

	got, err := app.GetAlbum(album.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.TrackCount != 2 || got.WrittenCount != 1 || got.MadeCount != 1 {
		t.Fatalf("counts wrong: tracks %d, written %d, made %d",
			got.TrackCount, got.WrittenCount, got.MadeCount)
	}
}
