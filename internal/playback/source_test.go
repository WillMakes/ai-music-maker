package playback

import (
	"encoding/binary"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"aimusicmaker/internal/platform"
)

// writeWAV makes a one-second sine WAV of the given shape.
func writeWAV(t *testing.T, path string, rate, channels, bits int, float bool, freq float64) {
	t.Helper()
	frames := rate
	var data []byte
	for i := 0; i < frames; i++ {
		v := math.Sin(2*math.Pi*freq*float64(i)/float64(rate)) * 0.5
		for c := 0; c < channels; c++ {
			switch {
			case float:
				data = binary.LittleEndian.AppendUint32(data, math.Float32bits(float32(v)))
			case bits == 16:
				data = binary.LittleEndian.AppendUint16(data, uint16(int16(v*32767)))
			case bits == 24:
				s := int32(v * 8388607)
				data = append(data, byte(s), byte(s>>8), byte(s>>16))
			}
		}
	}
	format := uint16(1)
	if float {
		format = 3
	}
	var b []byte
	b = append(b, "RIFF"...)
	b = binary.LittleEndian.AppendUint32(b, uint32(36+len(data)+12))
	b = append(b, "WAVE"...)
	// A chunk the reader must skip, as Suno's files carry.
	b = append(b, "LIST"...)
	b = binary.LittleEndian.AppendUint32(b, 4)
	b = append(b, "INFO"...)
	b = append(b, "fmt "...)
	b = binary.LittleEndian.AppendUint32(b, 16)
	b = binary.LittleEndian.AppendUint16(b, format)
	b = binary.LittleEndian.AppendUint16(b, uint16(channels))
	b = binary.LittleEndian.AppendUint32(b, uint32(rate))
	b = binary.LittleEndian.AppendUint32(b, uint32(rate*channels*bits/8))
	b = binary.LittleEndian.AppendUint16(b, uint16(channels*bits/8))
	b = binary.LittleEndian.AppendUint16(b, uint16(bits))
	b = append(b, "data"...)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(data)))
	b = append(b, data...)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// readAll decodes a source at the output rate and returns its left channel.
func readAll(t *testing.T, src pcm) []int16 {
	t.Helper()
	raw, err := io.ReadAll(src)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]int16, len(raw)/4)
	for i := range out {
		out[i] = int16(binary.LittleEndian.Uint16(raw[i*4:]))
	}
	return out
}

// tone checks a decoded sine: about a second long, about half scale, and at
// about the frequency it was made at (counted by zero crossings).
func tone(t *testing.T, name string, samples []int16, rate int, freq float64) {
	t.Helper()
	if d := float64(len(samples)) / float64(rate); math.Abs(d-1) > 0.08 {
		t.Errorf("%s: %.3fs long, want about 1s", name, d)
	}
	// Crossings are counted with hysteresis: an MP3 starts with the
	// encoder's near-silent priming, whose noise crosses zero constantly.
	peak, crossings, side := 0, 0, 0
	for _, s := range samples {
		if a := int(math.Abs(float64(s))); a > peak {
			peak = a
		}
		now := side
		if s > 4000 {
			now = 1
		} else if s < -4000 {
			now = -1
		}
		if side != 0 && now != side {
			crossings++
		}
		side = now
	}
	if peak < 12000 || peak > 20000 {
		t.Errorf("%s: peak %d, want about 16384", name, peak)
	}
	if got := float64(crossings) / 2; math.Abs(got-freq) > freq*0.05 {
		t.Errorf("%s: about %.0f Hz, want %.0f", name, got, freq)
	}
}

func TestWAV(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		name     string
		rate     int
		channels int
		bits     int
		float    bool
	}{
		{"16-bit stereo 48k", 48000, 2, 16, false},
		{"24-bit stereo 48k", 48000, 2, 24, false},
		{"float mono 48k", 48000, 1, 32, true},
		{"16-bit stereo 44.1k, resampled", 44100, 2, 16, false},
	} {
		path := filepath.Join(dir, c.name+".wav")
		writeWAV(t, path, c.rate, c.channels, c.bits, c.float, 440)
		src, err := open(path)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		src = withRate(src)
		if src.Rate() != OutputRate {
			t.Fatalf("%s: rate %d", c.name, src.Rate())
		}
		if d := float64(src.Frames()) / OutputRate; math.Abs(d-1) > 0.01 {
			t.Errorf("%s: Frames says %.3fs", c.name, d)
		}
		tone(t, c.name, readAll(t, src), OutputRate, 440)

		// A seek to the middle reads the second half.
		if _, err := src.Seek(OutputRate/2*bytesPerFrame, io.SeekStart); err != nil {
			t.Fatal(err)
		}
		rest := readAll(t, src)
		if d := float64(len(rest)) / OutputRate; math.Abs(d-0.5) > 0.02 {
			t.Errorf("%s: after seeking to 0.5s, %.3fs is left", c.name, d)
		}
		src.Close()
	}
}

// TestDecodedFormats runs the formats a producer imports through their
// decoders, from fixtures the real ffmpeg makes.
func TestDecodedFormats(t *testing.T) {
	ffmpeg, ok := platform.LookUp("ffmpeg")
	if !ok {
		t.Skip("ffmpeg is not installed here to make the fixtures")
	}
	dir := t.TempDir()
	for _, c := range []struct{ name, file string }{
		{"mp3 44.1k", "a.mp3"},
		{"flac 44.1k 24-bit", "b.flac"},
		{"m4a via ffmpeg", "c.m4a"},
	} {
		path := filepath.Join(dir, c.file)
		args := []string{"-y", "-loglevel", "error", "-f", "lavfi", "-i", "aevalsrc=0.5*sin(440*2*PI*t)|0.5*sin(440*2*PI*t):s=44100:d=1"}
		if filepath.Ext(c.file) == ".flac" {
			args = append(args, "-sample_fmt", "s32")
		}
		if out, err := exec.Command(ffmpeg, append(args, path)...).CombinedOutput(); err != nil {
			t.Fatalf("fixture %s: %v %s", c.file, err, out)
		}
		src, err := open(path)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		src = withRate(src)
		tone(t, c.name, readAll(t, src), OutputRate, 440)
		if _, err := src.Seek(OutputRate/2*bytesPerFrame, io.SeekStart); err != nil {
			t.Fatalf("%s: seek: %v", c.name, err)
		}
		rest := readAll(t, src)
		if d := float64(len(rest)) / OutputRate; math.Abs(d-0.5) > 0.08 {
			t.Errorf("%s: after seeking to 0.5s, %.3fs is left", c.name, d)
		}
		src.Close()
	}
}

func TestOpenRefuses(t *testing.T) {
	if _, err := open(filepath.Join(t.TempDir(), "gone.mp3")); err == nil {
		t.Error("a missing take opened")
	}
	if _, err := open("take.aiff"); err == nil {
		t.Error("a format nothing decodes opened")
	}
}
