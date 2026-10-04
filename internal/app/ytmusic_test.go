package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aimusicmaker/internal/audio"
	"aimusicmaker/internal/ytmusic"
)

const ytmHeaders = "Cookie: SAPISID=abc; __Secure-3PAPISID=abc\nX-Goog-AuthUser: 0\nUser-Agent: Test"

// TestUploadAlbumToYouTubeMusic is #124's binding: a paste is checked before
// it is kept and never shown back, every take goes up tagged for its place on
// the record, and a failure partway says how many are already in the library.
func TestUploadAlbumToYouTubeMusic(t *testing.T) {
	app := newTestApp(t)
	prevAccount, prevUpload, prevTag := ytmusicAccount, ytmusicUpload, audio.TagForUpload
	t.Cleanup(func() { ytmusicAccount, ytmusicUpload, audio.TagForUpload = prevAccount, prevUpload, prevTag })
	ytmusicAccount = func(context.Context, ytmusic.Auth) (string, error) { return "Will", nil }
	var tagged []audio.Tags
	audio.TagForUpload = func(src, dest string, tags audio.Tags) error {
		tagged = append(tagged, tags)
		return os.WriteFile(dest, []byte("mp3"), 0o600)
	}
	var sent []string
	failOn := ""
	ytmusicUpload = func(_ context.Context, auth ytmusic.Auth, path string, progress func(float64)) error {
		if auth.Cookie == "" {
			t.Error("uploaded without the session")
		}
		if failOn != "" && strings.Contains(path, failOn) {
			return errors.New("refused")
		}
		sent = append(sent, filepath.Base(path))
		progress(1)
		return nil
	}

	artist, _ := app.SaveArtist(Artist{Name: "Cassette Motel", Inspirations: []Inspiration{}})
	album, _ := app.SaveAlbum(Album{ArtistID: artist.ID, Name: "Night Drive"})
	dir := t.TempDir()
	for _, name := range []string{"Neon Rain", "Dawn: Reprise"} {
		track, _ := app.SaveTrack(Track{AlbumID: album.ID, Name: name})
		path := filepath.Join(dir, safeFileName(name)+".mp3")
		_ = os.WriteFile(path, []byte("bytes"), 0o600)
		if _, err := app.ImportTrackFile(track.ID, path, ""); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := app.UploadAlbumToYouTubeMusic(album.ID); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("uploaded with no session: %v", err)
	}
	if _, err := app.ConnectYouTubeMusic("Accept: */*"); err == nil {
		t.Fatal("headers with no cookie were kept")
	}
	status, err := app.ConnectYouTubeMusic(ytmHeaders)
	if err != nil || !status.Connected || status.Account != "Will" {
		t.Fatalf("status = %+v, %v", status, err)
	}
	for _, tool := range []string{"get_setting", "set_setting"} {
		if out := callTool(t, app, tool, map[string]any{"key": keyYTMusic, "value": "{}"}); strings.Contains(out, "SAPISID") || !strings.Contains(out, "get_youtube_music") {
			t.Errorf("%s handed the session over: %s", tool, out)
		}
	}

	uploaded, err := app.UploadAlbumToYouTubeMusic(album.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 || sent[0] != "01 Neon Rain.mp3" || sent[1] != "02 Dawn- Reprise.mp3" {
		t.Errorf("sent %v", sent)
	}
	if tagged[1].Title != "Dawn: Reprise" || tagged[1].Track != 2 || tagged[1].TrackCount != 2 ||
		tagged[1].Artist != "Cassette Motel" || tagged[1].Album != "Night Drive" {
		t.Errorf("tags = %+v", tagged[1])
	}
	if uploaded.YTMusicUploadedAt == "" {
		t.Error("the album does not record the upload")
	}

	sent, failOn = nil, "Dawn"
	_, err = app.UploadAlbumToYouTubeMusic(album.ID)
	if err == nil || !strings.Contains(err.Error(), "Dawn: Reprise") || !strings.Contains(err.Error(), "first 1") {
		t.Errorf("a failure partway: %v", err)
	}

	if status, _ := app.DisconnectYouTubeMusic(); status.Connected {
		t.Error("still connected after disconnecting")
	}
}
