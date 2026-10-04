package video

import (
	"context"
	"math"
	"os/exec"
	"path/filepath"
	"testing"

	"aimusicmaker/internal/platform"
)

// TestRenderJoinsSongsWithAGap is #118 end to end with the real ffmpeg: two
// short tones and a cover become one video whose length is the songs plus the
// gap, with each song's start where the description will say it is.
func TestRenderJoinsSongsWithAGap(t *testing.T) {
	ffmpeg, ok := platform.LookUp("ffmpeg")
	if !ok {
		t.Skip("ffmpeg is not installed here")
	}
	ffprobe, ok := platform.LookUp("ffprobe")
	if !ok {
		t.Skip("ffprobe is not installed here")
	}
	dir := t.TempDir()
	make := func(args ...string) {
		t.Helper()
		out, err := exec.Command(ffmpeg, append([]string{"-hide_banner", "-loglevel", "error", "-y"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("making a fixture: %v\n%s", err, out)
		}
	}
	cover := filepath.Join(dir, "cover.png")
	make("-f", "lavfi", "-i", "testsrc=s=640x640", "-frames:v", "1", cover)
	one := filepath.Join(dir, "one.mp3")
	two := filepath.Join(dir, "two.mp3")
	make("-f", "lavfi", "-i", "sine=frequency=440:duration=3", one)
	make("-f", "lavfi", "-i", "sine=frequency=660:duration=2", two)

	for _, style := range []Style{{}, {Visualizer: "bars", Background: "black", Color: "#ff8800"}} {
		out := filepath.Join(dir, "video-"+style.Normalized().Visualizer+".mp4")
		var last float64
		got, err := Render(context.Background(), Spec{
			Artist: "Test Artist",
			Album:  "Latent: Space \\ 'Quoted'",
			Cover:  cover,
			Songs:  []Song{{Title: "First: Song", Audio: one}, {Title: "Second's", Audio: two}},
			Gap:    1,
			Style:  style,
			Out:    out,
		}, func(f float64) { last = f })
		if err != nil {
			t.Fatalf("%+v: %v", style, err)
		}
		if last != 1 {
			t.Errorf("progress ended at %v, want 1", last)
		}
		if len(got.Starts) != 2 || got.Starts[0] != 0 || math.Abs(got.Starts[1]-4) > 0.15 {
			t.Errorf("starts = %v, want [0 4]", got.Starts)
		}
		seconds, err := probeSeconds(context.Background(), ffprobe, out)
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(seconds-6) > 0.3 {
			t.Errorf("the video is %.2fs, want about 6 (3 + 1 gap + 2)", seconds)
		}
	}
}

// TestStyleNormalized: whatever a skill writes, only values the filtergraph
// accepts reach it.
func TestStyleNormalized(t *testing.T) {
	got := Style{Visualizer: "lasers", Color: "red;drawtext", Background: "video", Fade: 99}.Normalized()
	if got != DefaultStyle {
		t.Errorf("bad values survived: %+v", got)
	}
	got = Style{Visualizer: "bars", Color: "#a1b2c3", Background: "black", Fade: 0.5}.Normalized()
	want := Style{Visualizer: "bars", Color: "A1B2C3", Background: "black", Fade: 0.5}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
