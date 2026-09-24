package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// The reconcile sweep, against a stub forge
//
// These are the tests newTestApp deliberately makes impossible everywhere else:
// they need an issue queue, so they put one up themselves rather than reaching
// the real one. githubAPI and githubToken are both variables for exactly this,
// and the stub is a real http.Server so the code under test makes real requests
// through its own client.
// ---------------------------------------------------------------------------

// stubForge is a GitHub stand-in holding a handful of issues.
type stubForge struct {
	// issues by number. state is "open" or "closed".
	issues map[int]githubIssue
	// hideFromList is the issue the *list* endpoint pretends not to have while
	// the single-issue endpoint still reports it. That is the whole of the bug
	// this file exists for: a real list is eventually consistent and lags a
	// just-created issue by a moment.
	hideFromList int
	// listCalls and issueCalls count what was asked, so a test can assert the
	// direct read happened at all; patches counts writes, so a test can assert
	// one did not happen.
	listCalls  int
	issueCalls int
	patches    int
}

func (f *stubForge) start(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if r.Method == http.MethodPatch {
			f.patches++
		}
		switch {
		// The label-creation calls ensureLabels makes are best effort and
		// unchecked; answered so they do not look like failures in a trace.
		case strings.HasSuffix(path, "/labels") && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("{}"))

		case strings.HasSuffix(path, "/issues") && r.Method == http.MethodGet:
			f.listCalls++
			out := []githubIssue{}
			for number, issue := range f.issues {
				if issue.State == "open" && number != f.hideFromList {
					out = append(out, issue)
				}
			}
			_ = json.NewEncoder(w).Encode(out)

		case strings.HasSuffix(path, "/issues") && r.Method == http.MethodPost:
			number := len(f.issues) + 100
			created := githubIssue{
				Number: number,
				URL:    fmt.Sprintf("https://example.test/issues/%d", number),
				State:  "open",
			}
			f.issues[number] = created
			_ = json.NewEncoder(w).Encode(created)

		default:
			// /repos/<slug>/issues/<number>
			var number int
			if _, err := fmt.Sscanf(path[strings.LastIndex(path, "/")+1:], "%d", &number); err != nil {
				http.NotFound(w, r)
				return
			}
			f.issueCalls++
			issue, ok := f.issues[number]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(issue)
		}
	}))
	t.Cleanup(server.Close)

	previousAPI, previousToken := githubAPI, githubToken
	githubAPI = server.URL
	githubToken = func() string { return "stub-token" }
	t.Cleanup(func() { githubAPI, githubToken = previousAPI, previousToken })
	standInAppCheckout(t, "https://github.com/WillMakes/suno-maker.git")
}

