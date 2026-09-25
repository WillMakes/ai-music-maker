package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sunomaker/internal/platform"
)

// ---------------------------------------------------------------------------
// Which repository reports are filed against
//
// Reports are GitHub issues, so every one needs a repository, and that answer
// is pinned in settings (#94) rather than read afresh from wherever the process
// happens to be standing.
//
// It used to be read from the working directory's git remote every time, on
// the reasoning that where the checkout points is always right. That holds for
// the window and for `go run . bugs` in this checkout, and nowhere else: the
// MCP server is started by Claude Code or Codex in whatever project the agent
// is working in, and a report filed from an agent in Jax's checkout became an
// issue on Jax's repository — the right report in the wrong queue, found only
// when somebody noticed it missing from this one. And the queue that agent then
// listed was Jax's issues, offered as SunoMaker's reports.
//
// So the repository is a setting. A checkout is still how it is found the
// first time — but only a checkout of SunoMaker, recognised by its go.mod, and
// the first one found is pinned so every later process, from anywhere, files in
// the same place. A checkout of anything else is named, so Settings can say
// why it was not used, and never filed against. It can be changed in Settings
// → Bug reports, and a new one is checked on GitHub before it is kept.
//
// An installed build run from anywhere on a machine that has never pinned one
// has no repository and keeps its reports locally, as before: a build somebody
// else installed should not be opening issues on the developer's repository.
// ---------------------------------------------------------------------------

// keyBugRepo is the pinned repository, as "owner/repo".
const keyBugRepo = "bug_repo"

// appModule is the first line of this app's go.mod, which is how a checkout of
// SunoMaker is told from a checkout of anything else.
const appModule = "module sunomaker"

// BugRepo is where reports are filed, for Settings and for `bugs repo`.
type BugRepo struct {
	// Remote is where the repository lives, as git or the setting states it.
	Remote string `json:"remote"`
	// Slug is "owner/repo", "" when there is nowhere to file.
	Slug string `json:"slug"`
	// Dir is the checkout this process is running in, "" when it is in none.
	Dir string `json:"dir"`
	// Source says where Slug came from: "pinned" for the setting, "checkout"
	// for this checkout (which pins it), "" for nowhere.
	Source string `json:"source"`
	// Ignored names a checkout this process is standing in that is not
	// SunoMaker — the case that used to file reports in the wrong queue.
	Ignored string `json:"ignored"`
}

// workingDir is where this process was started. A variable so tests can stand
// somewhere other than this checkout.
var workingDir = os.Getwd

