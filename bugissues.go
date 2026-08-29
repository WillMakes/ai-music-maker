package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// The queue is the repository's issue list
//
// A report is a GitHub issue. Filing one opens an issue; resolving one closes
// it; an issue somebody wrote on github.com the ordinary way is in this queue at
// the next read. That is what makes the queue workable by anybody rather than
// only by the person sitting in front of the app.
//
// The local table (bugs.go) is a projection of that list, reconciled at the top
// of every read. Two things keep it from being a cache with the usual problems:
//
//   - reconciliation is one-directional per fact. The forge owns whether an
//     issue is open and what it says; this app owns which local report an issue
//     corresponds to and what Jax knows about it.
//   - nothing here may fail a filing. No remote, no credential, no network — the
//     report is written locally and gets its issue on a later sweep.
//
// Two labels are used and neither is a status:
//
//	bug-queue    provenance. Written on the issues this app files, so
//	             `gh issue list --label bug-queue` still separates them from the
//	             ones somebody wrote by hand.
//	being-worked mirrors a claim onto the issue, so somebody reading the issue
//	             list can see an agent is on it.
//
// And one that is an opt-out:
//
//	parked       keeps an issue out of the queue — an epic, a discussion, a
//	             question. Parking keeps an issue out; it does not take one out.
// ---------------------------------------------------------------------------

const (
	bugQueueLabel  = "bug-queue"
	bugClaimLabel  = "being-worked"
	bugParkedLabel = "parked"
	// issuePageSize is one read of the queue. A hundred is GitHub's maximum for
	// a single page and comfortably more open issues than this app will ever
	// have; paginating for a case that does not arise would be code with no way
	// to be tested against reality.
	issuePageSize = 100
	// reconcileInterval is how stale the projection may be before a read goes to
	// the forge. The window polls the counts every couple of seconds and a call
	// per poll would spend a rate limit on it, so reads inside the window answer
	// from the projection and the sweep happens at most this often.
	reconcileInterval = 30 * time.Second
)

// errIssuesUnavailable is "there is nowhere to file this": no remote, a remote
// that is not GitHub, or no credential to act with. An ordinary state rather
// than a failure — a checkout with no remote is a thing that is simply true
// sometimes — so callers say nothing and leave the report local.
var errIssuesUnavailable = errors.New("no issue queue to file into")

// issuesHTTP is bounded: a forge that will not answer must not leave a filing,
// or a sweep, waiting on it.
var issuesHTTP = &http.Client{Timeout: 20 * time.Second}

// githubAPI is the API root, as a variable so a test can point the whole file at
// a stub. Every call here goes through one function, so one variable redirects
// all of them.
var githubAPI = "https://api.github.com"

// lastReconcile is when the projection was last swept. Package-level rather than
// on App because the bug CLI runs in its own process with no App worth the name,
// and both want the same rate limit.
var lastReconcile time.Time

// issueRef is where a report lives on the forge.
type issueRef struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
}

// issueQueue is one repository's issue list, as this queue reads and writes it.
type issueQueue struct {
	slug  string
	token string
}

// findIssueQueue works out where reports go, and answers errIssuesUnavailable
// when the answer is nowhere.
func (a *App) findIssueQueue() (issueQueue, error) {
	repo := currentBugRepo()
	if repo.Slug == "" {
		return issueQueue{}, errIssuesUnavailable
	}
	token := githubToken()
	if token == "" {
		return issueQueue{}, errIssuesUnavailable
	}
	return issueQueue{slug: repo.Slug, token: token}, nil
}

// githubToken is a credential to act as.
//
// A variable, and that is not decoration. The tests run inside this very
// checkout, so currentBugRepo finds the real remote and `gh auth token` answers
// with the producer's real credential — a test suite that filed reports would
// open real issues on the real repository, which is exactly what happened the
// first time this was written. newTestApp points this at "" so findIssueQueue
// answers errIssuesUnavailable immediately, with no network call to wait on.
var githubToken = realGitHubToken

