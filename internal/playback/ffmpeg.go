package playback

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"

	"aimusicmaker/internal/platform"
)

// ffmpegSource decodes what Go has no decoder for — M4A, AAC, OGG — by
// running ffmpeg and reading its raw output. It decodes straight to the
// output rate, so it needs no resampling. A seek starts ffmpeg again at the
// new position: these are the rare takes, and a restart is a fraction of a
// second.
type ffmpegSource struct {
	ffmpeg string
	path   string
	frames int64
	pos    int64 // in frames
	cmd    *exec.Cmd
	out    io.ReadCloser
	cancel context.CancelFunc
}

func openFFmpeg(path string) (pcm, error) {
	ffmpeg, ok := platform.LookUp("ffmpeg")
	if !ok {
		return nil, errors.New("playing this take needs ffmpeg — install it, or import the take as MP3, WAV or FLAC")
	}
	s := &ffmpegSource{ffmpeg: ffmpeg, path: path}
	if ffprobe, ok := platform.LookUp("ffprobe"); ok {
		cmd := exec.Command(ffprobe, "-v", "error", "-show_entries", "format=duration",
			"-of", "default=noprint_wrappers=1:nokey=1", path)
		platform.HideWindow(cmd)
		if out, err := cmd.Output(); err == nil {
			if seconds, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64); err == nil {
				s.frames = int64(seconds * OutputRate)
			}
		}
	}
	if err := s.start(0); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *ffmpegSource) start(frame int64) error {
	s.stop()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, s.ffmpeg, "-nostdin", "-hide_banner", "-loglevel", "error",
		"-ss", strconv.FormatFloat(float64(frame)/OutputRate, 'f', 3, 64), "-i", s.path,
		"-f", "s16le", "-ac", "2", "-ar", strconv.Itoa(OutputRate), "pipe:1")
	platform.HideWindow(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("ffmpeg could not start: %w", err)
	}
	s.cmd, s.out, s.cancel, s.pos = cmd, out, cancel, frame
	return nil
}

func (s *ffmpegSource) stop() {
	if s.cancel != nil {
		s.cancel()
		_ = s.cmd.Wait()
		s.cancel = nil
	}
}

func (s *ffmpegSource) Read(p []byte) (int, error) {
	n, err := io.ReadAtLeast(s.out, p[:len(p)/bytesPerFrame*bytesPerFrame], bytesPerFrame)
	n -= n % bytesPerFrame
	s.pos += int64(n / bytesPerFrame)
	if err == io.ErrUnexpectedEOF {
		err = io.EOF
	}
	if n > 0 {
		return n, nil
	}
	return 0, err
}

func (s *ffmpegSource) Seek(offset int64, whence int) (int64, error) {
	var to int64
	switch whence {
	case io.SeekStart:
		to = offset
	case io.SeekCurrent:
		to = s.pos*bytesPerFrame + offset
	case io.SeekEnd:
		to = s.frames*bytesPerFrame + offset
	}
	frame := max(0, to/bytesPerFrame)
	if err := s.start(frame); err != nil {
		return 0, err
	}
	return frame * bytesPerFrame, nil
}

func (s *ffmpegSource) Rate() int     { return OutputRate }
func (s *ffmpegSource) Frames() int64 { return s.frames }
func (s *ffmpegSource) Close() error  { s.stop(); return nil }
