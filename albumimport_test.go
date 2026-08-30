package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImportAlbumBringsTheFolderIn(t *testing.T) {
	app := newTestApp(t)
	artist, err := app.SaveArtist(Artist{Name: "Cassette Motel", Inspirations: []Inspiration{}})
	if err != nil {
		t.Fatalf("artist: %v", err)
	}

	// A folder the way downloads arrive: numbered with different separators,
	// out of name-sort order past nine, with a non-audio file to ignore.
	dir := filepath.Join(t.TempDir(), "Night Drive")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"2 - Static.mp3", "10. Dawn.mp3", "01 Neon Rain.wav", "cover.png"} {
		if err := os.WriteFile(filepath.Join(dir, file), []byte("bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	album, err := app.ImportAlbum(artist.ID, "", dir)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if album.Name != "Night Drive" {
		t.Fatalf("the album is not named after its folder: %q", album.Name)
	}

	tracks, err := app.ListTracks(album.ID)
	if err != nil {
		t.Fatalf("tracks: %v", err)
	}
	if len(tracks) != 3 {
		t.Fatalf("want a track per audio file, got %d", len(tracks))
	}
	// Running order from the numbers, not from a string sort that would put 10
	// before 2 — and names without their numbers and extensions.
	want := []string{"Neon Rain", "Static", "Dawn"}
	for i, track := range tracks {
		if track.Name != want[i] {
			t.Fatalf("track %d is %q, want %q", i, track.Name, want[i])
		}
		if len(track.Files) != 1 {
			t.Fatalf("%s did not get its file: %+v", track.Name, track.Files)
		}
		if !track.Files[0].Chosen {
			t.Fatalf("%s's only take is not the chosen one", track.Name)
		}
	}
}

func TestImportAlbumByNameAloneAndRefusals(t *testing.T) {
	app := newTestApp(t)
	artist, err := app.SaveArtist(Artist{Name: "Cassette Motel", Inspirations: []Inspiration{}})
	if err != nil {
		t.Fatalf("artist: %v", err)
	}

	// A name with no folder is the blank album — content this app cannot read
	// yet still deserves a record.
	album, err := app.ImportAlbum(artist.ID, "Unreleased", "")
	if err != nil {
		t.Fatalf("blank import: %v", err)
	}
	if album.Name != "Unreleased" {
		t.Fatalf("wrong name: %q", album.Name)
	}

	if _, err := app.ImportAlbum(artist.ID, "", ""); err == nil {
		t.Fatal("nothing at all was accepted")
	}
	if _, err := app.ImportAlbum(artist.ID, "", filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("a folder that does not exist was imported")
	}
	empty := t.TempDir()
	if _, err := app.ImportAlbum(artist.ID, "", empty); err == nil {
		t.Fatal("a folder with no audio was imported")
	}
}

func TestTrackNameFromFileReadsTheCommonShapes(t *testing.T) {
	cases := map[string]string{
		"01 Neon Rain.mp3":  "Neon Rain",
		"1. Neon Rain.flac": "Neon Rain",
		"02 - Static.wav":   "Static",
		"Dawn.m4a":          "Dawn",
		"07.mp3":            "07",
	}
	for file, want := range cases {
		if got := trackNameFromFile(file); got != want {
			t.Errorf("%s -> %q, want %q", file, got, want)
		}
	}
}

// TestGenerateAlbumStyleRefusals is #38's plumbing: an album with no prompts
// has nothing to amalgamate, and no connected provider fails with the message
// rather than a half-written style.
func TestGenerateAlbumStyleRefusals(t *testing.T) {
	app := newTestApp(t)
	artist, err := app.SaveArtist(Artist{Name: "Cassette Motel", Inspirations: []Inspiration{}})
	if err != nil {
		t.Fatalf("artist: %v", err)
	}
	album, err := app.SaveAlbum(Album{ArtistID: artist.ID, Name: "Night Drive"})
	if err != nil {
		t.Fatalf("album: %v", err)
	}

	// Tracks with no prompts do not count as material.
	if _, err := app.SaveTrack(Track{AlbumID: album.ID, Name: "Untitled"}); err != nil {
		t.Fatalf("track: %v", err)
	}
	if _, err := app.GenerateAlbumStyle(album.ID); err == nil {
		t.Fatal("a promptless album was amalgamated")
	}

	// With a prompt but no provider, the failure is the provider's absence.
	if _, err := app.SaveTrack(Track{
		AlbumID: album.ID, Name: "Neon Rain", Prompt: "synthwave, 110 BPM",
	}); err != nil {
		t.Fatalf("track: %v", err)
	}
	if _, err := app.GenerateAlbumStyle(album.ID); err == nil {
		t.Fatal("a style was generated with no provider connected")
	}
}
