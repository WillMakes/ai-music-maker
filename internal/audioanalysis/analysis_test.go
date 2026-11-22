package audioanalysis

import (
	"math"
	"strings"
	"testing"
)

// synth builds seconds of stereo audio: a kick on every beat at bpm, and a
// sustained chord of the given frequencies (with two harmonics each) under it.
// sideGain puts that much of the chord out of phase between the channels.
func synth(seconds, bpm float64, chord []float64, sideGain float64) ([]float64, []float64) {
	const rate = 44100
	n := int(seconds * rate)
	left, right := make([]float64, n), make([]float64, n)
	period := 60 / bpm
	for i := 0; i < n; i++ {
		t := float64(i) / rate
		var pad float64
		for _, f := range chord {
			pad += 0.08*math.Sin(2*math.Pi*f*t) + 0.03*math.Sin(2*math.Pi*2*f*t) +
				0.015*math.Sin(2*math.Pi*3*f*t)
		}
		since := math.Mod(t, period)
		kick := 0.0
		if since < 0.12 {
			// A pitched-down thump with a click on top: onsets need a broadband
			// edge, which is what a real kick has.
			kick = 0.6 * math.Exp(-since*30) * math.Sin(2*math.Pi*(55+120*math.Exp(-since*40))*since)
			if since < 0.004 {
				kick += 0.3 * math.Sin(2*math.Pi*3000*since)
			}
		}
		left[i] = kick + pad*(1+sideGain)
		right[i] = kick + pad*(1-sideGain)
	}
	return left, right
}

var aMinor = []float64{220.00, 261.63, 329.63} // A3 C4 E4

func TestTempoOfAKnownPulse(t *testing.T) {
	for _, bpm := range []float64{96, 116, 128, 140} {
		left, right := synth(30, bpm, aMinor, 0)
		m := AnalyzePCM(left, right, 44100)
		if math.Abs(m.TempoBPM-bpm) > 1 {
			t.Errorf("%v BPM measured as %v (confidence %v)", bpm, m.TempoBPM, m.TempoConfidence)
		}
	}
}

func TestKeyOfAKnownChord(t *testing.T) {
	left, right := synth(20, 120, aMinor, 0)
	m := AnalyzePCM(left, right, 44100)
	if m.Key != "A minor" {
		t.Errorf("A–C–E measured as %q, want A minor", m.Key)
	}
	cMajor := []float64{261.63, 329.63, 392.00, 130.81} // C4 E4 G4 + C3
	left, right = synth(20, 120, cMajor, 0)
	if m := AnalyzePCM(left, right, 44100); m.Key != "C major" {
		t.Errorf("C–E–G measured as %q, want C major", m.Key)
	}
}

func TestStereoWidthAndDynamics(t *testing.T) {
	left, right := synth(10, 120, aMinor, 0)
	mono := AnalyzePCM(left, right, 44100)
	if mono.StereoWidth != 0 || mono.StereoCorrelation != 1 {
		t.Errorf("identical channels: width %v correlation %v, want 0 and 1", mono.StereoWidth, mono.StereoCorrelation)
	}
	left, right = synth(10, 120, aMinor, 0.8)
	wide := AnalyzePCM(left, right, 44100)
	if wide.StereoWidth <= 0.2 || wide.StereoCorrelation >= 0.9 {
		t.Errorf("a wide pad: width %v correlation %v", wide.StereoWidth, wide.StereoCorrelation)
	}
	if mono.CrestDB <= 0 || mono.PeakDBFS <= mono.RMSDBFS || mono.Seconds != 10 {
		t.Errorf("dynamics: crest %v peak %v seconds %v", mono.CrestDB, mono.PeakDBFS, mono.Seconds)
	}
	var total float64
	for _, b := range mono.Bands {
		total += b.Percent
	}
	if len(mono.Bands) != 5 || math.Abs(total-100) > 1 {
		t.Errorf("bands %+v add to %v%%", mono.Bands, total)
	}
	if len(mono.EnergyProfile) != 10 || mono.EnergyStepSeconds != 1 {
		t.Errorf("energy profile %v step %v", mono.EnergyProfile, mono.EnergyStepSeconds)
	}
}

func TestSilenceIsACaveatNotAGuess(t *testing.T) {
	m := AnalyzePCM(make([]float64, 44100*5), make([]float64, 44100*5), 44100)
	if m.Key != "" {
		t.Errorf("silence has key %q", m.Key)
	}
	if len(m.Caveats) == 0 {
		t.Error("silence measured without a caveat")
	}
}

func TestParseEBUR128ReadsOnlyTheSummary(t *testing.T) {
	out := `[Parsed_ebur128_0 @ 0x1] t: 0.1  TARGET:-23 LUFS    M: -40.0 S:-120.7     I: -40.0 LUFS       LRA:   0.0 LU  FTPK: -5.0 dBFS  TPK: -5.0 dBFS
[Parsed_ebur128_0 @ 0x1] Summary:

  Integrated loudness:
    I:         -10.8 LUFS
    Threshold: -20.8 LUFS

  Loudness range:
    LRA:         1.6 LU
    Threshold: -30.8 LUFS
    LRA low:   -11.6 LUFS
    LRA high:  -10.0 LUFS

  True peak:
    Peak:        1.2 dBFS`
	r, ok := ParseEBUR128(out)
	if !ok || r.Integrated != -10.8 || r.Range != 1.6 || r.TruePeak != 1.2 {
		t.Errorf("parsed %+v ok=%v", r, ok)
	}
	if _, ok := ParseEBUR128(strings.Split(out, "Summary")[0]); ok {
		t.Error("parsed a meter with no summary")
	}
}

func TestTempoOffersTheOtherOctaveOnlyWhenHeard(t *testing.T) {
	// A kick alone at 96: nothing between the beats, so no double-time.
	left, right := synth(30, 96, aMinor, 0)
	if m := AnalyzePCM(left, right, 44100); m.TempoAlternate != 0 {
		t.Errorf("a bare 96 BPM kick offered %v as the other octave", m.TempoAlternate)
	}
	// The same kick with a hat on every off-beat: 192 is as audible as 96.
	left, right = synth(30, 192, aMinor, 0)
	m := AnalyzePCM(left, right, 44100)
	if !((math.Abs(m.TempoBPM-96) < 1 && math.Abs(m.TempoAlternate-192) < 2) ||
		(math.Abs(m.TempoBPM-192) < 1 && math.Abs(m.TempoAlternate-96) < 1)) {
		t.Errorf("a 192 BPM pulse: %v BPM, alternate %v", m.TempoBPM, m.TempoAlternate)
	}
}
