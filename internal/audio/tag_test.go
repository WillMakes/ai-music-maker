package audio

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"

	"aimusicmaker/internal/platform"
)

// TestTagForUpload is #124 with the real ffmpeg: a WAV becomes a tagged MP3
// with its cover embedded, and the source's own tags do not survive.
func TestTagForUpload(t *testing.T) {
	ffmpeg, ok := platform.LookUp("ffmpeg")
	if !ok {
		t.Skip("ffmpeg is not installed here")
	}
	ffprobe, ok := platform.LookUp("ffprobe")
	if !ok {
		t.Skip("ffprobe is not installed here")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "take.wav")
	cover := filepath.Join(dir, "cover.png")
	for _, args := range [][]string{
		{"-f", "lavfi", "-i", "sine=duration=1", "-metadata", "comment=Made with Suno", src},
		{"-f", "lavfi", "-i", "testsrc=s=64x64", "-frames:v", "1", cover},
	} {
		if out, err := exec.Command(ffmpeg, append([]string{"-y", "-loglevel", "error"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("fixture: %v %s", err, out)
		}
	}
	dest := filepath.Join(dir, "tagged.mp3")
	err := TagForUpload(src, dest, Tags{
		Title: "Neon Rain: Part 1", Artist: "Cassette Motel", Album: "Night Drive",
		Track: 2, TrackCount: 9, Year: 2026, Cover: cover,
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(ffprobe, "-v", "error", "-show_entries", "format_tags:stream=codec_name,codec_type",
		"-of", "json", dest).Output()
	if err != nil {
		t.Fatal(err)
	}
	var probe struct {
		Streams []struct {
			CodecName string `json:"codec_name"`
			CodecType string `json:"codec_type"`
		} `json:"streams"`
		Format struct {
			Tags map[string]string `json:"tags"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &probe); err != nil {
		t.Fatal(err)
	}
	tags := probe.Format.Tags
	if tags["title"] != "Neon Rain: Part 1" || tags["artist"] != "Cassette Motel" || tags["album"] != "Night Drive" ||
		tags["track"] != "2/9" || tags["album_artist"] != "Cassette Motel" {
		t.Errorf("tags = %v", tags)
	}
	if _, kept := tags["comment"]; kept {
		t.Errorf("the source's comment survived: %v", tags)
	}
	var audioCodec, picture string
	for _, s := range probe.Streams {
		if s.CodecType == "audio" {
			audioCodec = s.CodecName
		} else if s.CodecType == "video" {
			picture = s.CodecName
		}
	}
	if audioCodec != "mp3" || picture != "mjpeg" {
		t.Errorf("streams: audio %q, cover %q", audioCodec, picture)
	}
}