// realGitHubToken reads a credential from the environment or from gh.
//
// The environment first, because a CI run or an agent's shell says so
// explicitly. Then `gh auth token`, which is the case that needs no setup at
// all: a producer signed in to gh has already done the work, and without this
// path the queue would be unreachable on exactly the machines most set up for
// it.
//
// Nothing is stored here. A token this app kept a copy of would be one that
// outlives the sign-in it came from, and gh refreshes its own.
func realGitHubToken() string {
	for _, name := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if token := strings.TrimSpace(os.Getenv(name)); token != "" {
			return token
		}
	}
	path, found := lookUpCommand("gh")
	if !found {
		return ""
	}
	cmd := exec.Command(path, "auth", "token")
	hideWindow(cmd)
	// Output rather than CombinedOutput: an unauthenticated gh writes its
	// complaint to stderr and exits non-zero, and folding that into the value
	// would hand back an error message as a bearer token.
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// call makes one API request. Every request in this file goes through here, so
// the headers, the error wording and the test redirection are settled once.
func (q issueQueue) call(method, path string, send any, out any) error {
	var body io.Reader
	if send != nil {
		encoded, err := json.Marshal(send)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, githubAPI+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+q.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	if send != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := issuesHTTP.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("GitHub rejected the credential for %s", q.slug)
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("GitHub has no repository called %s, or the credential cannot see it", q.slug)
	case resp.StatusCode >= 300:
		return fmt.Errorf("GitHub answered %s", resp.Status)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("could not read GitHub's answer: %w", err)
	}
	return nil
}

// githubIssue is one issue, in the fields this queue reads.
type githubIssue struct {
	Number int    `json:"number"`
	URL    string `json:"html_url"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	State  string `json:"state"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	CreatedAt string `json:"created_at"`
	// PullRequest is set on the entries that are pull requests. GitHub's issue
	// list returns those too, and a pull request read as a bug report is a queue
	// item nobody filed.
	PullRequest *struct{} `json:"pull_request"`
}

func (i githubIssue) labelled(name string) bool {
	for _, label := range i.Labels {
		if strings.EqualFold(label.Name, name) {
			return true
		}
	}
	return false
}

// openIssues reads the queue: every open issue, oldest first — the working
// order, which is also the order the API returns when asked for it.
func (q issueQueue) openIssues() ([]githubIssue, error) {
	path := fmt.Sprintf(
		"/repos/%s/issues?state=open&sort=created&direction=asc&per_page=%d",
		q.slug, issuePageSize)
	var found []githubIssue
	if err := q.call(http.MethodGet, path, nil, &found); err != nil {
		return nil, err
	}
	issues := make([]githubIssue, 0, len(found))
	for _, issue := range found {
		if issue.PullRequest == nil && !issue.labelled(bugParkedLabel) {
			issues = append(issues, issue)
		}
	}
	return issues, nil
}

// issue reads one by number.
//
// The only way to learn whether an issue is closed. Its absence from openIssues
// is not that: see the note on reconcileIssues.
func (q issueQueue) issue(number int) (githubIssue, error) {
	var found githubIssue
	err := q.call(http.MethodGet,
		fmt.Sprintf("/repos/%s/issues/%d", q.slug, number), nil, &found)
	return found, err
}

// createIssue opens an issue for a report.
func (q issueQueue) createIssue(report BugReport) (issueRef, error) {
	q.ensureLabels()
	var created githubIssue
	err := q.call(http.MethodPost, "/repos/"+q.slug+"/issues", map[string]any{
		"title":  issueTitle(report),
		"body":   issueBody(report),
		"labels": []string{bugQueueLabel},
	}, &created)
	if err != nil {
		return issueRef{}, err
	}
	return issueRef{Number: created.Number, URL: created.URL}, nil
}

// writeIssue corrects an existing issue after the report was edited.
func (q issueQueue) writeIssue(report BugReport) error {
	if report.IssueNumber == 0 {
		return errIssuesUnavailable
	}
	return q.call(http.MethodPatch,
		fmt.Sprintf("/repos/%s/issues/%d", q.slug, report.IssueNumber),
		map[string]any{"title": issueTitle(report), "body": issueBody(report)}, nil)
}

// closeIssue closes one, leaving a comment saying where the fix is when there is
// one to point at. The comment first: an issue that closed but never said why is
// a worse record than one that says why and is still open.
func (q issueQueue) closeIssue(number int, commitURL string) error {
	if number == 0 {
		return errIssuesUnavailable
	}
	if strings.TrimSpace(commitURL) != "" {
		_ = q.call(http.MethodPost,
			fmt.Sprintf("/repos/%s/issues/%d/comments", q.slug, number),
			map[string]any{"body": "Fixed in " + commitURL}, nil)
	}
	return q.call(http.MethodPatch,
		fmt.Sprintf("/repos/%s/issues/%d", q.slug, number),
		map[string]any{"state": "closed"}, nil)
}

