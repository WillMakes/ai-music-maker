package audioanalysis

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"sort"
	"strconv"

	"aimusicmaker/internal/platform"
)

// ---------------------------------------------------------------------------
// Finding the tracks in a long recording
//
// A full-album upload or a DJ mix is one video with many songs in it. When
// YouTube gives no chapters and the description no tracklist, the songs have
// to be heard apart. Two cues do it:
//
//   - a gap: most albums put a second or two of near-silence between songs;
//   - a change of sound: a gapless album or a mix moves from one song's
//     timbre and level to the next's, which shows as a jump between the
//     average spectrum of the seconds before a point and the seconds after.
//
// Both are scored per second, and the strongest points at least a minimum
// song length apart become boundaries. It is a proposal: the producer sees it
// as a list of ranges, and renames, merges or deletes what it got wrong.
// ---------------------------------------------------------------------------

// Segment is one detected track, in seconds from the start of the recording.
type Segment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	// Cue is what put a boundary at the segment's start: "start", "gap" or
	// "change".
	Cue string `json:"cue"`
}

const (
	segmentRate  = 11025
	segmentFrame = 2048
	// maxSegmentSeconds bounds the recording: three hours decodes as a stream
	// and keeps only per-second features, so memory is not the limit — time
	// is, and nothing longer is an album.
	maxSegmentSeconds = 3 * 3600
)

// SegmentTracks decodes a long recording with ffmpeg and proposes its tracks.
// minTrack is the shortest song it will propose (seconds).
func SegmentTracks(ctx context.Context, path string, minTrack float64) ([]Segment, float64, error) {
	ffmpeg, ok := platform.LookUp("ffmpeg")
	if !ok {
		return nil, 0, ErrNoFFmpeg
	}
	cmd := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-nostdin", "-i", path,
		"-t", strconv.Itoa(maxSegmentSeconds), "-ac", "1", "-ar", strconv.Itoa(segmentRate),
		"-f", "f32le", "pipe:1")
	platform.HideWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, 0, err
	}
	if err := cmd.Start(); err != nil {
		return nil, 0, fmt.Errorf("ffmpeg could not start: %w", err)
	}
	features, seconds := secondFeatures(bufio.NewReaderSize(stdout, 1<<16))
	if err := cmd.Wait(); err != nil && len(features) == 0 {
		return nil, 0, fmt.Errorf("ffmpeg could not decode the recording: %w", err)
	}
	if len(features) == 0 {
		return nil, 0, errors.New("the recording decoded to no audio")
	}
	return segmentFeatures(features, minTrack), seconds, nil
}

// feature is one second: its level, and its spectrum in log-spaced bands.
type feature struct {
	rmsDB float64
	bands []float64
}

const bandCount = 12

// secondFeatures reads mono float samples and keeps one feature per second.
func secondFeatures(r io.Reader) ([]feature, float64) {
	window := hann(segmentFrame)
	buf := make([]complex128, segmentFrame)
	frame := make([]float64, segmentFrame)
	edges := bandEdges()
	var (
		out                 []feature
		acc                 = make([]float64, bandCount)
		accSq               float64
		accN, frames, total int
		raw                 = make([]byte, 4)
	)
	flush := func() {
		if frames == 0 {
			return
		}
		f := feature{rmsDB: dB(math.Sqrt(accSq / float64(accN))), bands: make([]float64, bandCount)}
		for i := range acc {
			f.bands[i] = math.Log10(1e-9 + acc[i]/float64(frames))
			acc[i] = 0
		}
		out = append(out, f)
		accSq, accN, frames = 0, 0, 0
	}
	n := 0
	for {
		if _, err := io.ReadFull(r, raw); err != nil {
			break
		}
		v := float64(math.Float32frombits(binary.LittleEndian.Uint32(raw)))
		frame[n] = v
		accSq += v * v
		accN++
		n++
		total++
		if n == segmentFrame {
			for i := range buf {
				buf[i] = complex(frame[i]*window[i], 0)
			}
			fft(buf)
			for b := 0; b < bandCount; b++ {
				var e float64
				for k := edges[b]; k < edges[b+1]; k++ {
					re, im := real(buf[k]), imag(buf[k])
					e += re*re + im*im
				}
				acc[b] += e
			}
			frames++
			n = 0
		}
		// A frame is ~0.19 s; five or six make a second. The feature is cut at
		// the frame that crosses each whole second, so seconds stay aligned
		// with the recording's clock however the frames fall.
		if n == 0 && total >= (len(out)+1)*segmentRate {
			flush()
		}
	}
	flush()
	return out, float64(total) / segmentRate
}

