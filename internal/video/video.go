// Package video renders an album's music video (#118): one segment per song,
// each the cover over a blurred copy of itself with the song's title and a
// visualisation of its audio, faded in and out, with a silent gap between
// songs — then the segments joined into one file.
//
// The work is ffmpeg's, run as a program for the reason audio.EncodeMP3 runs
// it (see internal/audio): it is already how this app encodes, and nothing
// compiled in draws a waveform. One ffmpeg per segment rather than one
// filtergraph for the whole record: a ten-song graph is unreadable when it
// fails, a segment's failure names its song, and segments encoded with the
// same settings join with a stream copy, so the join costs nothing.
package video

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"aimusicmaker/internal/platform"
)

// ErrNoFFmpeg is Render's answer when ffmpeg or ffprobe is not installed.
var ErrNoFFmpeg = errors.New("rendering a music video needs ffmpeg — install it (winget install ffmpeg, brew install ffmpeg) and try again")

// Song is one track of the video.
type Song struct {
	Title string
	// Audio is the official take's file.
	Audio string
}

// Spec is everything one render needs.
type Spec struct {
	Artist string
	Album  string
	// Cover is the album's artwork file.
	Cover string
	Songs []Song
	// Gap is the silence between two songs, in seconds.
	Gap float64
	// Style is the look (#119); the zero value is DefaultStyle.
	Style Style
	// Out is the finished file. Written beside itself first and renamed into
	// place, so a failed render never leaves half a video under the real name.
	Out string
}

// Style is what the video looks like. Kept to choices a filtergraph can make
// safely: a colour is a hex value, a mode one of a few words — anything a
// skill writes is checked against these before it reaches ffmpeg.
type Style struct {
	// Visualizer is "waves" (the waveform as a line) or "bars" (an equaliser
	// of frequency bars).
	Visualizer string
	// Color draws the visualisation and the titles, as RRGGBB.
	Color string
	// Background is "blur" (the cover, blurred and darkened, filling the
	// frame) or "black".
	Background string
	// Layout is "centered" (the cover in the middle, the titles under it, the
	// visualisation across the foot) or "split" (the cover on the left, the
	// titles and the visualisation beside it on the right).
	Layout string
	// Fade is how long each song's picture fades in and out, in seconds.
	Fade float64
}

// DefaultStyle is the look before any skill has a say.
var DefaultStyle = Style{Visualizer: "waves", Color: "FFFFFF", Background: "blur", Layout: "centered", Fade: 1}

// Normalized fills what is missing from DefaultStyle and drops what is not a
// value the filtergraph accepts, so a style read from anywhere is safe to use.
func (s Style) Normalized() Style {
	out := DefaultStyle
	if s.Visualizer == "waves" || s.Visualizer == "bars" {
		out.Visualizer = s.Visualizer
	}
	if c := strings.TrimPrefix(strings.TrimSpace(s.Color), "#"); isHex6(c) {
		out.Color = strings.ToUpper(c)
	}
	if s.Background == "blur" || s.Background == "black" {
		out.Background = s.Background
	}
	if s.Layout == "centered" || s.Layout == "split" {
		out.Layout = s.Layout
	}
	if s.Fade > 0 && s.Fade <= 5 {
		out.Fade = s.Fade
	}
	return out
}

func isHex6(s string) bool {
	if len(s) != 6 {
		return false
	}
	_, err := strconv.ParseUint(s, 16, 32)
	return err == nil
}

// Rendered is what a render made.
type Rendered struct {
	// Starts is when each song begins in the finished video, in seconds — the
	// description's timestamps (#120).
	Starts []float64
	// Seconds is the whole video's length.
	Seconds float64
}

const (
	width  = 1920
	height = 1080
	fps    = 30
)

