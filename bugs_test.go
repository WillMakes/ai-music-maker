package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// The bug tests never reach GitHub or Jax. newTestApp stubs toJax, and
// findIssueQueue answers errIssuesUnavailable in a temp directory with no
// remote — which is exactly the state this feature is written to survive, so the
// tests exercise the path that matters most.

func TestFileBugReportSurvivesNoTracker(t *testing.T) {
	app := newTestApp(t)

	report, err := app.FileBugReport(BugReport{
		Description: "The prompt field loses its last line on save.",
		Route:       "track-details",
		SubjectKind: "track",
	})
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	// The producer pressed a button that says "report a bug" and the report must
	// exist when they let go of it — whatever the forge and the tracker did.
	if report.ID == "" || report.CreatedAt == "" {
		t.Error("the report was not written down")
	}
	if report.IssueNumber != 0 || report.JaxReportID != 0 {
		t.Error("a report claimed a counterpart that was never made")
	}

	open, err := app.store.bugReports()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(open) != 1 {
		t.Fatalf("%d open reports, want 1", len(open))
	}
}

func TestFileBugReportNeedsADescription(t *testing.T) {
	app := newTestApp(t)
	if _, err := app.FileBugReport(BugReport{Description: "  "}); err == nil {
		t.Fatal("an empty report was accepted")
	}
}

func TestResolveMovesTheReportBetweenTables(t *testing.T) {
	app := newTestApp(t)

	report, err := app.FileBugReport(BugReport{Description: "Something is broken."})
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	resolved, err := app.ResolveBugReport(report.ID, "abc123", "https://example.test/abc123")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.CommitSHA != "abc123" || resolved.CommitURL == "" {
		t.Error("where the fix landed was not recorded")
	}

	// An empty open queue has to mean nothing is known to be broken, which is
	// why resolving is a move rather than a status flag.
	open, _ := app.store.bugReports()
	if len(open) != 0 {
		t.Errorf("%d reports still open", len(open))
	}
	history, _ := app.ListResolvedReports()
	if len(history) != 1 {
		t.Fatalf("%d in the history, want 1", len(history))
	}
	if history[0].ID != report.ID {
		t.Error("the history entry is not the report that was resolved")
	}
}

func TestCountsAndSeenFlag(t *testing.T) {
	app := newTestApp(t)

	first, _ := app.FileBugReport(BugReport{Description: "One"})
	if _, err := app.FileBugReport(BugReport{Description: "Two"}); err != nil {
		t.Fatalf("file: %v", err)
	}
	if _, err := app.ResolveBugReport(first.ID, "", ""); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	counts, err := app.GetBugCounts()
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if counts.Open != 1 || counts.Resolved != 1 || counts.Unseen != 1 {
		t.Errorf("counts = %+v, want 1 open, 1 resolved, 1 unseen", counts)
	}

	// The notice is shown once, which is what makes it worth reading.
	if err := app.MarkFixesSeen(); err != nil {
		t.Fatalf("mark seen: %v", err)
	}
	counts, _ = app.GetBugCounts()
	if counts.Unseen != 0 {
		t.Errorf("unseen = %d after marking, want 0", counts.Unseen)
	}
}

func TestClaimAndRelease(t *testing.T) {
	app := newTestApp(t)
	report, _ := app.FileBugReport(BugReport{Description: "Two agents must not both take this."})

	claimed, err := app.ClaimBugReport(report.ID)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if !claimed.CheckedOut {
		t.Error("the claim was not recorded")
	}
	released, err := app.ReleaseBugReport(report.ID)
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if released.CheckedOut {
		t.Error("the release was not recorded")
	}
}

func TestSearchFindsByDescription(t *testing.T) {
	app := newTestApp(t)
	if _, err := app.FileBugReport(BugReport{Description: "The lyrics field scrolls oddly."}); err != nil {
		t.Fatalf("file: %v", err)
	}
	if _, err := app.FileBugReport(BugReport{Description: "The cover will not generate."}); err != nil {
		t.Fatalf("file: %v", err)
	}

	found, err := app.SearchBugReports("lyrics")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("%d matches, want 1", len(found))
	}
}

