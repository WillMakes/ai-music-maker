package playback

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/frame"
)

// flacSource reads a lossless master — the format a producer who has been
// through mastering imports (see audioExtensions in the app).
type flacSource struct {
	f      *os.File
	stream *flac.Stream
	pos    int64 // in frames
	// pending is decoded audio not yet read, as 16-bit stereo bytes.
	pending []byte
}

func openFLAC(path string) (pcm, error) {
	f, err := openFile(path)
	if err != nil {
		return nil, err
	}
	stream, err := flac.NewSeek(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("the FLAC could not be read: %w", err)
	}
	return &flacSource{f: f, stream: stream}, nil
}

func (s *flacSource) Read(p []byte) (int, error) {
	for len(s.pending) == 0 {
		fr, err := s.stream.ParseNext()
		if err == io.EOF {
			return 0, io.EOF
		}
		if err != nil {
			return 0, fmt.Errorf("the FLAC could not be decoded: %w", err)
		}
		s.pending = interleave(fr)
	}
	n := copy(p[:len(p)/bytesPerFrame*bytesPerFrame], s.pending)
	s.pending = s.pending[n:]
	s.pos += int64(n / bytesPerFrame)
	return n, nil
}

// interleave turns one FLAC frame into 16-bit stereo bytes: the first two
// channels, a mono one on both sides, every depth scaled to 16 bits.
func interleave(fr *frame.Frame) []byte {
	left := fr.Subframes[0].Samples
	right := left
	if len(fr.Subframes) > 1 {
		right = fr.Subframes[1].Samples
	}
	shift := int(fr.BitsPerSample) - 16
	to16 := func(v int32) int16 {
		if shift > 0 {
			return int16(v >> shift)
		}
		return clamp16(int64(v) << -shift)
	}
	out := make([]byte, len(left)*bytesPerFrame)
	for i := range left {
		binary.LittleEndian.PutUint16(out[i*4:], uint16(to16(left[i])))
		binary.LittleEndian.PutUint16(out[i*4+2:], uint16(to16(right[i])))
	}
	return out
}

func (s *flacSource) Seek(offset int64, whence int) (int64, error) {
	var to int64
	switch whence {
	case io.SeekStart:
		to = offset
	case io.SeekCurrent:
		to = s.pos*bytesPerFrame + offset
	case io.SeekEnd:
		to = s.Frames()*bytesPerFrame + offset
	}
	target := uint64(max(0, to/bytesPerFrame))
	if total := uint64(s.Frames()); total > 0 && target > total {
		target = total
	}
	// FLAC seeks to the frame holding the sample; the rest of the way is
	// decoded and dropped, so a seek lands where it was asked to.
	first, err := s.stream.Seek(target)
	if err != nil {
		return 0, err
	}
	s.pending = nil
	s.pos = int64(first)
	skip := int64(target - first)
	for skip > 0 {
		fr, err := s.stream.ParseNext()
		if err != nil {
			break
		}
		block := interleave(fr)
		drop := min(skip*bytesPerFrame, int64(len(block)))
		s.pending = block[drop:]
		s.pos += drop / bytesPerFrame
		skip -= drop / bytesPerFrame
	}
	return s.pos * bytesPerFrame, nil
}

func (s *flacSource) Rate() int     { return int(s.stream.Info.SampleRate) }
func (s *flacSource) Frames() int64 { return int64(s.stream.Info.NSamples) }
func (s *flacSource) Close() error  { s.stream.Close(); return s.f.Close() }
