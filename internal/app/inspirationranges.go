package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"aimusicmaker/internal/audioanalysis"
)

// ---------------------------------------------------------------------------
// Inspiration ranges and citations
//
// A reference used to be a whole song. A producer rarely means a whole song:
// they mean "1:12–1:48, the pre-chorus build", and the research that describes
// the song (musicanalysis.go) cannot say anything about one section of it. So
// an inspiration carries ranges, each with its own label and its own breakdown
// measured from that section's audio (inspirationbreakdown.go).
//
// Each inspiration and each range has a citation key — [INS-3], [INS-3a] — so
// the album interview and plan can say which reference shaped which choice,
// and the record keeps the citation after the conversation is gone. A key is
// handed out once and never reused, even after its reference is deleted: an
// album made last year that cites [INS-4] must not come to mean a different
// song because the old INS-4 was removed and a new reference took its number.
// ---------------------------------------------------------------------------

// InspirationRange is one section of an inspiration's video.
type InspirationRange struct {
	ID           string  `json:"id"`
	StartSeconds float64 `json:"startSeconds"`
	EndSeconds   float64 `json:"endSeconds"`
	// Label is the producer's name for the section — "the pre-chorus build".
	Label string `json:"label"`
	// CiteKey ("INS-3a") and Citation (the one-line reference) are decided by
	// the store and filled on read; whatever a caller sends is ignored.
	CiteKey  string `json:"citeKey"`
	Citation string `json:"citation"`
	// Breakdown is empty (Source "") until the section is broken down.
	Breakdown RangeBreakdown `json:"breakdown"`
	Position  int            `json:"position"`
	// letter is the stored number behind the key's letter (1 = a), for the
	// save that carries it across.
	letter int
}

// RangeBreakdown is a section's technical breakdown, with what was measured
// and what a model wrote kept in separate fields — a BPM read off the audio
// and a BPM a model remembers are not the same kind of claim, and the
// producer citing one in an album brief should know which they are citing.
type RangeBreakdown struct {
	// Source is "audio" when the section's audio was measured, "research"
	// for the fallback written from the song's research alone, "" for none.
	Source string `json:"source"`
	// ForStart/ForEnd are the times it was made for. A range edited since no
	// longer matches, and its breakdown is dropped rather than left
	// describing a section that is no longer the one cited.
	ForStart float64 `json:"forStart"`
	ForEnd   float64 `json:"forEnd"`
	// Measurements are the numbers taken from the audio; nil for research.
	Measurements *audioanalysis.Measurements `json:"measurements"`
	// Summary is the model's one line — what the briefs carry.
	Summary string `json:"summary"`
	// Interpretation is the model's markdown: instrumentation, arrangement,
	// vocal treatment, production and mix, why it works.
	Interpretation string `json:"interpretation"`
	// AudioError is why the audio could not be measured, for a research-only
	// breakdown. ProseError is why the model's half is missing, when the
	// numbers were kept without it.
	AudioError string `json:"audioError"`
	ProseError string `json:"proseError"`
	At         string `json:"at"`
}

// maxRangeSeconds bounds one range. Long enough for a whole track found in a
// long video (see inspirationtracks.go) — an extended mix or a prog epic runs
// past ten minutes — and short enough that a range is never the whole of an
// hour-long upload. The measurement itself covers the first ten minutes.
const maxRangeSeconds = 1800

// ---------------------------------------------------------------------------
// Times
// ---------------------------------------------------------------------------

var (
	unitTime  = regexp.MustCompile(`^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+(?:\.\d+)?)s?)?$`)
	rangeSeps = regexp.MustCompile(`\s*(?:–|—|-|\bto\b)\s*`)
)

