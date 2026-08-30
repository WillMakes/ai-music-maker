package main

import (
	"encoding/base64"
	"os"
	"testing"
)

// TestImportDroppedTrackFile is #57: a file dropped onto the window arrives
// as bytes and a name, and must end up in the library exactly as if it had
// been picked — copied into the album folder, recorded on the track. The
// refusals guard the same line ImportTrackFile holds: only audio gets filed.
func TestImportDroppedTrackFile(t *testing.T) {
	app := newTestApp(t)
	artist, err := app.SaveArtist(Artist{Name: "Cassette Motel", Inspirations: []Inspiration{}})
	if err != nil {
		t.Fatal(err)
	}
	album, err := app.SaveAlbum(Album{ArtistID: artist.ID, Name: "Off-Season"})
	if err != nil {
		t.Fatal(err)
	}
	track, err := app.SaveTrack(Track{AlbumID: album.ID, Name: "Cold Open"})
	if err != nil {
		t.Fatal(err)
	}

	payload := base64.StdEncoding.EncodeToString([]byte("not really mp3 bytes"))
	got, err := app.ImportDroppedTrackFile(track.ID, "Cold Open.mp3",
		"data:audio/mpeg;base64,"+payload, "")
	if err != nil {
		t.Fatalf("the drop was not imported: %v", err)
	}
	if len(got.Files) != 1 {
		t.Fatalf("want one take, got %d", len(got.Files))
	}
	if _, err := os.Stat(got.Files[0].Path); err != nil {
		t.Fatalf("the take is not on disk: %v", err)
	}

	if _, err := app.ImportDroppedTrackFile(track.ID, "notes.txt",
		"data:text/plain;base64,"+payload, ""); err == nil {
		t.Fatal("a text file was filed as a take")
	}
	if _, err := app.ImportDroppedTrackFile(track.ID, "../escape.mp3",
		"data:audio/mpeg;base64,"+payload, ""); err == nil {
		t.Fatal("a path-shaped name was accepted")
	}
}