// markClaimed adds or removes the claim label.
//
// A missing label on removal is success, not a failure: the end state asked for
// is "this issue is not marked as being worked", and it already is.
func (q issueQueue) markClaimed(number int, claimed bool) error {
	if number == 0 {
		return errIssuesUnavailable
	}
	if !claimed {
		_ = q.call(http.MethodDelete,
			fmt.Sprintf("/repos/%s/issues/%d/labels/%s", q.slug, number, bugClaimLabel),
			nil, nil)
		return nil
	}
	q.ensureLabels()
	return q.call(http.MethodPost,
		fmt.Sprintf("/repos/%s/issues/%d/labels", q.slug, number),
		map[string]any{"labels": []string{bugClaimLabel}}, nil)
}

// ensureLabels creates the labels this queue uses if the repository does not
// have them.
//
// Best effort and deliberately unchecked: a label that already exists is a 422,
// which is the ordinary case and not worth reporting. A repository whose labels
// cannot be written is one where the create will fail with a message that
// actually says something.
func (q issueQueue) ensureLabels() {
	labels := []struct{ name, colour, description string }{
		{bugQueueLabel, "d73a4a", "Filed from SunoMaker's bug button"},
		{bugClaimLabel, "fbca04", "An agent has claimed this report"},
		{bugParkedLabel, "cfd3d7", "Not work to pick up — kept out of the queue"},
	}
	for _, l := range labels {
		_ = q.call(http.MethodPost, "/repos/"+q.slug+"/labels", map[string]any{
			"name": l.name, "color": l.colour, "description": l.description,
		}, nil)
	}
}

// ---------------------------------------------------------------------------
// What an issue says
// ---------------------------------------------------------------------------

// issueTitleMax is where a description is cut for the title. Long enough to
// carry a sentence, short enough that the issue list is scannable — and the
// whole description is in the body regardless, so nothing is lost by cutting.
const issueTitleMax = 72

// issueTitle is the report's first line, cut to length. A report has no title
// field of its own: asking for one is asking somebody who just found a bug to
// summarise it before they report it, which is how bugs go unreported.
func issueTitle(report BugReport) string {
	title := strings.TrimSpace(report.Description)
	if i := strings.IndexAny(title, "\r\n"); i >= 0 {
		title = title[:i]
	}
	if len(title) > issueTitleMax {
		// Cut at a space so the title ends on a word. Only when there is one
		// reasonably near the end — a URL with no spaces in it is cut mid-token
		// rather than back to nothing.
		cut := title[:issueTitleMax]
		if space := strings.LastIndex(cut, " "); space > issueTitleMax/2 {
			cut = cut[:space]
		}
		title = strings.TrimSpace(cut) + "…"
	}
	if title == "" {
		title = "Bug report"
	}
	return title
}

// capitalise upper-cases the first letter, for a subject kind used as a label.
// Not strings.Title, which is deprecated and, being word-wise, would also
// capitalise the second word of something that never has one.
func capitalise(word string) string {
	if word == "" {
		return ""
	}
	return strings.ToUpper(word[:1]) + word[1:]
}

func issueBody(report BugReport) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(report.Description))
	b.WriteString("\n\n")
	b.WriteString(issueDetails(report))
	return b.String()
}

// issueDetails is the footer every filed issue carries: where the report was
// made from, and the id that pairs it with the local row.
//
// A footer rather than a template the producer fills in. What is worth recording
// is what the app already knows, and a form asking somebody to retype the page
// they are standing on is a form that gets left blank.
func issueDetails(report BugReport) string {
	var b strings.Builder
	b.WriteString("---\n\n")
	b.WriteString("Filed from SunoMaker.\n\n")
	// The page is kept whether or not the bug is app-wide (#20): global says
	// the problem is not confined to that screen; the route still says where
	// it showed up, which is worth having either way.
	if report.Route != "" {
		b.WriteString("- Page: `" + report.Route + "`\n")
	}
	if report.Global {
		b.WriteString("- Scope: the whole app\n")
	}
	if report.SubjectKind != "" && report.SubjectID != "" {
		name := report.SubjectName
		if name == "" {
			name = "(deleted)"
		}
		b.WriteString(fmt.Sprintf("- %s: %s (`%s`)\n",
			capitalise(report.SubjectKind), name, report.SubjectID))
	}
	if len(report.Attachments) > 0 {
		// Paths rather than pictures: GitHub has no public API for attaching a
		// file to an issue, and a path on the reporter's machine is still what
		// an agent working the report opens (#20).
		files := make([]string, 0, len(report.Attachments))
		for _, path := range report.Attachments {
			files = append(files, "`"+path+"`")
		}
		b.WriteString("- Files: " + strings.Join(files, ", ") + " (on the reporter's machine)\n")
	}
	b.WriteString("- Report: `" + report.ID + "`\n")
	if report.CreatedAt != "" {
		b.WriteString("- Filed: " + report.CreatedAt + "\n")
	}
	return b.String()
}

