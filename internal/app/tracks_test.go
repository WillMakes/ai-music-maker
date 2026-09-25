package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// aRecord sets up an artist and an album to hang tracks off, which almost every
// test here needs and none of them is about.
func aRecord(t *testing.T, app *App) (Artist, Album) {
	t.Helper()
	artist, err := app.SaveArtist(Artist{
		Name:      "Static Field",
		BaseStyle: "Instruments: analogue polysynth pads, FM bass.",
	})
	if err != nil {
		t.Fatalf("artist: %v", err)
	}
	album, err := app.SaveAlbum(Album{
		ArtistID:       artist.ID,
		Name:           "Volume One",
		TechnicalStyle: "This record: slower and wetter than usual.",
	})
	if err != nil {
		t.Fatalf("album: %v", err)
	}
	return artist, album
}

func TestCombinedStylePutsTheArtistFirst(t *testing.T) {
	artist := Artist{BaseStyle: "base"}
	album := Album{TechnicalStyle: "album"}

	// The order is the point: a model reading a comma-led style list weights the
	// later terms as refinements of the earlier ones, so the album's colour has
	// to come after the artist's palette.
	if got := combinedStyle(artist, album); got != "base\n\nalbum" {
		t.Errorf("combined = %q, want the artist's first", got)
	}
	// An album with nothing of its own contributes nothing, rather than a blank
	// paragraph the model has to read past.
	if got := combinedStyle(artist, Album{}); got != "base" {
		t.Errorf("with no album style = %q, want just the base", got)
	}
	if got := combinedStyle(Artist{}, album); got != "album" {
		t.Errorf("with no base style = %q, want just the album's", got)
	}
}

func TestNewTracksAppend(t *testing.T) {
	app := newTestApp(t)
	_, album := aRecord(t, app)

	for i, name := range []string{"Opener", "Middle", "Closer"} {
		track, err := app.SaveTrack(Track{AlbumID: album.ID, Name: name})
		if err != nil {
			t.Fatalf("track %d: %v", i, err)
		}
		// Asking the producer to number a track is asking them to know how many
		// there already are, which is the app's job.
		if track.Position != i+1 {
			t.Errorf("%s position = %d, want %d", name, track.Position, i+1)
		}
	}
}

func TestTrackTakesTheDefaultLength(t *testing.T) {
	app := newTestApp(t)
	_, album := aRecord(t, app)

	track, err := app.SaveTrack(Track{AlbumID: album.ID, Name: "Opener"})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if track.TargetSeconds != 180 {
		t.Errorf("length = %d, want the 180s default", track.TargetSeconds)
	}

	if err := app.SetSetting(keyDefaultTrackLength, "240"); err != nil {
		t.Fatalf("set: %v", err)
	}
	next, err := app.SaveTrack(Track{AlbumID: album.ID, Name: "Second"})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if next.TargetSeconds != 240 {
		t.Errorf("length = %d, want the configured 240s", next.TargetSeconds)
	}
}

func TestTrackNeedsAnAlbumThatExists(t *testing.T) {
	app := newTestApp(t)
	// Read rather than trusted: a track filed against an id that names nothing
	// would be invisible on every page and undeletable from the UI.
	if _, err := app.SaveTrack(Track{AlbumID: "alb_nothing", Name: "Ghost"}); err == nil {
		t.Fatal("a track was filed against an album that does not exist")
	}
}

