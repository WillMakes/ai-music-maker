package app

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aimusicmaker/internal/audioanalysis"
)

func TestParseTimestamp(t *testing.T) {
	good := map[string]float64{
		"72":      72,
		"72.5":    72.5,
		"72s":     72,
		"1:12":    72,
		"01:12":   72,
		"0:05":    5,
		"1:02:03": 3723,
		"1m12s":   72,
		"1h2m3s":  3723,
		"2m":      120,
		"https://www.youtube.com/watch?v=abc&t=72":       72,
		"https://youtu.be/abc?t=1m12s":                   72,
		"https://music.youtube.com/watch?v=abc&start=90": 90,
		"https://www.youtube.com/watch?v=abc#t=1m5s":     65,
	}
	for in, want := range good {
		got, err := parseTimestamp(in)
		if err != nil || got != want {
			t.Errorf("parseTimestamp(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "abc", "1:75", "1:2:3:4", "-5", "1::2",
		"https://www.youtube.com/watch?v=abc", "1.5:20"} {
		if got, err := parseTimestamp(bad); err == nil {
			t.Errorf("parseTimestamp(%q) = %v, want an error", bad, got)
		}
	}
}

func TestParseRangeTimesAndValidation(t *testing.T) {
	cases := []struct {
		start, end string
		duration   float64
		want       RangeTimes
	}{
		{"1:12", "1:48", 0, RangeTimes{72, 108}},
		{"1:12–1:48", "", 249, RangeTimes{72, 108}},
		{"1:12 - 1:48", "", 0, RangeTimes{72, 108}},
		{"1:12 to 1:48", "", 0, RangeTimes{72, 108}},
		{"https://www.youtube.com/watch?v=x&t=72", "1:48", 0, RangeTimes{72, 108}},
		{"4:00", "4:09", 249.4, RangeTimes{240, 249}},
	}
	for _, c := range cases {
		got, err := parseRangeTimes(c.start, c.end, c.duration)
		if err != nil || got != c.want {
			t.Errorf("parseRangeTimes(%q, %q) = %+v, %v; want %+v", c.start, c.end, got, err, c.want)
		}
	}
	for _, c := range []struct {
		start, end string
		duration   float64
		says       string
	}{
		{"1:48", "1:12", 0, "before it starts"},
		{"1:12", "1:12", 0, "before it starts"},
		{"4:00", "4:30", 249, "only 4:09 long"},
		{"0:00", "31:00", 0, "under 30 minutes"},
		{"1:12", "", 0, "needs an end"},
	} {
		_, err := parseRangeTimes(c.start, c.end, c.duration)
		if err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("parseRangeTimes(%q, %q, %v) = %v; want an error saying %q", c.start, c.end, c.duration, err, c.says)
		}
	}
}

func TestFormatTimestamp(t *testing.T) {
	for in, want := range map[float64]string{0: "0:00", 72: "1:12", 3723: "1:02:03", 72.5: "1:12.5", 59.99: "0:59"} {
		if got := formatTimestamp(in); got != want {
			t.Errorf("formatTimestamp(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestCitationKeysAndLines(t *testing.T) {
	for n, want := range map[int]string{1: "a", 2: "b", 26: "z", 27: "aa", 28: "ab", 52: "az", 53: "ba"} {
		if got := rangeSuffix(n); got != want {
			t.Errorf("rangeSuffix(%d) = %q, want %q", n, got, want)
		}
	}
	for link, want := range map[string]string{
		"https://music.youtube.com/watch?v=5NV6Rdv1a3I&si=x": "https://www.youtube.com/watch?v=5NV6Rdv1a3I&t=72s",
		"https://youtu.be/5NV6Rdv1a3I":                       "https://www.youtube.com/watch?v=5NV6Rdv1a3I&t=72s",
		"https://www.youtube.com/shorts/abc":                 "https://www.youtube.com/watch?v=abc&t=72s",
	} {
		if got := timestampedLink(link, 72.4); got != want {
			t.Errorf("timestampedLink(%q) = %q, want %q", link, got, want)
		}
	}
	insp := Inspiration{CiteKey: "INS-3", Title: "Get Lucky", Artist: "Daft Punk",
		Album: "Random Access Memories", URL: "https://music.youtube.com/watch?v=5NV6Rdv1a3I"}
	r := InspirationRange{CiteKey: "INS-3a", StartSeconds: 72, EndSeconds: 108, Label: "the pre-chorus build"}
	if got, want := rangeCitation(insp, r),
		"[INS-3a] \u201cGet Lucky\u201d \u2014 Daft Punk, 1:12\u20131:48 \u201cthe pre-chorus build\u201d \u2014 https://www.youtube.com/watch?v=5NV6Rdv1a3I&t=72s"; got != want {
		t.Errorf("range citation\n got %s\nwant %s", got, want)
	}
	if got := inspirationCitation(insp); !strings.HasPrefix(got, "[INS-3] \u201cGet Lucky\u201d \u2014 Daft Punk (Random Access Memories) \u2014 https://www.youtube.com/watch?v=5NV6Rdv1a3I") {
		t.Errorf("inspiration citation %s", got)
	}
	insp.Ranges = []InspirationRange{r}
	lines := resolveCitations(Artist{Inspirations: []Inspiration{insp}},
		[]string{"[INS-3a]", "INS-9", "INS-3", "INS-3a"})
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "[INS-3a]") || !strings.HasPrefix(lines[1], "[INS-3]") {
		t.Errorf("resolveCitations = %q; want INS-3a then INS-3, the invented INS-9 and the repeat dropped", lines)
	}
}

// legacyInspirationsDB writes a database the way builds before ranges left
// it: artist_inspirations with no citation columns, no ranges table.
func legacyInspirationsDB(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range []string{
		`CREATE TABLE artists (id TEXT PRIMARY KEY, name TEXT NOT NULL DEFAULT '', slug TEXT NOT NULL DEFAULT '',
			summary TEXT NOT NULL DEFAULT '', base_style TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE artist_inspirations (id TEXT PRIMARY KEY, artist_id TEXT NOT NULL,
			url TEXT NOT NULL DEFAULT '', title TEXT NOT NULL DEFAULT '', note TEXT NOT NULL DEFAULT '',
			position INTEGER NOT NULL DEFAULT 0, at TEXT NOT NULL DEFAULT '')`,
		`INSERT INTO artists (id, name, slug) VALUES ('art_a', 'Old Artist', 'old-artist'), ('art_b', 'Other', 'other')`,
		`INSERT INTO artist_inspirations (id, artist_id, url, title, note, position, at) VALUES
			('insp_2', 'art_a', 'https://youtu.be/two', 'Second', 'kept note two', 1, '2026-01-01'),
			('insp_1', 'art_a', 'https://youtu.be/one', 'First', 'kept note one', 0, '2026-01-01'),
			('insp_3', 'art_b', 'https://youtu.be/three', 'Third', '', 0, '2026-01-01')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
}

func TestMigrationKeysExistingInspirations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacyInspirationsDB(t, path)

	for open := 1; open <= 2; open++ { // a second launch must change nothing
		store, err := openStoreAt(path)
		if err != nil {
			t.Fatalf("open %d: %v", open, err)
		}
		a, err := store.artist("art_a")
		if err != nil {
			t.Fatal(err)
		}
		if len(a.Inspirations) != 2 || a.Inspirations[0].Title != "First" || a.Inspirations[0].Note != "kept note one" {
			t.Fatalf("open %d: inspirations not kept: %+v", open, a.Inspirations)
		}
		if a.Inspirations[0].CiteKey != "INS-1" || a.Inspirations[1].CiteKey != "INS-2" {
			t.Errorf("open %d: keys %q %q, want INS-1 INS-2 in listed order", open,
				a.Inspirations[0].CiteKey, a.Inspirations[1].CiteKey)
		}
		if len(a.Inspirations[0].Ranges) != 0 || a.Inspirations[0].Ranges == nil {
			t.Errorf("open %d: a legacy reference has ranges %v; want an empty list", open, a.Inspirations[0].Ranges)
		}
		b, _ := store.artist("art_b")
		if b.Inspirations[0].CiteKey != "INS-1" {
			t.Errorf("keys are per artist; the other artist's first is %q", b.Inspirations[0].CiteKey)
		}
		_ = store.Close()
	}
}

func rangeOf(start, end float64, label string) InspirationRange {
	return InspirationRange{StartSeconds: start, EndSeconds: end, Label: label}
}

func TestSaveArtistKeepsRangesKeysAndBreakdowns(t *testing.T) {
	app := newTestApp(t)
	saved, err := app.SaveArtist(Artist{Name: "Range Test", Inspirations: []Inspiration{
		{URL: "https://www.youtube.com/watch?v=one", Title: "One",
			Ranges: []InspirationRange{rangeOf(72, 108, "pre-chorus build"), rangeOf(120, 150, "")}},
		{URL: "https://music.youtube.com/watch?v=two", Title: "Two"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	one := saved.Inspirations[0]
	if one.CiteKey != "INS-1" || saved.Inspirations[1].CiteKey != "INS-2" {
		t.Fatalf("keys %q %q", one.CiteKey, saved.Inspirations[1].CiteKey)
	}
	if len(one.Ranges) != 2 || one.Ranges[0].CiteKey != "INS-1a" || one.Ranges[1].CiteKey != "INS-1b" ||
		one.Ranges[0].Label != "pre-chorus build" || one.Ranges[0].StartSeconds != 72 {
		t.Fatalf("ranges did not round-trip: %+v", one.Ranges)
	}
	if !strings.Contains(one.Ranges[0].Citation, "1:12\u20131:48") || !strings.Contains(one.Ranges[0].Citation, "&t=72s") {
		t.Errorf("citation %q", one.Ranges[0].Citation)
	}

	// A breakdown lands behind the window's back.
	b := RangeBreakdown{Source: "audio", ForStart: 72, ForEnd: 108, Summary: "a build",
		Measurements: &audioanalysis.Measurements{TempoBPM: 116.1, Key: "B minor"}}
	if ok, err := app.store.setRangeBreakdown(one.Ranges[0].ID, b); err != nil || !ok {
		t.Fatalf("setRangeBreakdown = %v, %v", ok, err)
	}

	// The window's draft predates it, and its auto-save must not erase it.
	stale := saved
	again, err := app.SaveArtist(stale)
	if err != nil {
		t.Fatal(err)
	}
	got := again.Inspirations[0].Ranges[0].Breakdown
	if got.Source != "audio" || got.Measurements == nil || got.Measurements.TempoBPM != 116.1 || got.Summary != "a build" {
		t.Errorf("a stale save erased the breakdown: %+v", got)
	}

	// An MCP client that never heard of ranges sends none: they are kept.
	noRanges := again
	noRanges.Inspirations = []Inspiration{
		{ID: again.Inspirations[0].ID, URL: again.Inspirations[0].URL, Title: "One"},
		{ID: again.Inspirations[1].ID, URL: again.Inspirations[1].URL, Title: "Two"},
	}
	kept, err := app.SaveArtist(noRanges)
	if err != nil {
		t.Fatal(err)
	}
	if len(kept.Inspirations[0].Ranges) != 2 || kept.Inspirations[0].Ranges[0].Breakdown.Source != "audio" {
		t.Errorf("ranges left out were dropped: %+v", kept.Inspirations[0].Ranges)
	}

	// Moving the range drops its breakdown: it described other seconds.
	moved := kept
	moved.Inspirations[0].Ranges[0].StartSeconds = 80
	afterMove, err := app.SaveArtist(moved)
	if err != nil {
		t.Fatal(err)
	}
	if r := afterMove.Inspirations[0].Ranges[0]; r.Breakdown.Source != "" || r.CiteKey != "INS-1a" {
		t.Errorf("moved range: key %q breakdown %+v; want INS-1a kept and the breakdown dropped", r.CiteKey, r.Breakdown)
	}

	// Removing a range or a reference never frees its key for the next one.
	trimmed := afterMove
	trimmed.Inspirations = []Inspiration{afterMove.Inspirations[0]}
	trimmed.Inspirations[0].Ranges = []InspirationRange{afterMove.Inspirations[0].Ranges[0], rangeOf(200, 210, "new")}
	trimmed.Inspirations = append(trimmed.Inspirations, Inspiration{URL: "https://youtu.be/three", Title: "Three"})
	final, err := app.SaveArtist(trimmed)
	if err != nil {
		t.Fatal(err)
	}
	if k := final.Inspirations[0].Ranges[1].CiteKey; k != "INS-1c" {
		t.Errorf("a new range after INS-1b was removed got %q, want INS-1c", k)
	}
	if k := final.Inspirations[1].CiteKey; k != "INS-3" {
		t.Errorf("a new reference after INS-2 was removed got %q, want INS-3", k)
	}

	// An empty list removes the ranges.
	cleared := final
	cleared.Inspirations[0].Ranges = []InspirationRange{}
	none, err := app.SaveArtist(cleared)
	if err != nil {
		t.Fatal(err)
	}
	if len(none.Inspirations[0].Ranges) != 0 {
		t.Errorf("an empty list kept %d ranges", len(none.Inspirations[0].Ranges))
	}
}

func TestSaveArtistRefusesARangeOutsideTheVideo(t *testing.T) {
	app := newTestApp(t)
	_, err := app.SaveArtist(Artist{Name: "Bounds", Inspirations: []Inspiration{
		{URL: "https://youtu.be/x", Title: "Short", DurationSeconds: 100,
			Ranges: []InspirationRange{rangeOf(90, 120, "")}},
	}})
	if err == nil || !strings.Contains(err.Error(), "only 1:40 long") {
		t.Errorf("a range past the end saved: %v", err)
	}
}

// fakeBreakdown stands in for yt-dlp, ffmpeg and the models, and records
// where the section was put.
type fakeBreakdown struct {
	fetchErr, measureErr, proseErr error
	duration                       float64
	clipDir                        string
	prompt                         string
}

func (f *fakeBreakdown) install(t *testing.T) {
	prevFetch, prevMeasure, prevProse, prevResearch := fetchRangeAudio, measureRangeAudio, writeRangeProse, researchSong
	t.Cleanup(func() {
		fetchRangeAudio, measureRangeAudio, writeRangeProse, researchSong = prevFetch, prevMeasure, prevProse, prevResearch
	})
	fetchRangeAudio = func(_ context.Context, _ string, start, end float64, dir string) (string, float64, error) {
		f.clipDir = dir
		if f.fetchErr != nil {
			return "", f.duration, f.fetchErr
		}
		path := filepath.Join(dir, "section.webm")
		return path, f.duration, os.WriteFile(path, []byte("audio"), 0o600)
	}
	measureRangeAudio = func(context.Context, string) (audioanalysis.Measurements, error) {
		if f.measureErr != nil {
			return audioanalysis.Measurements{}, f.measureErr
		}
		return audioanalysis.Measurements{Seconds: 36, TempoBPM: 116.1, TempoConfidence: 0.8, Key: "B minor",
			KeyConfidence: 0.08, LoudnessMeasured: true, IntegratedLUFS: -10.9, CrestDB: 13.8,
			StereoWidth: 0.14, StereoCorrelation: 0.96}, nil
	}
	writeRangeProse = func(_ *App, _ context.Context, system, prompt string) (string, error) {
		f.prompt = system + "\n---\n" + prompt
		if f.proseErr != nil {
			return "", f.proseErr
		}
		return "```json\n{\"summary\":\"Four-on-the-floor build under a dry falsetto.\",\"breakdown\":\"**Instrumentation** — guitar, bass\"}\n```", nil
	}
	researchSong = func(*App, string) (MusicAnalysis, error) {
		return MusicAnalysis{}, errors.New("Antigravity is not connected")
	}
}

func savedRangeArtist(t *testing.T, app *App) (Artist, Inspiration, InspirationRange) {
	t.Helper()
	a, err := app.SaveArtist(Artist{Name: "Breakdown Test", Inspirations: []Inspiration{
		{URL: "https://music.youtube.com/watch?v=5NV6Rdv1a3I", Title: "Get Lucky", Artist: "Daft Punk",
			Note: "Disco-funk, Nile Rodgers guitar.", Ranges: []InspirationRange{rangeOf(72, 108, "pre-chorus build")}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return a, a.Inspirations[0], a.Inspirations[0].Ranges[0]
}

func TestBreakDownMeasuresTheSection(t *testing.T) {
	app := newTestApp(t)
	f := &fakeBreakdown{duration: 249}
	f.install(t)
	a, insp, r := savedRangeArtist(t, app)

	got, err := app.BreakDownInspirationRange(a.ID, insp.ID, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	b := got.Ranges[0].Breakdown
	if b.Source != "audio" || b.Measurements == nil || b.Measurements.TempoBPM != 116.1 {
		t.Fatalf("breakdown %+v", b)
	}
	if b.Summary != "Four-on-the-floor build under a dry falsetto." || !strings.HasPrefix(b.Interpretation, "**Instrumentation**") {
		t.Errorf("prose not kept apart from the numbers: summary %q interpretation %q", b.Summary, b.Interpretation)
	}
	if got.DurationSeconds != 249 {
		t.Errorf("the video's length was not kept: %v", got.DurationSeconds)
	}
	if !strings.Contains(f.prompt, "116.1 BPM") || !strings.Contains(f.prompt, "Disco-funk") ||
		!strings.Contains(f.prompt, "pre-chorus build") {
		t.Errorf("the model was not handed the numbers, the research and the label:\n%s", f.prompt)
	}
	if _, err := os.Stat(f.clipDir); !os.IsNotExist(err) {
		t.Errorf("the section's audio was left on disk at %s", f.clipDir)
	}
}

func TestBreakDownFallsBackToResearch(t *testing.T) {
	app := newTestApp(t)
	f := &fakeBreakdown{fetchErr: errNoYTDLP}
	f.install(t)
	a, insp, r := savedRangeArtist(t, app)

	got, err := app.BreakDownInspirationRange(a.ID, insp.ID, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	b := got.Ranges[0].Breakdown
	if b.Source != "research" || b.Measurements != nil || !strings.Contains(b.AudioError, "yt-dlp is not installed") {
		t.Errorf("fallback %+v", b)
	}
	if !strings.Contains(f.prompt, "No audio could be measured") {
		t.Errorf("the model was not told it had no audio:\n%s", f.prompt)
	}
}

func TestBreakDownFailuresAreClear(t *testing.T) {
	app := newTestApp(t)
	f := &fakeBreakdown{fetchErr: errors.New("video unavailable"), proseErr: errors.New("no AI connected")}
	f.install(t)
	a, insp, r := savedRangeArtist(t, app)
	if _, err := app.BreakDownInspirationRange(a.ID, insp.ID, r.ID); err == nil ||
		!strings.Contains(err.Error(), "[INS-1a]") || !strings.Contains(err.Error(), "video unavailable") {
		t.Errorf("both halves failing: %v", err)
	}

	// Numbers without prose are kept, with the reason.
	f.fetchErr = nil
	got, err := app.BreakDownInspirationRange(a.ID, insp.ID, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b := got.Ranges[0].Breakdown; b.Source != "audio" || b.ProseError != "no AI connected" {
		t.Errorf("measured without prose: %+v", b)
	}

	// A range past the end of the video is the range's own fault.
	f.duration = 100
	if _, err := app.BreakDownInspirationRange(a.ID, insp.ID, r.ID); err == nil ||
		!strings.Contains(err.Error(), "only 1:40 long") {
		t.Errorf("range past the end: %v", err)
	}
}

func TestBriefsCiteTheReferences(t *testing.T) {
	app := newTestApp(t)
	f := &fakeBreakdown{duration: 249}
	f.install(t)
	a, insp, r := savedRangeArtist(t, app)
	if _, err := app.BreakDownInspirationRange(a.ID, insp.ID, r.ID); err != nil {
		t.Fatal(err)
	}

	brief, err := app.interviewBrief(subjectAlbum, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"[INS-1] \u201cGet Lucky\u201d \u2014 Daft Punk",
		"[INS-1a] 1:12\u20131:48 \u201cpre-chorus build\u201d \u2014 measured from the audio: 116.1 BPM",
		"Four-on-the-floor build under a dry falsetto.",
		"cite its key",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("the album brief is missing %q:\n%s", want, brief)
		}
	}
	if strings.Contains(brief, "**Instrumentation**") {
		t.Error("the brief carries the full breakdown; it should carry the summary and the key")
	}

	album, err := app.SaveAlbum(Album{ArtistID: a.ID, Name: "Cited"})
	if err != nil {
		t.Fatal(err)
	}
	trackBrief, err := app.interviewBrief(subjectTrack, album.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(trackBrief, "[INS-1a]") {
		t.Errorf("the track brief has no citation keys:\n%s", trackBrief)
	}
}

func TestPlanCitationsResolveIntoNotes(t *testing.T) {
	app := newTestApp(t)
	a, _, _ := savedRangeArtist(t, app)
	a, _ = app.GetArtist(a.ID)

	tracks := []TrackDraft{{Name: "One", Citations: []string{"INS-1a", "INS-7"}}, {Name: "Two"}}
	citeTracks(a, tracks)
	if len(tracks[0].Citations) != 1 || tracks[0].Citations[0] != "INS-1a" {
		t.Errorf("track citations %v; want the invented key dropped", tracks[0].Citations)
	}
	if !strings.HasPrefix(tracks[0].Notes, "Inspirations cited:\n- [INS-1a] \u201cGet Lucky\u201d") {
		t.Errorf("track notes %q", tracks[0].Notes)
	}
	if tracks[1].Notes != "" || tracks[1].Citations == nil {
		t.Errorf("an uncited track: notes %q citations %v", tracks[1].Notes, tracks[1].Citations)
	}

	// The album keeps its citations through a save that does not send them.
	album, err := app.SaveAlbum(Album{ArtistID: a.ID, Name: "Kept", Citations: citationNotes(resolveCitations(a, []string{"INS-1"}))})
	if err != nil {
		t.Fatal(err)
	}
	album.Citations = ""
	album.Name = "Kept, renamed"
	again, err := app.SaveAlbum(album)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(again.Citations, "[INS-1]") {
		t.Errorf("a save without citations erased them: %q", again.Citations)
	}
}

func TestGetInspirationCitation(t *testing.T) {
	app := newTestApp(t)
	a, _, _ := savedRangeArtist(t, app)
	got, err := app.GetInspirationCitation(a.ID, "[INS-1a]")
	if err != nil || got.Range == nil || got.Range.Label != "pre-chorus build" || !strings.HasPrefix(got.Citation, "[INS-1a]") {
		t.Errorf("GetInspirationCitation = %+v, %v", got, err)
	}
	if _, err := app.GetInspirationCitation(a.ID, "INS-9"); err == nil {
		t.Error("an unknown key was found")
	}
}

// TestAnOlderBuildCannotRenumberKeys is the side-by-side case: a build from
// before citation keys saves an artist by deleting and reinserting its
// references with no number. The keys must come back as they were.
func TestAnOlderBuildCannotRenumberKeys(t *testing.T) {
	app := newTestApp(t)
	a, _, _ := savedRangeArtist(t, app)
	a, err := app.SaveArtist(Artist{ID: a.ID, Name: a.Name, Inspirations: append(a.Inspirations,
		Inspiration{URL: "https://youtu.be/second", Title: "Second"})})
	if err != nil {
		t.Fatal(err)
	}
	// The older build's save, reversed order and no citation columns.
	db := app.store.db
	if _, err := db.Exec(`DELETE FROM artist_inspirations WHERE artist_id = ?`, a.ID); err != nil {
		t.Fatal(err)
	}
	for n, i := range []Inspiration{a.Inspirations[1], a.Inspirations[0]} {
		if _, err := db.Exec(`INSERT INTO artist_inspirations (id, artist_id, url, title, position, at)
			VALUES (?, ?, ?, ?, ?, ?)`, i.ID, a.ID, i.URL, i.Title, n, nowStamp()); err != nil {
			t.Fatal(err)
		}
	}
	check := func(when string) {
		got, err := app.GetArtist(a.ID)
		if err != nil {
			t.Fatal(err)
		}
		keys := map[string]string{}
		for _, i := range got.Inspirations {
			keys[i.Title] = i.CiteKey
			for _, r := range i.Ranges {
				keys[r.Label] = r.CiteKey
			}
		}
		if keys["Get Lucky"] != "INS-1" || keys["Second"] != "INS-2" || keys["pre-chorus build"] != "INS-1a" {
			t.Errorf("%s: keys %v", when, keys)
		}
	}
	check("read straight after")
	if err := app.store.backfillCitations(); err != nil {
		t.Fatal(err)
	}
	check("after the next launch's backfill")
	got, _ := app.GetArtist(a.ID)
	got.Inspirations = append(got.Inspirations, Inspiration{URL: "https://youtu.be/third", Title: "Third"})
	saved, err := app.SaveArtist(got)
	if err != nil {
		t.Fatal(err)
	}
	if k := saved.Inspirations[2].CiteKey; k != "INS-3" {
		t.Errorf("a new reference after the old build's save got %q, want INS-3", k)
	}
}
