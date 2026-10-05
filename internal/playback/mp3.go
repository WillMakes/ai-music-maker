package playback

import (
	"fmt"
	"os"

	"github.com/hajimehoshi/go-mp3"
)

// mp3Source is a take Suno handed back, the common case. go-mp3 already
// decodes to 16-bit stereo, so it is read straight through.
type mp3Source struct {
	f   *os.File
	dec *mp3.Decoder
}

func openMP3(path string) (pcm, error) {
	f, err := openFile(path)
	if err != nil {
		return nil, err
	}
	dec, err := mp3.NewDecoder(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("the MP3 could not be read: %w", err)
	}
	return &mp3Source{f: f, dec: dec}, nil
}

func (s *mp3Source) Read(p []byte) (int, error) { return s.dec.Read(p) }

func (s *mp3Source) Seek(offset int64, whence int) (int64, error) {
	return s.dec.Seek(offset, whence)
}

func (s *mp3Source) Rate() int { return s.dec.SampleRate() }

func (s *mp3Source) Frames() int64 {
	if n := s.dec.Length(); n > 0 {
		return n / bytesPerFrame
	}
	return 0
}

func (s *mp3Source) Close() error { return s.f.Close() }
