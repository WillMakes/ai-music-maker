package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"aimusicmaker/internal/audioanalysis"
	"aimusicmaker/internal/platform"
)

// ---------------------------------------------------------------------------
// Breaking down one range
//
// The section's audio is fetched — that section only, with yt-dlp's
// --download-sections — into a temp folder that is deleted when the run
// ends, and measured (internal/audioanalysis). The model is then handed the
// numbers and what research knows about the song, and writes what numbers
// cannot say: instrumentation, arrangement, vocal treatment, why it works.
// The two are stored apart (RangeBreakdown), so a producer citing the
// breakdown knows which half was measured.
//
// Nothing about the audio may cost the producer the breakdown: when yt-dlp or
// ffmpeg is missing, or the video will not fetch, the range still gets one,
// written from research alone and labelled as such, with the reason the audio
// could not be heard beside it.
//
// The audio never goes near the music library or the repository. It is a few
// seconds of somebody else's record, held only long enough to measure.
// ---------------------------------------------------------------------------

// errNoYTDLP is the fetch's answer when yt-dlp is not on this machine.
var errNoYTDLP = errors.New("yt-dlp is not installed, so the section's audio cannot be fetched")

// The seams a test replaces: the network, ffmpeg and the models.
var (
	fetchRangeAudio   = ytdlpFetchSection
	measureRangeAudio = audioanalysis.Measure
	writeRangeProse   = func(a *App, ctx context.Context, system, prompt string) (string, error) {
		return a.askAI(ctx, system, []chatMsg{{Role: "user", Text: prompt}})
	}
	researchSong = func(a *App, url string) (MusicAnalysis, error) { return a.AnalyzeMusicURL(url) }
	probeVideo   = ytdlpDuration
)

// rangeClipPrefix names the temp folders, so a sweep can find the ones a
// crashed run left behind.
const rangeClipPrefix = "aimusicmaker-range-"

// BreakDownInspirationRange measures one saved range of a reference and
// writes its breakdown, and returns the reference as stored afterwards.
//
// Slow — a fetch, a decode and a model call, a minute or more — and
// synchronous, like AnalyzeMusicURL: the window runs it behind the card.
func (a *App) BreakDownInspirationRange(artistID, inspirationID, rangeID string) (Inspiration, error) {
	if a.store == nil {
		return Inspiration{}, errNoStore
	}
	artist, err := a.store.artist(artistID)
	if err != nil {
		return Inspiration{}, err
	}
	insp, r, err := findRange(artist, inspirationID, rangeID)
	if err != nil {
		return Inspiration{}, err
	}
	if strings.TrimSpace(insp.URL) == "" {
		return Inspiration{}, fmt.Errorf("[%s] has no link to fetch the section from", r.CiteKey)
	}
	ctx := a.interviewContext()

	// The song's identity first: it is what the model writes against, and a
	// reference nobody has researched yet gets researched now (when
	// Antigravity is connected). Its absence is not a failure — the numbers
	// and the producer's label still make a breakdown.
	research := strings.TrimSpace(insp.Note)
	researchNote := ""
	if research == "" {
		found, err := researchSong(a, insp.URL)
		if err != nil {
			researchNote = "The song was not researched: " + err.Error()
		} else {
			research = found.Style
			if err := a.store.fillInspirationIdentity(insp.ID, found, 0); err == nil {
				insp = fillIdentity(insp, found)
			}
		}
	}

	measured, duration, audioErr := a.measureRange(ctx, insp.URL, r)
	if duration > 0 {
		_ = a.store.fillInspirationIdentity(insp.ID, MusicAnalysis{}, duration)
		if err := validateRange(r.StartSeconds, r.EndSeconds, duration); err != nil {
			// The one failure that is the range's own fault: there is nothing
			// to describe past the end of the video, from audio or research.
			a.store.changed(scopeArtists)
			return Inspiration{}, fmt.Errorf("[%s] %w", r.CiteKey, err)
		}
	}

	b := RangeBreakdown{ForStart: r.StartSeconds, ForEnd: r.EndSeconds, At: nowStamp()}
	if audioErr == nil {
		b.Source = "audio"
		b.Measurements = measured
	} else {
		b.Source = "research"
		b.AudioError = audioErr.Error()
	}
	system, prompt := rangeProsePrompt(insp, r, research, researchNote, measured, audioErr)
	answer, proseErr := writeRangeProse(a, ctx, system, prompt)
	if proseErr == nil {
		summary, interpretation := parseRangeProse(answer)
		b.Summary, b.Interpretation = summary, interpretation
	} else if b.Source == "audio" {
		// The numbers are worth keeping without the prose; the card says why
		// the prose is missing and the button is the retry.
		b.ProseError = proseErr.Error()
	} else {
		return Inspiration{}, fmt.Errorf("[%s] could not be broken down: the audio could not be measured (%v) and the model could not write from research (%v)",
			r.CiteKey, audioErr, proseErr)
	}

	written, err := a.store.setRangeBreakdown(r.ID, b)
	if err != nil {
		return Inspiration{}, err
	}
	if !written {
		return Inspiration{}, fmt.Errorf("[%s] changed while it was being broken down — run it again", r.CiteKey)
	}
	a.store.changed(scopeArtists)
	fresh, err := a.store.artist(artistID)
	if err != nil {
		return Inspiration{}, err
	}
	stored, _, err := findRange(fresh, inspirationID, rangeID)
	return stored, err
}

