// Package audioanalysis measures a short piece of music: tempo, key, loudness,
// dynamics, spectral balance and stereo image.
//
// It exists for the inspiration ranges (see internal/app/inspirationbreakdown.go).
// A producer pointing at "1:12–1:48, the pre-chorus build" wants that section
// described, and a model researching the song on the web can only describe the
// song. These numbers are what was actually heard, and they are kept apart from
// anything a model writes about them so the producer can tell the two apart.
//
// Everything here is plain DSP on decoded samples except loudness, which is
// ffmpeg's ebur128 filter: the EBU R128 gating and true-peak oversampling are a
// standard with a reference implementation, and reimplementing a standard is
// how a number ends up a decibel off the meter every mastering engineer uses.
package audioanalysis

import (
	"math"
	"sort"
)

// Measurements is what was measured, never what was inferred. Every value is
// read off the samples (or ffmpeg's R128 meter); a field the analysis could
// not settle says so in Caveats rather than guessing.
type Measurements struct {
	// Seconds is the decoded section's real length.
	Seconds    float64 `json:"seconds"`
	SampleRate int     `json:"sampleRate"`
	// TempoBPM is the dominant beat period. TempoConfidence is 0–1: how far
	// the winning period stands above the rest of the autocorrelation. Below
	// ~0.3 the section has no steady pulse (a rubato intro, a breakdown).
	TempoBPM        float64 `json:"tempoBpm"`
	TempoConfidence float64 `json:"tempoConfidence"`
	// TempoAlternate is the double- or half-time reading when the audio
	// supports it as well, 0 otherwise. Beat trackers and people disagree by
	// exactly an octave all the time — a 171 BPM synth-pop chorus pulses at
	// 85.5 — and stating one of the two as certain is how a brief gets the
	// feel wrong.
	TempoAlternate float64 `json:"tempoAlternate"`
	// Key is "F# minor" and the like, from a chromagram correlated with the
	// Krumhansl–Kessler profiles. KeyConfidence is the margin over the best
	// different key, 0–1; small margins are usually the relative major/minor.
	Key           string  `json:"key"`
	KeyConfidence float64 `json:"keyConfidence"`
	// KeyRunnerUp is the next-best key, so an ambiguous reading shows what
	// it was torn between (usually the parallel or relative key).
	KeyRunnerUp string `json:"keyRunnerUp"`
	// The EBU R128 figures, from ffmpeg. Zero with LoudnessMeasured false
	// when ffmpeg's meter could not be read.
	LoudnessMeasured bool    `json:"loudnessMeasured"`
	IntegratedLUFS   float64 `json:"integratedLufs"`
	LoudnessRangeLU  float64 `json:"loudnessRangeLu"`
	TruePeakDBTP     float64 `json:"truePeakDbtp"`
	// Sample-level dynamics across both channels.
	PeakDBFS float64 `json:"peakDbfs"`
	RMSDBFS  float64 `json:"rmsDbfs"`
	CrestDB  float64 `json:"crestDb"`
	// Spectral balance. Centroid is the "brightness" a mix engineer means;
	// rolloff is the frequency 85% of the spectral magnitude sits below.
	SpectralCentroidHz float64 `json:"spectralCentroidHz"`
	RolloffHz          float64 `json:"rolloffHz"`
	Bands              []Band  `json:"bands"`
	// StereoWidth is side RMS over mid RMS: 0 is mono, ~0.3–0.6 a typical
	// modern mix, above 1 more out-of-phase than not. Correlation is the
	// phase meter's reading, 1 mono, 0 uncorrelated, negative out of phase.
	StereoWidth       float64 `json:"stereoWidth"`
	StereoCorrelation float64 `json:"stereoCorrelation"`
	// EnergyProfile is RMS dBFS per EnergyStepSeconds across the section, so
	// a build or a drop shows as numbers and not only as an adjective.
	EnergyProfile     []float64 `json:"energyProfile"`
	EnergyStepSeconds float64   `json:"energyStepSeconds"`
	// Caveats is what the reader should not over-trust.
	Caveats []string `json:"caveats"`
}

// Band is one region of the spectrum and its share of the section's energy.
type Band struct {
	Name    string  `json:"name"`
	LowHz   float64 `json:"lowHz"`
	HighHz  float64 `json:"highHz"`
	Percent float64 `json:"percent"`
}

