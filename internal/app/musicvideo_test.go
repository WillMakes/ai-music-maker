package app

import (
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aimusicmaker/internal/video"
)

// TestRenderAlbumVideo is #118's binding: it refuses an album that is not
// ready — no cover, a track with no official take — naming what is missing,
// and otherwise hands the render the takes in running order with the gap from
// Settings, and records the file on the album.
func TestRenderAlbumVideo(t *testing.T) {
	app := newTestApp(t)
	artist, err := app.SaveArtist(Artist{Name: "Cassette Motel", Inspirations: []Inspiration{}})
	if err != nil {
		t.Fatal(err)
	}
	album, err := app.SaveAlbum(Album{ArtistID: artist.ID, Name: "Night Drive"})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	take := func(name string) string {
		path := filepath.Join(dir, name+".mp3")
		if err := os.WriteFile(path, []byte("bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	first, _ := app.SaveTrack(Track{AlbumID: album.ID, Name: "Neon Rain"})
	second, _ := app.SaveTrack(Track{AlbumID: album.ID, Name: "Dawn"})
	if _, err := app.ImportTrackFile(first.ID, take("neon"), ""); err != nil {
		t.Fatal(err)
	}
	// Two takes: the first import is starred on arrival, and the producer
	// stars the second below.
	if _, err := app.ImportTrackFile(second.ID, take("dawn-a"), "A"); err != nil {
		t.Fatal(err)
	}
	dawn, err := app.ImportTrackFile(second.ID, take("dawn-b"), "B")
	if err != nil {
		t.Fatal(err)
	}

	// Written but not made yet.
	unmade, _ := app.SaveTrack(Track{AlbumID: album.ID, Name: "Unmade"})

	var got video.Spec
	renderVideo = func(_ context.Context, spec video.Spec, progress func(float64)) (video.Rendered, error) {
		got = spec
		progress(0.5)
		progress(1)
		return video.Rendered{Starts: []float64{0, 200}}, os.WriteFile(spec.Out, []byte("mp4"), 0o600)
	}

	if _, err := app.RenderAlbumVideo(album.ID); err == nil || !strings.Contains(err.Error(), "no cover") {
		t.Fatalf("an album without a cover rendered: %v", err)
	}

	coverPath := filepath.Join(dir, "cover.png")
	f, _ := os.Create(coverPath)
	_ = png.Encode(f, image.NewRGBA(image.Rect(0, 0, 8, 8)))
	f.Close()
	if _, err := app.ImportAlbumArtwork(album.ID, coverPath); err != nil {
		t.Fatal(err)
	}

	_, err = app.RenderAlbumVideo(album.ID)
	if err == nil || !strings.Contains(err.Error(), "Unmade") || strings.Contains(err.Error(), "Neon Rain") {
		t.Fatalf("want a refusal naming only Unmade, got %v", err)
	}
	if err := app.DeleteTrack(unmade.ID); err != nil {
		t.Fatal(err)
	}

	// Star Dawn's second take: the video uses the starred one.
	var chosenB, chosenPath string
	for _, f := range dawn.Files {
		if f.Label == "B" {
			chosenB, chosenPath = f.ID, f.Path
		}
	}
	if _, err := app.ChooseTrackFile(second.ID, chosenB); err != nil {
		t.Fatal(err)
	}
	if err := app.SetSetting(keyMusicVideoGap, "2.5"); err != nil {
		t.Fatal(err)
	}

	rendered, err := app.RenderAlbumVideo(album.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Songs) != 2 || got.Songs[0].Title != "Neon Rain" || got.Songs[1].Audio != chosenPath {
		t.Errorf("songs = %+v, want Neon Rain then Dawn's starred take", got.Songs)
	}
	if got.Gap != 2.5 {
		t.Errorf("gap = %v, want the setting's 2.5", got.Gap)
	}
	if rendered.VideoFile == "" || rendered.VideoFile != got.Out {
		t.Errorf("the album records %q, the render wrote %q", rendered.VideoFile, got.Out)
	}
	// A save of the album's other fields must not lose the video.
	rendered.Name = "Night Drive (Deluxe)"
	resaved, err := app.SaveAlbum(rendered)
	if err != nil {
		t.Fatal(err)
	}
	if resaved.VideoFile != got.Out {
		t.Errorf("saving the album cleared its video: %q", resaved.VideoFile)
	}
}