func findRange(artist Artist, inspirationID, rangeID string) (Inspiration, InspirationRange, error) {
	for _, insp := range artist.Inspirations {
		if insp.ID != inspirationID {
			continue
		}
		for _, r := range insp.Ranges {
			if r.ID == rangeID {
				return insp, r, nil
			}
		}
		return insp, InspirationRange{}, fmt.Errorf("that range is not saved yet — wait for Saved, then try again")
	}
	return Inspiration{}, InspirationRange{}, fmt.Errorf("that reference is not saved yet — wait for Saved, then try again")
}

func fillIdentity(insp Inspiration, found MusicAnalysis) Inspiration {
	set := func(field *string, v string) {
		if strings.TrimSpace(*field) == "" {
			*field = v
		}
	}
	set(&insp.Title, found.Title)
	set(&insp.Artist, found.Artist)
	set(&insp.Album, found.Album)
	set(&insp.ArtworkURL, found.ArtworkURL)
	set(&insp.Note, found.Style)
	return insp
}

// measureRange fetches and measures the section, in a temp folder removed
// before it returns. duration is the video's length when yt-dlp reported it,
// even when the rest failed.
func (a *App) measureRange(ctx context.Context, url string, r InspirationRange) (*audioanalysis.Measurements, float64, error) {
	sweepRangeClips(time.Hour)
	dir, err := os.MkdirTemp("", rangeClipPrefix+"*")
	if err != nil {
		return nil, 0, fmt.Errorf("no temp folder for the section: %w", err)
	}
	defer os.RemoveAll(dir)

	path, duration, err := fetchRangeAudio(ctx, url, r.StartSeconds, r.EndSeconds, dir)
	if err != nil {
		return nil, duration, err
	}
	if duration > 0 && validateRange(r.StartSeconds, r.EndSeconds, duration) != nil {
		return nil, duration, nil // the caller reports it
	}
	m, err := measureRangeAudio(ctx, path)
	if err != nil {
		return nil, duration, err
	}
	return &m, duration, nil
}

// sweepRangeClips removes temp folders older than age — a run killed with
// the app never reached its own cleanup, and the folder would otherwise sit
// in temp until the OS got round to it.
func sweepRangeClips(age time.Duration) {
	matches, _ := filepath.Glob(filepath.Join(os.TempDir(), rangeClipPrefix+"*"))
	for _, dir := range matches {
		if info, err := os.Stat(dir); err == nil && time.Since(info.ModTime()) > age {
			_ = os.RemoveAll(dir)
		}
	}
}

// ytdlpFetchSection downloads [start, end] of a video's audio into dir.
//
// --force-keyframes-at-cuts makes the cut exact: without it the section
// starts at the nearest keyframe, which on YouTube audio is seconds early and
// would put the previous section's bars into the measurement. Node is handed
// to yt-dlp as its JavaScript runtime where present — current YouTube needs
// one to serve audio formats at all, and yt-dlp only looks for Deno by itself.
func ytdlpFetchSection(ctx context.Context, url string, start, end float64, dir string) (string, float64, error) {
	ytdlp, ok := platform.LookUp("yt-dlp")
	if !ok {
		return "", 0, errNoYTDLP
	}
	ffmpeg, ok := platform.LookUp("ffmpeg")
	if !ok {
		return "", 0, errors.New("ffmpeg is not installed, so the section cannot be cut out of the video")
	}
	args := []string{"--no-playlist", "--no-progress", "--no-warnings", "-f", "bestaudio/best",
		"--download-sections", "*" + seconds(start) + "-" + seconds(end), "--force-keyframes-at-cuts",
		"--ffmpeg-location", ffmpeg, "-o", filepath.Join(dir, "section.%(ext)s"),
		"--print", "before_dl:%(duration)s", "--no-simulate"}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	out, err := runYTDLP(ctx, ytdlp, args, url)
	duration := firstNumber(out)
	if err != nil {
		return "", duration, err
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "section.*"))
	for _, m := range matches {
		if !strings.HasSuffix(m, ".part") && !strings.HasSuffix(m, ".ytdl") {
			return m, duration, nil
		}
	}
	return "", duration, errors.New("yt-dlp finished but wrote no audio for the section")
}

