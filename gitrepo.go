package main

import (
	"os"
	"os/exec"
	"strings"
)

// ---------------------------------------------------------------------------
// Which repository this build came from
//
// Reports are filed as issues on the repository the running checkout points at,
// and that is read from git rather than stored anywhere.
//
// Stored would be a fact that can be wrong: a fork, a clone under a different
// account, a remote renamed after the setting was written. Where the checkout
// points is a fact about the directory the binary is being run from, and reading
// it costs one `git remote get-url` — cheap enough to do on demand and always
// right.
//
// An installed build run from anywhere is not in a checkout at all. It gets no
// slug, files no issues, and keeps its reports locally — which is the correct
// behaviour rather than a degraded one: a build somebody else installed should
// not be opening issues on the developer's repository.
// ---------------------------------------------------------------------------

// BugRepo is where reports are filed, for Settings and for `bugs repo`.
type BugRepo struct {
	// Remote is the origin URL, as git states it.
	Remote string `json:"remote"`
	// Slug is "owner/repo", "" when the remote is not GitHub or there is none.
	Slug string `json:"slug"`
	// Dir is the checkout the answer came from, for the panel to show.
	Dir string `json:"dir"`
}

// currentBugRepo reads the working directory's origin.
func currentBugRepo() BugRepo {
	dir, err := os.Getwd()
	if err != nil {
		return BugRepo{}
	}
	git, found := lookUpCommand("git")
	if !found {
		return BugRepo{Dir: dir}
	}
	cmd := exec.Command(git, "remote", "get-url", "origin")
	cmd.Dir = dir
	hideWindow(cmd)
	// Output rather than CombinedOutput: outside a checkout git writes its
	// complaint to stderr and exits non-zero, and folding that in would hand back
	// an error message as a URL.
	out, err := cmd.Output()
	if err != nil {
		return BugRepo{Dir: dir}
	}
	remote := strings.TrimSpace(string(out))
	return BugRepo{Remote: remote, Slug: repoSlug(remote), Dir: dir}
}

// GetBugRepo is the frontend's read of the same.
func (a *App) GetBugRepo() BugRepo {
	return currentBugRepo()
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
