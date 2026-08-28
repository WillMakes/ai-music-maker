package main

import (
	"runtime"
	"runtime/debug"
)

// appVersion is what this build calls itself. Set at link time by a release
// build (-ldflags "-X main.appVersion=1.2.0"); "dev" is what a developer's own
// build honestly is, and saying so is more use than a version number nobody
// released.
var appVersion = "dev"

// BuildInfo is the About panel's whole content.
type BuildInfo struct {
	Version string `json:"version"`
	// Commit is the revision this build was made from, as Go's own build
	// information records it. Empty for a `go run`, which has no revision to
	// stamp — a fact worth showing as "unknown" rather than hiding.
	Commit string `json:"commit"`
	// Dirty says the working copy had uncommitted changes when it was built, so
	// "this is commit abc123" is not the whole truth about what is running.
	Dirty     bool   `json:"dirty"`
	GoVersion string `json:"goVersion"`
	Platform  string `json:"platform"`
	// Repo is where reports are filed and where the source is, joined here so
	// About has one call rather than two.
	Repo BugRepo `json:"repo"`
}

// GetBuildInfo reports what is running.
func (a *App) GetBuildInfo() BuildInfo {
	info := BuildInfo{
		Version:   appVersion,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
		Repo:      currentBugRepo(),
	}
	built, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}
	for _, setting := range built.Settings {
		switch setting.Key {
		case "vcs.revision":
			info.Commit = setting.Value
		case "vcs.modified":
			info.Dirty = setting.Value == "true"
		}
	}
	return info
}