// ytdlpDuration asks yt-dlp for a video's length without downloading it.
func ytdlpDuration(ctx context.Context, url string) (float64, error) {
	ytdlp, ok := platform.LookUp("yt-dlp")
	if !ok {
		return 0, errNoYTDLP
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	out, err := runYTDLP(ctx, ytdlp, []string{"--no-playlist", "--no-warnings", "--skip-download",
		"--print", "%(duration)s"}, url)
	if err != nil {
		return 0, err
	}
	if d := firstNumber(out); d > 0 {
		return d, nil
	}
	return 0, errors.New("YouTube did not say how long the video is")
}

// runYTDLP runs yt-dlp, with Node as its JavaScript runtime when there is
// one, and retries without the flag on a yt-dlp too old to know it.
func runYTDLP(ctx context.Context, ytdlp string, args []string, url string) (string, error) {
	run := func(extra []string) (string, string, error) {
		cmd := exec.CommandContext(ctx, ytdlp, append(append(extra, args...), "--", url)...)
		platform.HideWindow(cmd)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		return stdout.String(), stderr.String(), err
	}
	var extra []string
	if node, ok := platform.LookUp("node"); ok {
		extra = []string{"--js-runtimes", "node:" + node}
	}
	stdout, stderr, err := run(extra)
	if err != nil && extra != nil && strings.Contains(stderr, "no such option") {
		stdout, stderr, err = run(nil)
	}
	if err != nil {
		if ctx.Err() != nil {
			return stdout, fmt.Errorf("yt-dlp took too long to fetch the section")
		}
		return stdout, fmt.Errorf("yt-dlp could not fetch it: %s", ytdlpError(stderr, err))
	}
	return stdout, nil
}

// ytdlpError is the line of yt-dlp's stderr that says what went wrong.
func ytdlpError(stderr string, err error) string {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); strings.HasPrefix(line, "ERROR:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "ERROR:"))
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" && !strings.HasPrefix(line, "Deprecated Feature") {
			return line
		}
	}
	return err.Error()
}

func firstNumber(out string) float64 {
	for _, line := range strings.Split(out, "\n") {
		if v, err := strconv.ParseFloat(strings.TrimSpace(line), 64); err == nil && v > 0 {
			return v
		}
	}
	return 0
}

func seconds(v float64) string { return strconv.FormatFloat(v, 'f', 3, 64) }

// LinkInfo is what ProbeInspirationLink learns about a link.
type LinkInfo struct {
	VideoID         string  `json:"videoId"`
	DurationSeconds float64 `json:"durationSeconds"`
	// StartSeconds is the link's own t=/start=, HasStart whether it had one.
	StartSeconds float64 `json:"startSeconds"`
	HasStart     bool    `json:"hasStart"`
}

// ProbeInspirationLink reads a link's start time and asks yt-dlp for the
// video's length, so a range can be checked against it as it is typed. A
// failed lookup is not an error: the range is checked again when it is
// broken down, and a producer without yt-dlp can still write ranges.
func (a *App) ProbeInspirationLink(url string) (LinkInfo, error) {
	url = strings.TrimSpace(url)
	if len(musicURLs(url)) == 0 {
		return LinkInfo{}, fmt.Errorf("that is not a YouTube or YouTube Music link")
	}
	info := LinkInfo{VideoID: youtubeVideoID(url)}
	info.StartSeconds, info.HasStart = linkStart(url)
	if d, err := probeVideo(a.interviewContext(), url); err == nil {
		info.DurationSeconds = d
	}
	return info, nil
}

// ---------------------------------------------------------------------------
// The model's half
// ---------------------------------------------------------------------------