func TestReorderTracksIsScopedToItsAlbum(t *testing.T) {
	app := newTestApp(t)
	artist, album := aRecord(t, app)

	other, err := app.SaveAlbum(Album{ArtistID: artist.ID, Name: "Volume Two"})
	if err != nil {
		t.Fatalf("second album: %v", err)
	}
	mine, err := app.SaveTrack(Track{AlbumID: album.ID, Name: "Mine"})
	if err != nil {
		t.Fatalf("track: %v", err)
	}
	theirs, err := app.SaveTrack(Track{AlbumID: other.ID, Name: "Theirs"})
	if err != nil {
		t.Fatalf("other track: %v", err)
	}

	// A stale list from another page must not renumber tracks on a record it was
	// never showing.
	if _, err := app.ReorderTracks(album.ID, []string{theirs.ID, mine.ID}); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	untouched, err := app.GetTrack(theirs.ID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if untouched.Position != 1 {
		t.Errorf("the other album's track moved to %d", untouched.Position)
	}
}

func TestImportFilesTheAudioUnderArtistAndAlbum(t *testing.T) {
	app := newTestApp(t)
	_, album := aRecord(t, app)
	track, err := app.SaveTrack(Track{AlbumID: album.ID, Name: "The Opener"})
	if err != nil {
		t.Fatalf("track: %v", err)
	}

	source := filepath.Join(t.TempDir(), "suno-download.mp3")
	if err := os.WriteFile(source, []byte("not really audio"), 0o600); err != nil {
		t.Fatalf("write the source: %v", err)
	}

	imported, err := app.ImportTrackFile(track.ID, source, "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(imported.Files) != 1 {
		t.Fatalf("%d files, want 1", len(imported.Files))
	}
	file := imported.Files[0]

	// Copied, not referenced: Downloads is the one folder on any machine that
	// gets emptied without ceremony.
	if _, err := os.Stat(source); err != nil {
		t.Error("the original was moved rather than copied")
	}
	if _, err := os.Stat(file.Path); err != nil {
		t.Fatalf("the copy is not there: %v", err)
	}
	// The library reads the way any other music library does.
	if !strings.Contains(filepath.ToSlash(file.Path), "static-field/volume-one/") {
		t.Errorf("filed at %q, want it under artist/album", file.Path)
	}
	if filepath.Base(file.Path) != "01 the-opener.mp3" {
		t.Errorf("named %q, want it numbered by running order", filepath.Base(file.Path))
	}
	// The first take is the take: a track with files and none chosen is a state
	// the rest of the app would have to keep guarding for.
	if !file.Chosen {
		t.Error("the only take was not marked as the take")
	}
	if file.Label == "" {
		t.Error("no label was derived from the file name")
	}
}

func TestImportRefusesWhatIsNotAudio(t *testing.T) {
	app := newTestApp(t)
	_, album := aRecord(t, app)
	track, _ := app.SaveTrack(Track{AlbumID: album.ID, Name: "Opener"})

	notAudio := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(notAudio, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := app.ImportTrackFile(track.ID, notAudio, ""); err == nil {
		t.Error("a text file was accepted as a take")
	}
	if _, err := app.ImportTrackFile(track.ID, t.TempDir(), ""); err == nil {
		t.Error("a folder was accepted as a take")
	}
}

func TestChoosingATakeUnmarksTheOthers(t *testing.T) {
	app := newTestApp(t)
	_, album := aRecord(t, app)
	track, _ := app.SaveTrack(Track{AlbumID: album.ID, Name: "Opener"})

	dir := t.TempDir()
	for _, name := range []string{"take-1.mp3", "take-2.mp3"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("audio"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		if _, err := app.ImportTrackFile(track.ID, path, name); err != nil {
			t.Fatalf("import %s: %v", name, err)
		}
	}

	current, err := app.GetTrack(track.ID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(current.Files) != 2 {
		t.Fatalf("%d takes, want 2", len(current.Files))
	}

	chosen, err := app.ChooseTrackFile(track.ID, current.Files[1].ID)
	if err != nil {
		t.Fatalf("choose: %v", err)
	}
	// "Which of these is the song" has one answer, and a list where two are
	// ticked has not answered it.
	marked := 0
	for _, file := range chosen.Files {
		if file.Chosen {
			marked++
			if file.ID != current.Files[1].ID {
				t.Error("the wrong take is marked")
			}
		}
	}
	if marked != 1 {
		t.Errorf("%d takes marked, want exactly 1", marked)
	}
}

func TestChoosingATakeFromAnotherTrackIsRefused(t *testing.T) {
	app := newTestApp(t)
	_, album := aRecord(t, app)
	mine, _ := app.SaveTrack(Track{AlbumID: album.ID, Name: "Mine"})
	theirs, _ := app.SaveTrack(Track{AlbumID: album.ID, Name: "Theirs"})

	path := filepath.Join(t.TempDir(), "take.mp3")
	if err := os.WriteFile(path, []byte("audio"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	other, err := app.ImportTrackFile(theirs.ID, path, "one")
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	// Without the check, this would clear the first track's own choice and then
	// report success.
	if _, err := app.ChooseTrackFile(mine.ID, other.Files[0].ID); err == nil {
		t.Error("a take belonging to another track was accepted")
	}
}

func TestRemovingTheChosenTakePromotesAnother(t *testing.T) {
	app := newTestApp(t)
	_, album := aRecord(t, app)
	track, _ := app.SaveTrack(Track{AlbumID: album.ID, Name: "Opener"})

	dir := t.TempDir()
	for _, name := range []string{"a.mp3", "b.mp3"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("audio"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		if _, err := app.ImportTrackFile(track.ID, path, name); err != nil {
			t.Fatalf("import: %v", err)
		}
	}
	current, _ := app.GetTrack(track.ID)

	// deleteFile false: forgetting a row is undoable by importing it again,
	// deleting somebody's music is not.
	after, err := app.RemoveTrackFile(track.ID, current.Files[0].ID, false)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if len(after.Files) != 1 {
		t.Fatalf("%d takes left, want 1", len(after.Files))
	}
	if !after.Files[0].Chosen {
		t.Error("no take is chosen — the app would keep guarding for this state")
	}
	if _, err := os.Stat(current.Files[0].Path); err != nil {
		t.Error("the audio was deleted even though deleteFile was false")
	}
}
