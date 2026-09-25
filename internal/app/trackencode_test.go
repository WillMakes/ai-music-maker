package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sunomaker/internal/audio"
)

// importFixture is one track on one album, and a WAV to import onto it.
func importFixture(t *testing.T, app *App) (Track, string) {
	t.Helper()
	artist, err := app.SaveArtist(Artist{Name: "Cassette Motel", Inspirations: []Inspiration{}})
	if err != nil {
		t.Fatal(err)
	}
	album, err := app.SaveAlbum(Album{ArtistID: artist.ID, Name: "Late Checkout"})
	if err != nil {
		t.Fatal(err)
	}
	track, err := app.SaveTrack(Track{AlbumID: album.ID, Name: "Ice Machine"})
	if err != nil {
		t.Fatal(err)
	}
	wav := filepath.Join(t.TempDir(), "Ice Machine.wav")
	if err := os.WriteFile(wav, testWAV(2), 0o600); err != nil {
		t.Fatal(err)
	}
	return track, wav
}

// filesIn lists the file names in dir.
func filesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// TestWAVTakesAreFiledAsMP3 is #93: a WAV imported onto a track is kept as an
// MP3, recorded as an MP3, and the WAV itself is not kept beside it.
func TestWAVTakesAreFiledAsMP3(t *testing.T) {
	app := newTestApp(t)
	track, wav := importFixture(t, app)
	encoded := []byte("an mp3, as far as this test cares")
	calls := 0
	audio.EncodeMP3 = func(src, dest string) error {
		calls++
		if src != wav || filepath.Ext(dest) != ".mp3" {
			t.Errorf("asked to encode %s into %s", src, dest)
		}
		return os.WriteFile(dest, encoded, 0o600)
	}

	got, err := app.ImportTrackFile(track.ID, wav, "")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(got.Files) != 1 {
		t.Fatalf("want one encode and one take, got %d and %d", calls, len(got.Files))
	}
	file := got.Files[0]
	if filepath.Ext(file.Name) != ".mp3" || filepath.Ext(file.Path) != ".mp3" {
		t.Errorf("the take was not filed as an MP3: %+v", file)
	}
	if file.Bytes != int64(len(encoded)) {
		t.Errorf("the size recorded is not the MP3's: %d", file.Bytes)
	}
	if file.Label != "Ice Machine" {
		t.Errorf("the label should still come from the download's name: %q", file.Label)
	}
	for _, name := range filesIn(t, filepath.Dir(file.Path)) {
		if strings.HasSuffix(name, ".wav") {
			t.Errorf("the WAV was kept beside the MP3: %s", name)
		}
	}
	// The picked file is the producer's; filing a copy of it never touches it.
	if _, err := os.Stat(wav); err != nil {
		t.Errorf("the original WAV was removed: %v", err)
	}
}

// TestRecordedTakesAreFiledAsMP3: the recordings #92 makes arrive as WAV, and
// go through the same door.
func TestRecordedTakesAreFiledAsMP3(t *testing.T) {
	app := newTestApp(t)
	captureFixture(t, app, "Late Checkout", "Late Checkout")
	audio.EncodeMP3 = func(_, dest string) error { return os.WriteFile(dest, []byte("mp3"), 0o600) }
	written := 0

	got, err := app.captureSunoPlay(SunoPlay{ClipID: testClip, Title: "Late Checkout"}, recording(&written))
	if err != nil || got.Outcome != "filed" {
		t.Fatalf("not filed: %+v, %v", got, err)
	}
	track, _ := app.store.track(got.TrackID)
	if len(track.Files) != 1 || filepath.Ext(track.Files[0].Name) != ".mp3" ||
		track.Files[0].SunoClip != testClip {
		t.Errorf("the recording was not filed as an MP3 of its clip: %+v", track.Files)
	}
}

// TestAWAVTheEncoderCannotTakeIsKept: no ffmpeg, or ffmpeg failing, files the
// WAV as it is and leaves no half-written MP3 behind.
func TestAWAVTheEncoderCannotTakeIsKept(t *testing.T) {
	for name, encoder := range map[string]func(string, string) error{
		"no encoder": func(string, string) error { return audio.ErrNoEncoder },
		"encoder fails": func(_, dest string) error {
			_ = os.WriteFile(dest, []byte("half"), 0o600)
			_ = os.Remove(dest)
			return errors.New("ffmpeg could not encode the take")
		},
	} {
		t.Run(name, func(t *testing.T) {
			app := newTestApp(t)
			track, wav := importFixture(t, app)
			audio.EncodeMP3 = encoder

			got, err := app.ImportTrackFile(track.ID, wav, "")
			if err != nil {
				t.Fatalf("an encoder problem failed the import: %v", err)
			}
			file := got.Files[0]
			if filepath.Ext(file.Name) != ".wav" || file.Seconds != 2 {
				t.Errorf("the WAV should have been kept as it was: %+v", file)
			}
			for _, n := range filesIn(t, filepath.Dir(file.Path)) {
				if strings.HasSuffix(n, ".mp3") {
					t.Errorf("an MP3 was left behind: %s", n)
				}
			}
		})
	}
}

// TestOtherFormatsAreNotReencoded: an MP3 is already an MP3, and a FLAC is a
// producer's lossless master — neither goes near the encoder.
func TestOtherFormatsAreNotReencoded(t *testing.T) {
	app := newTestApp(t)
	track, _ := importFixture(t, app)
	audio.EncodeMP3 = func(src, _ string) error {
		t.Errorf("asked to encode %s", src)
		return nil
	}
	for _, name := range []string{"take.mp3", "master.flac"} {
		src := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(src, []byte("audio"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := app.ImportTrackFile(track.ID, src, "")
		if err != nil {
			t.Fatal(err)
		}
		last := got.Files[len(got.Files)-1]
		if filepath.Ext(last.Name) != filepath.Ext(name) {
			t.Errorf("%s was filed as %s", name, last.Name)
		}
	}
}
