package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"aimusicmaker/internal/audioanalysis"
	"aimusicmaker/internal/platform"
)

// ---------------------------------------------------------------------------
// The tracks in a long video
//
// A full-album upload or a mix is one link holding many songs, and the
// reference the producer means is usually one or two of them. Finding them
// turns the video into ranges — one per track, labelled with its title — and
// from there each is a range like any other: broken down, cited, planned from.
//
// In order of trust: YouTube's own chapters; a tracklist in the description
// (uploaders write "03:12 Song Title" far more often than they make
// chapters); and, when neither exists, the audio itself — the recording is
// fetched to a temp folder, listened through for gaps and changes of sound
// (audioanalysis.SegmentTracks), and deleted. The last is a proposal and says
// so: titles are "Track 3", and the producer fixes what it got wrong.
// ---------------------------------------------------------------------------

// DetectedTrack is one track found in a long video.
type DetectedTrack struct {
	StartSeconds float64 `json:"startSeconds"`
	EndSeconds   float64 `json:"endSeconds"`
	Title        string  `json:"title"`
}

// TrackSplit is what FindInspirationTracks found.
type TrackSplit struct {
	// Source is "chapters", "description" or "audio".
	Source          string          `json:"source"`
	DurationSeconds float64         `json:"durationSeconds"`
	Tracks          []DetectedTrack `json:"tracks"`
	// Note says how far to trust it.
	Note string `json:"note"`
}

