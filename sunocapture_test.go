package main

import (
	"bytes"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

const testClip = "0f4c2b1a-9d3e-4a5b-8c7d-112233445566"

// testWAV is a real, silent WAV of the given length — 8 kHz mono 16-bit, the
// smallest a header can honestly describe — so the take's duration is read
// off it the way it would be off a recording.
func testWAV(seconds int) []byte {
	const rate, channels, bits = 8000, 1, 16
	data := make([]byte, seconds*rate*channels*bits/8)
	var b bytes.Buffer
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(36+len(data)))
	b.WriteString("WAVEfmt ")
	_ = binary.Write(&b, binary.LittleEndian, uint32(16))
	_ = binary.Write(&b, binary.LittleEndian, uint16(1))
	_ = binary.Write(&b, binary.LittleEndian, uint16(channels))
	_ = binary.Write(&b, binary.LittleEndian, uint32(rate))
	_ = binary.Write(&b, binary.LittleEndian, uint32(rate*channels*bits/8))
	_ = binary.Write(&b, binary.LittleEndian, uint16(channels*bits/8))
	_ = binary.Write(&b, binary.LittleEndian, uint16(bits))
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(data)))
	b.Write(data)
	return b.Bytes()
}

// recording stands in for the extension's upload, and counts how many
// recordings the app actually asked to have written.
func recording(written *int) func(dest string) error {
	return func(dest string) error {
		*written++
		return writeWAV(bytes.NewReader(testWAV(3)))(dest)
	}
}

// captureFixture is one artist with one album holding the named tracks.
func captureFixture(t *testing.T, app *App, albumName string, names ...string) (Album, []Track) {
	t.Helper()
	artists, _ := app.ListArtists()
	var artist Artist
	if len(artists) > 0 {
		artist = artists[0]
	} else {
		var err error
		artist, err = app.SaveArtist(Artist{Name: "Cassette Motel", Inspirations: []Inspiration{}})
		if err != nil {
			t.Fatal(err)
		}
	}
	album, err := app.SaveAlbum(Album{ArtistID: artist.ID, Name: albumName})
	if err != nil {
		t.Fatal(err)
	}
	tracks := []Track{}
	for _, name := range names {
		track, err := app.SaveTrack(Track{AlbumID: album.ID, Name: name})
		if err != nil {
			t.Fatal(err)
		}
		tracks = append(tracks, track)
	}
	return album, tracks
}

