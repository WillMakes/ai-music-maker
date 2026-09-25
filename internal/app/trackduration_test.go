package app

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// makeWAV writes a minimal RIFF/WAVE file whose data chunk plays for exactly
// seconds at byteRate bytes per second.
func makeWAV(t *testing.T, seconds, byteRate int) string {
	t.Helper()
	data := make([]byte, seconds*byteRate)
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+len(data)))
	b.WriteString("WAVE")
	b.WriteString("fmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))
	fmtChunk := make([]byte, 16)
	binary.LittleEndian.PutUint32(fmtChunk[8:12], uint32(byteRate))
	b.Write(fmtChunk)
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(len(data)))
	b.Write(data)
	path := filepath.Join(t.TempDir(), "take.wav")
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestImportSetsTheTracksDuration is #85 end to end: a WAV take lands and the
// track's targetSeconds becomes the file's real length. A second, unchosen
// take must not change it — the track's duration follows the chosen file.
func TestImportSetsTheTracksDuration(t *testing.T) {
	app := newTestApp(t)
	artist, err := app.SaveArtist(Artist{Name: "Cassette Motel", Inspirations: []Inspiration{}})
	if err != nil {
		t.Fatal(err)
	}
	album, err := app.SaveAlbum(Album{ArtistID: artist.ID, Name: "Season 3"})
	if err != nil {
		t.Fatal(err)
	}
	track, err := app.SaveTrack(Track{AlbumID: album.ID, Name: "Cold Open", TargetSeconds: 210})
	if err != nil {
		t.Fatal(err)
	}

	got, err := app.ImportTrackFile(track.ID, makeWAV(t, 187, 176400), "")
	if err != nil {
		t.Fatal(err)
	}
	if got.TargetSeconds != 187 {
		t.Fatalf("the track's duration did not follow the file: %d", got.TargetSeconds)
	}
	if len(got.Files) != 1 || got.Files[0].Seconds != 187 {
		t.Fatalf("the take does not carry its own length: %+v", got.Files)
	}

	got, err = app.ImportTrackFile(track.ID, makeWAV(t, 42, 176400), "")
	if err != nil {
		t.Fatal(err)
	}
	if got.TargetSeconds != 187 {
		t.Fatalf("an unchosen take changed the track's duration: %d", got.TargetSeconds)
	}
}
