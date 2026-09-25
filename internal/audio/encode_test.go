package audio

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"sunomaker/internal/platform"
)

// stereoWAV is a real 44.1 kHz stereo 16-bit WAV of silence — the shape Suno
// downloads and #92's recordings have — for the one test that runs ffmpeg.
func stereoWAV(seconds int) []byte {
	const rate, channels, bits = 44100, 2, 16
	data := make([]byte, seconds*rate*channels*bits/8)
	var b bytes.Buffer
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(36+len(data)))
	b.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(channels), uint32(rate),
		uint32(rate * channels * bits / 8), uint16(channels * bits / 8), uint16(bits)} {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(data)))
	b.Write(data)
	return b.Bytes()
}

// TestFFmpegMakesAPlayableMP3 runs the real encoder, where there is one, so
// the arguments it is given are known to produce an MP3 of the right length.
func TestFFmpegMakesAPlayableMP3(t *testing.T) {
	if _, ok := platform.LookUp("ffmpeg"); !ok {
		t.Skip("ffmpeg is not installed on this machine")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "take.wav")
	if err := os.WriteFile(src, stereoWAV(3), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "take.mp3")
	if err := EncodeMP3(src, dest); err != nil {
		t.Fatal(err)
	}
	if got := DurationSeconds(dest); got != 3 {
		t.Errorf("the MP3 should be 3 seconds long, read %d", got)
	}
	wavInfo, _ := os.Stat(src)
	mp3Info, _ := os.Stat(dest)
	if mp3Info.Size() >= wavInfo.Size() {
		t.Errorf("the MP3 (%d bytes) is not smaller than the WAV (%d)", mp3Info.Size(), wavInfo.Size())
	}
}
