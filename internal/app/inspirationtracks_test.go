package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"aimusicmaker/internal/audioanalysis"
)

func TestParseTracklist(t *testing.T) {
	description := `Full album stream. Out now everywhere!

Tracklist:
00:00 Intro
1. Neon Rain - 2:14
[05:40] Glass Harbour (feat. Someone)
09:02 – Low Tide
1:02:03 Hidden Track

Lyrics: my favourite line is at 0:45`
	got := parseTracklist(description, 3800)
	want := []DetectedTrack{
		{0, 134, "Intro"},
		{134, 340, "Neon Rain"},
		{340, 542, "Glass Harbour (feat. Someone)"},
		{542, 3723, "Low Tide"},
		{3723, 3800, "Hidden Track"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d tracks: %+v", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("track %d = %+v, want %+v", i+1, got[i], want[i])
		}
	}
	if one := parseTracklist("Best part is at 2:30, trust me", 300); len(one) > 1 {
		t.Errorf("a passing mention became a tracklist: %+v", one)
	}
}

func fakeVideo(t *testing.T, info videoInfo, segments []audioanalysis.Segment) *string {
	t.Helper()
	prevInfo, prevFetch, prevSeg := fetchVideoInfo, fetchFullAudio, segmentAudio
	t.Cleanup(func() { fetchVideoInfo, fetchFullAudio, segmentAudio = prevInfo, prevFetch, prevSeg })
	fetchVideoInfo = func(context.Context, string) (videoInfo, error) { return info, nil }
	dir := new(string)
	fetchFullAudio = func(_ context.Context, _ string, d string) (string, error) {
		*dir = d
		path := filepath.Join(d, "recording.webm")
		return path, os.WriteFile(path, []byte("audio"), 0o600)
	}
	segmentAudio = func(context.Context, string, float64) ([]audioanalysis.Segment, float64, error) {
		if segments == nil {
			return nil, 0, errors.New("ffmpeg is not installed")
		}
		return segments, info.Duration, nil
	}
	return dir
}

func TestFindTracksPrefersChaptersThenDescriptionThenAudio(t *testing.T) {
	app := newTestApp(t)
	const link = "https://www.youtube.com/watch?v=album"

	info := videoInfo{Duration: 600, Description: "0:00 One\n4:00 Two"}
	info.Chapters = append(info.Chapters, struct {
		Start float64 `json:"start_time"`
		End   float64 `json:"end_time"`
		Title string  `json:"title"`
	}{0, 300, "1. Chapter One"}, struct {
		Start float64 `json:"start_time"`
		End   float64 `json:"end_time"`
		Title string  `json:"title"`
	}{300, 600, "Chapter Two"})
	fakeVideo(t, info, nil)
	split, err := app.FindInspirationTracks(link)
	if err != nil || split.Source != "chapters" || split.Tracks[0].Title != "Chapter One" || split.Tracks[1].Title != "Chapter Two" {
		t.Errorf("chapters: %+v, %v", split, err)
	}

	info.Chapters = nil
	fakeVideo(t, info, nil)
	split, err = app.FindInspirationTracks(link)
	if err != nil || split.Source != "description" || split.Tracks[1] != (DetectedTrack{240, 600, "Two"}) {
		t.Errorf("description: %+v, %v", split, err)
	}

	info.Description = "No tracklist here."
	dir := fakeVideo(t, info, []audioanalysis.Segment{{Start: 0, End: 250, Cue: "start"}, {Start: 250, End: 600, Cue: "gap"}})
	split, err = app.FindInspirationTracks(link)
	if err != nil || split.Source != "audio" || len(split.Tracks) != 2 || split.Tracks[1].StartSeconds != 250 ||
		split.Tracks[1].Title != "Track 2" {
		t.Errorf("audio: %+v, %v", split, err)
	}
	if _, err := os.Stat(*dir); !os.IsNotExist(err) {
		t.Errorf("the recording was left on disk at %s", *dir)
	}

	fakeVideo(t, info, nil)
	if _, err := app.FindInspirationTracks(link); err == nil {
		t.Error("listening failed and no error came back")
	}
}