// parseTimestamp reads one point in a video: "72", "72.5", "1:12", "1:02:03",
// "1m12s", or a link carrying t= or start=.
func parseTimestamp(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("a time is needed")
	}
	if strings.Contains(s, "://") {
		if t, ok := linkStart(s); ok {
			return t, nil
		}
		return 0, fmt.Errorf("that link has no t= or start= time in it")
	}
	if strings.Contains(s, ":") {
		parts := strings.Split(s, ":")
		if len(parts) > 3 {
			return 0, fmt.Errorf("%q is not a time — use m:ss or h:mm:ss", s)
		}
		total := 0.0
		for i, p := range parts {
			v, err := strconv.ParseFloat(p, 64)
			if err != nil || v < 0 || p == "" || strings.HasPrefix(p, "+") {
				return 0, fmt.Errorf("%q is not a time — use m:ss or h:mm:ss", s)
			}
			// Every field after the first is a clock field: 1:75 is a typo,
			// not a minute and seventy-five seconds.
			if i > 0 && v >= 60 {
				return 0, fmt.Errorf("%q is not a time — seconds and minutes run to 59", s)
			}
			if i < len(parts)-1 && v != math.Trunc(v) {
				return 0, fmt.Errorf("%q is not a time", s)
			}
			total = total*60 + v
		}
		return total, nil
	}
	m := unitTime.FindStringSubmatch(strings.ToLower(s))
	if m == nil || (m[1] == "" && m[2] == "" && m[3] == "") {
		return 0, fmt.Errorf("%q is not a time — use m:ss or h:mm:ss", s)
	}
	total := 0.0
	if m[1] != "" {
		h, _ := strconv.Atoi(m[1])
		total += float64(h) * 3600
	}
	if m[2] != "" {
		mins, _ := strconv.Atoi(m[2])
		total += float64(mins) * 60
	}
	if m[3] != "" {
		secs, _ := strconv.ParseFloat(m[3], 64)
		total += secs
	}
	return total, nil
}

// linkStart is the start time a YouTube link carries: t= or start= in the
// query, or t= in the fragment (#t=1m12s). Both forms YouTube writes — plain
// seconds and 1m12s — are read.
func linkStart(link string) (float64, bool) {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil {
		return 0, false
	}
	values := []string{u.Query().Get("t"), u.Query().Get("start")}
	if frag, err := url.ParseQuery(u.Fragment); err == nil {
		values = append(values, frag.Get("t"))
	}
	for _, v := range values {
		if v == "" {
			continue
		}
		if t, err := parseTimestamp(v); err == nil {
			return t, true
		}
	}
	return 0, false
}

// RangeTimes is a parsed start and end.
type RangeTimes struct {
	StartSeconds float64 `json:"startSeconds"`
	EndSeconds   float64 `json:"endSeconds"`
}

// ParseRangeTimes reads what the producer typed into a range's two fields
// and checks it against the video's length (0 when not yet known).
//
// start may hold the whole range ("1:12–1:48") with end left empty, and
// either may be a pasted link with a t= in it.
func (a *App) ParseRangeTimes(start, end string, durationSeconds float64) (RangeTimes, error) {
	return parseRangeTimes(start, end, durationSeconds)
}

func parseRangeTimes(start, end string, duration float64) (RangeTimes, error) {
	start, end = strings.TrimSpace(start), strings.TrimSpace(end)
	if end == "" && !strings.Contains(start, "://") {
		if parts := rangeSeps.Split(start, -1); len(parts) == 2 {
			start, end = parts[0], parts[1]
		}
	}
	s, err := parseTimestamp(start)
	if err != nil {
		return RangeTimes{}, fmt.Errorf("start: %w", err)
	}
	if end == "" {
		return RangeTimes{}, fmt.Errorf("the range needs an end time")
	}
	e, err := parseTimestamp(end)
	if err != nil {
		return RangeTimes{}, fmt.Errorf("end: %w", err)
	}
	r := RangeTimes{StartSeconds: roundMillis(s), EndSeconds: roundMillis(e)}
	return r, validateRange(r.StartSeconds, r.EndSeconds, duration)
}

