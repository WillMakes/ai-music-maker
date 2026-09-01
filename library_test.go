package main

import "testing"

func TestLibraryTreeShapesTheRail(t *testing.T) {
	app := newTestApp(t)

	solo, err := app.SaveArtist(Artist{Name: "Solo, No Albums", Inspirations: []Inspiration{}})
	if err != nil {
		t.Fatalf("artist: %v", err)
	}
	artist, err := app.SaveArtist(Artist{Name: "Cassette Motel", Inspirations: []Inspiration{}})
	if err != nil {
		t.Fatalf("artist: %v", err)
	}
	album, err := app.SaveAlbum(Album{ArtistID: artist.ID, Name: "Night Drive"})
	if err != nil {
		t.Fatalf("album: %v", err)
	}
	if _, err := app.SaveTrack(Track{AlbumID: album.ID, Name: "Neon Rain"}); err != nil {
		t.Fatalf("track: %v", err)
	}

	tree, err := app.LibraryTree()
	if err != nil {
		t.Fatalf("tree: %v", err)
	}
	if len(tree) != 2 {
		t.Fatalf("want both artists in the rail, got %d", len(tree))
	}

	byID := map[string]LibraryArtist{}
	for _, node := range tree {
		byID[node.ID] = node
	}
	// An artist with no albums is still in the rail — a brand-new artist must
	// not appear only once they have a record.
	if node := byID[solo.ID]; len(node.Albums) != 0 || node.Albums == nil {
		t.Fatalf("the empty artist came back wrong: %+v", node)
	}
	node := byID[artist.ID]
	if len(node.Albums) != 1 || node.Albums[0].Name != "Night Drive" {
		t.Fatalf("the album is missing from its artist: %+v", node)
	}
	if node.Albums[0].TrackCount != 1 {
		t.Fatalf("the album's badge is wrong: %d", node.Albums[0].TrackCount)
	}
	if node.Albums[0].UpdatedAt == "" {
		t.Fatal("the album carries no updatedAt for the resume ordering")
	}
}

// TestLibraryTreeCarriesTheArtwork pins #81: the rail draws each album's cover
// as its thumbnail, so the tree must hand over the same bridge URL the album
// page uses — and an empty one, not a broken one, for a record with no cover.
func TestLibraryTreeCarriesTheArtwork(t *testing.T) {
	app := newTestApp(t)

	artist, err := app.SaveArtist(Artist{Name: "Cassette Motel", Inspirations: []Inspiration{}})
	if err != nil {
		t.Fatalf("artist: %v", err)
	}
	covered, err := app.SaveAlbum(Album{ArtistID: artist.ID, Name: "Night Drive",
		ArtworkFile: "night-drive.png"})
	if err != nil {
		t.Fatalf("album: %v", err)
	}
	bare, err := app.SaveAlbum(Album{ArtistID: artist.ID, Name: "Demos"})
	if err != nil {
		t.Fatalf("album: %v", err)
	}

	tree, err := app.LibraryTree()
	if err != nil {
		t.Fatalf("tree: %v", err)
	}
	urls := map[string]string{}
	for _, node := range tree {
		for _, album := range node.Albums {
			urls[album.ID] = album.ArtworkURL
		}
	}
	if urls[covered.ID] == "" {
		t.Fatal("the covered album came back with no artwork URL")
	}
	if urls[covered.ID] != covered.ArtworkURL {
		t.Fatalf("the rail and the album page disagree about the cover: %s vs %s",
			urls[covered.ID], covered.ArtworkURL)
	}
	if urls[bare.ID] != "" {
		t.Fatalf("the coverless album claims a cover: %s", urls[bare.ID])
	}
}
