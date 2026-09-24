package main

import (
	"path/filepath"
	"testing"
)

// newTestApp builds an app backed by a temp-dir database and a temp music
// folder.
//
// Both overrides matter and for different reasons. dataDir keeps the test suite
// away from the producer's real ~/.sunomaker, which holds their library and
// their API keys. osMusicDir keeps it out of their actual music collection —
// this app copies audio files, and a test that imported into ~/Music would put
// junk in something a person listens to.
//
// The originals are restored by t.Cleanup rather than by the caller, so a test
// that fails halfway does not leave the next one pointed at a directory that has
// since been deleted.
func newTestApp(t *testing.T) *App {
	t.Helper()

	dir := t.TempDir()
	previousData, previousMusic, previousToken := dataDir, osMusicDir, githubToken
	dataDir = func() (string, error) { return dir, nil }
	osMusicDir = func() (string, error) { return filepath.Join(dir, "music"), nil }
	// No credential, so findIssueQueue answers errIssuesUnavailable at once.
	//
	// This one is not optional and is not about isolation in the abstract: the
	// tests run inside this checkout, so currentBugRepo finds the real remote and
	// `gh auth token` answers with the producer's real credential. Without this
	// line, `go test` opens real issues on the real repository — which it did,
	// once, and which is why the line is here rather than in a comment about what
	// might happen.
	githubToken = func() string { return "" }
	// No encoder, so a WAV import files the WAV (#93) and no test runs ffmpeg
	// by accident. A test of the conversion sets its own.
	previousEncode := encodeMP3
	encodeMP3 = func(string, string) error { return errNoEncoder }
	t.Cleanup(func() {
		dataDir, osMusicDir, githubToken = previousData, previousMusic, previousToken
		encodeMP3 = previousEncode
	})

	// The same file openStore() would choose, not a test-only name: the bug CLI
	// opens its own store through openStore, so a differently named database
	// would leave a CLI test reading an empty file beside the one the app wrote.
	store, err := openStoreAt(filepath.Join(dir, "sunomaker.db"))
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// toJax is stubbed to "there is nowhere to file this", which is the state a
	// machine with Jax closed is in — and the state a test must be in, because
	// the alternative is a suite whose results depend on whether the producer
	// happens to have another application running.
	return &App{store: store, toJax: func(BugReport) (int, error) { return 0, errJaxUnavailable }}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"The Long Dark": "the-long-dark",
		"  Spaces  ":    "spaces",
		"Rock & Roll!":  "rock-roll",
		"---Leading":    "leading",
		"Volume 2":      "volume-2",
		"":              "",
		"!!!":           "",
		"Æther":         "ther", // non-ASCII letters are separators, by design
		"a  b":          "a-b",
	}
	for input, want := range cases {
		if got := slugify(input); got != want {
			t.Errorf("slugify(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestUniqueSlugCounts(t *testing.T) {
	app := newTestApp(t)

	// Three artists with the same name. The point is that the second and third
	// are accepted rather than rejected: the producer meant a second one, and
	// "that already exists" is not a useful answer to somebody typing a name.
	names := []string{"Static Field", "Static Field", "Static Field"}
	want := []string{"static-field", "static-field-2", "static-field-3"}
	for i, name := range names {
		artist, err := app.SaveArtist(Artist{Name: name})
		if err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
		if artist.Slug != want[i] {
			t.Errorf("artist %d slug = %q, want %q", i, artist.Slug, want[i])
		}
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	app := newTestApp(t)

	// An unwritten key answers empty rather than failing: every caller treats
	// "missing" and "empty" as the same thing — the producer has not chosen.
	if value, err := app.GetSetting(keyTheme); err != nil || value != "" {
		t.Fatalf("unset key = (%q, %v), want (\"\", nil)", value, err)
	}
	if err := app.SetSetting(keyTheme, "dark"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if value, _ := app.GetSetting(keyTheme); value != "dark" {
		t.Errorf("after set = %q, want \"dark\"", value)
	}
	// Overwritten rather than duplicated — the upsert is the whole reason the
	// table has a primary key on `key`.
	if err := app.SetSetting(keyTheme, "light"); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if value, _ := app.GetSetting(keyTheme); value != "light" {
		t.Errorf("after overwrite = %q, want \"light\"", value)
	}
}

func TestNilStoreReadsEmptyAndWritesFail(t *testing.T) {
	// The state a launch is in when the database will not open. Reads have to
	// answer something — a window that cannot show its data is still a window
	// somebody reads Settings in — but a write that silently did nothing would
	// be a lie.
	app := &App{}

	if artists, err := app.ListArtists(); err != nil || len(artists) != 0 {
		t.Errorf("ListArtists = (%v, %v), want (empty, nil)", artists, err)
	}
	if _, err := app.SaveArtist(Artist{Name: "Anything"}); err != errNoStore {
		t.Errorf("SaveArtist error = %v, want errNoStore", err)
	}
	if _, err := app.FileBugReport(BugReport{Description: "x"}); err != errNoStore {
		t.Errorf("FileBugReport error = %v, want errNoStore", err)
	}
}