// bandEdges are FFT bin indices for twelve log-spaced bands, 60 Hz–5 kHz.
func bandEdges() []int {
	edges := make([]int, bandCount+1)
	lo, hi := math.Log(60), math.Log(5000)
	for i := range edges {
		hz := math.Exp(lo + (hi-lo)*float64(i)/bandCount)
		edges[i] = int(hz * segmentFrame / segmentRate)
	}
	for i := 1; i < len(edges); i++ {
		if edges[i] <= edges[i-1] {
			edges[i] = edges[i-1] + 1
		}
	}
	return edges
}

// segmentFeatures scores every second as a boundary and picks the strongest.
func segmentFeatures(fs []feature, minTrack float64) []Segment {
	n := len(fs)
	total := float64(n)
	if minTrack <= 0 {
		minTrack = 60
	}
	if total < 2*minTrack {
		return []Segment{{Start: 0, End: total, Cue: "start"}}
	}

	// Standardise each dimension so level and every band count alike.
	dims := bandCount + 1
	vec := make([][]float64, n)
	for i, f := range fs {
		vec[i] = append([]float64{f.rmsDB / 10}, f.bands...)
	}
	for d := 0; d < dims; d++ {
		var mean, sq float64
		for i := range vec {
			mean += vec[i][d]
		}
		mean /= total
		for i := range vec {
			sq += (vec[i][d] - mean) * (vec[i][d] - mean)
		}
		// A floor on the spread (3 dB in level, half a decade in a band): in
		// one steady song every dimension barely moves, and dividing by that
		// would blow its noise up into boundaries.
		sd := math.Max(math.Sqrt(sq/total), 0.3)
		for i := range vec {
			vec[i][d] = (vec[i][d] - mean) / sd
		}
	}

	// Gaps: seconds far below the recording's typical level.
	levels := make([]float64, n)
	for i, f := range fs {
		levels[i] = f.rmsDB
	}
	sorted := append([]float64(nil), levels...)
	sort.Float64s(sorted)
	median := sorted[n/2]
	quiet := make([]bool, n)
	for i := range fs {
		quiet[i] = levels[i] < median-25 || levels[i] < -50
	}

	// Change: distance between the mean of the 10 s before and after.
	const w = 10
	score := make([]float64, n)
	cue := make([]string, n)
	for t := w; t < n-w; t++ {
		var dist float64
		for d := 0; d < dims; d++ {
			var before, after float64
			for k := t - w; k < t; k++ {
				before += vec[k][d]
			}
			for k := t; k < t+w; k++ {
				after += vec[k][d]
			}
			diff := (after - before) / w
			dist += diff * diff
		}
		score[t] = math.Sqrt(dist)
		cue[t] = "change"
	}
	// Normalise the change score, then let a gap outrank any change: a run of
	// quiet seconds scores at the second the sound comes back.
	raw := append([]float64(nil), score...)
	var mean, sq float64
	for _, v := range score {
		mean += v
	}
	mean /= total
	for _, v := range score {
		sq += (v - mean) * (v - mean)
	}
	sd := math.Sqrt(sq/total) + 1e-9
	for t := range score {
		score[t] = (score[t] - mean) / sd
	}
	for t := 1; t < n; t++ {
		if quiet[t-1] && !quiet[t] {
			score[t] += 6
			cue[t] = "gap"
		}
	}

	// Greedy peak picking: strongest first, never closer than minTrack to the
	// start, the end or another boundary, and only points that stand out.
	order := make([]int, 0, n)
	for t := range score {
		order = append(order, t)
	}
	sort.Slice(order, func(i, j int) bool { return score[order[i]] > score[order[j]] })
	maxBoundaries := int(total / minTrack)
	chosen := []int{}
	for _, t := range order {
		if score[t] < 2.0 || len(chosen) >= maxBoundaries {
			break
		}
		// And it must be a real change, not merely the largest wobble in a
		// recording that never changes: a mean shift across all thirteen
		// dimensions of at least ~0.5 floor units each.
		if cue[t] == "change" && raw[t] < 1.8 {
			continue
		}
		if float64(t) < minTrack || total-float64(t) < minTrack {
			continue
		}
		ok := true
		for _, c := range chosen {
			if math.Abs(float64(t-c)) < minTrack {
				ok = false
				break
			}
		}
		if ok {
			chosen = append(chosen, t)
		}
	}
	sort.Ints(chosen)

	out := []Segment{}
	start, startCue := 0.0, "start"
	for _, c := range chosen {
		out = append(out, Segment{Start: start, End: float64(c), Cue: startCue})
		start, startCue = float64(c), cue[c]
	}
	out = append(out, Segment{Start: start, End: total, Cue: startCue})
	return out
}