// TestHeardTakeIsRecordedOntoItsTrack is #91 and #92: a song recorded while it
// played on Suno lands on the track of the same name, with nobody importing
// it — and hearing it again does not file it twice. The test app has no
// encoder, so the recording is kept as the WAV it arrived as; the MP3 it
// becomes where there is one is mp3encode_test.go's business (#93).
func TestHeardTakeIsRecordedOntoItsTrack(t *testing.T) {
	app := newTestApp(t)
	written := 0
	_, tracks := captureFixture(t, app, "Late Checkout", "Late Checkout", "Ice Machine")

	got, err := app.captureSunoPlay(SunoPlay{
		ClipID: strings.ToUpper(testClip), Title: "  late checkout ",
	}, recording(&written))
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != "filed" || got.TrackID != tracks[0].ID || got.AlbumName != "Late Checkout" {
		t.Fatalf("not filed onto the matching track: %+v", got)
	}

	track, err := app.store.track(tracks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(track.Files) != 1 {
		t.Fatalf("want one take, got %d", len(track.Files))
	}
	file := track.Files[0]
	if file.SunoClip != testClip || !file.Chosen || !strings.HasPrefix(file.Label, "Recorded on Suno ") {
		t.Errorf("the take does not say where it came from: %+v", file)
	}
	if !strings.HasSuffix(file.Name, ".wav") || file.Seconds != 3 {
		t.Errorf("with no encoder, the recording should be kept as the WAV it arrived as: %+v", file)
	}
	if _, err := os.Stat(file.Path); err != nil {
		t.Errorf("the take was not copied into the music folder: %v", err)
	}

	again, err := app.captureSunoPlay(SunoPlay{ClipID: testClip, Title: "Late Checkout"}, recording(&written))
	if err != nil {
		t.Fatal(err)
	}
	if again.Outcome != "already" || again.TrackID != tracks[0].ID {
		t.Errorf("a replay should be recognised: %+v", again)
	}
	if track, _ := app.store.track(tracks[0].ID); len(track.Files) != 1 {
		t.Errorf("a replay filed a second copy: %d takes", len(track.Files))
	}
	if written != 1 {
		t.Errorf("a replay's recording was written to disk: %d writes", written)
	}
}

// TestTheTrackTheFormWasFilledFromWins: when the extension filled Suno's form
// from a track, that track gets the take even if another has the same name.
func TestTheTrackTheFormWasFilledFromWins(t *testing.T) {
	app := newTestApp(t)
	written := 0
	_, first := captureFixture(t, app, "Season 1", "Cold Open")
	captureFixture(t, app, "Season 2", "Cold Open")

	got, err := app.captureSunoPlay(SunoPlay{
		ClipID: testClip, Title: "Cold Open", TrackID: first[0].ID,
	}, recording(&written))
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != "filed" || got.TrackID != first[0].ID {
		t.Errorf("the filled track was not the one used: %+v", got)
	}
}

// TestNameMatchingRefusesToGuess: the newest album settles a name used twice
// across albums, but a name used twice on one album, a partial name and a
// stranger are not filed at all.
func TestNameMatchingRefusesToGuess(t *testing.T) {
	app := newTestApp(t)
	written := 0
	old, _ := captureFixture(t, app, "Season 1", "Cold Open", "Dawn")
	_, newer := captureFixture(t, app, "Season 2", "Cold Open", "Dawn", "Dawn", "Mr. Blue", "1999")
	// Albums made in the same second share a timestamp; this one is older.
	if _, err := app.store.db.Exec(
		`UPDATE albums SET created_at = '2020-01-01T00:00:00Z' WHERE id = ?`, old.ID); err != nil {
		t.Fatal(err)
	}

	got, err := app.captureSunoPlay(SunoPlay{ClipID: testClip, Title: "Cold Open"}, recording(&written))
	if err != nil || got.Outcome != "filed" || got.TrackID != newer[0].ID {
		t.Fatalf("the newest album should settle a shared name: %+v, %v", got, err)
	}

	for _, title := range []string{"Dawn", "Cold", "Something Else", "Mr", ""} {
		got, err := app.captureSunoPlay(SunoPlay{
			ClipID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", Title: title}, recording(&written))
		if err != nil || got.Outcome != "unmatched" {
			t.Errorf("%q: want unmatched, got %+v, %v", title, got, err)
		}
	}
	// A dot in a title is part of it, not an extension to strip.
	got, err = app.captureSunoPlay(SunoPlay{
		ClipID: "aaaaaaaa-bbbb-cccc-dddd-000000000001", Title: "Mr. Blue"}, recording(&written))
	if err != nil || got.TrackID != newer[3].ID {
		t.Errorf("Mr. Blue: %+v, %v", got, err)
	}
	// Nor is a title made of digits a running order to strip.
	got, err = app.captureSunoPlay(SunoPlay{
		ClipID: "aaaaaaaa-bbbb-cccc-dddd-000000000002", Title: "1999"}, recording(&written))
	if err != nil || got.TrackID != newer[4].ID {
		t.Errorf("1999: %+v, %v", got, err)
	}
	if written != 3 {
		t.Errorf("only the three filed takes should have been written: %d", written)
	}
}

// TestCaptureCanBeTurnedOff: the setting stops everything, and a malformed
// clip id is refused before anything is written.
func TestCaptureCanBeTurnedOff(t *testing.T) {
	app := newTestApp(t)
	written := 0
	captureFixture(t, app, "Late Checkout", "Late Checkout")

	if _, err := app.captureSunoPlay(SunoPlay{
		ClipID: "../../etc/passwd", Title: "Late Checkout"}, recording(&written)); err == nil {
		t.Error("a malformed clip id was accepted")
	}

	if err := app.SetSetting(keySunoCapture, "off"); err != nil {
		t.Fatal(err)
	}
	got, err := app.captureSunoPlay(SunoPlay{ClipID: testClip, Title: "Late Checkout"}, recording(&written))
	if err != nil || got.Outcome != "off" {
		t.Errorf("turned off, but: %+v, %v", got, err)
	}
	if written != 0 {
		t.Errorf("something was written: %d", written)
	}
}

// TestCaptureRouteTakesAWAV: the bridge route is gated like every other one
// that acts, takes the recording as its body and the song in its query, and
// refuses a body that is not a WAV without filing anything.
func TestCaptureRouteTakesAWAV(t *testing.T) {
	app := newTestApp(t)
	_, tracks := captureFixture(t, app, "Late Checkout", "Late Checkout")
	token, err := app.extensionToken()
	if err != nil {
		t.Fatal(err)
	}
	address := "/capture?" + url.Values{
		"clip": {testClip}, "title": {"Late Checkout"}}.Encode()
	post := func(body []byte, authed bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, address, bytes.NewReader(body))
		if authed {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		app.bridgeCapture(rec, req)
		return rec
	}

	if rec := post(testWAV(2), false); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: want 401, got %d", rec.Code)
	}
	if rec := post([]byte("ID3 an mp3, not a recording"), true); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("not a WAV: want 422, got %d %s", rec.Code, rec.Body.String())
	}
	if track, _ := app.store.track(tracks[0].ID); len(track.Files) != 0 {
		t.Errorf("a refused body was filed: %+v", track.Files)
	}
	if rec := post(testWAV(2), true); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), `"outcome":"filed"`) {
		t.Errorf("with the token: %d %s", rec.Code, rec.Body.String())
	}
	if track, _ := app.store.track(tracks[0].ID); len(track.Files) != 1 || track.Files[0].Seconds != 2 {
		t.Errorf("the recording did not land whole: %+v", track.Files)
	}
}