// bands are the regions a mix is talked about in.
var bands = []Band{
	{Name: "sub", LowHz: 20, HighHz: 60},
	{Name: "low", LowHz: 60, HighHz: 250},
	{Name: "low-mid", LowHz: 250, HighHz: 2000},
	{Name: "high-mid", LowHz: 2000, HighHz: 6000},
	{Name: "air", LowHz: 6000, HighHz: 20000},
}

// AnalyzePCM measures interleaved stereo float samples. Loudness is left
// unmeasured here — see Measure, which adds ffmpeg's meter.
func AnalyzePCM(left, right []float64, rate int) Measurements {
	m := Measurements{SampleRate: rate, Bands: []Band{}, EnergyProfile: []float64{}, Caveats: []string{}}
	n := len(left)
	if len(right) < n {
		n = len(right)
	}
	if n == 0 || rate <= 0 {
		m.Caveats = append(m.Caveats, "the section decoded to no audio")
		return m
	}
	left, right = left[:n], right[:n]
	m.Seconds = round(float64(n)/float64(rate), 2)

	mono := make([]float64, n)
	var peak, sumSq, sumL, sumR, sumLR, sumMid, sumSide float64
	for i := 0; i < n; i++ {
		l, r := left[i], right[i]
		mono[i] = (l + r) / 2
		peak = math.Max(peak, math.Max(math.Abs(l), math.Abs(r)))
		sumSq += l*l + r*r
		sumL += l * l
		sumR += r * r
		sumLR += l * r
		mid, side := (l+r)/2, (l-r)/2
		sumMid += mid * mid
		sumSide += side * side
	}
	m.PeakDBFS = round(dB(peak), 1)
	m.RMSDBFS = round(dB(math.Sqrt(sumSq/float64(2*n))), 1)
	m.CrestDB = round(m.PeakDBFS-m.RMSDBFS, 1)
	if sumMid > 0 {
		m.StereoWidth = round(math.Sqrt(sumSide/sumMid), 2)
	}
	if sumL > 0 && sumR > 0 {
		m.StereoCorrelation = round(sumLR/math.Sqrt(sumL*sumR), 2)
	}
	m.EnergyProfile, m.EnergyStepSeconds = energyProfile(mono, rate)

	// One STFT for tempo and spectral balance: 2048 at 44.1 kHz is ~46 ms,
	// fine enough in time for onsets and in frequency for five broad bands.
	frames := magnitudes(mono, 2048, 512)
	if len(frames) < 8 {
		m.Caveats = append(m.Caveats, "the section is too short to measure tempo or spectrum")
		return m
	}
	spectral(&m, frames, rate, 2048)
	m.TempoBPM, m.TempoConfidence, m.TempoAlternate = tempo(frames, float64(rate)/512)
	if m.TempoConfidence < 0.3 {
		m.Caveats = append(m.Caveats, "no steady pulse — the tempo is a weak estimate")
	}

	// Key wants frequency resolution instead: 16384 points is ~2.7 Hz per bin,
	// enough to tell a low E from an F.
	m.Key, m.KeyConfidence, m.KeyRunnerUp = key(magnitudes(mono, 16384, 8192), rate, 16384)
	if m.Key != "" && m.KeyConfidence < 0.1 {
		m.Caveats = append(m.Caveats, "the key is ambiguous — likely its relative major/minor or a modal centre")
	}
	return m
}

func spectral(m *Measurements, frames [][]float64, rate, size int) {
	binHz := float64(rate) / float64(size)
	// Centroid and rolloff on magnitude, bands on power. Weighted by power, the
	// centroid of any modern mix sits on the kick and bass and says nothing
	// about the top end; magnitude is what brightness measures conventionally
	// use. Band shares are energy, which is what "62% of it is low end" means.
	power := make([]float64, len(frames[0]))
	mag := make([]float64, len(frames[0]))
	for _, f := range frames {
		for i, v := range f {
			power[i] += v * v
			mag[i] += v
		}
	}
	var total, magTotal, weighted float64
	for i := range power {
		total += power[i]
		magTotal += mag[i]
		weighted += mag[i] * float64(i) * binHz
	}
	if total == 0 {
		m.Caveats = append(m.Caveats, "the section is silent")
		return
	}
	m.SpectralCentroidHz = math.Round(weighted / magTotal)
	var running float64
	for i, v := range mag {
		running += v
		if running >= 0.85*magTotal {
			m.RolloffHz = math.Round(float64(i) * binHz)
			break
		}
	}
	for _, b := range bands {
		var e float64
		for i, p := range power {
			f := float64(i) * binHz
			if f >= b.LowHz && f < b.HighHz {
				e += p
			}
		}
		b.Percent = round(100*e/total, 1)
		m.Bands = append(m.Bands, b)
	}
}

