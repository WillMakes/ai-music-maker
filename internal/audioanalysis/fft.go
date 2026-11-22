package audioanalysis

import (
	"math"
	"math/cmplx"
)

// fft is an in-place iterative radix-2 transform. len(x) must be a power of
// two; every caller here frames its own input, so that is always true.
//
// Hand-written rather than a dependency: it is thirty lines, the analysis
// runs a few hundred frames of it, and a numeric library pulled in for one
// function is a module every platform build has to carry.
func fft(x []complex128) {
	n := len(x)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		step := cmplx.Exp(complex(0, -2*math.Pi/float64(size)))
		for start := 0; start < n; start += size {
			w := complex(1, 0)
			for k := 0; k < size/2; k++ {
				a, b := x[start+k], x[start+k+size/2]*w
				x[start+k], x[start+k+size/2] = a+b, a-b
				w *= step
			}
		}
	}
}

// hann is the analysis window, cached per size by the caller.
func hann(n int) []float64 {
	w := make([]float64, n)
	for i := range w {
		w[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n))
	}
	return w
}

// magnitudes frames signal at size/hop and returns each frame's magnitude
// spectrum (bins 0..size/2).
func magnitudes(signal []float64, size, hop int) [][]float64 {
	if len(signal) < size {
		return nil
	}
	window := hann(size)
	buf := make([]complex128, size)
	frames := [][]float64{}
	for start := 0; start+size <= len(signal); start += hop {
		for i := 0; i < size; i++ {
			buf[i] = complex(signal[start+i]*window[i], 0)
		}
		fft(buf)
		mags := make([]float64, size/2+1)
		for i := range mags {
			mags[i] = cmplx.Abs(buf[i])
		}
		frames = append(frames, mags)
	}
	return frames
}