func TestIssueTitleIsCutAtAWord(t *testing.T) {
	long := strings.Repeat("the prompt field ", 20)
	title := issueTitle(BugReport{Description: long})

	if len(title) > issueTitleMax+3 {
		t.Errorf("title is %d characters, want it cut near %d", len(title), issueTitleMax)
	}
	if !strings.HasSuffix(title, "…") {
		t.Error("a cut title does not say it was cut")
	}
	// A report has no title field: asking somebody who just found a bug to
	// summarise it first is how bugs go unreported. So an empty one still gets a
	// usable issue rather than a blank heading.
	if issueTitle(BugReport{}) != "Bug report" {
		t.Error("an empty description produced an empty issue title")
	}
	// Only the first line — the rest is in the body.
	if got := issueTitle(BugReport{Description: "First line\nSecond line"}); got != "First line" {
		t.Errorf("title = %q, want only the first line", got)
	}
}

func TestReadIssueDerivesAStableID(t *testing.T) {
	issue := githubIssue{Number: 42, Title: "Covers are square", Body: "But mine is not."}

	first := readIssue(issue)
	second := readIssue(issue)
	// The same issue read twice must be the same report both times — that is the
	// whole of how the local table stays a projection rather than accumulating
	// duplicates on every sweep.
	if first.ID != second.ID || first.ID != "issue_42" {
		t.Errorf("ids = %q and %q, want a stable issue_42", first.ID, second.ID)
	}
	// The title is where a hand-written issue puts most of the bug; reading only
	// the body would list it by its second paragraph.
	if !strings.HasPrefix(first.Description, "Covers are square") {
		t.Errorf("description = %q, want it to open with the title", first.Description)
	}
	if !strings.Contains(first.Description, "But mine is not.") {
		t.Error("the body was dropped")
	}
}

func TestRepoSlug(t *testing.T) {
	cases := map[string]string{
		"https://github.com/WillMakes/suno-maker.git": "WillMakes/suno-maker",
		"https://github.com/WillMakes/suno-maker":     "WillMakes/suno-maker",
		"git@github.com:WillMakes/suno-maker.git":     "WillMakes/suno-maker",
		"ssh://git@github.com/WillMakes/suno-maker":   "WillMakes/suno-maker",
		// Not GitHub: this app knows how to file in one place, and guessing at a
		// second forge's API would be worse than declining.
		"https://gitlab.com/WillMakes/suno-maker.git": "",
		"":          "",
		"not a url": "",
	}
	for remote, want := range cases {
		if got := repoSlug(remote); got != want {
			t.Errorf("repoSlug(%q) = %q, want %q", remote, got, want)
		}
	}
}

func TestBugCLIListsAsJSON(t *testing.T) {
	// The CLI opens its own store from dataDir, which newTestApp has pointed at a
	// temp directory — so this is the same database the app below writes to.
	app := newTestApp(t)
	if _, err := app.FileBugReport(BugReport{Description: "Findable from a terminal."}); err != nil {
		t.Fatalf("file: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if code := bugCommand([]string{"list"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	var reports []BugReport
	if err := json.Unmarshal(stdout.Bytes(), &reports); err != nil {
		t.Fatalf("the output is not JSON an agent can parse: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("%d reports, want 1", len(reports))
	}
}

func TestLeadingIDLetsTheSubjectComeFirst(t *testing.T) {
	// Go's flag package stops at the first non-flag argument, so
	// `bugs resolve <id> --sha abc` parses no flags at all. Every other CLI takes
	// the subject first, so the positional is lifted out before parsing.
	id, flags := leadingID([]string{"bug_123", "--sha", "abc"})
	if id != "bug_123" {
		t.Errorf("id = %q, want bug_123", id)
	}
	if len(flags) != 2 || flags[0] != "--sha" {
		t.Errorf("flags = %v, want the two that follow", flags)
	}
	// Nothing to lift when the arguments open with a flag.
	if id, flags := leadingID([]string{"--sha", "abc"}); id != "" || len(flags) != 2 {
		t.Errorf("leadingID(flags only) = (%q, %v)", id, flags)
	}
	if id, _ := leadingID(nil); id != "" {
		t.Errorf("leadingID(nil) = %q, want empty", id)
	}
}

func TestBugCLIRejectsWhatItCannotDo(t *testing.T) {
	newTestApp(t) // point dataDir at a temp directory for the CLI's own openStore

	var stdout, stderr bytes.Buffer
	if code := bugCommand(nil, &stdout, &stderr); code != 2 {
		t.Errorf("no subcommand exited %d, want 2", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := bugCommand([]string{"resolve"}, &stdout, &stderr); code != 2 {
		t.Errorf("resolve with no id exited %d, want 2", code)
	}
}