// tempo is spectral-flux onset strength, autocorrelated over the lags of 50–
// 220 BPM with a prior toward 120, then refined on a fine grid by how well a
// pulse train at each candidate lines up with the onsets. The autocorrelation
// alone is quantised to whole frames — 117 and 120 BPM are a frame apart at
// this hop — and the refinement is what makes the answer usable as a number
// rather than a neighbourhood.
func tempo(frames [][]float64, frameRate float64) (float64, float64, float64) {
	onset := make([]float64, len(frames))
	prev := make([]float64, len(frames[0]))
	for t, f := range frames {
		var flux float64
		for i, v := range f {
			c := math.Log1p(100 * v)
			if d := c - prev[i]; d > 0 && t > 0 {
				flux += d
			}
			prev[i] = c
		}
		onset[t] = flux
	}
	// Remove the slow trend (a crescendo is not a beat) and keep only rises.
	win := int(frameRate / 2)
	trend := movingAverage(onset, win)
	for i := range onset {
		onset[i] = math.Max(0, onset[i]-trend[i])
	}

	minLag := int(math.Floor(60 * frameRate / 220))
	maxLag := int(math.Ceil(60 * frameRate / 50))
	if maxLag >= len(onset)/2 {
		maxLag = len(onset)/2 - 1
	}
	if minLag < 1 || maxLag <= minLag {
		return 0, 0, 0
	}
	ac := make([]float64, maxLag+2)
	for lag := 0; lag <= maxLag+1; lag++ {
		var s float64
		for i := 0; i+lag < len(onset); i++ {
			s += onset[i] * onset[i+lag]
		}
		ac[lag] = s / float64(len(onset)-lag)
	}
	if ac[0] == 0 {
		return 0, 0, 0
	}
	best, bestScore := 0, -1.0
	for lag := minLag; lag <= maxLag; lag++ {
		bpm := 60 * frameRate / float64(lag)
		// A one-octave-wide prior around 120 BPM: the autocorrelation is just
		// as high at twice and half the period, and listeners (and Suno's tempo
		// words) sit in the middle.
		prior := math.Exp(-0.5 * math.Pow(math.Log2(bpm/120)/1.0, 2))
		if score := ac[lag] * prior; score > bestScore {
			best, bestScore = lag, score
		}
	}
	// Parabolic interpolation around the integer peak.
	lag := float64(best)
	if best > 1 && best < len(ac)-1 {
		a, b, c := ac[best-1], ac[best], ac[best+1]
		if d := a - 2*b + c; d != 0 {
			lag += 0.5 * (a - c) / d
		}
	}
	coarse := 60 * frameRate / lag

	refined, bestFit := coarse, -1.0
	for bpm := coarse * 0.97; bpm <= coarse*1.03; bpm += 0.05 {
		if fit := pulseFit(onset, 60*frameRate/bpm); fit > bestFit {
			refined, bestFit = bpm, fit
		}
	}

	// Confidence: how far the chosen period stands above the typical lag.
	others := append([]float64(nil), ac[minLag:maxLag+1]...)
	sort.Float64s(others)
	median := others[len(others)/2]
	conf := 0.0
	if ac[best] > 0 {
		conf = (ac[best] - median) / ac[best]
	}
	// The other octave. Half-time is always as periodic as the beat (every
	// second beat lines up too), so it is offered only for a fast reading;
	// double-time only when there are onsets between the beats strong enough
	// to carry a pulse of their own.
	alternate := 0.0
	if half := int(math.Round(lag / 2)); refined < 100 && half >= 1 && ac[best] > 0 &&
		ac[half]/ac[best] >= 0.5 {
		alternate = refined * 2
	} else if refined > 140 {
		alternate = refined / 2
	}
	return round(refined, 1), round(clamp(conf, 0, 1), 2), round(alternate, 1)
}

