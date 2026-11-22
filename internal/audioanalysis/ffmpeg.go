package audioanalysis

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"aimusicmaker/internal/platform"
)

// ErrNoFFmpeg is Measure's answer when ffmpeg is not on this machine.
var ErrNoFFmpeg = errors.New("ffmpeg is not installed, so the section's audio cannot be measured")

// analysisRate is what the section is decoded at. 44.1 kHz whatever the
// source: the band edges and the key's bin spacing are worked out against it.
const analysisRate = 44100

// maxDecodeSeconds bounds the decode. A range is a section of a song, and a
// "section" an hour long is a mistake that would otherwise decode 1.2 GB of
// floats into memory.
const maxDecodeSeconds = 600

// Measure decodes an audio file with ffmpeg and measures it.
func Measure(ctx context.Context, path string) (Measurements, error) {
	ffmpeg, ok := platform.LookUp("ffmpeg")
	if !ok {
		return Measurements{}, ErrNoFFmpeg
	}
	cmd := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-nostdin", "-i", path,
		"-t", strconv.Itoa(maxDecodeSeconds), "-ac", "2", "-ar", strconv.Itoa(analysisRate),
		"-f", "f32le", "pipe:1")
	platform.HideWindow(cmd)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return Measurements{}, fmt.Errorf("ffmpeg could not decode the section: %s", firstLine(stderr.String(), err))
	}
	left, right := deinterleave(out.Bytes())
	if len(left) == 0 {
		return Measurements{}, errors.New("ffmpeg decoded the section to no audio")
	}
	m := AnalyzePCM(left, right, analysisRate)
	if m.Seconds >= maxDecodeSeconds-0.5 {
		m.Caveats = append(m.Caveats, fmt.Sprintf(
			"only the first %d minutes of the section were measured", maxDecodeSeconds/60))
	}

	// The meter is a second pass rather than a filter on the decode, so a
	// meter that fails costs the loudness figures and not the rest.
	cmd = exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-nostats", "-nostdin", "-i", path,
		"-t", strconv.Itoa(maxDecodeSeconds), "-af", "ebur128=peak=true", "-f", "null", "-")
	platform.HideWindow(cmd)
	stderr.Reset()
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
		if r, ok := ParseEBUR128(stderr.String()); ok {
			m.LoudnessMeasured = true
			m.IntegratedLUFS, m.LoudnessRangeLU, m.TruePeakDBTP = r.Integrated, r.Range, r.TruePeak
		}
	}
	if !m.LoudnessMeasured {
		m.Caveats = append(m.Caveats, "ffmpeg's loudness meter could not be read, so there are no LUFS figures")
	}
	return m, nil
}

func deinterleave(raw []byte) ([]float64, []float64) {
	frames := len(raw) / 8
	left, right := make([]float64, frames), make([]float64, frames)
	for i := 0; i < frames; i++ {
		left[i] = float64(math.Float32frombits(binary.LittleEndian.Uint32(raw[i*8:])))
		right[i] = float64(math.Float32frombits(binary.LittleEndian.Uint32(raw[i*8+4:])))
	}
	return left, right
}

// R128 is the summary ffmpeg's ebur128 filter prints.
type R128 struct {
	Integrated, Range, TruePeak float64
}

var (
	r128Integrated = regexp.MustCompile(`(?m)^\s*I:\s+(-?[\d.]+|-inf)\s+LUFS`)
	r128Range      = regexp.MustCompile(`(?m)^\s*LRA:\s+(-?[\d.]+)\s+LU`)
	r128Peak       = regexp.MustCompile(`(?m)^\s*Peak:\s+(-?[\d.]+|-inf)\s+dBFS`)
)

// ParseEBUR128 reads the summary block at the end of ebur128's output. Only
// the block after "Summary:" — the running per-frame lines above it carry the
// same labels with momentary values.
func ParseEBUR128(stderr string) (R128, bool) {
	at := strings.LastIndex(stderr, "Summary:")
	if at < 0 {
		return R128{}, false
	}
	summary := stderr[at:]
	var r R128
	parse := func(re *regexp.Regexp) (float64, bool) {
		match := re.FindStringSubmatch(summary)
		if match == nil {
			return 0, false
		}
		if match[1] == "-inf" {
			return -200, true
		}
		v, err := strconv.ParseFloat(match[1], 64)
		return v, err == nil
	}
	var ok bool
	if r.Integrated, ok = parse(r128Integrated); !ok {
		return R128{}, false
	}
	r.Range, _ = parse(r128Range)
	r.TruePeak, _ = parse(r128Peak)
	return r, true
}

func firstLine(stderr string, err error) string {
	for _, line := range strings.Split(stderr, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return err.Error()
}