// Render makes the video. progress is called with the fraction done, from 0
// to 1, as ffmpeg reports it.
func Render(ctx context.Context, spec Spec, progress func(float64)) (Rendered, error) {
	ffmpeg, ok := platform.LookUp("ffmpeg")
	if !ok {
		return Rendered{}, ErrNoFFmpeg
	}
	ffprobe, ok := platform.LookUp("ffprobe")
	if !ok {
		return Rendered{}, ErrNoFFmpeg
	}
	if len(spec.Songs) == 0 {
		return Rendered{}, errors.New("there are no songs to put in the video")
	}
	if progress == nil {
		progress = func(float64) {}
	}
	style := spec.Style.Normalized()
	gap := spec.Gap
	if gap < 0 {
		gap = 0
	}

	// Durations first: the fades are placed from the end, and the progress
	// and the timestamps need the whole record's length before any encoding.
	lengths := make([]float64, len(spec.Songs))
	for i, song := range spec.Songs {
		seconds, err := probeSeconds(ctx, ffprobe, song.Audio)
		if err != nil {
			return Rendered{}, fmt.Errorf("reading the length of %q: %w", song.Title, err)
		}
		lengths[i] = seconds
	}
	var result Rendered
	total := 0.0
	for i, seconds := range lengths {
		if i > 0 {
			total += gap
		}
		result.Starts = append(result.Starts, total)
		total += seconds
	}
	result.Seconds = total

	// Every input under a plain name in a folder of its own, and ffmpeg run in
	// it: a filtergraph parses ':' and '\' in its option values, and a Windows
	// path is made of both. Relative names need no escaping at all.
	work, err := os.MkdirTemp("", "aimusicmaker-video-")
	if err != nil {
		return Rendered{}, err
	}
	defer os.RemoveAll(work)
	if err := copyFile(spec.Cover, filepath.Join(work, "cover"+filepath.Ext(spec.Cover))); err != nil {
		return Rendered{}, fmt.Errorf("reading the cover: %w", err)
	}
	cover := "cover" + filepath.Ext(spec.Cover)
	font := ""
	if path := platform.FontFile(); path != "" {
		if copyFile(path, filepath.Join(work, "font"+filepath.Ext(path))) == nil {
			font = "font" + filepath.Ext(path)
		}
	}
	if err := os.WriteFile(filepath.Join(work, "artist.txt"), []byte(oneLine(spec.Artist)+" — "+oneLine(spec.Album)), 0o600); err != nil {
		return Rendered{}, err
	}

	var segments []string
	done := 0.0
	run := func(name string, seconds float64, args []string) error {
		base := done
		err := runFFmpeg(ctx, ffmpeg, work, args, func(at float64) {
			if total > 0 {
				progress(min(1, (base+min(at, seconds))/total))
			}
		})
		if err != nil {
			return err
		}
		done += seconds
		segments = append(segments, name)
		return nil
	}

	for i, song := range spec.Songs {
		if i > 0 && gap > 0 {
			name := fmt.Sprintf("gap%03d.mp4", i)
			if err := run(name, gap, gapArgs(gap, name)); err != nil {
				return Rendered{}, fmt.Errorf("rendering the gap before %q: %w", song.Title, err)
			}
		}
		title := fmt.Sprintf("title%03d.txt", i)
		if err := os.WriteFile(filepath.Join(work, title), []byte(oneLine(song.Title)), 0o600); err != nil {
			return Rendered{}, err
		}
		name := fmt.Sprintf("song%03d.mp4", i)
		args := songArgs(cover, song.Audio, title, font, lengths[i], style, name)
		if err := run(name, lengths[i], args); err != nil {
			return Rendered{}, fmt.Errorf("rendering %q: %w", song.Title, err)
		}
	}

	var list strings.Builder
	for _, name := range segments {
		fmt.Fprintf(&list, "file '%s'\n", name)
	}
	if err := os.WriteFile(filepath.Join(work, "segments.txt"), []byte(list.String()), 0o600); err != nil {
		return Rendered{}, err
	}
	if err := os.MkdirAll(filepath.Dir(spec.Out), 0o755); err != nil {
		return Rendered{}, err
	}
	partial := spec.Out + ".partial.mp4"
	join := []string{"-f", "concat", "-safe", "0", "-i", "segments.txt",
		"-c", "copy", "-movflags", "+faststart", partial}
	if err := runFFmpeg(ctx, ffmpeg, work, join, nil); err != nil {
		os.Remove(partial)
		return Rendered{}, fmt.Errorf("joining the songs: %w", err)
	}
	if err := os.Rename(partial, spec.Out); err != nil {
		os.Remove(partial)
		return Rendered{}, err
	}
	progress(1)
	return result, nil
}

