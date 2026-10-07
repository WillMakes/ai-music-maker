package app

import (
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	// The description is the render's timestamps, song by song (#120).
	if !strings.HasPrefix(rendered.VideoDescription, "Night Drive by Cassette Motel\n\n0:00 Neon Rain\n3:20 Dawn") {
		t.Errorf("description = %q", rendered.VideoDescription)
	}
	// A save of the album's other fields must not lose the video.
	rendered.Name = "Night Drive (Deluxe)"
	resaved, err := app.SaveAlbum(rendered)
	if err != nil {
		t.Fatal(err)
	}
	if resaved.VideoFile != got.Out || resaved.VideoDescription != rendered.VideoDescription {
		t.Errorf("saving the album cleared its video: %q", resaved.VideoFile)
	}
}

// TestMusicVideoStyleFromTheSkill is #119: with an AI connected, the look comes
// from the music-video skill read against the record; the answer is held to
// the fixed options, and anything that goes wrong draws the default look
// rather than refusing the render.
func TestMusicVideoStyleFromTheSkill(t *testing.T) {
	app := newTestApp(t)
	artist, _ := app.SaveArtist(Artist{Name: "Cassette Motel", BaseStyle: "gated drums, FM bass", Inspirations: []Inspiration{}})
	album, _ := app.SaveAlbum(Album{ArtistID: artist.ID, Name: "Night Drive", ArtworkPrompt: "neon cyan on black"})

	// No AI connected: the default, and the model is never asked.
	asked := false
	previous := askVideoStyle
	t.Cleanup(func() { askVideoStyle = previous })
	askVideoStyle = func(*App, context.Context, string, []chatMsg) (string, error) {
		asked = true
		return "", nil
	}
	if got := app.musicVideoStyle(artist, album); got != video.DefaultStyle || asked {
		t.Fatalf("without AI: style %+v, asked %v", got, asked)
	}

	withCLIs(t, "claude")
	if _, err := app.ConnectAIAccount(serviceAnthropic); err != nil {
		t.Fatal(err)
	}
	var brief, record string
	askVideoStyle = func(_ *App, _ context.Context, system string, turns []chatMsg) (string, error) {
		brief, record = system, turns[len(turns)-1].Text
		return "```json\n" + `{"visualizer":"bars","color":"#00e5ff","background":"black","layout":"split","fade":0.5}` + "\n```", nil
	}
	got := app.musicVideoStyle(artist, album)
	want := video.Style{Visualizer: "bars", Color: "00E5FF", Background: "black", Layout: "split", Fade: 0.5}
	if got != want {
		t.Errorf("style = %+v, want %+v", got, want)
	}
	if !strings.Contains(brief, "# Music video") {
		t.Errorf("the brief is not the music-video skill: %.80q", brief)
	}
	for _, part := range []string{"Night Drive", "gated drums", "neon cyan on black"} {
		if !strings.Contains(record, part) {
			t.Errorf("the record sent to the model lacks %q", part)
		}
	}

	// Off the menu: each bad field falls back on its own.
	askVideoStyle = func(*App, context.Context, string, []chatMsg) (string, error) {
		return `{"visualizer":"lasers","color":"red","background":"blur","layout":"split","fade":40}`, nil
	}
	got = app.musicVideoStyle(artist, album)
	if got.Visualizer != "waves" || got.Color != "FFFFFF" || got.Layout != "split" || got.Fade != 1 {
		t.Errorf("an off-menu answer reached the render: %+v", got)
	}

	// Not JSON at all, or a failed call: the default.
	askVideoStyle = func(*App, context.Context, string, []chatMsg) (string, error) {
		return "I think bars would look great!", nil
	}
	if got := app.musicVideoStyle(artist, album); got != video.DefaultStyle {
		t.Errorf("prose answer: %+v", got)
	}
}

// TestTheVideoIsServedForTheAlbumPage is #143: the album carries a URL for its
// rendered video, and the bridge streams the file behind it to a request with
// the token, and to nothing else.
func TestTheVideoIsServedForTheAlbumPage(t *testing.T) {
	app := newTestApp(t)
	artist, err := app.SaveArtist(Artist{Name: "Cassette Motel", Inspirations: []Inspiration{}})
	if err != nil {
		t.Fatal(err)
	}
	album, err := app.SaveAlbum(Album{ArtistID: artist.ID, Name: "Night Drive"})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := app.GetAlbum(album.ID); got.VideoURL != "" {
		t.Fatalf("an album with no video has a URL: %q", got.VideoURL)
	}

	file := filepath.Join(t.TempDir(), "Night Drive.mp4")
	if err := os.WriteFile(file, []byte("not really a video"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := app.store.setAlbumVideo(album.ID, file, ""); err != nil {
		t.Fatal(err)
	}
	got, err := app.GetAlbum(album.ID)
	if err != nil || got.VideoURL == "" {
		t.Fatalf("no video URL after a render: %q, %v", got.VideoURL, err)
	}

	link, err := url.Parse(got.VideoURL)
	if err != nil {
		t.Fatal(err)
	}
	serve := func(target string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		app.bridgeVideo(rec, httptest.NewRequest(http.MethodGet, target, nil))
		return rec
	}
	if rec := serve(link.RequestURI()); rec.Code != http.StatusOK || rec.Body.String() != "not really a video" {
		t.Fatalf("the video URL answered %d %q", rec.Code, rec.Body.String())
	}
	if rec := serve(link.Path); rec.Code != http.StatusUnauthorized {
		t.Errorf("without the token the video answered %d", rec.Code)
	}
	if rec := serve("/video/alb_nothing?" + link.RawQuery); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown album answered %d", rec.Code)
	}
}