// checkoutAt reads the git checkout containing dir: its top-level folder and
// its origin remote, "" for either when there is none. A variable so tests
// need not build a git repository to be one.
var checkoutAt = func(dir string) (root, remote string) {
	git, found := lookUpCommand("git")
	if !found {
		return "", ""
	}
	run := func(args ...string) string {
		cmd := exec.Command(git, args...)
		cmd.Dir = dir
		platform.HideWindow(cmd)
		// Output rather than CombinedOutput: outside a checkout git writes its
		// complaint to stderr and exits non-zero, and folding that in would
		// hand back an error message as a path or a URL.
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	root = run("rev-parse", "--show-toplevel")
	if root == "" {
		return "", ""
	}
	return filepath.FromSlash(root), run("remote", "get-url", "origin")
}

// isAppCheckout says whether root is a checkout of SunoMaker itself.
func isAppCheckout(root string) bool {
	f, err := os.Open(filepath.Join(root, "go.mod"))
	if err != nil {
		return false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	return scanner.Scan() && strings.TrimSpace(scanner.Text()) == appModule
}

// checkoutHere is what the working directory says, before the setting is
// consulted: a SunoMaker checkout's repository, or the name of the other
// project's checkout that was not used.
func checkoutHere() BugRepo {
	dir, err := workingDir()
	if err != nil {
		return BugRepo{}
	}
	root, remote := checkoutAt(dir)
	if root == "" {
		return BugRepo{}
	}
	if !isAppCheckout(root) {
		ignored := repoSlug(remote)
		if ignored == "" {
			ignored = root
		}
		return BugRepo{Ignored: ignored}
	}
	slug := repoSlug(remote)
	if slug == "" {
		return BugRepo{Remote: remote, Dir: root}
	}
	return BugRepo{Remote: remote, Slug: slug, Dir: root, Source: "checkout"}
}

// resolveBugRepo is where this process files: the pinned repository, or else
// the SunoMaker checkout it is running in — which is pinned on the spot, so
// the next process to ask gets the same answer from wherever it stands.
func resolveBugRepo(store *Store) BugRepo {
	here := checkoutHere()
	if store != nil {
		if pinned, _ := store.setting(keyBugRepo); validRepoSlug(pinned) {
			return BugRepo{
				Remote:  "https://github.com/" + pinned,
				Slug:    pinned,
				Dir:     here.Dir,
				Source:  "pinned",
				Ignored: here.Ignored,
			}
		}
		if here.Slug != "" {
			_ = store.setSetting(keyBugRepo, here.Slug)
		}
	}
	return here
}

// bugRepo is resolveBugRepo against this app's store.
func (a *App) bugRepo() BugRepo {
	return resolveBugRepo(a.store)
}

// GetBugRepo is the frontend's read of where reports go.
func (a *App) GetBugRepo() BugRepo {
	return a.bugRepo()
}

// SetBugRepo pins the repository reports are filed against. It takes
// "owner/repo" or any GitHub URL for one; empty unpins, and the next process
// started in a SunoMaker checkout pins that checkout's again.
//
// A repository is checked on GitHub before it is kept, when there is a
// credential to check with: a typo pinned would file every report into a 404
// with nothing on screen to say so. Without a credential nothing can be filed
// either, so the setting is kept and the check happens on the first filing.
func (a *App) SetBugRepo(repo string) (BugRepo, error) {
	if a.store == nil {
		return BugRepo{}, errNoStore
	}
	repo = strings.TrimSpace(repo)
	slug := repo
	if !validRepoSlug(slug) {
		slug = repoSlug(repo)
	}
	if repo != "" && slug == "" {
		return BugRepo{}, fmt.Errorf("%q is not a GitHub repository — use owner/repo", repo)
	}
	if slug != "" {
		if err := checkRepoTakesIssues(slug); err != nil {
			return BugRepo{}, err
		}
	}
	if err := a.store.setSetting(keyBugRepo, slug); err != nil {
		return BugRepo{}, err
	}
	a.store.changed(keyBugRepo)
	return a.bugRepo(), nil
}

// checkRepoTakesIssues asks GitHub whether slug exists, is visible to the
// credential, and has issues switched on. A variable so tests never reach
// GitHub.
var checkRepoTakesIssues = func(slug string) error {
	token := githubToken()
	if token == "" {
		return nil
	}
	var repo struct {
		FullName  string `json:"full_name"`
		HasIssues bool   `json:"has_issues"`
	}
	if err := (issueQueue{slug: slug, token: token}).call("GET", "/repos/"+slug, nil, &repo); err != nil {
		return err
	}
	if !repo.HasIssues {
		return fmt.Errorf("%s has issues switched off, so reports cannot be filed there", slug)
	}
	return nil
}

// validRepoSlug says whether s is already a bare "owner/repo".
func validRepoSlug(s string) bool {
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return false
	}
	for _, p := range parts {
		if p == "" || strings.ContainsAny(p, " \t:@") {
			return false
		}
	}
	return true
}

// repoSlug reduces a GitHub remote to "owner/repo", in either form git writes
// them: https://github.com/owner/repo.git and git@github.com:owner/repo.git.
// Anything else answers "" — a GitLab or a self-hosted remote is not somewhere
// this app knows how to file.
func repoSlug(remote string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return ""
	}
	remote = strings.TrimSuffix(remote, ".git")
	switch {
	case strings.HasPrefix(remote, "git@github.com:"):
		remote = strings.TrimPrefix(remote, "git@github.com:")
	case strings.HasPrefix(remote, "https://github.com/"):
		remote = strings.TrimPrefix(remote, "https://github.com/")
	case strings.HasPrefix(remote, "ssh://git@github.com/"):
		remote = strings.TrimPrefix(remote, "ssh://git@github.com/")
	default:
		return ""
	}
	parts := strings.Split(strings.Trim(remote, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

// isGitHubRemote says whether a remote is one this app can file against.
func isGitHubRemote(remote string) bool {
	return repoSlug(remote) != ""
}
