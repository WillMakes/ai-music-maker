package main

import "testing"

func TestSaveArtistRequiresName(t *testing.T) {
	app := newTestApp(t)
	if _, err := app.SaveArtist(Artist{Name: "   "}); err == nil {
		t.Fatal("a whitespace name was accepted")
	}
}

func TestSaveArtistReturnsTheStoredRecord(t *testing.T) {
	app := newTestApp(t)

	saved, err := app.SaveArtist(Artist{
		Name:      "  Static Field  ",
		Summary:   "Slow synths for long drives",
		BaseStyle: "Instruments: analogue polysynth pads",
		Inspirations: []Inspiration{
			{URL: "https://music.youtube.com/watch?v=one", Note: "the drum sound only"},
			// An empty row is one the producer started and abandoned; saving it
			// would put a reference to nothing on the record.
			{URL: "   ", Note: "nothing"},
		},
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	if saved.Name != "Static Field" {
		t.Errorf("name = %q, want the trimmed form", saved.Name)
	}
	if saved.Slug != "static-field" {
		t.Errorf("slug = %q, want static-field", saved.Slug)
	}
	if saved.ID == "" || saved.CreatedAt == "" || saved.UpdatedAt == "" {
		t.Error("the caller was not told the id or the stamps it could not have known")
	}
	if len(saved.Inspirations) != 1 {
		t.Fatalf("kept %d references, want 1 — the empty row should be dropped",
			len(saved.Inspirations))
	}
	if saved.Inspirations[0].ID == "" {
		t.Error("the reference was stored without an id")
	}
}

func TestRenamingAnArtistMovesTheSlug(t *testing.T) {
	app := newTestApp(t)

	artist, err := app.SaveArtist(Artist{Name: "Working Title"})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	artist.Name = "Static Field"
	renamed, err := app.SaveArtist(artist)
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	// The slug follows the name on every save, not only the first: an artist
	// renamed before their first album should not keep a folder named after the
	// working title.
	if renamed.Slug != "static-field" {
		t.Errorf("slug = %q, want static-field", renamed.Slug)
	}
	if renamed.ID != artist.ID {
		t.Error("the rename made a second artist")
	}
}

func TestInspirationsAreReplacedWholesale(t *testing.T) {
	app := newTestApp(t)

	artist, err := app.SaveArtist(Artist{
		Name: "Static Field",
		Inspirations: []Inspiration{
			{URL: "https://one"}, {URL: "https://two"}, {URL: "https://three"},
		},
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	artist.Inspirations = artist.Inspirations[:1]
	trimmed, err := app.SaveArtist(artist)
	if err != nil {
		t.Fatalf("resave: %v", err)
	}
	if len(trimmed.Inspirations) != 1 {
		t.Fatalf("kept %d references, want 1 — a removed row must not survive",
			len(trimmed.Inspirations))
	}
	if trimmed.Inspirations[0].URL != "https://one" {
		t.Errorf("kept %q, want the one that was left", trimmed.Inspirations[0].URL)
	}
}

func TestDeleteArtistTakesEverythingUnderIt(t *testing.T) {
	app := newTestApp(t)

	artist, err := app.SaveArtist(Artist{Name: "Static Field"})
	if err != nil {
		t.Fatalf("artist: %v", err)
	}
	album, err := app.SaveAlbum(Album{ArtistID: artist.ID, Name: "Volume One"})
	if err != nil {
		t.Fatalf("album: %v", err)
	}
	if _, err := app.SaveTrack(Track{AlbumID: album.ID, Name: "Opener"}); err != nil {
		t.Fatalf("track: %v", err)
	}

	if err := app.DeleteArtist(artist.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if artists, _ := app.ListArtists(); len(artists) != 0 {
		t.Errorf("%d artists left", len(artists))
	}
	// Every level, not just the top: an album whose artist is gone is invisible
	// on every page (they all list by artist) and undeletable from the UI.
	if albums, _ := app.ListAlbums(artist.ID); len(albums) != 0 {
		t.Errorf("%d albums left", len(albums))
	}
	if tracks, _ := app.ListTracks(album.ID); len(tracks) != 0 {
		t.Errorf("%d tracks left", len(tracks))
	}
}

func TestChangeScopesAreReported(t *testing.T) {
	app := newTestApp(t)

	seen := map[string]bool{}
	app.store.onChange = func(scope string) { seen[scope] = true }

	artist, err := app.SaveArtist(Artist{Name: "Static Field"})
	if err != nil {
		t.Fatalf("artist: %v", err)
	}
	if !seen[scopeArtists] {
		t.Error("saving an artist reported no scope, so open pages would not refresh")
	}

	if _, err := app.SaveAlbum(Album{ArtistID: artist.ID, Name: "Volume One"}); err != nil {
		t.Fatalf("album: %v", err)
	}
	if !seen[scopeAlbums] {
		t.Error("saving an album reported no scope")
	}
}

// TestInspirationCarriesTheSongsIdentity: the metadata analysis grabs (#51)
// has to survive the save/read round trip, or the artist page forgets what
// every reference is each time it reloads.
func TestInspirationCarriesTheSongsIdentity(t *testing.T) {
	app := newTestApp(t)
	artist, err := app.SaveArtist(Artist{
		Name: "Cassette Motel",
		Inspirations: []Inspiration{{
			URL:        "https://youtu.be/abc",
			Title:      "Night Drive",
			Artist:     "Kavinsky",
			Album:      "OutRun",
			ArtworkURL: "https://img.example/a.jpg",
			Note:       "**Genre** — synthwave",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := artist.Inspirations[0]
	if got.Artist != "Kavinsky" || got.Album != "OutRun" ||
		got.ArtworkURL != "https://img.example/a.jpg" || got.Title != "Night Drive" {
		t.Fatalf("the identity did not round-trip: %+v", got)
	}
}
