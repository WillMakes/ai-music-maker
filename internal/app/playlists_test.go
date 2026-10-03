package app

import (
	"os"
	"path/filepath"
	"testing"
)

// playlistFixture is an artist with one album of three tracks, each with a
// take on disk, for the playlist tests to arrange.
func playlistFixture(t *testing.T, app *App) (album Album, tracks []Track) {
	t.Helper()
	artist, err := app.SaveArtist(Artist{Name: "Cassette Motel", Inspirations: []Inspiration{}})
	if err != nil {
		t.Fatalf("artist: %v", err)
	}
	album, err = app.SaveAlbum(Album{ArtistID: artist.ID, Name: "Night Drive"})
	if err != nil {
		t.Fatalf("album: %v", err)
	}
	dir := t.TempDir()
	for _, name := range []string{"Neon Rain", "Overpass", "Dawn"} {
		track, err := app.SaveTrack(Track{AlbumID: album.ID, Name: name})
		if err != nil {
			t.Fatalf("track: %v", err)
		}
		path := filepath.Join(dir, track.ID+".mp3")
		if err := os.WriteFile(path, []byte("bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := app.ImportTrackFile(track.ID, path, ""); err != nil {
			t.Fatalf("import: %v", err)
		}
		tracks = append(tracks, track)
	}
	return album, tracks
}

func entryNames(p SavedPlaylist) []string {
	names := []string{}
	for _, e := range p.Entries {
		names = append(names, e.TrackName)
	}
	return names
}

// TestPlaylistsHoldTracksInTheOrderAdded is #107: the rail has a Playlists
// section under the artists, so a playlist is a record of its own — a name and
// tracks from any album, in the order they were put there, played in that
// order.
func TestPlaylistsHoldTracksInTheOrderAdded(t *testing.T) {
	app := newTestApp(t)
	album, tracks := playlistFixture(t, app)

	saved, err := app.SavePlaylist(SavedPlaylist{Name: "  Late drive  "})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if saved.ID == "" || saved.Name != "Late drive" || saved.Entries == nil {
		t.Fatalf("stored playlist = %+v, want an id, a trimmed name and a non-nil entry list", saved)
	}

	if _, err := app.AddToPlaylist(saved.ID, tracks[2].ID); err != nil {
		t.Fatalf("add: %v", err)
	}
	got, err := app.AddToPlaylist(saved.ID, tracks[0].ID)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if names := entryNames(got); len(names) != 2 || names[0] != "Dawn" || names[1] != "Neon Rain" {
		t.Fatalf("entries = %v, want Dawn then Neon Rain, the order they were added", names)
	}
	first := got.Entries[0]
	if first.AlbumName != album.Name || first.ArtistName != "Cassette Motel" || !first.Playable {
		t.Errorf("entry carries too little to draw: %+v", first)
	}

	// Adding a song that is already there changes nothing: a playlist is a
	// set the producer orders, and a second press must not double a song.
	again, err := app.AddToPlaylist(saved.ID, tracks[0].ID)
	if err != nil {
		t.Fatalf("add again: %v", err)
	}
	if len(again.Entries) != 2 {
		t.Errorf("a repeated add doubled the song: %v", entryNames(again))
	}

	list, err := app.ListPlaylists()
	if err != nil || len(list) != 1 || list[0].TrackCount != 2 {
		t.Fatalf("list = %+v, %v; want one playlist of two tracks", list, err)
	}

	// Playing it queues the playlist's order, not the album's.
	queue, err := app.Playlist("playlist", saved.ID)
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	if len(queue) != 2 || queue[0].Name != "Dawn" || queue[1].Name != "Neon Rain" {
		t.Fatalf("queue = %+v, want Dawn then Neon Rain", queue)
	}

	removed, err := app.RemoveFromPlaylist(saved.ID, first.ID)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if names := entryNames(removed); len(names) != 1 || names[0] != "Neon Rain" {
		t.Fatalf("after removing Dawn: %v", names)
	}
}

// TestPlaylistsFollowTheLibrary: a playlist names tracks that live elsewhere,
// so a deleted track leaves it, a rename shows through, and the list reads in
// name order however many there are.
func TestPlaylistsFollowTheLibrary(t *testing.T) {
	app := newTestApp(t)
	_, tracks := playlistFixture(t, app)

	if _, err := app.SavePlaylist(SavedPlaylist{Name: "   "}); err == nil {
		t.Error("a playlist with no name was saved")
	}

	road, err := app.SavePlaylist(SavedPlaylist{Name: "road"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.SavePlaylist(SavedPlaylist{Name: "Ambient"}); err != nil {
		t.Fatal(err)
	}
	for _, track := range tracks {
		if _, err := app.AddToPlaylist(road.ID, track.ID); err != nil {
			t.Fatalf("add: %v", err)
		}
	}

	renamed, err := app.SavePlaylist(SavedPlaylist{ID: road.ID, Name: "Road trip"})
	if err != nil || renamed.Name != "Road trip" || len(renamed.Entries) != 3 {
		t.Fatalf("rename = %+v, %v; want the new name and the tracks kept", renamed, err)
	}

	list, err := app.ListPlaylists()
	if err != nil || len(list) != 2 || list[0].Name != "Ambient" || list[1].Name != "Road trip" {
		t.Fatalf("list = %+v, %v; want Ambient then Road trip, by name", list, err)
	}

	if err := app.DeleteTrack(tracks[1].ID); err != nil {
		t.Fatal(err)
	}
	after, err := app.GetPlaylist(road.ID)
	if err != nil {
		t.Fatal(err)
	}
	if names := entryNames(after); len(names) != 2 || names[0] != "Neon Rain" || names[1] != "Dawn" {
		t.Errorf("after deleting Overpass: %v", names)
	}

	if _, err := app.AddToPlaylist(road.ID, "trk_nothing"); err == nil {
		t.Error("a track that does not exist was added")
	}

	if err := app.DeletePlaylist(road.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := app.GetPlaylist(road.ID); err == nil {
		t.Error("a deleted playlist could still be read")
	}
	if list, _ := app.ListPlaylists(); len(list) != 1 {
		t.Errorf("list after delete = %+v", list)
	}
}
