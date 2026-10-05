package playback

import (
	"encoding/binary"
	"io"
)

// resampled plays a take at OutputRate whatever rate it was made at, by
// cubic (Catmull-Rom) interpolation between its frames. Cubic rather than
// linear because linear dulls the top end audibly on music, and rather than a
// windowed sinc because this is a listening player, not a mastering tool: the
// takes on disk are untouched, and anything exported is ffmpeg's to convert.
type resampled struct {
	src pcm
	// step is how far through the source one output frame moves.
	step float64
	// at is the position between hist[1] and hist[2], 0 to 1.
	at float64
	// hist holds four consecutive source frames, left and right.
	hist  [4][2]float64
	ended int // frames past the source's end fed as silence
	pos   int64
	in    []byte
	queue [][2]float64
}

// withRate returns src at OutputRate, or src itself when it already is.
func withRate(src pcm) pcm {
	if src.Rate() == OutputRate || src.Rate() <= 0 {
		return src
	}
	r := &resampled{src: src, step: float64(src.Rate()) / OutputRate}
	r.reset()
	return r
}

func (r *resampled) reset() {
	r.at, r.ended, r.queue = 0, 0, nil
	r.hist = [4][2]float64{}
	// Prime hist[1..3] so the first output frame is the source's first.
	for i := 1; i < 4; i++ {
		f, _ := r.next()
		r.hist[i] = f
	}
	r.hist[0] = r.hist[1]
}

// next is the source's next frame; past its end, silence, counted so Read
// knows when the interpolation has run out of real audio.
func (r *resampled) next() ([2]float64, bool) {
	if len(r.queue) == 0 {
		if cap(r.in) == 0 {
			r.in = make([]byte, 4096*bytesPerFrame)
		}
		n, _ := io.ReadFull(r.src, r.in)
		for i := 0; i+bytesPerFrame <= n; i += bytesPerFrame {
			r.queue = append(r.queue, [2]float64{
				float64(int16(binary.LittleEndian.Uint16(r.in[i:]))),
				float64(int16(binary.LittleEndian.Uint16(r.in[i+2:]))),
			})
		}
		if len(r.queue) == 0 {
			r.ended++
			return [2]float64{}, false
		}
	}
	f := r.queue[0]
	r.queue = r.queue[1:]
	return f, true
}

func (r *resampled) Read(p []byte) (int, error) {
	n := 0
	for n+bytesPerFrame <= len(p) {
		// Once two silent frames have entered, hist[2] is past the end.
		if r.ended >= 2 {
			break
		}
		for ch := 0; ch < 2; ch++ {
			y0, y1, y2, y3 := r.hist[0][ch], r.hist[1][ch], r.hist[2][ch], r.hist[3][ch]
			t := r.at
			v := y1 + 0.5*t*(y2-y0+t*(2*y0-5*y1+4*y2-y3+t*(3*(y1-y2)+y3-y0)))
			binary.LittleEndian.PutUint16(p[n+ch*2:], uint16(clamp16(int64(v))))
		}
		n += bytesPerFrame
		r.pos++
		r.at += r.step
		for r.at >= 1 {
			r.at--
			f, _ := r.next()
			r.hist[0], r.hist[1], r.hist[2], r.hist[3] = r.hist[1], r.hist[2], r.hist[3], f
		}
	}
	if n == 0 {
		return 0, io.EOF
	}
	return n, nil
}

func (r *resampled) Seek(offset int64, whence int) (int64, error) {
	var to int64
	switch whence {
	case io.SeekStart:
		to = offset
	case io.SeekCurrent:
		to = r.pos*bytesPerFrame + offset
	case io.SeekEnd:
		to = r.Frames()*bytesPerFrame + offset
	}
	frame := max(0, to/bytesPerFrame)
	srcFrame := int64(float64(frame) * r.step)
	if _, err := r.src.Seek(srcFrame*bytesPerFrame, io.SeekStart); err != nil {
		return 0, err
	}
	r.reset()
	r.pos = frame
	return frame * bytesPerFrame, nil
}

func (r *resampled) Rate() int { return OutputRate }

func (r *resampled) Frames() int64 {
	return int64(float64(r.src.Frames()) / r.step)
}

func (r *resampled) Close() error { return r.src.Close() }
