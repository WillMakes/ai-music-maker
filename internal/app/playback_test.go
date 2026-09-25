package app

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPlaylistBuildsTheQueue is #45's ordering contract: an album plays in
// running order, an artist plays album by album, and a track with no takes is
// skipped rather than silencing the whole queue.
func TestPlaylistBuildsTheQueue(t *testing.T) {
	app := newTestApp(t)
	artist, err := app.SaveArtist(Artist{Name: "Cassette Motel", Inspirations: []Inspiration{}})
	if err != nil {
		t.Fatalf("artist: %v", err)
	}
	album, err := app.SaveAlbum(Album{ArtistID: artist.ID, Name: "Night Drive"})
	if err != nil {
		t.Fatalf("album: %v", err)
	}

	dir := t.TempDir()
	makeTake := func(name string) string {
		path := filepath.Join(dir, name+".mp3")
		if err := os.WriteFile(path, []byte("bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	first, err := app.SaveTrack(Track{AlbumID: album.ID, Name: "Neon Rain"})
	if err != nil {
		t.Fatalf("track: %v", err)
	}
	if _, err := app.ImportTrackFile(first.ID, makeTake("neon"), ""); err != nil {
		t.Fatalf("import: %v", err)
	}
	// A written track with nothing made yet — the skip case.
	if _, err := app.SaveTrack(Track{AlbumID: album.ID, Name: "Unmade"}); err != nil {
		t.Fatalf("track: %v", err)
	}
	third, err := app.SaveTrack(Track{AlbumID: album.ID, Name: "Dawn"})
	if err != nil {
		t.Fatalf("track: %v", err)
	}
	if _, err := app.ImportTrackFile(third.ID, makeTake("dawn"), ""); err != nil {
		t.Fatalf("import: %v", err)
	}

	queue, err := app.Playlist("album", album.ID)
	if err != nil {
		t.Fatalf("playlist: %v", err)
	}
	if len(queue) != 2 {
		t.Fatalf("want the two made tracks, got %d", len(queue))
	}
	if queue[0].Name != "Neon Rain" || queue[1].Name != "Dawn" {
		t.Fatalf("running order lost: %q, %q", queue[0].Name, queue[1].Name)
	}
	if queue[0].AudioURL == "" || queue[0].ArtistName != "Cassette Motel" {
		t.Fatalf("entry incomplete: %+v", queue[0])
	}

	// One track queues its whole record (#55): the frontend starts at the
	// pressed track, but next and previous need the rest of the album there.
	fromTrack, err := app.Playlist("track", third.ID)
	if err != nil {
		t.Fatalf("track playlist: %v", err)
	}
	if len(fromTrack) != 2 {
		t.Fatalf("a track press should queue the record, got %d entries", len(fromTrack))
	}
	if fromTrack[1].TrackID != third.ID {
		t.Fatalf("the pressed track is missing from its own queue: %+v", fromTrack)
	}

	// The artist and everything both reach the same songs.
	for _, kind := range []string{"artist", "all"} {
		q, err := app.Playlist(kind, artist.ID)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if len(q) != 2 {
			t.Fatalf("%s queue wrong: %d", kind, len(q))
		}
	}

	if _, err := app.Playlist("nonsense", ""); err == nil {
		t.Fatal("an unknown kind was played")
	}
}
