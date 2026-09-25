package app

import (
	"encoding/base64"
	"os"
	"testing"
)

// TestMatchAlbumTrack pins the routing rules (#76): a download named after
// its track lands on it, this app's own numbered naming and a version suffix
// still match, and ambiguity or a stranger is refused rather than guessed —
// a take filed onto the wrong track is worse than one that asks to be
// dropped on the track itself.
func TestMatchAlbumTrack(t *testing.T) {
	tracks := []Track{
		{ID: "trk_1", Name: "Cold Open"},
		{ID: "trk_2", Name: "Cold Night"},
		{ID: "trk_3", Name: "Dawn"},
	}

	got, err := matchAlbumTrack(tracks, "Dawn.mp3")
	if err != nil || got.ID != "trk_3" {
		t.Fatalf("plain name: got %v, %v", got.ID, err)
	}
	got, err = matchAlbumTrack(tracks, "03 dawn.mp3")
	if err != nil || got.ID != "trk_3" {
		t.Fatalf("numbered name: got %v, %v", got.ID, err)
	}
	got, err = matchAlbumTrack(tracks, "Cold Open (v2).mp3")
	if err != nil || got.ID != "trk_1" {
		t.Fatalf("version suffix: got %v, %v", got.ID, err)
	}
	if _, err := matchAlbumTrack(tracks, "Cold.mp3"); err == nil {
		t.Fatal("an ambiguous name was routed instead of refused")
	}
	if _, err := matchAlbumTrack(tracks, "Something Else.mp3"); err == nil {
		t.Fatal("a stranger was routed instead of refused")
	}
}

// TestImportAlbumTakeDropRoutesToTheTrack is the end-to-end half: a dropped
// batch member ends up as a take on the track its name says, through the
// same road a single-track drop takes (#57).
func TestImportAlbumTakeDropRoutesToTheTrack(t *testing.T) {
	app := newTestApp(t)
	artist, err := app.SaveArtist(Artist{Name: "Cassette Motel", Inspirations: []Inspiration{}})
	if err != nil {
		t.Fatal(err)
	}
	album, err := app.SaveAlbum(Album{ArtistID: artist.ID, Name: "Season 3"})
	if err != nil {
		t.Fatal(err)
	}
	track, err := app.SaveTrack(Track{AlbumID: album.ID, Name: "Cold Open"})
	if err != nil {
		t.Fatal(err)
	}

	payload := base64.StdEncoding.EncodeToString([]byte("not really mp3 bytes"))
	got, err := app.ImportAlbumTakeDrop(album.ID, "Cold Open.mp3",
		"data:audio/mpeg;base64,"+payload)
	if err != nil {
		t.Fatalf("the drop was not routed: %v", err)
	}
	if got.ID != track.ID || len(got.Files) != 1 {
		t.Fatalf("landed on the wrong track or without a file: %+v", got)
	}
	if _, err := os.Stat(got.Files[0].Path); err != nil {
		t.Fatalf("the take is not on disk: %v", err)
	}
}

// TestSeveralFilesLandOnOneTrackAsTakes reproduces #84: a Suno session ends
// with more than one download of the same song - the browser names the second
// "Name (1).mp3" - and importing the batch must file every one of them as a
// take on that track, first in chosen, none overwriting another.
func TestSeveralFilesLandOnOneTrackAsTakes(t *testing.T) {
	app := newTestApp(t)
	artist, err := app.SaveArtist(Artist{Name: "Cassette Motel", Inspirations: []Inspiration{}})
	if err != nil {
		t.Fatal(err)
	}
	album, err := app.SaveAlbum(Album{ArtistID: artist.ID, Name: "Season 3"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.SaveTrack(Track{AlbumID: album.ID, Name: "Cold Open"}); err != nil {
		t.Fatal(err)
	}

	payload := base64.StdEncoding.EncodeToString([]byte("not really mp3 bytes"))
	var got Track
	for _, name := range []string{"Cold Open.mp3", "Cold Open (1).mp3", "Cold Open (2).mp3"} {
		got, err = app.ImportAlbumTakeDrop(album.ID, name, "data:audio/mpeg;base64,"+payload)
		if err != nil {
			t.Fatalf("%s was not routed: %v", name, err)
		}
	}
	if len(got.Files) != 3 {
		t.Fatalf("want 3 takes on the track, got %d: %+v", len(got.Files), got.Files)
	}
	paths := map[string]bool{}
	chosen := 0
	for _, f := range got.Files {
		paths[f.Path] = true
		if f.Chosen {
			chosen++
		}
	}
	if len(paths) != 3 {
		t.Fatalf("takes overwrote each other on disk: %+v", got.Files)
	}
	if chosen != 1 {
		t.Fatalf("want exactly one chosen take, got %d", chosen)
	}
}
