package playback

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// TestEngineOnTheDevice plays a tone through the real audio output, for a
// person or a capture tool to hear. Off unless PLAYBACK_DEVICE_TEST names a
// file to write this process's id to: a test suite that made noise on every
// run would be switched off, and CI has no speaker.
func TestEngineOnTheDevice(t *testing.T) {
	pidFile := os.Getenv("PLAYBACK_DEVICE_TEST")
	if pidFile == "" {
		t.Skip("set PLAYBACK_DEVICE_TEST to play through the real output")
	}
	path := filepath.Join(t.TempDir(), "tone.wav")
	writeWAV(t, path, 44100, 2, 16, false, 440)
	var ended bool
	e := New(func(s State) {
		if s.Ended {
			ended = true
		}
	})
	defer e.Close()
	if _, d, err := e.Load(path); err != nil || d < 0.9 {
		t.Fatalf("load: %v, %v", d, err)
	}
	_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o600)
	// Loop the one-second tone for a few seconds, the way repeat does.
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		ended = false
		if err := e.Play(); err != nil {
			t.Fatal(err)
		}
		for !ended && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
	}
}
