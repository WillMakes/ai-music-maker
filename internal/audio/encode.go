package audio

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"aimusicmaker/internal/platform"
)

// ---------------------------------------------------------------------------
// WAV takes filed as MP3 (#93)
//
// Suno's WAV downloads, and the takes recorded off its player (#92), are ten
// times the size of the MP3 of the same song, and the library is where the
// producer keeps every take of every track. So a WAV is encoded to MP3 as it is
// filed, and only the MP3 is kept.
//
// The encoder is ffmpeg's LAME at 320 kbps, run as a separate program. The
// alternative was an encoder compiled in: the only pure-Go one is a port of
// Shine, which encodes at a fixed 128 kbps — audibly short of what a producer
// would keep of their own song — and is LGPL, which a statically linked binary
// cannot honour as simply as a program it runs. LAME at 320 is the best an MP3
// gets, and ffmpeg is where LAME is found on all three platforms.
//
// Without ffmpeg the WAV is filed as it is. A take kept larger than asked is a
// take; a take lost because an encoder was missing is not, and nothing about a
// missing encoder should stop an import that would otherwise have worked.
// ---------------------------------------------------------------------------

// ErrNoEncoder is EncodeMP3's answer when ffmpeg is not on this machine.
var ErrNoEncoder = errors.New("ffmpeg is not installed, so WAV takes are kept as WAV")

// mp3EncodeTimeout bounds one encode. A five-minute song encodes in seconds;
// an encoder still running after this is stuck, and the import falls back to
// keeping the WAV rather than waiting on it.
const mp3EncodeTimeout = 5 * time.Minute

// EncodeMP3 encodes the audio at src into an MP3 at dest. A variable so tests
// can file a take without running ffmpeg — and so a test of the real encoder
// can say, by skipping, that this machine has none.
var EncodeMP3 = func(src, dest string) error {
	ffmpeg, ok := platform.LookUp("ffmpeg")
	if !ok {
		return ErrNoEncoder
	}
	ctx, cancel := context.WithTimeout(context.Background(), mp3EncodeTimeout)
	defer cancel()
	// -nostdin because a child that waits on a terminal nobody is at never
	// finishes; -vn because a WAV has no picture, and a file that somehow does
	// should not make the encode fail over it.
	cmd := exec.CommandContext(ctx, ffmpeg,
		"-nostdin", "-hide_banner", "-loglevel", "error", "-y",
		"-i", src, "-vn", "-codec:a", "libmp3lame", "-b:a", "320k", dest)
	platform.HideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.Remove(dest)
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("ffmpeg could not encode the take: %s", msg)
		}
		return fmt.Errorf("ffmpeg could not encode the take: %w", err)
	}
	if info, err := os.Stat(dest); err != nil || info.Size() == 0 {
		_ = os.Remove(dest)
		return fmt.Errorf("ffmpeg wrote no MP3")
	}
	return nil
}