// standInAppCheckout makes the process look as if it runs in a checkout of
// SunoMaker whose origin is remote. These tests used to get a repository by
// running inside the real checkout, which is the dependence on the working
// directory #94 removed; now they say where they stand.
func standInAppCheckout(t *testing.T, remote string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(appModule+"\n\ngo 1.25.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	previousDir, previousCheckout := workingDir, checkoutAt
	workingDir = func() (string, error) { return root, nil }
	checkoutAt = func(string) (string, string) { return root, remote }
	t.Cleanup(func() { workingDir, checkoutAt = previousDir, previousCheckout })
	return root
}

// allowReconcile clears the rate limit, which is package-level state and would
// otherwise let one test's sweep suppress the next one's.
func allowReconcile(t *testing.T) {
	t.Helper()
	previous := lastReconcile
	lastReconcile = time.Time{}
	t.Cleanup(func() { lastReconcile = previous })
}

// TestAJustFiledReportIsNotResolvedByALaggingList is #11.
//
// The sweep used to read "not in the open list" as "closed on the forge", so a
// report whose issue was created a second ago — and therefore had not reached
// the list yet — was resolved at the instant it was filed.
func TestAJustFiledReportIsNotResolvedByALaggingList(t *testing.T) {
	app := newTestApp(t)
	forge := &stubForge{issues: map[int]githubIssue{
		10: {Number: 10, URL: "https://example.test/issues/10", State: "open"},
	}}
	// The issue is open, and the list has not caught up with it.
	forge.hideFromList = 10
	forge.start(t)
	allowReconcile(t)

	report := BugReport{
		ID: "bug_local", Description: "Filed a moment ago.", Route: "artists",
		IssueNumber: 10, IssueURL: "https://example.test/issues/10",
		CreatedAt: nowStamp(), UpdatedAt: nowStamp(),
	}
	if err := app.store.saveBugReport(report); err != nil {
		t.Fatalf("save: %v", err)
	}

	open, err := app.ListBugReports()
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if forge.issueCalls == 0 {
		t.Error("the sweep never read the issue — it is still trusting the list alone")
	}
	if len(open) != 1 || open[0].ID != "bug_local" {
		t.Fatalf("the queue holds %+v, want the report still open under its own id",
			idsOf(open))
	}
	resolved, _ := app.ListResolvedReports()
	if len(resolved) != 0 {
		t.Errorf("%d reports were resolved by a list that had not caught up", len(resolved))
	}
}

// TestAClosedIssueStillResolvesItsReport is the other half: closing an issue on
// github.com has to remain a way to work the queue.
func TestAClosedIssueStillResolvesItsReport(t *testing.T) {
	app := newTestApp(t)
	forge := &stubForge{issues: map[int]githubIssue{
		10: {Number: 10, URL: "https://example.test/issues/10", State: "closed"},
	}}
	forge.start(t)
	allowReconcile(t)

	if err := app.store.saveBugReport(BugReport{
		ID: "bug_local", Description: "Fixed by somebody on the forge.",
		IssueNumber: 10, CreatedAt: nowStamp(),
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	open, err := app.ListBugReports()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(open) != 0 {
		t.Errorf("the queue still holds %v after its issue was closed", idsOf(open))
	}
	resolved, _ := app.ListResolvedReports()
	if len(resolved) != 1 || resolved[0].ID != "bug_local" {
		t.Errorf("resolved %v, want the report itself", resolved)
	}
}

// TestAnUnreachableIssueLeavesTheReportAlone: a read that fails says nothing
// about whether the issue is closed, so the report stays where it is and the
// next sweep can correct it.
func TestAnUnreachableIssueLeavesTheReportAlone(t *testing.T) {
	app := newTestApp(t)
	// No issue 10 at all, so the direct read 404s.
	forge := &stubForge{issues: map[int]githubIssue{}}
	forge.start(t)
	allowReconcile(t)

	if err := app.store.saveBugReport(BugReport{
		ID: "bug_local", Description: "Its issue cannot be read.",
		IssueNumber: 10, CreatedAt: nowStamp(),
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	open, err := app.ListBugReports()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(open) != 1 {
		t.Errorf("the report was resolved on the strength of a failed read")
	}
}

// TestAnIssueWrittenOnTheForgeJoinsTheQueue is the adoption half of the sweep,
// which the fix must not have broken: a queue you could only add to from inside
// the app would be a local queue with extra steps.
func TestAnIssueWrittenOnTheForgeJoinsTheQueue(t *testing.T) {
	app := newTestApp(t)
	forge := &stubForge{issues: map[int]githubIssue{
		42: {
			Number: 42, URL: "https://example.test/issues/42", State: "open",
			Title: "Covers are square", Body: "But mine is not.",
			CreatedAt: nowStamp(),
		},
	}}
	forge.start(t)
	allowReconcile(t)

	open, err := app.ListBugReports()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(open) != 1 || open[0].ID != "issue_42" {
		t.Fatalf("the queue holds %v, want the adopted issue_42", idsOf(open))
	}
	if !strings.HasPrefix(open[0].Description, "Covers are square") {
		t.Errorf("description = %q, want it to open with the issue title", open[0].Description)
	}
}

// TestASweepDoesNotDuplicateItsOwnReports is #11's visible symptom, stated
// directly: two sweeps over a report and its own issue must leave one row.
func TestASweepDoesNotDuplicateItsOwnReports(t *testing.T) {
	app := newTestApp(t)
	forge := &stubForge{issues: map[int]githubIssue{
		10: {Number: 10, URL: "https://example.test/issues/10", State: "open"},
	}}
	forge.hideFromList = 10 // lagging on the first sweep
	forge.start(t)
	allowReconcile(t)

	if err := app.store.saveBugReport(BugReport{
		ID: "bug_local", Description: "One report.", IssueNumber: 10,
		CreatedAt: nowStamp(),
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	if _, err := app.ListBugReports(); err != nil {
		t.Fatalf("first sweep: %v", err)
	}
	// The list catches up, and the second sweep sees the issue where it expects.
	forge.hideFromList = 0
	allowReconcile(t)
	open, err := app.ListBugReports()
	if err != nil {
		t.Fatalf("second sweep: %v", err)
	}

	if len(open) != 1 {
		t.Fatalf("the queue holds %v, want one row rather than a report and a copy of its issue",
			idsOf(open))
	}
	if open[0].ID != "bug_local" {
		t.Errorf("the surviving row is %q, want the original report", open[0].ID)
	}
}

func idsOf(reports []BugReport) []string {
	out := make([]string, 0, len(reports))
	for _, r := range reports {
		out = append(out, r.ID)
	}
	return out
}

// TestTheSweepClearsTheReportInJax is #16.
//
// Closing an issue on github.com is presented in skills/bug-reports.md as a way
// to work the queue, and the sweep is what turns that into a resolved report. It
// used to call the store method directly, which moves the row and nothing else —
// so the report was cleared here and left open in Jax for ever. Jax enforces
// oldest-first within an app's queue, so those stranded reports then sat at the
// head of it and refused every later check-out.
func TestTheSweepClearsTheReportInJax(t *testing.T) {
	app := newTestApp(t)
	forge := &stubForge{issues: map[int]githubIssue{
		10: {Number: 10, URL: "https://example.test/issues/10", State: "closed"},
	}}
	forge.start(t)
	jax := &jaxStub{}
	jax.start(t)
	allowReconcile(t)

	if err := app.store.saveBugReport(BugReport{
		ID: "bug_local", Description: "Somebody closed its issue on the forge.",
		IssueNumber: 10, JaxReportID: 993, CreatedAt: nowStamp(),
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	if _, err := app.ListBugReports(); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	resolved, _ := app.ListResolvedReports()
	if len(resolved) != 1 {
		t.Fatalf("%d resolved, want 1 — the sweep did not resolve it at all", len(resolved))
	}
	if len(jax.calls) != 1 || jax.calls[0] != jaxResolveTool {
		t.Fatalf("Jax was called with %v, want one %s — the report is cleared here "+
			"and still open there", jax.calls, jaxResolveTool)
	}
}

// TestTheSweepRecordsAJaxRefusal: a refusal on that path must be kept too, for
// the reason in #15 — the two queues disagreeing in silence is the failure.
func TestTheSweepRecordsAJaxRefusal(t *testing.T) {
	app := newTestApp(t)
	forge := &stubForge{issues: map[int]githubIssue{
		10: {Number: 10, URL: "https://example.test/issues/10", State: "closed"},
	}}
	forge.start(t)
	jax := &jaxStub{refuseWith: "debug report 993 is checked out by another agent"}
	jax.start(t)
	allowReconcile(t)

	if err := app.store.saveBugReport(BugReport{
		ID: "bug_local", IssueNumber: 10, JaxReportID: 993, CreatedAt: nowStamp(),
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := app.ListBugReports(); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	resolved, _ := app.ListResolvedReports()
	if len(resolved) != 1 {
		t.Fatalf("%d resolved, want 1", len(resolved))
	}
	if resolved[0].JaxNote == "" {
		t.Error("Jax refused the sweep's resolve and the history carries no note")
	}
}

// TestTheSweepDoesNotReCloseTheIssue: it is resolving *because* the issue is
// closed, so calling the forge again would be an API call per sweep to set a
// state that is already set.
func TestTheSweepDoesNotReCloseTheIssue(t *testing.T) {
	app := newTestApp(t)
	forge := &stubForge{issues: map[int]githubIssue{
		10: {Number: 10, URL: "https://example.test/issues/10", State: "closed"},
	}}
	forge.start(t)
	jax := &jaxStub{}
	jax.start(t)
	allowReconcile(t)

	if err := app.store.saveBugReport(BugReport{
		ID: "bug_local", IssueNumber: 10, JaxReportID: 993, CreatedAt: nowStamp(),
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	before := forge.patches
	if _, err := app.ListBugReports(); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if forge.patches != before {
		t.Errorf("the sweep sent %d write(s) to the forge for an issue already closed",
			forge.patches-before)
	}
}

// TestAnOrphanedTwinIsNotAdopted is #35. Filing once created two issues — the
// window and the sweep raced — and the sweep then adopted the orphan as a
// brand-new report even though its body named the report it belonged to.
func TestAnOrphanedTwinIsNotAdopted(t *testing.T) {
	app := newTestApp(t)

	report := BugReport{
		ID: "bug_deadbeef00000001", Description: "the redesign",
		IssueNumber: 34, IssueURL: "https://example.test/issues/34",
		CreatedAt: "2026-08-29T00:00:00Z", UpdatedAt: "2026-08-29T00:00:00Z",
	}
	if err := app.store.saveBugReport(report); err != nil {
		t.Fatalf("seed: %v", err)
	}

	forge := &stubForge{issues: map[int]githubIssue{
		34: {Number: 34, URL: "https://example.test/issues/34", State: "open"},
		33: {Number: 33, URL: "https://example.test/issues/33", State: "open",
			Body: "…\n- Report: `bug_deadbeef00000001`\n"},
	}}
	forge.start(t)
	allowReconcile(t)

	app.reconcileIssues()

	reports, err := app.store.bugReports()
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("the orphaned twin was adopted: %+v", reports)
	}
	if reports[0].IssueNumber != 34 {
		t.Fatalf("the report's own link moved: %+v", reports[0])
	}
	// And the orphan was closed as the duplicate it is.
	if forge.patches == 0 {
		t.Fatal("the orphaned twin was left open on the forge")
	}
}

// TestASweepCreatedIssueIsLinkedNotAdopted is the same race the other way
// round: the sweep created the issue while the filing process was still
// writing the number down, so the row is unlinked and the issue names it.
func TestASweepCreatedIssueIsLinkedNotAdopted(t *testing.T) {
	app := newTestApp(t)

	report := BugReport{
		ID: "bug_deadbeef00000002", Description: "still linking",
		CreatedAt: "2026-08-29T00:00:00Z", UpdatedAt: "2026-08-29T00:00:00Z",
	}
	if err := app.store.saveBugReport(report); err != nil {
		t.Fatalf("seed: %v", err)
	}

	forge := &stubForge{issues: map[int]githubIssue{
		40: {Number: 40, URL: "https://example.test/issues/40", State: "open",
			Body: "…\n- Report: `bug_deadbeef00000002`\n"},
	}}
	forge.start(t)
	allowReconcile(t)

	app.reconcileIssues()

	reports, err := app.store.bugReports()
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("the report was duplicated: %+v", reports)
	}
	if reports[0].ID != report.ID || reports[0].IssueNumber != 40 {
		t.Fatalf("the report was not linked to its own issue: %+v", reports[0])
	}
}

// TestAJustFiledReportIsNotRecreatedByTheSweep: the other half of #35's race —
// the catch-up create must not fire for a report filed seconds ago, because
// the process that filed it is usually still creating its issue.
func TestAJustFiledReportIsNotRecreatedByTheSweep(t *testing.T) {
	app := newTestApp(t)

	report := BugReport{
		ID: "bug_deadbeef00000003", Description: "just filed",
		CreatedAt: nowStamp(), UpdatedAt: nowStamp(),
	}
	if err := app.store.saveBugReport(report); err != nil {
		t.Fatalf("seed: %v", err)
	}

	forge := &stubForge{issues: map[int]githubIssue{}}
	forge.start(t)
	allowReconcile(t)

	app.reconcileIssues()

	reports, _ := app.store.bugReports()
	if len(reports) != 1 || reports[0].IssueNumber != 0 {
		t.Fatalf("a seconds-old report was given an issue by the sweep: %+v", reports)
	}
	if len(forge.issues) != 0 {
		t.Fatalf("the sweep created an issue for a report mid-filing: %v", forge.issues)
	}
}
