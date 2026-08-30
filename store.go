package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	// modernc.org/sqlite rather than mattn/go-sqlite3: it is pure Go, so the
	// three platform builds need no cgo toolchain and cross-compiling from any
	// one of them stays a single `wails build`.
	_ "modernc.org/sqlite"
)

// errNoStore is what a binding returns when the database could not be opened.
// Reads answer empty rather than failing — a window that cannot show its data is
// still a window somebody can read Settings in — but a write that silently did
// nothing would be a lie, so writes say so.
var errNoStore = errors.New("the SunoMaker database is not available")

// Setting keys used in the key/value `settings` table. Keys shared with the
// frontend are mirrored in frontend/src/lib/settings.ts — change both together.
const (
	// keyTheme holds the UI theme preference: "system" | "light" | "dark".
	keyTheme = "theme"
	// keyNavCollapsed ("true"/"false") remembers the sidebar rail's width.
	keyNavCollapsed = "nav_collapsed"
	// keyLastRoute is the route last open, as the frontend's JSON, so the app
	// reopens where it was closed. Written and read by the frontend alone; named
	// here so the key list is the whole list.
	keyLastRoute = "last_route"
	// keyMusicRoot is where finished Suno files are filed. Empty means the OS
	// music folder's SunoMaker directory (see musicRoot in musicdir.go) — a
	// place outside ~/.sunomaker on purpose: the audio is the thing that should
	// outlive an uninstall, and it belongs where a music player will find it.
	keyMusicRoot = "music_root"
	// keyDefaultTrackLength is the target length new tracks start at, in
	// seconds. Stored because a producer making an album works to one shape.
	keyDefaultTrackLength = "default_track_length"
)

// Change scopes emitted on the "data:changed" event. A scope is either a
// settings key or a table name; the frontend subscribes by scope so an open page
// picks up writes made behind its back (see lib/dataChanged.ts).
const (
	scopeArtists = "artists"
	// scopeAlbums covers albums and their artwork together: the panel reads an
	// album as its cover plus its fields, and two scopes would only ever make it
	// reload twice.
	scopeAlbums = "albums"
	// scopeTracks covers tracks and the files attached to them, for the same
	// reason — importing four variations writes one track and four files, and a
	// list that reloaded per file would flicker through the import.
	scopeTracks          = "tracks"
	scopeSkills          = "skills"
	scopeBugReports      = "bug_reports"
	scopeResolvedReports = "resolved_reports"
	// scopeAIServices is reported after a provider connection changes, so an
	// open Settings page picks it up.
	scopeAIServices = "ai_services"
	// scopeChats is a stored interview transcript moving (see interview.go).
	scopeChats = "chats"
)

// Store wraps the SQLite database that persists all app data. The database lives
// under the user's home directory so it is shared across builds and survives
// reinstalls.
type Store struct {
	db *sql.DB
	// onChange, when set, is called with a change scope after every write — how
	// the app tells open pages their data moved underneath them (see startup's
	// "data:changed" emit). Called from whichever goroutine wrote, so the hook
	// must be safe to call concurrently.
	onChange func(scope string)
}

// changed reports a write to scope through the onChange hook (nil-safe).
func (s *Store) changed(scope string) {
	if s == nil || s.onChange == nil {
		return
	}
	s.onChange(scope)
}

// dataDir returns the ~/.sunomaker directory, creating it (0700) if necessary.
// Everything the app owns — the database, the exported extension, generated
// artwork — hangs off this one place, so uninstalling is deleting a folder.
//
// A variable so a test can point it somewhere disposable. Not defensive
// plumbing: artwork lands under here, and a test suite that wrote into the
// producer's real directory is exactly what CLAUDE.md means by never touching
// ~/.sunomaker. newTestApp overrides it.
var dataDir = realDataDir

func realDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".sunomaker")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// openStore opens (creating if needed) the app's database and applies the
// schema.
func openStore() (*Store, error) {
	dir, err := dataDir()
	if err != nil {
		return nil, err
	}
	return openStoreAt(filepath.Join(dir, "sunomaker.db"))
}