// videoInfo is the part of yt-dlp's JSON this reads.
type videoInfo struct {
	Duration    float64 `json:"duration"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Chapters    []struct {
		Start float64 `json:"start_time"`
		End   float64 `json:"end_time"`
		Title string  `json:"title"`
	} `json:"chapters"`
}

// The seams a test replaces.
var (
	fetchVideoInfo = ytdlpInfo
	fetchFullAudio = ytdlpFetchAll
	segmentAudio   = audioanalysis.SegmentTracks
)

// minDetectedTrack is the shortest song the audio split proposes. An interlude
// shorter than this is folded into a neighbour — fewer, longer guesses are
// easier to correct than a scatter of thirty-second ones.
const minDetectedTrack = 60

// maxSplitSeconds is the longest recording the audio split will fetch.
const maxSplitSeconds = 3 * 3600

// FindInspirationTracks finds the tracks in a long YouTube or YouTube Music
// video. Nothing is saved: the window offers the result as ranges to add.
func (a *App) FindInspirationTracks(url string) (TrackSplit, error) {
	url = strings.TrimSpace(url)
	if len(musicURLs(url)) == 0 {
		return TrackSplit{}, fmt.Errorf("that is not a YouTube or YouTube Music link")
	}
	ctx := a.interviewContext()
	info, err := fetchVideoInfo(ctx, url)
	if err != nil {
		return TrackSplit{}, err
	}
	split := TrackSplit{DurationSeconds: info.Duration, Tracks: []DetectedTrack{}}

	if len(info.Chapters) >= 2 {
		for _, c := range info.Chapters {
			split.Tracks = append(split.Tracks, DetectedTrack{
				StartSeconds: roundMillis(c.Start), EndSeconds: roundMillis(c.End),
				Title: strings.TrimSpace(c.Title)})
		}
		split.Source = "chapters"
		split.Note = "From the video's chapters."
		return split, nil
	}
	if tracks := parseTracklist(info.Description, info.Duration); len(tracks) >= 2 {
		split.Tracks, split.Source = tracks, "description"
		split.Note = "From the tracklist in the video's description."
		return split, nil
	}

	if info.Duration > maxSplitSeconds {
		return TrackSplit{}, fmt.Errorf("the video is %s long; listening for tracks stops at %s",
			formatTimestamp(info.Duration), formatTimestamp(maxSplitSeconds))
	}
	sweepRangeClips(time.Hour)
	dir, err := os.MkdirTemp("", rangeClipPrefix+"*")
	if err != nil {
		return TrackSplit{}, fmt.Errorf("no temp folder for the recording: %w", err)
	}
	defer os.RemoveAll(dir)
	path, err := fetchFullAudio(ctx, url, dir)
	if err != nil {
		return TrackSplit{}, fmt.Errorf("no chapters or tracklist, and the audio could not be fetched to listen for tracks: %w", err)
	}
	segments, seconds, err := segmentAudio(ctx, path, minDetectedTrack)
	if err != nil {
		return TrackSplit{}, fmt.Errorf("no chapters or tracklist, and the audio could not be listened through: %w", err)
	}
	if split.DurationSeconds == 0 {
		split.DurationSeconds = seconds
	}
	for n, s := range segments {
		end := s.End
		if split.DurationSeconds > 0 && end > split.DurationSeconds {
			end = split.DurationSeconds
		}
		split.Tracks = append(split.Tracks, DetectedTrack{
			StartSeconds: s.Start, EndSeconds: end, Title: fmt.Sprintf("Track %d", n+1)})
	}
	split.Source = "audio"
	split.Note = "No chapters or tracklist, so the tracks were found by listening for gaps and " +
		"changes of sound. Check the boundaries and name them before relying on them."
	if len(split.Tracks) == 1 {
		split.Note = "No chapters or tracklist, and listening found no clear boundary — it may " +
			"be one long piece, or a gapless mix with no change strong enough to split on."
	}
	return split, nil
}

// tracklistLine is a description line carrying a timestamp: "03:12 Title",
// "1. Title - 3:12", "[1:02:03] Title", "Title (12:40)".
var tracklistLine = regexp.MustCompile(`(?:^|[\s\[(])((?:\d{1,2}:)?\d{1,2}:\d{2})(?:[\])]|\s|$)`)

// leadingNumber is "1." or "01 -" in front of a title.
var leadingNumber = regexp.MustCompile(`^\d{1,3}[.)]\s+|^\d{1,3}\s+[-–—]\s+`)

// parseTracklist reads timestamped lines out of a description. Only a run of
// strictly rising times counts as a tracklist — a description that mentions
// "my favourite part is 2:30" once is not one.
func parseTracklist(description string, duration float64) []DetectedTrack {
	out := []DetectedTrack{}
	for _, line := range strings.Split(description, "\n") {
		m := tracklistLine.FindStringSubmatchIndex(line)
		if m == nil {
			continue
		}
		at, err := parseTimestamp(line[m[2]:m[3]])
		if err != nil {
			continue
		}
		// The brackets around the time go with it; any others are the title's.
		title := strings.TrimRight(line[:m[2]], "[( ") + " " + strings.TrimLeft(line[m[3]:], "]) ")
		title = strings.Trim(strings.TrimSpace(title), " -–—|:•·")
		title = strings.TrimSpace(leadingNumber.ReplaceAllString(title, ""))
		if duration > 0 && at >= duration {
			continue
		}
		if len(out) > 0 && at <= out[len(out)-1].StartSeconds {
			// Times that go backwards start a second list (a tracklist then
			// "lyrics at 1:20"); the first run is the tracklist.
			break
		}
		if title == "" {
			title = fmt.Sprintf("Track %d", len(out)+1)
		}
		out = append(out, DetectedTrack{StartSeconds: at, Title: title})
	}
	for i := range out {
		if i+1 < len(out) {
			out[i].EndSeconds = out[i+1].StartSeconds
		} else {
			out[i].EndSeconds = duration
		}
	}
	if len(out) > 0 && out[len(out)-1].EndSeconds <= out[len(out)-1].StartSeconds {
		// No duration to close the last track with; it cannot be a range.
		out = out[:len(out)-1]
	}
	return out
}

// ytdlpInfo reads a video's metadata — duration, description, chapters —
// without downloading it.
func ytdlpInfo(ctx context.Context, url string) (videoInfo, error) {
	ytdlp, ok := platform.LookUp("yt-dlp")
	if !ok {
		return videoInfo{}, errNoYTDLP
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	out, err := runYTDLP(ctx, ytdlp, []string{"--no-playlist", "--no-warnings", "--skip-download",
		"--dump-single-json"}, url)
	if err != nil {
		return videoInfo{}, err
	}
	var info videoInfo
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		return videoInfo{}, fmt.Errorf("yt-dlp's answer could not be read: %w", err)
	}
	return info, nil
}

// ytdlpFetchAll downloads a video's whole audio into dir, as it comes —
// SegmentTracks decodes whatever container YouTube served.
func ytdlpFetchAll(ctx context.Context, url, dir string) (string, error) {
	ytdlp, ok := platform.LookUp("yt-dlp")
	if !ok {
		return "", errNoYTDLP
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	if _, err := runYTDLP(ctx, ytdlp, []string{"--no-playlist", "--no-progress", "--no-warnings",
		"-f", "bestaudio/best", "-o", filepath.Join(dir, "recording.%(ext)s")}, url); err != nil {
		return "", err
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "recording.*"))
	for _, m := range matches {
		if !strings.HasSuffix(m, ".part") && !strings.HasSuffix(m, ".ytdl") {
			return m, nil
		}
	}
	return "", errors.New("yt-dlp finished but wrote no audio")
}
