package main

import (
	"testing"
)

// TestWriteAlbumTracksRefusals pins the writer's gates (#71): a record that
// already has tracks is refused (the track interview adds to a started
// record), a styleless record has nothing to write from, and with a style but
// no provider connected the failure is loud rather than a silent empty list.
func TestWriteAlbumTracksRefusals(t *testing.T) {
	app := newTestApp(t)
	artist, err := app.SaveArtist(Artist{Name: "Cassette Motel", Inspirations: []Inspiration{}})
	if err != nil {
		t.Fatal(err)
	}

	bare, err := app.SaveAlbum(Album{ArtistID: artist.ID, Name: "Season 2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.WriteAlbumTracks(bare.ID); err == nil {
		t.Fatal("a styleless record had its tracks written from nothing")
	}

	styled, err := app.SaveAlbum(Album{
		ArtistID:       artist.ID,
		Name:           "Season 3",
		TechnicalStyle: "synthwave, analog warmth, 100-110 BPM",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.WriteAlbumTracks(styled.ID); err == nil {
		t.Fatal("a track list was written with no provider connected")
	}

	if _, err := app.SaveTrack(Track{AlbumID: styled.ID, Name: "Cold Open"}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.WriteAlbumTracks(styled.ID); err == nil {
		t.Fatal("a record that already has tracks was overwritten with a new list")
	}
}
