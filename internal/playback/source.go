// Package playback plays the producer's takes from this process (#125).
//
// The player used to be an <audio> element in the webview. That made the
// sound come out of WebView2's own processes, which Windows does not count as
// part of aimusicmaker.exe's process tree for loopback capture — so OBS's
// Application Audio Capture, pointed at the app's window, recorded silence.
// Measured: process-loopback aimed at aimusicmaker.exe heard peak 1 of 32767
// while a song played; aimed at msedgewebview2.exe it heard the song. The
// sound has to be made here for it to be the app's.
//
// So the takes are decoded in Go and played through oto, which reaches the
// OS's audio API without cgo on every platform. The queue, shuffle and repeat
// stay in the window; this is only the speaker.
package playback

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// OutputRate is the one rate the output runs at. An oto context has one rate
// for its life, and takes come at 44.1 and 48 kHz, so whatever is not 48 kHz
// is resampled to it.
const OutputRate = 48000

// bytesPerFrame is one stereo frame of 16-bit samples, the only shape the
// output takes.
const bytesPerFrame = 4

// pcm is a decoded take: 16-bit little-endian stereo frames at Rate(), read
// and sought in bytes like any io.ReadSeeker.
type pcm interface {
	io.ReadSeeker
	io.Closer
	// Rate is the frames per second.
	Rate() int
	// Frames is the take's length in frames, 0 when it cannot be known.
	Frames() int64
}

// ErrUnsupported is open's answer for a format nothing here decodes.
var ErrUnsupported = errors.New("this take's format cannot be played")

// open decodes the file at path by its extension. MP3, WAV and FLAC are
// decoded in Go, so the common takes play on any machine; the rest (M4A, AAC,
// OGG) go through ffmpeg when it is installed, since there is no AAC decoder
// in Go worth trusting with a master.
func open(path string) (pcm, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mp3":
		return openMP3(path)
	case ".wav":
		return openWAV(path)
	case ".flac":
		return openFLAC(path)
	case ".m4a", ".aac", ".ogg":
		return openFFmpeg(path)
	}
	return nil, fmt.Errorf("%w: %s", ErrUnsupported, ext)
}

// openFile is os.Open, with the error a producer can read.
func openFile(path string) (*os.File, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("the take is gone from %s", path)
	}
	return f, err
}

// clamp16 squeezes a sample into 16 bits.
func clamp16(v int64) int16 {
	if v > 32767 {
		return 32767
	}
	if v < -32768 {
		return -32768
	}
	return int16(v)
}