// reportIDFromBody reads the report id an app-filed issue carries in its
// footer (see issueDetails). "" for an issue with no such line — one written
// by hand on github.com.
func reportIDFromBody(body string) string {
	match := reportIDPattern.FindStringSubmatch(body)
	if match == nil {
		return ""
	}
	return match[1]
}

var reportIDPattern = regexp.MustCompile("- Report: `((?:bug|issue)_[0-9a-fA-F]+)`")

// closeDuplicate closes an issue that duplicates another, saying so first: an
// issue that closed without a word is a worse record than one that names its
// twin.
func (q issueQueue) closeDuplicate(number, of int) {
	why := "The report this issue was filed for has already been resolved."
	if of != 0 {
		why = fmt.Sprintf("Duplicate of #%d — the same report reached the forge twice.", of)
	}
	_ = q.call(http.MethodPost,
		fmt.Sprintf("/repos/%s/issues/%d/comments", q.slug, number),
		map[string]any{"body": why}, nil)
	_ = q.call(http.MethodPatch,
		fmt.Sprintf("/repos/%s/issues/%d", q.slug, number),
		map[string]any{"state": "closed"}, nil)
}

// readIssue turns an issue nobody filed through the app into a report.
//
// The id is derived from the number rather than minted, so the same issue read
// twice is the same report both times — which is the whole of how the projection
// stays a projection.
//
// Its description is the title with the body under it. The title is where a
// hand-written issue puts most of the bug, and reading only the body would leave
// the queue listing it by its second paragraph.
func readIssue(issue githubIssue) BugReport {
	description := strings.TrimSpace(issue.Title)
	if body := strings.TrimSpace(issue.Body); body != "" {
		description += "\n\n" + body
	}
	return BugReport{
		ID:          "issue_" + strconv.Itoa(issue.Number),
		Description: description,
		Global:      true, // nothing says which page it was about
		CheckedOut:  issue.labelled(bugClaimLabel),
		IssueURL:    issue.URL,
		IssueNumber: issue.Number,
		CreatedAt:   issue.CreatedAt,
		UpdatedAt:   issue.CreatedAt,
	}
}

// ---------------------------------------------------------------------------
// The app's side
// ---------------------------------------------------------------------------

// linkToIssue gives a report an issue, returning it with the reference written
// on. Silent on failure: the report exists locally either way, and the producer
// pressing the bug button is not the person to tell about a credential problem.
func (a *App) linkToIssue(report BugReport) BugReport {
	queue, err := a.findIssueQueue()
	if err != nil {
		return report
	}
	ref, err := queue.createIssue(report)
	if err != nil {
		return report
	}
	report.IssueURL, report.IssueNumber = ref.URL, ref.Number
	if a.store != nil {
		_ = a.store.saveBugReport(report)
	}
	return report
}

func (a *App) writeIssue(report BugReport) {
	if queue, err := a.findIssueQueue(); err == nil {
		_ = queue.writeIssue(report)
	}
}

func (a *App) markIssueClaimed(report BugReport, claimed bool) {
	if queue, err := a.findIssueQueue(); err == nil {
		_ = queue.markClaimed(report.IssueNumber, claimed)
	}
}

func (a *App) closeIssue(report BugReport, commitURL string) {
	if queue, err := a.findIssueQueue(); err == nil {
		_ = queue.closeIssue(report.IssueNumber, commitURL)
	}
}