// Timestamp is a position in the video as YouTube writes one in a
// description: m:ss, or h:mm:ss from an hour on. Rounded down, so a link
// lands on the song's first second rather than just after it.
func Timestamp(seconds float64) string {
	s := int(max(0, seconds))
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// songArgs is one song's segment: the picture, the song's audio, and the
// fades. The audio fades only briefly — a fade-in as long as the picture's
// would swallow the song's first bar — while the picture takes style.Fade.
func songArgs(cover, audio, title, font string, seconds float64, style Style, out string) []string {
	fade := min(style.Fade, seconds/4)
	fadeOut := max(0, seconds-fade)
	color := "0x" + style.Color

	var g strings.Builder
	if style.Background == "black" {
		fmt.Fprintf(&g, "color=c=black:s=%dx%d:r=%d[bg];", width, height, fps)
	} else {
		fmt.Fprintf(&g, "[0:v]scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d,gblur=sigma=40,eq=brightness=-0.2[bg];", width, height, width, height)
	}
	l := layoutOf(style.Layout)
	fmt.Fprintf(&g, "[0:v]scale=%d:%d[cv];", l.cover, l.cover)
	g.WriteString("[1:a]asplit=2[aud][vis];")
	if style.Visualizer == "bars" {
		fmt.Fprintf(&g, "[vis]showfreqs=s=%dx%d:mode=bar:ascale=sqrt:fscale=log:win_size=2048:colors=%s,format=rgba,colorkey=black:0.1:0.0[wv];", l.visW, l.visH, color)
	} else {
		fmt.Fprintf(&g, "[vis]showwaves=s=%dx%d:mode=cline:rate=%d:colors=%s@0.9,format=rgba[wv];", l.visW, l.visH, fps, color)
	}
	fmt.Fprintf(&g, "[bg][cv]overlay=%s:%s:shortest=1[l1];", l.coverX, l.coverY)
	fmt.Fprintf(&g, "[l1][wv]overlay=%s:%s[l2];", l.visX, l.visY)
	g.WriteString("[l2]")
	if font != "" {
		fmt.Fprintf(&g, "drawtext=fontfile=%s:textfile=%s:expansion=none:fontcolor=%s:fontsize=60:x=%s:y=%d,", font, title, color, l.textX, l.titleY)
		fmt.Fprintf(&g, "drawtext=fontfile=%s:textfile=artist.txt:expansion=none:fontcolor=%s@0.7:fontsize=34:x=%s:y=%d,", font, color, l.textX, l.titleY+80)
	}
	fmt.Fprintf(&g, "fade=t=in:st=0:d=%.3f,fade=t=out:st=%.3f:d=%.3f,setsar=1,format=yuv420p[v];", fade, fadeOut, fade)
	fmt.Fprintf(&g, "[aud]afade=t=in:st=0:d=0.05,afade=t=out:st=%.3f:d=%.3f,aresample=48000[a]", max(0, seconds-0.5), min(0.5, seconds))

	args := []string{
		"-loop", "1", "-framerate", strconv.Itoa(fps), "-i", cover,
		"-i", audio,
		"-filter_complex", g.String(),
		"-map", "[v]", "-map", "[a]",
		"-t", fmt.Sprintf("%.3f", seconds),
	}
	return append(args, encodeArgs(out)...)
}

// layout is where a Layout puts things, in the 1920x1080 frame. Positions are
// filtergraph expressions where they depend on what is drawn (W, w, text_w).
type layout struct {
	cover          int
	coverX, coverY string
	visW, visH     int
	visX, visY     string
	textX          string
	titleY         int
}

func layoutOf(name string) layout {
	if name == "split" {
		return layout{
			cover: 640, coverX: "160", coverY: "(H-h)/2",
			visW: 760, visH: 200, visX: "960", visY: "600",
			textX: "960", titleY: 360,
		}
	}
	return layout{
		cover: 560, coverX: "(W-w)/2", coverY: "130",
		visW: 1600, visH: 220, visX: "(W-w)/2", visY: "H-h-70",
		textX: "(w-text_w)/2", titleY: 725,
	}
}

// gapArgs is the black, silent second (or however long) between two songs.
func gapArgs(seconds float64, out string) []string {
	args := []string{
		"-f", "lavfi", "-i", fmt.Sprintf("color=c=black:s=%dx%d:r=%d", width, height, fps),
		"-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo",
		"-vf", "setsar=1,format=yuv420p",
		"-t", fmt.Sprintf("%.3f", seconds),
	}
	return append(args, encodeArgs(out)...)
}

// encodeArgs is shared by every segment, so the join can copy rather than
// re-encode: the concat demuxer needs one codec, frame rate and sample rate.
func encodeArgs(out string) []string {
	return []string{
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "20", "-r", strconv.Itoa(fps),
		"-c:a", "aac", "-b:a", "192k", "-ar", "48000", "-ac", "2",
		"-video_track_timescale", "15360",
		out,
	}
}

// runFFmpeg runs one ffmpeg in dir, reporting how far into its output it is.
func runFFmpeg(ctx context.Context, ffmpeg, dir string, args []string, at func(float64)) error {
	full := append([]string{"-nostdin", "-hide_banner", "-loglevel", "error", "-y",
		"-progress", "pipe:1", "-nostats"}, args...)
	cmd := exec.CommandContext(ctx, ffmpeg, full...)
	cmd.Dir = dir
	platform.HideWindow(cmd)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		// out_time_us is microseconds; out_time_ms is too, despite its name.
		if v, ok := strings.CutPrefix(scanner.Text(), "out_time_us="); ok && at != nil {
			if us, err := strconv.ParseFloat(v, 64); err == nil && us >= 0 {
				at(us / 1e6)
			}
		}
	}
	_, _ = io.Copy(io.Discard, stdout)
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("ffmpeg: %s", lastLine(msg))
		}
		return err
	}
	return nil
}

func probeSeconds(ctx context.Context, ffprobe, path string) (float64, error) {
	cmd := exec.CommandContext(ctx, ffprobe, "-v", "error",
		"-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", path)
	platform.HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || seconds <= 0 {
		return 0, fmt.Errorf("ffprobe found no duration")
	}
	return seconds, nil
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// oneLine keeps a title to one line: drawtext would draw a newline as one.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
