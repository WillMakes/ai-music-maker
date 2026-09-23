package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

const testClip = "0f4c2b1a-9d3e-4a5b-8c7d-112233445566"

// stubSunoDownload stands in for Suno's CDN, so no test reaches the network,
// and records every address the app asked for.
func stubSunoDownload(t *testing.T) *[]string {
	t.Helper()
	asked := &[]string{}
	previous := fetchSunoAudio
	fetchSunoAudio = func(address, dest string) error {
		*asked = append(*asked, address)
		return os.WriteFile(dest, []byte("not really mp3 bytes"), 0o600)
	}
	t.Cleanup(func() { fetchSunoAudio = previous })
	return asked
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

// TestHeardTakeIsFiledOntoItsTrack is #91: a song played to the end on Suno
// lands on the track of the same name with nobody importing it — and hearing
// it again does not file it twice.
func TestHeardTakeIsFiledOntoItsTrack(t *testing.T) {
	app := newTestApp(t)
	asked := stubSunoDownload(t)
	_, tracks := captureFixture(t, app, "Late Checkout", "Late Checkout", "Ice Machine")

	got, err := app.captureSunoPlay(SunoPlay{
		ClipID: strings.ToUpper(testClip), Title: "  late checkout ",
		AudioURL: "https://cdn1.suno.ai/" + testClip + ".mp3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != "filed" || got.TrackID != tracks[0].ID || got.AlbumName != "Late Checkout" {
		t.Fatalf("not filed onto the matching track: %+v", got)
	}
	// The canonical address, once: the page's own address was the same one.
	if len(*asked) != 1 || (*asked)[0] != "https://cdn1.suno.ai/"+testClip+".mp3" {
		t.Errorf("downloaded from %v", *asked)
	}

	track, err := app.store.track(tracks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(track.Files) != 1 {
		t.Fatalf("want one take, got %d", len(track.Files))
	}
	file := track.Files[0]
	if file.SunoClip != testClip || !file.Chosen || !strings.HasPrefix(file.Label, "Suno ") {
		t.Errorf("the take does not say where it came from: %+v", file)
	}
	if _, err := os.Stat(file.Path); err != nil {
		t.Errorf("the take was not copied into the music folder: %v", err)
	}

	again, err := app.captureSunoPlay(SunoPlay{ClipID: testClip, Title: "Late Checkout"})
	if err != nil {
		t.Fatal(err)
	}
	if again.Outcome != "already" || again.TrackID != tracks[0].ID {
		t.Errorf("a replay should be recognised: %+v", again)
	}
	if track, _ := app.store.track(tracks[0].ID); len(track.Files) != 1 {
		t.Errorf("a replay filed a second copy: %d takes", len(track.Files))
	}
	if len(*asked) != 1 {
		t.Errorf("a replay downloaded again: %v", *asked)
	}
}

// TestTheTrackTheFormWasFilledFromWins: when the extension filled Suno's form
// from a track, that track gets the take even if another has the same name.
func TestTheTrackTheFormWasFilledFromWins(t *testing.T) {
	app := newTestApp(t)
	stubSunoDownload(t)
	_, first := captureFixture(t, app, "Season 1", "Cold Open")
	captureFixture(t, app, "Season 2", "Cold Open")

	got, err := app.captureSunoPlay(SunoPlay{
		ClipID: testClip, Title: "Cold Open", TrackID: first[0].ID,
	})
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
	asked := stubSunoDownload(t)
	old, _ := captureFixture(t, app, "Season 1", "Cold Open", "Dawn")
	_, newer := captureFixture(t, app, "Season 2", "Cold Open", "Dawn", "Dawn", "Mr. Blue", "1999")
	// Albums made in the same second share a timestamp; this one is older.
	if _, err := app.store.db.Exec(
		`UPDATE albums SET created_at = '2020-01-01T00:00:00Z' WHERE id = ?`, old.ID); err != nil {
		t.Fatal(err)
	}

	got, err := app.captureSunoPlay(SunoPlay{ClipID: testClip, Title: "Cold Open"})
	if err != nil || got.Outcome != "filed" || got.TrackID != newer[0].ID {
		t.Fatalf("the newest album should settle a shared name: %+v, %v", got, err)
	}

	for _, title := range []string{"Dawn", "Cold", "Something Else", "Mr", ""} {
		got, err := app.captureSunoPlay(SunoPlay{
			ClipID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", Title: title})
		if err != nil || got.Outcome != "unmatched" {
			t.Errorf("%q: want unmatched, got %+v, %v", title, got, err)
		}
	}
	// A dot in a title is part of it, not an extension to strip.
	got, err = app.captureSunoPlay(SunoPlay{
		ClipID: "aaaaaaaa-bbbb-cccc-dddd-000000000001", Title: "Mr. Blue"})
	if err != nil || got.TrackID != newer[3].ID {
		t.Errorf("Mr. Blue: %+v, %v", got, err)
	}
	// Nor is a title made of digits a running order to strip.
	got, err = app.captureSunoPlay(SunoPlay{
		ClipID: "aaaaaaaa-bbbb-cccc-dddd-000000000002", Title: "1999"})
	if err != nil || got.TrackID != newer[4].ID {
		t.Errorf("1999: %+v, %v", got, err)
	}
	if len(*asked) != 3 {
		t.Errorf("only the three filed takes should have been downloaded: %v", *asked)
	}
}

// TestCaptureCanBeTurnedOffAndStaysOnSuno: the setting stops everything, a
// malformed clip id is refused before it reaches an address, and an address
// off Suno's own hosts is never fetched.
func TestCaptureCanBeTurnedOffAndStaysOnSuno(t *testing.T) {
	app := newTestApp(t)
	asked := stubSunoDownload(t)
	captureFixture(t, app, "Late Checkout", "Late Checkout")

	if _, err := app.captureSunoPlay(SunoPlay{ClipID: "../../etc/passwd", Title: "Late Checkout"}); err == nil {
		t.Error("a malformed clip id was accepted")
	}

	addresses := sunoAudioAddresses(SunoPlay{ClipID: testClip, AudioURL: "https://evil.example/x.mp3"})
	if len(addresses) != 1 {
		t.Errorf("an address off Suno would be fetched: %v", addresses)
	}
	addresses = sunoAudioAddresses(SunoPlay{ClipID: testClip, AudioURL: "https://audiopipe.suno.ai/?item_id=" + testClip})
	if len(addresses) != 2 {
		t.Errorf("Suno's own stream address should be the fallback: %v", addresses)
	}
	for host, want := range map[string]bool{
		"cdn1.suno.ai": true, "suno.com": true, "notsuno.ai": false, "suno.ai.evil.example": false,
	} {
		if isSunoHost(host) != want {
			t.Errorf("isSunoHost(%q) = %v", host, !want)
		}
	}

	if err := app.SetSetting(keySunoCapture, "off"); err != nil {
		t.Fatal(err)
	}
	got, err := app.captureSunoPlay(SunoPlay{ClipID: testClip, Title: "Late Checkout"})
	if err != nil || got.Outcome != "off" {
		t.Errorf("turned off, but: %+v, %v", got, err)
	}
	if len(*asked) != 0 {
		t.Errorf("something was downloaded: %v", *asked)
	}
}

// TestCaptureRouteNeedsTheToken: the bridge route is gated like every other
// one that acts, and answers the outcome as JSON.
func TestCaptureRouteNeedsTheToken(t *testing.T) {
	app := newTestApp(t)
	stubSunoDownload(t)
	captureFixture(t, app, "Late Checkout", "Late Checkout")
	token, err := app.extensionToken()
	if err != nil {
		t.Fatal(err)
	}
	body := `{"clipId":"` + testClip + `","title":"Late Checkout"}`

	req := httptest.NewRequest(http.MethodPost, "/capture", strings.NewReader(body))
	rec := httptest.NewRecorder()
	app.bridgeCapture(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: want 401, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/capture", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	app.bridgeCapture(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"outcome":"filed"`) {
		t.Errorf("with the token: %d %s", rec.Code, rec.Body.String())
	}
}