// reconcileIssues brings the local projection in line with the forge.
//
// Three things happen, and the order is what keeps them from fighting:
//
//  1. an open issue with no local row gets one, so an issue written on
//     github.com is in the queue;
//  2. a local row whose issue is *confirmed closed* leaves the queue, resolved;
//  3. a local row with no issue gets one, catching up anything filed while the
//     forge was unreachable.
//
// "Confirmed" is doing real work in step 2 (#11). Absence from the open list is
// not evidence of closure — see the branch itself.
//
// Rate-limited and silent. Every failure mode here — closed, offline, no
// credential — is a state where the right answer is the local projection.
func (a *App) reconcileIssues() {
	if a.store == nil || time.Since(lastReconcile) < reconcileInterval {
		return
	}
	queue, err := a.findIssueQueue()
	if err != nil {
		return
	}
	issues, err := queue.openIssues()
	if err != nil {
		return
	}
	// Stamped only after a successful read, so a run of failures keeps retrying
	// rather than backing off for a minute at a time on the strength of one.
	lastReconcile = time.Now()

	local, err := a.store.bugReports()
	if err != nil {
		return
	}
	byNumber := map[int]BugReport{}
	byID := map[string]BugReport{}
	for _, r := range local {
		byID[r.ID] = r
		if r.IssueNumber != 0 {
			byNumber[r.IssueNumber] = r
		}
	}
	resolvedIDs := map[string]bool{}
	if history, err := a.store.resolvedReports(); err == nil {
		for _, r := range history {
			resolvedIDs[r.ID] = true
		}
	}
	openNumbers := map[int]bool{}
	linkedNow := map[string]bool{}
	changed := false

	for _, issue := range issues {
		openNumbers[issue.Number] = true
		existing, known := byNumber[issue.Number]
		if !known {
			// An app-filed issue names its report in the footer, and that name
			// outranks the issue number (#35): filing raced the sweep once and
			// one report reached the forge as two issues — the row linked to
			// the second while the first sat orphaned, and the next sweep
			// adopted the orphan as a brand-new report even though its body
			// said exactly which report it was.
			if id := reportIDFromBody(issue.Body); id != "" {
				if owner, mine := byID[id]; mine {
					if owner.IssueNumber == 0 {
						// The race the other way round: this issue IS the
						// report's, created by a sweep while the filing was
						// still writing the number down. Link, don't adopt —
						// and tell the catch-up loop below, which is iterating
						// the rows as they were read and would otherwise still
						// see this one as unlinked and create a third issue.
						owner.IssueURL, owner.IssueNumber = issue.URL, issue.Number
						if err := a.store.saveBugReport(owner); err == nil {
							byNumber[issue.Number] = owner
							linkedNow[owner.ID] = true
							changed = true
						}
					} else if owner.IssueNumber != issue.Number {
						queue.closeDuplicate(issue.Number, owner.IssueNumber)
					}
					continue
				}
				if resolvedIDs[id] {
					// The report was fixed; this orphan outlived it.
					queue.closeDuplicate(issue.Number, 0)
					continue
				}
			}
			// An issue nobody filed through the app. Adopted rather than ignored:
			// a queue you can only add to from inside the app would be the local
			// queue again with extra steps.
			adopted := readIssue(issue)
			if err := a.store.saveBugReport(adopted); err == nil {
				changed = true
				a.linkToJax(adopted)
			}
			continue
		}
		// The forge owns the claim: an agent that labelled the issue directly is
		// working it, whatever this row says.
		if claimed := issue.labelled(bugClaimLabel); claimed != existing.CheckedOut {
			if err := a.store.setBugCheckedOut(existing.ID, claimed); err == nil {
				changed = true
			}
		}
	}

	for _, r := range local {
		switch {
		case r.IssueNumber != 0 && !openNumbers[r.IssueNumber]:
			// Missing from the open list, which is *not* proof it was closed
			// (#11). It is also what a list that has not caught up looks like:
			// GitHub's issue list is eventually consistent, so an issue created
			// a second ago is routinely absent from it. Reading that as closure
			// resolved reports at the instant they were filed, and the next
			// sweep then adopted their own issues back as hand-written ones,
			// losing the route, the subject and the id.
			//
			// So the issue is read directly and only a state it states itself
			// counts. Anything else — a network failure, a rate limit, an issue
			// that is merely parked and so filtered out of the list — leaves the
			// report where it is, which is the answer that can be corrected on
			// the next sweep. One extra call, only for a report whose issue is
			// not in the open list, which is rare.
			issue, err := queue.issue(r.IssueNumber)
			if err != nil || issue.State != "closed" {
				continue
			}
			// retireReport, not the store method: it is what also clears the
			// report in Jax (#16). Calling the store directly here is exactly how
			// Jax came to hold reports whose issues had been closed months —
			// well, hours — earlier.
			if _, err := a.retireReport(r, "", ""); err == nil {
				changed = true
			}
		case r.IssueNumber == 0 && !linkedNow[r.ID]:
			// Filed while the forge was unreachable — but not *just* filed: a
			// report seconds old is usually mid-link in the process that filed
			// it, and creating here too is how one report became two issues
			// (#35). Two minutes is longer than any create takes and shorter
			// than anyone waits for a lost report to surface.
			if created, err := time.Parse(time.RFC3339, r.CreatedAt); err == nil &&
				time.Since(created) < 2*time.Minute {
				continue
			}
			if ref, err := queue.createIssue(r); err == nil {
				r.IssueURL, r.IssueNumber = ref.URL, ref.Number
				if err := a.store.saveBugReport(r); err == nil {
					changed = true
				}
			}
		}
	}

	if changed {
		a.store.changed(scopeBugReports)
		a.store.changed(scopeResolvedReports)
	}
}