const rangeProseSystem = `You are a record producer and mix engineer writing the
technical breakdown of ONE SECTION of a song, for a producer who writes style
prompts for AI music generation and will cite this breakdown when planning an
album. Answer with one JSON object and nothing else:

{"summary": "one line, under 30 words: what this section is and what makes it work", "breakdown": "the breakdown in Markdown"}

The breakdown has a bold lead-in per aspect, each a few dense, concrete lines:
**Instrumentation**, **Arrangement and energy** (how it moves across the
section, start to end), **Vocal treatment** (or "instrumental"),
**Production and mix**, **Why it works**, and **Prompt language** (comma-led
terms a style prompt could borrow). No preamble, no disclaimers, nothing
outside the JSON object.`

func rangeProsePrompt(insp Inspiration, r InspirationRange, research, researchNote string,
	m *audioanalysis.Measurements, audioErr error) (string, string) {
	var b strings.Builder
	fmt.Fprintf(&b, "The song: %s\n", songName(insp))
	if insp.Album != "" {
		fmt.Fprintf(&b, "Release: %s\n", insp.Album)
	}
	fmt.Fprintf(&b, "Link: %s\n", insp.URL)
	fmt.Fprintf(&b, "The section: %s to %s (%s long)", formatTimestamp(r.StartSeconds),
		formatTimestamp(r.EndSeconds), formatTimestamp(r.EndSeconds-r.StartSeconds))
	if r.Label != "" {
		fmt.Fprintf(&b, ", which the producer calls %q", r.Label)
	}
	b.WriteString("\n\n")

	system := rangeProseSystem
	if m != nil {
		system += `

You are given measurements taken from this section's actual audio. They are
facts: do not contradict them, and do not invent other numbers. Interpret
them — say what a centroid, a width or a loudness range means for how this
section sounds — and use the energy profile to describe how it moves.`
		raw, _ := json.MarshalIndent(m, "", "  ")
		b.WriteString("Measured from the section's audio:\n")
		b.WriteString(measurementLine(m))
		b.WriteString("\n\nThe full measurements as JSON (energyProfile is RMS dBFS per energyStepSeconds, from the start of the section):\n")
		b.Write(raw)
		b.WriteString("\n\n")
	} else {
		system += `

No audio could be measured for this section. Write from the research and the
timestamps alone, give no numbers you were not given, and say plainly where a
detail is inferred rather than heard.`
		fmt.Fprintf(&b, "The audio could not be measured: %v\n\n", audioErr)
	}
	if research != "" {
		b.WriteString("Research about the whole song (it describes the song, not this section — apply it only where it plausibly holds here):\n")
		b.WriteString(research)
		b.WriteString("\n")
	} else if researchNote != "" {
		b.WriteString(researchNote + "\n")
	}
	return system, b.String()
}

// parseRangeProse reads the model's JSON. Like parseAnalysis, an answer that
// is not JSON becomes the breakdown whole — losing the one-line summary is a
// smaller failure than losing the prose it came with.
func parseRangeProse(answer string) (string, string) {
	var out struct {
		Summary   string `json:"summary"`
		Breakdown string `json:"breakdown"`
	}
	if err := json.Unmarshal([]byte(unfence(answer)), &out); err != nil ||
		strings.TrimSpace(out.Breakdown) == "" {
		text := strings.TrimSpace(answer)
		return excerpt(text, 160), text
	}
	return strings.TrimSpace(out.Summary), strings.TrimSpace(out.Breakdown)
}

// ---------------------------------------------------------------------------
// Looking a citation up
// ---------------------------------------------------------------------------

// CitedInspiration is everything behind one citation key.
type CitedInspiration struct {
	Key      string `json:"key"`
	Citation string `json:"citation"`
	// Inspiration is the reference; Range the section when the key is one.
	Inspiration Inspiration       `json:"inspiration"`
	Range       *InspirationRange `json:"range"`
}

// GetInspirationCitation looks one citation key up on an artist: the full
// breakdown the briefs only summarise.
func (a *App) GetInspirationCitation(artistID, key string) (CitedInspiration, error) {
	artist, err := a.GetArtist(artistID)
	if err != nil {
		return CitedInspiration{}, err
	}
	key = strings.Trim(strings.TrimSpace(key), "[]")
	for _, insp := range artist.Inspirations {
		if insp.CiteKey == key {
			return CitedInspiration{Key: key, Citation: insp.Citation, Inspiration: insp}, nil
		}
		for k := range insp.Ranges {
			if insp.Ranges[k].CiteKey == key {
				r := insp.Ranges[k]
				return CitedInspiration{Key: key, Citation: r.Citation, Inspiration: insp, Range: &r}, nil
			}
		}
	}
	return CitedInspiration{}, fmt.Errorf("%s has no inspiration cited as %s", artist.Name, key)
}
