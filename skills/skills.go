// Package skills ships the built-in skills' text inside the binary.
//
// The briefs are markdown documents, edited as documents, so they live here as
// .md files rather than as Go string literals. A package of their own because
// go:embed only reaches files at or below the embedding package's directory,
// and the app's code no longer sits above this folder (#99).
package skills

import "embed"

// Files holds every built-in brief, by file name.
//
//go:embed *.md
var Files embed.FS

// BugReports is the bug-queue workflow that `bugs workflow` prints. The same
// file is the built-in bug-reports skill.
//
//go:embed bug-reports.md
var BugReports string