// validateRange is the one rule every route a range comes in by is held to.
func validateRange(start, end, duration float64) error {
	switch {
	case start < 0 || end < 0:
		return fmt.Errorf("a range cannot start before the video does")
	case end <= start:
		return fmt.Errorf("the range ends (%s) before it starts (%s)", formatTimestamp(end), formatTimestamp(start))
	case end-start > maxRangeSeconds:
		return fmt.Errorf("a range is a section — keep it under %d minutes", maxRangeSeconds/60)
	case duration > 0 && end > duration+0.5:
		// Half a second of slack: YouTube reports whole seconds, and a range
		// typed to the last second of a 4:09.4 video is the end of the video.
		return fmt.Errorf("the range ends at %s but the video is only %s long",
			formatTimestamp(end), formatTimestamp(duration))
	}
	return nil
}

// formatTimestamp writes seconds as m:ss or h:mm:ss, with tenths when the
// time has them.
func formatTimestamp(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	whole := int(seconds)
	frac := seconds - float64(whole)
	h, m, s := whole/3600, (whole/60)%60, whole%60
	out := fmt.Sprintf("%d:%02d", m, s)
	if h > 0 {
		out = fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	if tenths := int(math.Round(frac * 10)); tenths > 0 && tenths < 10 {
		out += fmt.Sprintf(".%d", tenths)
	}
	return out
}

func roundMillis(v float64) float64 { return math.Round(v*1000) / 1000 }

// ---------------------------------------------------------------------------
// Citations
// ---------------------------------------------------------------------------

// citeKey is an inspiration's key: INS-3.
func citeKey(n int) string {
	if n <= 0 {
		return ""
	}
	return "INS-" + strconv.Itoa(n)
}

// rangeSuffix is a range's letter under its inspiration: 1 → a, 26 → z,
// 27 → aa — the way spreadsheet columns count, so it never runs out.
func rangeSuffix(n int) string {
	out := ""
	for n > 0 {
		n--
		out = string(rune('a'+n%26)) + out
		n /= 26
	}
	return out
}

// youtubeVideoID is the video a YouTube or YouTube Music link points at.
func youtubeVideoID(link string) string {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	host = strings.TrimPrefix(host, "m.")
	switch host {
	case "youtu.be":
		return strings.Trim(u.Path, "/")
	case "youtube.com", "music.youtube.com":
		if v := u.Query().Get("v"); v != "" {
			return v
		}
		for _, prefix := range []string{"/shorts/", "/embed/", "/live/"} {
			if strings.HasPrefix(u.Path, prefix) {
				return strings.Trim(strings.TrimPrefix(u.Path, prefix), "/")
			}
		}
	}
	return ""
}

// timestampedLink is the link a citation gives: youtube.com, because YouTube
// Music ignores t= and a citation that opens at 0:00 is not citing the range.
func timestampedLink(link string, start float64) string {
	id := youtubeVideoID(link)
	if id == "" {
		return strings.TrimSpace(link)
	}
	out := "https://www.youtube.com/watch?v=" + url.QueryEscape(id)
	if start >= 1 {
		out += "&t=" + strconv.Itoa(int(start)) + "s"
	}
	return out
}

// songName is how a citation names the song: quoted title, then performer.
func songName(i Inspiration) string {
	title := strings.TrimSpace(i.Title)
	if title == "" {
		title = "Untitled reference"
	}
	out := "“" + title + "”"
	if artist := strings.TrimSpace(i.Artist); artist != "" {
		out += " — " + artist
	}
	return out
}

// inspirationCitation is one line: key, song, release, link.
func inspirationCitation(i Inspiration) string {
	if i.CiteKey == "" {
		return ""
	}
	line := "[" + i.CiteKey + "] " + songName(i)
	if album := strings.TrimSpace(i.Album); album != "" {
		line += " (" + album + ")"
	}
	if link := timestampedLink(i.URL, 0); link != "" {
		line += " — " + link
	}
	return line
}

// rangeCitation is one line: key, song, range, label, timestamped link.
func rangeCitation(i Inspiration, r InspirationRange) string {
	if r.CiteKey == "" {
		return ""
	}
	line := "[" + r.CiteKey + "] " + songName(i) + ", " +
		formatTimestamp(r.StartSeconds) + "–" + formatTimestamp(r.EndSeconds)
	if label := strings.TrimSpace(r.Label); label != "" {
		line += " “" + label + "”"
	}
	if link := timestampedLink(i.URL, r.StartSeconds); link != "" {
		line += " — " + link
	}
	return line
}

// fillCitations sets every key's one-line citation on an artist's references.
func fillCitations(inspirations []Inspiration) {
	for n := range inspirations {
		insp := &inspirations[n]
		insp.Citation = inspirationCitation(*insp)
		for k := range insp.Ranges {
			insp.Ranges[k].Citation = rangeCitation(*insp, insp.Ranges[k])
		}
	}
}

// citationKeyPattern is a key as a model writes it, bracketed or not.
var citationKeyPattern = regexp.MustCompile(`\bINS-\d+[a-z]*\b`)

// resolveCitations turns the keys a model cited into the artist's citation
// lines, in the order given, dropping keys the artist does not have — a model
// that invents [INS-9] has cited nothing, and the record should not say it did.
func resolveCitations(artist Artist, keys []string) []string {
	lines := map[string]string{}
	for _, insp := range artist.Inspirations {
		if insp.CiteKey != "" {
			lines[insp.CiteKey] = inspirationCitation(insp)
		}
		for _, r := range insp.Ranges {
			if r.CiteKey != "" {
				lines[r.CiteKey] = rangeCitation(insp, r)
			}
		}
	}
	out := []string{}
	seen := map[string]bool{}
	for _, raw := range keys {
		for _, key := range citationKeyPattern.FindAllString(raw, -1) {
			if line, ok := lines[key]; ok && !seen[key] {
				seen[key] = true
				out = append(out, line)
			}
		}
	}
	return out
}

// citationNotes is the block a record keeps its citations in.
func citationNotes(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return "Inspirations cited:\n- " + strings.Join(lines, "\n- ")
}

// measurementLine is a breakdown's numbers on one line, for the briefs.
func measurementLine(m *audioanalysis.Measurements) string {
	if m == nil {
		return ""
	}
	parts := []string{}
	if m.TempoBPM > 0 {
		p := fmt.Sprintf("%.1f BPM", m.TempoBPM)
		if m.TempoAlternate > 0 {
			p += fmt.Sprintf(" (or %.1f, the other octave)", m.TempoAlternate)
		}
		if m.TempoConfidence < 0.3 {
			p += " (weak pulse)"
		}
		parts = append(parts, p)
	}
	if m.Key != "" {
		p := m.Key
		if m.KeyConfidence < 0.1 && m.KeyRunnerUp != "" {
			p += " (ambiguous with " + m.KeyRunnerUp + ")"
		}
		parts = append(parts, p)
	}
	if m.LoudnessMeasured {
		parts = append(parts, fmt.Sprintf("%.1f LUFS, LRA %.1f LU, true peak %.1f dBTP",
			m.IntegratedLUFS, m.LoudnessRangeLU, m.TruePeakDBTP))
	}
	parts = append(parts, fmt.Sprintf("crest %.1f dB", m.CrestDB))
	if m.SpectralCentroidHz > 0 {
		parts = append(parts, fmt.Sprintf("centroid %.0f Hz", m.SpectralCentroidHz))
	}
	if len(m.Bands) > 0 {
		shares := []string{}
		for _, b := range m.Bands {
			shares = append(shares, fmt.Sprintf("%s %.0f%%", b.Name, b.Percent))
		}
		parts = append(parts, "energy "+strings.Join(shares, "/"))
	}
	parts = append(parts, fmt.Sprintf("stereo width %.2f (corr %.2f)", m.StereoWidth, m.StereoCorrelation))
	parts = append(parts, fmt.Sprintf("%.0f s", m.Seconds))
	return strings.Join(parts, " · ")
}

// ---------------------------------------------------------------------------
// Storage
// ---------------------------------------------------------------------------

// storedRange is what saveArtist needs to know about a range already on disk.
type storedRange struct {
	citeNo    int
	breakdown RangeBreakdown
}

// storedInspiration is the same for an inspiration.
type storedInspiration struct {
	citeNo   int
	rangeSeq int
	duration float64
	ranges   []InspirationRange
}

// rangesFor loads every range of an artist's inspirations, keyed by
// inspiration, in order.
func rangesFor(q interface {
	Query(string, ...any) (*sql.Rows, error)
}, artistID string) (map[string][]InspirationRange, error) {
	out := map[string][]InspirationRange{}
	rows, err := q.Query(
		`SELECT r.id, r.inspiration_id, r.start_ms, r.end_ms, r.label, r.cite_no, r.breakdown,
		        r.position, i.cite_no
		 FROM inspiration_ranges r
		 JOIN (SELECT a.id, CASE WHEN a.cite_no > 0 THEN a.cite_no ELSE COALESCE(k.cite_no, 0) END AS cite_no
		       FROM artist_inspirations a LEFT JOIN inspiration_keys k ON k.inspiration_id = a.id) i
		   ON i.id = r.inspiration_id
		 WHERE r.artist_id = ? ORDER BY r.position, r.at`, artistID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			r                  InspirationRange
			inspirationID, raw string
			startMS, endMS     int64
			citeNo, inspCiteNo int
		)
		if err := rows.Scan(&r.ID, &inspirationID, &startMS, &endMS, &r.Label, &citeNo, &raw,
			&r.Position, &inspCiteNo); err != nil {
			return out, err
		}
		r.StartSeconds, r.EndSeconds = float64(startMS)/1000, float64(endMS)/1000
		r.letter = citeNo
		if inspCiteNo > 0 && citeNo > 0 {
			r.CiteKey = citeKey(inspCiteNo) + rangeSuffix(citeNo)
		}
		if strings.TrimSpace(raw) != "" {
			// A breakdown this build cannot read is shown as none rather than
			// failing the whole artist: the numbers can be measured again.
			_ = json.Unmarshal([]byte(raw), &r.Breakdown)
		}
		out[inspirationID] = append(out[inspirationID], r)
	}
	return out, rows.Err()
}

