package audioanalysis

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

// pcm writes mono float32 samples the way ffmpeg's f32le pipe does.
func pcm(samples []float64) *bytes.Reader {
	var b bytes.Buffer
	for _, v := range samples {
		_ = binary.Write(&b, binary.LittleEndian, float32(v))
	}
	return bytes.NewReader(b.Bytes())
}

// song is seconds of a pulse over a chord, distinct per song by its pitches,
// brightness and tempo.
func song(seconds float64, freqs []float64, bpm, level float64) []float64 {
	n := int(seconds * segmentRate)
	out := make([]float64, n)
	for i := range out {
		t := float64(i) / segmentRate
		var v float64
		for _, f := range freqs {
			v += math.Sin(2 * math.Pi * f * t)
		}
		v /= float64(len(freqs))
		since := math.Mod(t, 60/bpm)
		pulse := 0.0
		if since < 0.08 {
			pulse = math.Exp(-since*40) * math.Sin(2*math.Pi*70*since)
		}
		out[i] = level * (0.6*v + 0.4*pulse)
	}
	return out
}

func TestSegmentFindsAGapAndAChange(t *testing.T) {
	var audio []float64
	audio = append(audio, song(90, []float64{220, 277, 330}, 120, 0.5)...)
	audio = append(audio, make([]float64, 2*segmentRate)...) // a two-second gap
	audio = append(audio, song(90, []float64{1760, 2637, 3520}, 140, 0.3)...)
	// Gapless into a dark, loud third song.
	audio = append(audio, song(90, []float64{82, 110}, 90, 0.8)...)

	features, seconds := secondFeatures(pcm(audio))
	if math.Abs(seconds-272) > 0.01 || len(features) < 270 {
		t.Fatalf("%v seconds, %d features", seconds, len(features))
	}
	segs := segmentFeatures(features, 45)
	if len(segs) != 3 {
		t.Fatalf("found %d tracks, want 3: %+v", len(segs), segs)
	}
	if math.Abs(segs[1].Start-92) > 2 || segs[1].Cue != "gap" {
		t.Errorf("second track %+v; want a gap boundary near 1:32", segs[1])
	}
	if math.Abs(segs[2].Start-182) > 3 || segs[2].Cue != "change" {
		t.Errorf("third track %+v; want a change boundary near 3:02", segs[2])
	}
	if segs[2].End < 271 {
		t.Errorf("the last track ends at %v, not the recording's end", segs[2].End)
	}
}

func TestSegmentLeavesOneSongWhole(t *testing.T) {
	features, _ := secondFeatures(pcm(song(240, []float64{220, 277, 330}, 120, 0.5)))
	if segs := segmentFeatures(features, 60); len(segs) != 1 {
		t.Errorf("one steady song split into %d: %+v", len(segs), segs)
	}
}
