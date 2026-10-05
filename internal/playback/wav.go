package playback

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
)

// wavSource reads PCM WAV: Suno's WAV download, and a take kept as WAV because
// ffmpeg was missing at import (#93). 16-, 24- and 32-bit integers and 32-bit
// float, mono or more channels — the first two are played, a mono take on
// both sides.
type wavSource struct {
	f        *os.File
	start    int64 // where the samples begin in the file
	frames   int64
	rate     int
	channels int
	bits     int
	float    bool
	pos      int64 // in frames
	buf      []byte
}

func openWAV(path string) (pcm, error) {
	f, err := openFile(path)
	if err != nil {
		return nil, err
	}
	s, err := parseWAV(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	return s, nil
}

func parseWAV(f *os.File) (*wavSource, error) {
	var riff [12]byte
	if _, err := io.ReadFull(f, riff[:]); err != nil || string(riff[0:4]) != "RIFF" || string(riff[8:12]) != "WAVE" {
		return nil, errors.New("the WAV could not be read: it is not a RIFF/WAVE file")
	}
	s := &wavSource{f: f}
	var haveFormat bool
	for {
		var head [8]byte
		if _, err := io.ReadFull(f, head[:]); err != nil {
			return nil, errors.New("the WAV could not be read: it has no audio in it")
		}
		size := int64(binary.LittleEndian.Uint32(head[4:8]))
		at, _ := f.Seek(0, io.SeekCurrent)
		switch string(head[0:4]) {
		case "fmt ":
			body := make([]byte, size)
			if _, err := io.ReadFull(f, body); err != nil || len(body) < 16 {
				return nil, errors.New("the WAV could not be read: its format is cut short")
			}
			format := binary.LittleEndian.Uint16(body[0:2])
			s.channels = int(binary.LittleEndian.Uint16(body[2:4]))
			s.rate = int(binary.LittleEndian.Uint32(body[4:8]))
			s.bits = int(binary.LittleEndian.Uint16(body[14:16]))
			// WAVE_FORMAT_EXTENSIBLE names the real format in its sub-format.
			if format == 0xFFFE && len(body) >= 26 {
				format = binary.LittleEndian.Uint16(body[24:26])
			}
			switch {
			case format == 1 && (s.bits == 16 || s.bits == 24 || s.bits == 32):
			case format == 3 && s.bits == 32:
				s.float = true
			default:
				return nil, fmt.Errorf("the WAV's encoding (format %d, %d-bit) cannot be played", format, s.bits)
			}
			if s.channels < 1 || s.rate <= 0 {
				return nil, errors.New("the WAV could not be read: its format is not one a player can use")
			}
			haveFormat = true
		case "data":
			if !haveFormat {
				return nil, errors.New("the WAV could not be read: its samples come before their format")
			}
			s.start = at
			s.frames = size / int64(s.frameBytes())
			// A WAV still being written says its data is 0 or ~4 GB long;
			// the file's own size is the truth then.
			if info, err := f.Stat(); err == nil {
				if most := (info.Size() - at) / int64(s.frameBytes()); s.frames == 0 || s.frames > most {
					s.frames = most
				}
			}
			return s, nil
		}
		// Chunks are padded to an even size.
		if _, err := f.Seek(at+size+size%2, io.SeekStart); err != nil {
			return nil, err
		}
	}
}

func (s *wavSource) frameBytes() int { return s.channels * s.bits / 8 }

func (s *wavSource) Read(p []byte) (int, error) {
	want := int64(len(p) / bytesPerFrame)
	if left := s.frames - s.pos; want > left {
		want = left
	}
	if want <= 0 {
		return 0, io.EOF
	}
	need := int(want) * s.frameBytes()
	if cap(s.buf) < need {
		s.buf = make([]byte, need)
	}
	raw := s.buf[:need]
	n, err := io.ReadFull(s.f, raw)
	got := n / s.frameBytes()
	step := s.bits / 8
	for i := 0; i < got; i++ {
		frame := raw[i*s.frameBytes():]
		left := s.sample(frame[0:step])
		right := left
		if s.channels > 1 {
			right = s.sample(frame[step : 2*step])
		}
		binary.LittleEndian.PutUint16(p[i*4:], uint16(left))
		binary.LittleEndian.PutUint16(p[i*4+2:], uint16(right))
	}
	s.pos += int64(got)
	if got == 0 && err != nil {
		return 0, io.EOF
	}
	return got * bytesPerFrame, nil
}

// sample is one channel of one frame, as 16 bits.
func (s *wavSource) sample(b []byte) int16 {
	switch {
	case s.float:
		v := math.Float32frombits(binary.LittleEndian.Uint32(b))
		return clamp16(int64(math.Round(float64(v) * 32767)))
	case s.bits == 16:
		return int16(binary.LittleEndian.Uint16(b))
	case s.bits == 24:
		v := int32(b[0]) | int32(b[1])<<8 | int32(int8(b[2]))<<16
		return int16(v >> 8)
	default: // 32-bit integer
		return int16(int32(binary.LittleEndian.Uint32(b)) >> 16)
	}
}

func (s *wavSource) Seek(offset int64, whence int) (int64, error) {
	var to int64
	switch whence {
	case io.SeekStart:
		to = offset
	case io.SeekCurrent:
		to = s.pos*bytesPerFrame + offset
	case io.SeekEnd:
		to = s.frames*bytesPerFrame + offset
	}
	frame := max(0, min(to/bytesPerFrame, s.frames))
	if _, err := s.f.Seek(s.start+frame*int64(s.frameBytes()), io.SeekStart); err != nil {
		return 0, err
	}
	s.pos = frame
	return frame * bytesPerFrame, nil
}

func (s *wavSource) Rate() int     { return s.rate }
func (s *wavSource) Frames() int64 { return s.frames }
func (s *wavSource) Close() error  { return s.f.Close() }