// keepBreakdown decides which breakdown a saved range carries: the one sent,
// else the one stored — but only one made for these exact times. The window
// saves its draft every pause in typing, and a breakdown that landed while a
// save was in flight must not be erased by a draft that predates it; an MCP
// client that never heard of breakdowns must not erase them either.
func keepBreakdown(r InspirationRange, stored *storedRange) RangeBreakdown {
	matches := func(b RangeBreakdown) bool {
		return b.Source != "" && math.Abs(b.ForStart-r.StartSeconds) < 0.01 &&
			math.Abs(b.ForEnd-r.EndSeconds) < 0.01
	}
	if matches(r.Breakdown) {
		return r.Breakdown
	}
	if stored != nil && matches(stored.breakdown) {
		return stored.breakdown
	}
	return RangeBreakdown{}
}

// setRangeBreakdown writes one range's breakdown, but only while the range
// still has the times it was made for — the producer may have moved it while
// the minutes of fetching and writing were under way.
func (s *Store) setRangeBreakdown(rangeID string, b RangeBreakdown) (bool, error) {
	if s == nil || s.db == nil {
		return false, errNoStore
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return false, err
	}
	res, err := s.db.Exec(
		`UPDATE inspiration_ranges SET breakdown = ? WHERE id = ? AND start_ms = ? AND end_ms = ?`,
		string(raw), rangeID, int64(math.Round(b.ForStart*1000)), int64(math.Round(b.ForEnd*1000)))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// fillInspirationIdentity writes what research found about a song onto its
// reference — only into fields that are empty, so research never overwrites
// what the producer typed.
func (s *Store) fillInspirationIdentity(id string, found MusicAnalysis, duration float64) error {
	if s == nil || s.db == nil {
		return errNoStore
	}
	_, err := s.db.Exec(
		`UPDATE artist_inspirations SET
		   title       = CASE WHEN TRIM(title) = '' THEN ? ELSE title END,
		   artist      = CASE WHEN TRIM(artist) = '' THEN ? ELSE artist END,
		   album       = CASE WHEN TRIM(album) = '' THEN ? ELSE album END,
		   artwork_url = CASE WHEN TRIM(artwork_url) = '' THEN ? ELSE artwork_url END,
		   note        = CASE WHEN TRIM(note) = '' THEN ? ELSE note END,
		   duration_seconds = CASE WHEN ? > 0 THEN ? ELSE duration_seconds END
		 WHERE id = ?`,
		found.Title, found.Artist, found.Album, found.ArtworkURL, found.Style,
		duration, duration, id)
	return err
}

// keepKey records an inspiration's numbers in inspiration_keys.
func keepKey(tx *sql.Tx, inspirationID, artistID string, citeNo, rangeSeq int) error {
	_, err := tx.Exec(
		`INSERT INTO inspiration_keys (inspiration_id, artist_id, cite_no, range_seq) VALUES (?, ?, ?, ?)
		 ON CONFLICT(inspiration_id) DO UPDATE SET cite_no = excluded.cite_no,
		   range_seq = MAX(inspiration_keys.range_seq, excluded.range_seq)`,
		inspirationID, artistID, citeNo, rangeSeq)
	return err
}

// backfillCitations gives every inspiration that has no key one, per artist in
// the order the producer listed them, and moves each artist's counter past
// them. It runs on every launch and does nothing once every row has a key —
// which is what makes it the migration for databases from before keys
// existed without being a step that could run twice.
func (s *Store) backfillCitations() error {
	// Numbers an older build's save dropped come back from inspiration_keys
	// first; only a reference that never had one gets a new number.
	if _, err := s.db.Exec(
		`UPDATE artist_inspirations SET
		   cite_no = (SELECT k.cite_no FROM inspiration_keys k WHERE k.inspiration_id = artist_inspirations.id),
		   range_seq = MAX(range_seq, (SELECT k.range_seq FROM inspiration_keys k WHERE k.inspiration_id = artist_inspirations.id))
		 WHERE cite_no = 0 AND EXISTS (
		   SELECT 1 FROM inspiration_keys k WHERE k.inspiration_id = artist_inspirations.id AND k.cite_no > 0)`); err != nil {
		return err
	}
	// And every number already on a row is mirrored there, so one handed out
	// before the table existed is protected too.
	if _, err := s.db.Exec(
		`INSERT OR IGNORE INTO inspiration_keys (inspiration_id, artist_id, cite_no, range_seq)
		 SELECT id, artist_id, cite_no, range_seq FROM artist_inspirations WHERE cite_no > 0`); err != nil {
		return err
	}
	rows, err := s.db.Query(
		`SELECT id, artist_id FROM artist_inspirations WHERE cite_no = 0 ORDER BY artist_id, position, at`)
	if err != nil {
		return err
	}
	type pending struct{ id, artistID string }
	var todo []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.artistID); err != nil {
			rows.Close()
			return err
		}
		todo = append(todo, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(todo) == 0 {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, p := range todo {
		var seq, highest, kept int
		_ = tx.QueryRow(`SELECT cite_seq FROM artists WHERE id = ?`, p.artistID).Scan(&seq)
		_ = tx.QueryRow(`SELECT COALESCE(MAX(cite_no), 0) FROM artist_inspirations WHERE artist_id = ?`,
			p.artistID).Scan(&highest)
		_ = tx.QueryRow(`SELECT COALESCE(MAX(cite_no), 0) FROM inspiration_keys WHERE artist_id = ?`,
			p.artistID).Scan(&kept)
		seq = max(seq, highest, kept) + 1
		if _, err := tx.Exec(`UPDATE artist_inspirations SET cite_no = ? WHERE id = ?`, seq, p.id); err != nil {
			return err
		}
		if err := keepKey(tx, p.id, p.artistID, seq, 0); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE artists SET cite_seq = ? WHERE id = ?`, seq, p.artistID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
