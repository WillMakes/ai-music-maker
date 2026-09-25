package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// standInForeignCheckout makes the process look as if it runs in a checkout of
// some other project — Jax's, in the report that found this.
func standInForeignCheckout(t *testing.T, remote string) {
	t.Helper()
	root := t.TempDir()
	previousDir, previousCheckout := workingDir, checkoutAt
	workingDir = func() (string, error) { return root, nil }
	checkoutAt = func(string) (string, string) { return root, remote }
	t.Cleanup(func() { workingDir, checkoutAt = previousDir, previousCheckout })
}

// TestAnotherProjectsCheckoutIsNeverFiledAgainst is #94. The MCP server is
// started by an agent in whatever project it is working in, and reports filed
// from an agent in Jax's checkout became issues on Jax's repository. Standing
// in a checkout that is not SunoMaker now files nowhere, and says why.
func TestAnotherProjectsCheckoutIsNeverFiledAgainst(t *testing.T) {
	app := newTestApp(t)
	standInForeignCheckout(t, "https://github.com/WillMakes/brand-producer.git")

	repo := app.GetBugRepo()
	if repo.Slug != "" {
		t.Fatalf("a checkout of another project was taken as SunoMaker's: %+v", repo)
	}
	if repo.Ignored != "WillMakes/brand-producer" {
		t.Errorf("the ignored checkout should be named so Settings can explain: %+v", repo)
	}
	if pinned, _ := app.store.setting(keyBugRepo); pinned != "" {
		t.Errorf("another project's repository was pinned: %q", pinned)
	}
	githubToken = func() string { return "a-real-looking-token" }
	if _, err := app.findIssueQueue(); !errors.Is(err, errIssuesUnavailable) {
		t.Errorf("there should be no queue to file into from here, got %v", err)
	}
}

// TestTheAppsOwnCheckoutPinsItself: the first process started in a SunoMaker
// checkout pins its repository, so every later one files there from anywhere —
// including from another project's checkout, which is the case that went wrong.
func TestTheAppsOwnCheckoutPinsItself(t *testing.T) {
	app := newTestApp(t)
	standInAppCheckout(t, "git@github.com:WillMakes/suno-maker.git")

	repo := app.GetBugRepo()
	if repo.Slug != "WillMakes/suno-maker" || repo.Source != "checkout" {
		t.Fatalf("the app's own checkout was not used: %+v", repo)
	}
	if pinned, _ := app.store.setting(keyBugRepo); pinned != "WillMakes/suno-maker" {
		t.Fatalf("the checkout's repository was not pinned: %q", pinned)
	}

	// The same database, from an agent standing in Jax's checkout.
	standInForeignCheckout(t, "https://github.com/WillMakes/brand-producer.git")
	repo = app.GetBugRepo()
	if repo.Slug != "WillMakes/suno-maker" || repo.Source != "pinned" {
		t.Errorf("the pin did not hold from another project's checkout: %+v", repo)
	}
	if repo.Ignored != "WillMakes/brand-producer" {
		t.Errorf("the foreign checkout should still be named: %+v", repo)
	}
	githubToken = func() string { return "token" }
	if q, err := app.findIssueQueue(); err != nil || q.slug != "WillMakes/suno-maker" {
		t.Errorf("the queue should be SunoMaker's, got %+v, %v", q, err)
	}
}

// TestAPinIsNotOverwrittenByAnotherCheckout: a fork or a second clone of the
// app does not move reports off the repository that was chosen.
func TestAPinIsNotOverwrittenByAnotherCheckout(t *testing.T) {
	app := newTestApp(t)
	if _, err := app.SetBugRepo("WillMakes/suno-maker"); err != nil {
		t.Fatal(err)
	}
	standInAppCheckout(t, "https://github.com/someone-else/suno-maker-fork.git")
	if repo := app.GetBugRepo(); repo.Slug != "WillMakes/suno-maker" || repo.Source != "pinned" {
		t.Errorf("a fork's checkout moved the pin: %+v", repo)
	}
}

// TestSetBugRepoValidates: owner/repo and GitHub URLs pin, anything else is
// refused, a repository GitHub refuses is not kept, and empty unpins.
func TestSetBugRepoValidates(t *testing.T) {
	app := newTestApp(t)

	for input, want := range map[string]string{
		"WillMakes/suno-maker":                        "WillMakes/suno-maker",
		" https://github.com/WillMakes/suno-maker ":   "WillMakes/suno-maker",
		"git@github.com:WillMakes/suno-maker.git":     "WillMakes/suno-maker",
		"https://github.com/WillMakes/suno-maker.git": "WillMakes/suno-maker",
	} {
		repo, err := app.SetBugRepo(input)
		if err != nil || repo.Slug != want || repo.Source != "pinned" {
			t.Errorf("%q: got %+v, %v", input, repo, err)
		}
	}
	for _, bad := range []string{"suno-maker", "https://gitlab.com/a/b", "a/b/c", "owner/has space"} {
		if _, err := app.SetBugRepo(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}

	checkRepoTakesIssues = func(slug string) error {
		return errors.New("GitHub has no repository called " + slug)
	}
	if _, err := app.SetBugRepo("WillMakes/typo"); err == nil {
		t.Error("a repository GitHub refused was kept")
	}
	if pinned, _ := app.store.setting(keyBugRepo); pinned != "WillMakes/suno-maker" {
		t.Errorf("a refused repository replaced the pin: %q", pinned)
	}

	repo, err := app.SetBugRepo("")
	if err != nil || repo.Slug != "" || repo.Source != "" {
		t.Errorf("empty should unpin: %+v, %v", repo, err)
	}
}

// TestJaxIsToldTheResolvedRepository: the app name Jax files under comes from
// the same resolution, not from the working directory.
func TestJaxIsToldTheResolvedRepository(t *testing.T) {
	app := newTestApp(t)
	standInForeignCheckout(t, "https://github.com/WillMakes/brand-producer.git")
	if _, err := app.SetBugRepo("WillMakes/suno-maker"); err != nil {
		t.Fatal(err)
	}
	var named string
	app.toJax = func(_ BugReport, repo string) (int, error) {
		named = repo
		return 7, nil
	}
	if _, err := app.FileBugReport(BugReport{Description: "Covers are square"}); err != nil {
		t.Fatal(err)
	}
	if named != "WillMakes/suno-maker" {
		t.Errorf("Jax was told the report belongs to %q", named)
	}
}

// TestBugsRepoCommandPins: `bugs repo <owner/repo>` pins from the terminal, and
// `bugs repo` reports the pin.
func TestBugsRepoCommandPins(t *testing.T) {
	_ = newTestApp(t)
	var stdout, stderr bytes.Buffer
	if code := bugCommand([]string{"repo", "WillMakes/suno-maker"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	stdout.Reset()
	if code := bugCommand([]string{"repo"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	var repo BugRepo
	if err := json.Unmarshal(stdout.Bytes(), &repo); err != nil {
		t.Fatal(err)
	}
	if repo.Slug != "WillMakes/suno-maker" || repo.Source != "pinned" {
		t.Errorf("bugs repo reported %+v", repo)
	}
	if code := bugCommand([]string{"repo", "not a repo"}, &stdout, &stderr); code == 0 ||
		!strings.Contains(stderr.String(), "not a GitHub repository") {
		t.Errorf("a bad argument was not refused: %s", stderr.String())
	}
}
