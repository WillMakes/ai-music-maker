package audio

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

// mp3Frame is one MPEG1 Layer III header: 128 kbps, 44100 Hz, stereo.
var mp3Frame = []byte{0xFF, 0xFB, 0x90, 0x00}

func TestWAVDurationIsHeaderArithmetic(t *testing.T) {
	if got := DurationSeconds(makeWAV(t, 7, 176400)); got != 7 {
		t.Fatalf("want 7 seconds, got %d", got)
	}
}

func TestMP3DurationFallsBackToConstantBitrate(t *testing.T) {
	// 128 kbps for 5 seconds is 80000 bytes of frames.
	payload := append(append([]byte{}, mp3Frame...), make([]byte, 80000-4)...)
	path := filepath.Join(t.TempDir(), "take.mp3")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := DurationSeconds(path); got != 5 {
		t.Fatalf("want 5 seconds, got %d", got)
	}
}

func TestMP3DurationPrefersTheXingFrameCount(t *testing.T) {
	// 3830 frames of 1152 samples at 44100 Hz is almost exactly 100 seconds —
	// and nothing like what the file's size would say, which is the point.
	frame := make([]byte, 4+32+16)
	copy(frame, mp3Frame)
	copy(frame[36:], "Xing")
	binary.BigEndian.PutUint32(frame[40:], 0x1)
	binary.BigEndian.PutUint32(frame[44:], 3830)
	path := filepath.Join(t.TempDir(), "take.mp3")
	if err := os.WriteFile(path, frame, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := DurationSeconds(path); got != 100 {
		t.Fatalf("want 100 seconds, got %d", got)
	}
}

func TestUnknownAudioAnswersZeroNotAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "take.flac")
	if err := os.WriteFile(path, []byte("not audio at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := DurationSeconds(path); got != 0 {
		t.Fatalf("want 0 for a format this does not read, got %d", got)
	}
}