// openStoreAt opens a store at an explicit path. Tests use it against a temp
// directory so they never touch the real database.
func openStoreAt(path string) (*Store, error) {
	// busy_timeout is what makes two *processes* able to share this file. The
	// window is not the only writer: the MCP server (see mcp.go) is this same
	// binary relaunched, and it writes here while the window is reading. Without
	// a timeout SQLite fails the second writer instantly rather than waiting.
	//
	// WAL is what makes the waiting short: readers do not block the writer, so a
	// page polling for changes cannot hold an insert off.
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	// SQLite allows a single writer; serialising connections avoids spurious
	// "database is locked" errors under this app's light, bursty write load.
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the underlying database handle.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// migrate creates the schema. Every statement is IF NOT EXISTS, so this runs on
// every launch and is the only place the shape of the database is stated.
func (s *Store) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
-- One row per artist. base_style is the technical breakdown every album under
-- this artist is combined with: the sound that makes them recognisably one
-- artist across records, as against an album's own colour.
CREATE TABLE IF NOT EXISTS artists (
	id         TEXT PRIMARY KEY,
	name       TEXT NOT NULL DEFAULT '',
	slug       TEXT NOT NULL DEFAULT '',
	summary    TEXT NOT NULL DEFAULT '',
	base_style TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_artists_slug ON artists(slug);
-- The references an artist's sound was decided from: YouTube or YouTube Music
-- links, each with what it was chosen for. Its own table rather than a column,
-- because deciding a style is gathering several of them and the note against
-- each one is the part that survives — a bare list of URLs a year later says
-- nothing about why any of them is there.
CREATE TABLE IF NOT EXISTS artist_inspirations (
	id          TEXT PRIMARY KEY,
	artist_id   TEXT NOT NULL,
	url         TEXT NOT NULL DEFAULT '',
	title       TEXT NOT NULL DEFAULT '',
	artist      TEXT NOT NULL DEFAULT '',
	album       TEXT NOT NULL DEFAULT '',
	artwork_url TEXT NOT NULL DEFAULT '',
	note        TEXT NOT NULL DEFAULT '',
	position    INTEGER NOT NULL DEFAULT 0,
	at          TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_inspirations_artist
	ON artist_inspirations(artist_id, position);
-- One row per album. artwork_file is a name under the app's artwork folder, not
-- the bytes: the album list draws every cover at once, and a base64 image in a
-- column is a payload every listing carries around to draw a thumbnail.
CREATE TABLE IF NOT EXISTS albums (
	id              TEXT PRIMARY KEY,
	artist_id       TEXT NOT NULL,
	name            TEXT NOT NULL DEFAULT '',
	slug            TEXT NOT NULL DEFAULT '',
	artwork_file    TEXT NOT NULL DEFAULT '',
	artwork_prompt  TEXT NOT NULL DEFAULT '',
	technical_style TEXT NOT NULL DEFAULT '',
	apple_music_url TEXT NOT NULL DEFAULT '',
	spotify_url     TEXT NOT NULL DEFAULT '',
	youtube_url     TEXT NOT NULL DEFAULT '',
	created_at      TEXT NOT NULL DEFAULT '',
	updated_at      TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_albums_artist ON albums(artist_id, created_at);
-- One row per track. prompt is what gets pasted into Suno's Style field and
-- lyrics into its Lyrics field, both reachable from the browser extension.
-- target_seconds is a target rather than a measurement: it shapes the prompt,
-- and what Suno actually returns is whatever the file turns out to be.
CREATE TABLE IF NOT EXISTS tracks (
	id             TEXT PRIMARY KEY,
	album_id       TEXT NOT NULL,
	name           TEXT NOT NULL DEFAULT '',
	position       INTEGER NOT NULL DEFAULT 0,
	target_seconds INTEGER NOT NULL DEFAULT 0,
	prompt         TEXT NOT NULL DEFAULT '',
	lyrics         TEXT NOT NULL DEFAULT '',
	notes          TEXT NOT NULL DEFAULT '',
	created_at     TEXT NOT NULL DEFAULT '',
	updated_at     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_tracks_album ON tracks(album_id, position);
-- The finished files Suno gave back, one row per variation. The bytes are on
-- disk under the OS music folder (see musicdir.go) and never in the row: this is
-- audio, it is megabytes, and it belongs somewhere a music player can see it.
-- path is absolute so a file whose library the producer moved is still findable
-- from the row that named it.
CREATE TABLE IF NOT EXISTS track_files (
	id       TEXT PRIMARY KEY,
	track_id TEXT NOT NULL,
	label    TEXT NOT NULL DEFAULT '',
	name     TEXT NOT NULL DEFAULT '',
	path     TEXT NOT NULL DEFAULT '',
	bytes    INTEGER NOT NULL DEFAULT 0,
	chosen   INTEGER NOT NULL DEFAULT 0,
	at       TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_track_files_track ON track_files(track_id, at);
-- Skills written in the app, and overrides of the built-in Application Skills
-- (see skills.go). A row under a built-in's own id is an override; there is no
-- row for a built-in left at its default, which is what makes resetting a delete
-- and lets a later build improve a default nobody has edited.
CREATE TABLE IF NOT EXISTS skills (
	id          TEXT PRIMARY KEY,
	title       TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT '',
	body        TEXT NOT NULL DEFAULT '',
	-- A JSON array. Tags are a handful of short strings read as a set, never
	-- joined against anything, so a table of them would be a join to maintain
	-- for a list the app always wants whole.
	tags       TEXT NOT NULL DEFAULT '[]',
	created_at TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL DEFAULT ''
);
-- One row per interview in progress (see interview.go). Keyed by what is being
-- talked about, because the three conversations are different conversations:
-- resuming an album's thread into a track's would hand somebody half a
-- discussion they never had.
CREATE TABLE IF NOT EXISTS chats (
	subject_kind TEXT NOT NULL,
	subject_id   TEXT NOT NULL,
	messages     TEXT NOT NULL DEFAULT '[]',
	at           TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (subject_kind, subject_id)
);
-- The open bug queue (see bugs.go). Rows leave this table when they are fixed
-- rather than gaining a status, so that an empty table means nothing is known to
-- be broken. created_at is what "oldest first" sorts on, and being oldest is
-- what decides which report an agent is allowed to claim.
CREATE TABLE IF NOT EXISTS bug_reports (
	id            TEXT PRIMARY KEY,
	description   TEXT NOT NULL DEFAULT '',
	route         TEXT NOT NULL DEFAULT '',
	global        INTEGER NOT NULL DEFAULT 0,
	-- What was on screen, where the page was about a record. The name is not
	-- copied here: it is joined at read time, so a rename lands on every report
	-- and a deleted record stops being offered as the example.
	subject_kind  TEXT NOT NULL DEFAULT '',
	subject_id    TEXT NOT NULL DEFAULT '',
	checked_out   INTEGER NOT NULL DEFAULT 0,
	issue_url     TEXT NOT NULL DEFAULT '',
	issue_number  INTEGER NOT NULL DEFAULT 0,
	-- The matching report in Jax, which is where this queue is watched from. 0
	-- means it has not been filed there yet.
	jax_report_id INTEGER NOT NULL DEFAULT 0,
	-- What Jax said when it declined to record a claim or a resolve (#15). A
	-- refusal is not a failure and does not stop the work; it is kept so the two
	-- queues cannot disagree in silence.
	jax_note      TEXT NOT NULL DEFAULT '',
	created_at    TEXT NOT NULL DEFAULT '',
	updated_at    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_bug_reports_queue
	ON bug_reports(checked_out, created_at);
-- Reports that were fixed, kept for good. Deliberately not the same table with a
-- flag: the open queue has to stay short enough to read at a glance.
CREATE TABLE IF NOT EXISTS resolved_reports (
	id            TEXT PRIMARY KEY,
	description   TEXT NOT NULL DEFAULT '',
	route         TEXT NOT NULL DEFAULT '',
	global        INTEGER NOT NULL DEFAULT 0,
	issue_url     TEXT NOT NULL DEFAULT '',
	issue_number  INTEGER NOT NULL DEFAULT 0,
	jax_report_id INTEGER NOT NULL DEFAULT 0,
	jax_note      TEXT NOT NULL DEFAULT '',
	created_at    TEXT NOT NULL DEFAULT '',
	resolved_at   TEXT NOT NULL DEFAULT '',
	seen          INTEGER NOT NULL DEFAULT 0,
	-- The commit the fix landed in, and where it can be read. Both stored rather
	-- than the url built from the sha when the list is drawn: this is history,
	-- and a repository that moves forge should not rewrite where a fix from two
	-- years ago was made.
	commit_sha TEXT NOT NULL DEFAULT '',
	commit_url TEXT NOT NULL DEFAULT ''
);
`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	return s.addColumns()
}

// addColumns widens tables that already exist.
//
// migrate's statements are all CREATE TABLE IF NOT EXISTS, which is what lets it
// run on every launch — and what makes it silently do nothing about a column
// added after somebody's database was created. Theirs is the only database that
// matters: a new one gets the column from the schema above, and every producer
// who has been running the app since before it does not.
//
// SQLite has no ADD COLUMN IF NOT EXISTS, so the duplicate is the expected
// answer on the second launch and is swallowed by name. Every entry here must be
// additive with a default — this runs unconditionally, so a statement that could
// lose data would lose it on every launch.
func (s *Store) addColumns() error {
	columns := []struct{ table, column, definition string }{
		// What Jax said when it declined to record a claim or a resolve (#15).
		{"bug_reports", "jax_note", "TEXT NOT NULL DEFAULT ''"},
		// The files on a report, as a JSON array of paths (#20, see
		// bugattachments.go).
		{"bug_reports", "attachments", "TEXT NOT NULL DEFAULT ''"},
		{"resolved_reports", "jax_note", "TEXT NOT NULL DEFAULT ''"},
		// The song's identity on a reference, grabbed at analysis time (#51).
		{"artist_inspirations", "artist", "TEXT NOT NULL DEFAULT ''"},
		{"artist_inspirations", "album", "TEXT NOT NULL DEFAULT ''"},
		{"artist_inspirations", "artwork_url", "TEXT NOT NULL DEFAULT ''"},
	}
	for _, c := range columns {
		_, err := s.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s",
			c.table, c.column, c.definition))
		if err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("adding %s.%s: %w", c.table, c.column, err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------

// setting reads one key, answering "" for a key nobody has written. A missing
// key and an empty value are the same thing to every caller here — "the producer
// has not chosen" — so they are not distinguished.
func (s *Store) setting(key string) (string, error) {
	if s == nil || s.db == nil {
		return "", nil
	}
	var value string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (s *Store) setSetting(key, value string) error {
	if s == nil || s.db == nil {
		return errNoStore
	}
	_, err := s.db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	if err == nil {
		s.changed(key)
	}
	return err
}

// GetSetting is the frontend's read of one key.
func (a *App) GetSetting(key string) (string, error) {
	if a.store == nil {
		return "", nil
	}
	return a.store.setting(key)
}

// SetSetting is the frontend's write of one key.
func (a *App) SetSetting(key, value string) error {
	if a.store == nil {
		return errNoStore
	}
	return a.store.setSetting(key, value)
}

// ---------------------------------------------------------------------------
// Identifiers, timestamps and names
// ---------------------------------------------------------------------------

// newID mints a prefixed random id. Prefixed so an id read in a log says what it
// identifies without a lookup; random rather than sequential so two processes
// (the window and the MCP server) never mint the same one.
func newID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail in practice, and a failure here must not
		// take a save down with it. The clock is a poor id and a fine fallback.
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

// nowStamp is the one timestamp format written to the database: RFC3339 in UTC,
// which sorts as a string and so needs no date functions in any query here.
func nowStamp() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// slugify reduces a name to the lowercase, hyphenated form used for uniqueness
// and for folder names on disk. Everything that is not a letter or a digit
// becomes a separator, which is what makes it safe as a path segment on all
// three platforms.
func slugify(name string) string {
	var b strings.Builder
	lastHyphen := true // leading hyphens are dropped by starting "already at one"
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// uniqueSlug returns base, or base-2, base-3 … — whichever is not yet taken in
// table. Uniqueness is settled here rather than by letting the unique index
// reject the insert, because the caller is a person typing a name and "that
// already exists" is not a useful answer when they meant a second one.
func (s *Store) uniqueSlug(table, base, exceptID string) (string, error) {
	if base == "" {
		base = "untitled"
	}
	if s == nil || s.db == nil {
		return base, nil
	}
	for n := 1; ; n++ {
		candidate := base
		if n > 1 {
			candidate = fmt.Sprintf("%s-%d", base, n)
		}
		var found string
		// The table name is interpolated rather than bound: SQLite binds values,
		// not identifiers. Every caller passes one of this file's own constants,
		// never anything from outside the process.
		err := s.db.QueryRow(
			fmt.Sprintf(`SELECT id FROM %s WHERE slug = ? AND id <> ?`, table),
			candidate, exceptID).Scan(&found)
		if err == sql.ErrNoRows {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
	}
}
