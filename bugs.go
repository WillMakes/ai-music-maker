package main

import (
	"database/sql"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// The bug queue
//
// A button in the top bar, on every page. Pressing it opens a box, the producer
// says what is wrong, and a report exists — with the route they were on and,
// where the page was about one, the record it was showing.
//
// The queue is the repository's issue list (see bugissues.go). Filing a report
// opens an issue; resolving one closes it; an issue written on github.com the
// ordinary way is in this queue within the minute. This table is a local
// projection of that list, and it exists for two reasons that are not
// compromises about where the truth lives:
//
//   - the bug button must go on working with no remote, no credential and no
//     network. A producer who found a bug offline should not have to remember it;
//   - the window watches the queue, and a poll that went to the API would spend
//     a rate limit on it.
//
// Every report also gets a counterpart in Jax (see jax.go), because that is
// where the producer watches the queue from. None of that can fail a filing: a
// report refused because a tracker was closed is a report lost, which is
// strictly worse than an unlinked one.
// ---------------------------------------------------------------------------

// BugReport is one open report.
type BugReport struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	// Route is the view the producer was on, as the frontend names it.
	Route string `json:"route"`
	// Global marks a report about the app rather than about the page it was
	// filed from — the difference between "this button is broken" and "the app
	// will not start".
	Global bool `json:"global"`
	// SubjectKind and SubjectID are what the page was showing: "artist",
	// "album", "track" and its id. Kept so a report can be reproduced against the
	// record that provoked it.
	SubjectKind string `json:"subjectKind"`
	SubjectID   string `json:"subjectId"`
	// SubjectName is joined at read time rather than copied onto the row, so a
	// renamed record is renamed on every report about it and a deleted one stops
	// being offered as the example.
	SubjectName string `json:"subjectName"`
	// CheckedOut marks a report an agent has claimed, so two do not work the same
	// one. A flag rather than a name: this queue has one producer and the useful
	// question is "is somebody on it", not "who".
	CheckedOut bool `json:"checkedOut"`
	// IssueURL and IssueNumber are the report's issue. 0 means the forge was
	// unreachable when it was filed; the next sweep gives it one.
	IssueURL    string `json:"issueUrl"`
	IssueNumber int    `json:"issueNumber"`
	// JaxReportID is the counterpart in Jax; 0 means it has not been filed there.
	JaxReportID int `json:"jaxReportId"`
	// JaxNote is what Jax said when it declined to record a claim or a resolve
	// (#15) — that the report is not the oldest workable one in its queue, that
	// somebody else holds it. Empty when Jax agreed, and empty when Jax was
	// closed, which is not a refusal.
	//
	// A note rather than an error: a refusal is a fact about Jax's queue and
	// never stops the work here (see the top of jax.go). What it must not do is
	// go unmentioned, which is how the two queues came to disagree in silence.
	JaxNote   string `json:"jaxNote"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// ResolvedReport is one that was fixed, kept for good.
type ResolvedReport struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Route       string `json:"route"`
	Global      bool   `json:"global"`
	IssueURL    string `json:"issueUrl"`
	IssueNumber int    `json:"issueNumber"`
	JaxReportID int    `json:"jaxReportId"`
	JaxNote     string `json:"jaxNote"`
	CreatedAt   string `json:"createdAt"`
	ResolvedAt  string `json:"resolvedAt"`
	// Seen marks a fix the producer has been told about. The window shows an
	// unseen fix once, as a notice, then never again — which is what makes the
	// notice worth reading.
	Seen      bool   `json:"seen"`
	CommitSHA string `json:"commitSha"`
	CommitURL string `json:"commitUrl"`
}

// BugCounts is the top bar's badge: how many are open, and how many fixes the
// producer has not been shown yet.
type BugCounts struct {
	Open     int `json:"open"`
	Resolved int `json:"resolved"`
	Unseen   int `json:"unseen"`
}

// FileBugReport files a report, opens its issue and files it into Jax.
//
// The local row is written first and unconditionally. Everything after it is
// best effort: a forge that will not answer or a Jax that is closed leaves the
// report here with issueNumber 0 or jaxReportId 0, and the sweep catches it up.
// The producer pressed a button that says "report a bug" and the report must
// exist when they let go of it.
func (a *App) FileBugReport(report BugReport) (BugReport, error) {
	if a.store == nil {
		return BugReport{}, errNoStore
	}
	report.Description = strings.TrimSpace(report.Description)
	if report.Description == "" {
		return BugReport{}, fmt.Errorf("say what is wrong")
	}
	report.ID = newID("bug")
	report.CreatedAt = nowStamp()
	report.UpdatedAt = report.CreatedAt
	if err := a.store.saveBugReport(report); err != nil {
		return BugReport{}, err
	}

	report = a.linkToIssue(report)
	report = a.linkToJax(report)

	a.store.changed(scopeBugReports)
	return a.store.bugReport(report.ID)
}

// ListBugReports reports the open queue, oldest first — the working order, and
// the order that decides which report an agent may claim.
func (a *App) ListBugReports() ([]BugReport, error) {
	if a.store == nil {
		return []BugReport{}, nil
	}
	// Reconciled against the forge before answering, so an issue written on
	// github.com is in the queue and one closed there has left it. Best effort:
	// no network means the local projection, which is the whole reason it exists.
	a.reconcileIssues()
	return a.store.bugReports()
}

// ListResolvedReports reports what has been fixed, newest first.
func (a *App) ListResolvedReports() ([]ResolvedReport, error) {
	if a.store == nil {
		return []ResolvedReport{}, nil
	}
	return a.store.resolvedReports()
}

// GetBugCounts is the badge's read. Deliberately not ListBugReports().length:
// the badge is polled and the list is not, and counting in SQL is what keeps the
// poll from carrying every report's description with it.
func (a *App) GetBugCounts() (BugCounts, error) {
	if a.store == nil {
		return BugCounts{}, nil
	}
	return a.store.bugCounts()
}

// UpdateBugReport edits a report's own fields.
func (a *App) UpdateBugReport(report BugReport) (BugReport, error) {
	if a.store == nil {
		return BugReport{}, errNoStore
	}
	existing, err := a.store.bugReport(report.ID)
	if err != nil {
		return BugReport{}, err
	}
	existing.Description = strings.TrimSpace(report.Description)
	existing.Route = report.Route
	existing.Global = report.Global
	existing.UpdatedAt = nowStamp()
	if err := a.store.saveBugReport(existing); err != nil {
		return BugReport{}, err
	}
	// The issue carries the description, so an edited report has to correct it —
	// otherwise the queue and the issue list say two different things about the
	// same bug.
	a.writeIssue(existing)
	a.store.changed(scopeBugReports)
	return a.store.bugReport(existing.ID)
}

// ClaimBugReport marks a report as being worked, here, on the issue and in Jax.
func (a *App) ClaimBugReport(id string) (BugReport, error) {
	if a.store == nil {
		return BugReport{}, errNoStore
	}
	if err := a.store.setBugCheckedOut(id, true); err != nil {
		return BugReport{}, err
	}
	report, err := a.store.bugReport(id)
	if err != nil {
		return BugReport{}, err
	}
	a.markIssueClaimed(report, true)
	// What Jax said about the claim, kept on the record (#15). A refusal does not
	// undo the claim — see claimInJax — but it must be visible, or this queue and
	// Jax's disagree and nothing says so.
	a.noteFromJax(&report, a.claimInJax(report))
	a.store.changed(scopeBugReports)
	return report, nil
}

// noteFromJax records what Jax said, or clears a note left by an earlier one.
//
// Cleared on success as well as set on refusal: a report claimed once while Jax
// was mid-sweep and again a minute later would otherwise carry a warning about a
// refusal that no longer applies. Best effort — a note that could not be written
// down is not worth failing the claim that provoked it.
func (a *App) noteFromJax(report *BugReport, note string) {
	if report.JaxNote == note {
		return
	}
	report.JaxNote = note
	report.UpdatedAt = nowStamp()
	_ = a.store.saveBugReport(*report)
}

// ReleaseBugReport puts a claimed report back.
func (a *App) ReleaseBugReport(id string) (BugReport, error) {
	if a.store == nil {
		return BugReport{}, errNoStore
	}
	if err := a.store.setBugCheckedOut(id, false); err != nil {
		return BugReport{}, err
	}
	report, err := a.store.bugReport(id)
	if err != nil {
		return BugReport{}, err
	}
	a.markIssueClaimed(report, false)
	// Releasing clears any refusal note: it was about a claim that no longer
	// stands, and leaving it would have the next reader chasing a stale warning.
	a.noteFromJax(&report, "")
	a.store.changed(scopeBugReports)
	return report, nil
}

// ResolveBugReport moves a report to the history, closes its issue and tells Jax.
//
// commitSHA and commitURL are where the fix landed. Both stored rather than the
// URL built from the SHA when the list is drawn: this is history, and a
// repository that moves forge should not rewrite where a fix from two years ago
// was made.
func (a *App) ResolveBugReport(id, commitSHA, commitURL string) (ResolvedReport, error) {
	if a.store == nil {
		return ResolvedReport{}, errNoStore
	}
	report, err := a.store.bugReport(id)
	if err != nil {
		return ResolvedReport{}, err
	}
	resolved, err := a.retireReport(report, commitSHA, commitURL)
	if err != nil {
		return ResolvedReport{}, err
	}
	// The forge is this caller's own step. The sweep's other caller is resolving
	// *because* the issue is already closed, so it must not do this.
	a.closeIssue(report, commitURL)
	a.store.changed(scopeBugReports)
	a.store.changed(scopeResolvedReports)
	return resolved, nil
}

// retireReport moves a report to the history and tells Jax, in that order.
//
// The one place a report is retired (#16). ResolveBugReport had these two steps
// written out and the reconcile sweep called the store method on its own, so
// closing an issue on github.com — which the workflow presents as a way to work
// the queue — cleared the report here and left the Jax counterpart open for ever.
// Those stranded reports then sat at the head of Jax's queue, and because Jax
// enforces oldest-first within an application's queue, they refused every later
// check-out too.
//
// Jax first, so what it says can be written onto the history row rather than onto
// a queue row that is about to be deleted. A refusal is kept, not obeyed: the fix
// is on main either way (see claimInJax).
//
// What each caller does about the *forge* stays with the caller, because they
// want opposite things — one is closing the issue, the other is reacting to it
// having been closed.
func (a *App) retireReport(report BugReport, commitSHA, commitURL string) (ResolvedReport, error) {
	report.JaxNote = a.resolveInJax(report)
	return a.store.resolveBugReport(report, commitSHA, commitURL)
}

// MarkFixesSeen clears the unseen badge, having shown the producer the notice.
func (a *App) MarkFixesSeen() error {
	if a.store == nil {
		return errNoStore
	}
	if err := a.store.markFixesSeen(); err != nil {
		return err
	}
	a.store.changed(scopeResolvedReports)
	return nil
}

// SearchBugReports finds open reports by description, for an agent that has a
// symptom rather than an id.
func (a *App) SearchBugReports(query string) ([]BugReport, error) {
	if a.store == nil {
		return []BugReport{}, nil
	}
	return a.store.searchBugReports(query)
}

// ---------------------------------------------------------------------------
// Storage
// ---------------------------------------------------------------------------

// bugColumns joins the subject's name, which is why every read here is a query
// against three tables rather than one. The alternative — a name column on the
// report — goes stale the first time somebody renames an artist.
const bugColumns = `b.id, b.description, b.route, b.global, b.subject_kind,
	b.subject_id, b.checked_out, b.issue_url, b.issue_number, b.jax_report_id,
	b.jax_note, b.created_at, b.updated_at,
	COALESCE((SELECT name FROM artists WHERE id = b.subject_id),
	         (SELECT name FROM albums  WHERE id = b.subject_id),
	         (SELECT name FROM tracks  WHERE id = b.subject_id), '')`

func scanBugReport(scan func(...any) error) (BugReport, error) {
	var r BugReport
	err := scan(&r.ID, &r.Description, &r.Route, &r.Global, &r.SubjectKind,
		&r.SubjectID, &r.CheckedOut, &r.IssueURL, &r.IssueNumber, &r.JaxReportID,
		&r.JaxNote, &r.CreatedAt, &r.UpdatedAt, &r.SubjectName)
	return r, err
}

func (s *Store) bugReports() ([]BugReport, error) {
	out := []BugReport{}
	if s == nil || s.db == nil {
		return out, nil
	}
	rows, err := s.db.Query(
		`SELECT ` + bugColumns + ` FROM bug_reports b ORDER BY b.created_at`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		r, err := scanBugReport(rows.Scan)
		if err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) searchBugReports(query string) ([]BugReport, error) {
	out := []BugReport{}
	if s == nil || s.db == nil {
		return out, nil
	}
	rows, err := s.db.Query(
		`SELECT `+bugColumns+` FROM bug_reports b
		 WHERE b.description LIKE ? ORDER BY b.created_at`,
		"%"+strings.TrimSpace(query)+"%")
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		r, err := scanBugReport(rows.Scan)
		if err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) bugReport(id string) (BugReport, error) {
	if s == nil || s.db == nil {
		return BugReport{}, errNoStore
	}
	r, err := scanBugReport(s.db.QueryRow(
		`SELECT `+bugColumns+` FROM bug_reports b WHERE b.id = ?`, id).Scan)
	if err == sql.ErrNoRows {
		return BugReport{}, fmt.Errorf("no report %s", id)
	}
	return r, err
}

func (s *Store) saveBugReport(r BugReport) error {
	if s == nil || s.db == nil {
		return errNoStore
	}
	if r.UpdatedAt == "" {
		r.UpdatedAt = nowStamp()
	}
	_, err := s.db.Exec(
		`INSERT INTO bug_reports (id, description, route, global, subject_kind,
		   subject_id, checked_out, issue_url, issue_number, jax_report_id, jax_note,
		   created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   description = excluded.description, route = excluded.route,
		   global = excluded.global, subject_kind = excluded.subject_kind,
		   subject_id = excluded.subject_id, checked_out = excluded.checked_out,
		   issue_url = excluded.issue_url, issue_number = excluded.issue_number,
		   jax_report_id = excluded.jax_report_id, jax_note = excluded.jax_note,
		   updated_at = excluded.updated_at`,
		r.ID, r.Description, r.Route, r.Global, r.SubjectKind, r.SubjectID,
		r.CheckedOut, r.IssueURL, r.IssueNumber, r.JaxReportID, r.JaxNote,
		r.CreatedAt, r.UpdatedAt)
	return err
}

func (s *Store) setBugCheckedOut(id string, claimed bool) error {
	if s == nil || s.db == nil {
		return errNoStore
	}
	_, err := s.db.Exec(
		`UPDATE bug_reports SET checked_out = ?, updated_at = ? WHERE id = ?`,
		claimed, nowStamp(), id)
	return err
}

func (s *Store) resolvedReports() ([]ResolvedReport, error) {
	out := []ResolvedReport{}
	if s == nil || s.db == nil {
		return out, nil
	}
	rows, err := s.db.Query(
		`SELECT id, description, route, global, issue_url, issue_number,
		        jax_report_id, jax_note, created_at, resolved_at, seen,
		        commit_sha, commit_url
		 FROM resolved_reports ORDER BY resolved_at DESC`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var r ResolvedReport
		if err := rows.Scan(&r.ID, &r.Description, &r.Route, &r.Global, &r.IssueURL,
			&r.IssueNumber, &r.JaxReportID, &r.JaxNote, &r.CreatedAt, &r.ResolvedAt,
			&r.Seen, &r.CommitSHA, &r.CommitURL); err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// resolveBugReport moves one row between the two tables in a single
// transaction. Two statements rather than a status flag, so the open queue stays
// short enough to read at a glance — and in one transaction because a report
// that appeared in both lists, or in neither, would be worse than either.
func (s *Store) resolveBugReport(r BugReport, sha, url string) (ResolvedReport, error) {
	if s == nil || s.db == nil {
		return ResolvedReport{}, errNoStore
	}
	tx, err := s.db.Begin()
	if err != nil {
		return ResolvedReport{}, err
	}
	defer tx.Rollback()

	resolved := ResolvedReport{
		ID: r.ID, Description: r.Description, Route: r.Route, Global: r.Global,
		IssueURL: r.IssueURL, IssueNumber: r.IssueNumber, JaxReportID: r.JaxReportID,
		JaxNote: r.JaxNote, CreatedAt: r.CreatedAt, ResolvedAt: nowStamp(),
		CommitSHA: strings.TrimSpace(sha), CommitURL: strings.TrimSpace(url),
	}
	if _, err := tx.Exec(
		`INSERT INTO resolved_reports (id, description, route, global, issue_url,
		   issue_number, jax_report_id, jax_note, created_at, resolved_at, seen,
		   commit_sha, commit_url)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   resolved_at = excluded.resolved_at, jax_note = excluded.jax_note,
		   commit_sha = excluded.commit_sha, commit_url = excluded.commit_url`,
		resolved.ID, resolved.Description, resolved.Route, resolved.Global,
		resolved.IssueURL, resolved.IssueNumber, resolved.JaxReportID,
		resolved.JaxNote, resolved.CreatedAt, resolved.ResolvedAt,
		resolved.CommitSHA, resolved.CommitURL); err != nil {
		return ResolvedReport{}, err
	}
	if _, err := tx.Exec(`DELETE FROM bug_reports WHERE id = ?`, r.ID); err != nil {
		return ResolvedReport{}, err
	}
	if err := tx.Commit(); err != nil {
		return ResolvedReport{}, err
	}
	return resolved, nil
}

func (s *Store) markFixesSeen() error {
	if s == nil || s.db == nil {
		return errNoStore
	}
	_, err := s.db.Exec(`UPDATE resolved_reports SET seen = 1 WHERE seen = 0`)
	return err
}

func (s *Store) bugCounts() (BugCounts, error) {
	var counts BugCounts
	if s == nil || s.db == nil {
		return counts, nil
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM bug_reports`).Scan(&counts.Open); err != nil {
		return counts, err
	}
	if err := s.db.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(CASE WHEN seen = 0 THEN 1 ELSE 0 END), 0)
		 FROM resolved_reports`).Scan(&counts.Resolved, &counts.Unseen); err != nil {
		return counts, err
	}
	return counts, nil
}
