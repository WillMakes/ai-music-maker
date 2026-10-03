package app

import (
	"os"
	"path/filepath"
	"strings"
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

	// One track plays just that track (#106). #55 had it queue the whole
	// record from the pressed track; the producer asked for the song on its
	// own, with the album's Play for the whole record.
	fromTrack, err := app.Playlist("track", third.ID)
	if err != nil {
		t.Fatalf("track playlist: %v", err)
	}
	if len(fromTrack) != 1 || fromTrack[0].TrackID != third.ID {
		t.Fatalf("a track press should queue only that track, got %+v", fromTrack)
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

// TestPlaylistSkipsTakesWhoseFileIsGone is #105. A take is filed by absolute
// path, and files moved or deleted outside the app left rows pointing at
// nothing. The queue was built from them anyway, so pressing play started a
// queue in which nearly every song failed with "This take could not be played".
// Now a take whose file is missing says so, the queue plays a take that still
// exists, and a record with nothing left to play says why.
func TestPlaylistSkipsTakesWhoseFileIsGone(t *testing.T) {
	app := newTestApp(t)
	artist, err := app.SaveArtist(Artist{Name: "Primlocked", Inspirations: []Inspiration{}})
	if err != nil {
		t.Fatal(err)
	}
	album, err := app.SaveAlbum(Album{ArtistID: artist.ID, Name: "The Long Quiet"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	makeTake := func(name string) string {
		path := filepath.Join(dir, name+".mp3")
		if err := os.WriteFile(path, []byte("bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	// Two takes; the chosen one's file is then deleted behind the app's back.
	track, err := app.SaveTrack(Track{AlbumID: album.ID, Name: "It Knows"})
	if err != nil {
		t.Fatal(err)
	}
	withFirst, err := app.ImportTrackFile(track.ID, makeTake("first"), "")
	if err != nil {
		t.Fatal(err)
	}
	withBoth, err := app.ImportTrackFile(track.ID, makeTake("second"), "")
	if err != nil {
		t.Fatal(err)
	}
	gone := withFirst.Files[0]
	kept := withBoth.Files[1]
	if _, err := app.ChooseTrackFile(track.ID, gone.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(gone.Path); err != nil {
		t.Fatal(err)
	}

	got, err := app.GetTrack(track.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range got.Files {
		if want := f.ID == gone.ID; f.Missing != want {
			t.Errorf("take %s: missing = %v, want %v", f.Name, f.Missing, want)
		}
	}

	queue, err := app.Playlist("album", album.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 1 || !strings.Contains(queue[0].AudioURL, kept.ID) {
		t.Fatalf("the queue should play the take that still exists (%s): %+v", kept.ID, queue)
	}

	// Nothing left on disk: an error that says so, not an empty queue.
	if err := os.Remove(kept.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Playlist("album", album.ID); err == nil || !strings.Contains(err.Error(), "moved or deleted") {
		t.Fatalf("want an error about the missing files, got %v", err)
	}
}