// pulseFit is the onset strength a pulse train of this period collects at
// its best phase.
func pulseFit(onset []float64, period float64) float64 {
	best := 0.0
	for phase := 0.0; phase < period; phase++ {
		var s float64
		count := 0
		for t := phase; t < float64(len(onset)-1); t += period {
			i := int(t)
			frac := t - float64(i)
			s += onset[i]*(1-frac) + onset[i+1]*frac
			count++
		}
		if count > 0 && s/float64(count) > best {
			best = s / float64(count)
		}
	}
	return best
}

// The Krumhansl–Kessler key profiles, C first.
var (
	majorProfile = []float64{6.35, 2.23, 3.48, 2.33, 4.38, 4.09, 2.52, 5.19, 2.39, 3.66, 2.29, 2.88}
	minorProfile = []float64{6.33, 2.68, 3.52, 5.38, 2.60, 3.53, 2.54, 4.75, 3.98, 2.69, 3.34, 3.17}
	pitchNames   = []string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}
)

func key(frames [][]float64, rate, size int) (string, float64, string) {
	if len(frames) == 0 {
		return "", 0, ""
	}
	binHz := float64(rate) / float64(size)
	chroma := make([]float64, 12)
	for _, f := range frames {
		frame := make([]float64, 12)
		var sum float64
		for i, v := range f {
			hz := float64(i) * binHz
			// C2 to C7: below, a bin spans several semitones; above, it is
			// harmonics and cymbals more than notes.
			if hz < 65 || hz > 2100 {
				continue
			}
			pc := (int(math.Round(69+12*math.Log2(hz/440)))%12 + 12) % 12
			frame[pc] += v
			sum += v
		}
		if sum == 0 {
			continue
		}
		for i := range frame {
			chroma[i] += frame[i] / sum
		}
	}
	type score struct {
		name  string
		tonic int
		r     float64
	}
	scores := []score{}
	for tonic := 0; tonic < 12; tonic++ {
		scores = append(scores,
			score{pitchNames[tonic] + " major", tonic, correlate(chroma, majorProfile, tonic)},
			score{pitchNames[tonic] + " minor", tonic, correlate(chroma, minorProfile, tonic)})
	}
	sort.Slice(scores, func(i, j int) bool { return scores[i].r > scores[j].r })
	if math.IsNaN(scores[0].r) {
		return "", 0, ""
	}
	return scores[0].name, round(clamp(scores[0].r-scores[1].r, 0, 1), 3), scores[1].name
}

// correlate is Pearson's r between the chroma and the profile rotated to tonic.
func correlate(chroma, profile []float64, tonic int) float64 {
	var mx, my float64
	for i := 0; i < 12; i++ {
		mx += chroma[i]
		my += profile[(i-tonic+12)%12]
	}
	mx, my = mx/12, my/12
	var sxy, sxx, syy float64
	for i := 0; i < 12; i++ {
		x, y := chroma[i]-mx, profile[(i-tonic+12)%12]-my
		sxy += x * y
		sxx += x * x
		syy += y * y
	}
	if sxx == 0 || syy == 0 {
		return math.NaN()
	}
	return sxy / math.Sqrt(sxx*syy)
}

// energyProfile is RMS per step, with the step chosen so a section of any
// length reads as at most sixteen numbers.
func energyProfile(mono []float64, rate int) ([]float64, float64) {
	seconds := float64(len(mono)) / float64(rate)
	step := math.Max(1, math.Ceil(seconds/16))
	size := int(step * float64(rate))
	out := []float64{}
	for start := 0; start < len(mono); start += size {
		end := start + size
		if end > len(mono) {
			end = len(mono)
		}
		if end-start < size/2 && len(out) > 0 {
			break // a trailing sliver is not a step
		}
		var s float64
		for _, v := range mono[start:end] {
			s += v * v
		}
		out = append(out, round(dB(math.Sqrt(s/float64(end-start))), 1))
	}
	return out, step
}

func movingAverage(x []float64, win int) []float64 {
	if win < 1 {
		win = 1
	}
	out := make([]float64, len(x))
	var sum float64
	for i := range x {
		sum += x[i]
		if i >= win {
			sum -= x[i-win]
		}
		n := win
		if i+1 < win {
			n = i + 1
		}
		out[i] = sum / float64(n)
	}
	return out
}

func dB(v float64) float64 {
	if v <= 1e-10 {
		return -200
	}
	return 20 * math.Log10(v)
}

func round(v float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(v*p) / p
}

func clamp(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, v))
}
