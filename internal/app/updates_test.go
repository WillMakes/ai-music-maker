package app

import (
	"testing"

	"aimusicmaker/internal/updater"
)

// A developer's own build answers from what it is, without reaching GitHub:
// it was not installed from a release, and `go test` must make no network
// call on the producer's credential.
func TestDevBuildDoesNotCheckForUpdates(t *testing.T) {
	app := newTestApp(t)
	previousVersion, previousClient := appVersion, updateClient
	t.Cleanup(func() { appVersion, updateClient = previousVersion, previousClient })
	appVersion = "dev"
	updateClient = func() updater.Client {
		t.Fatal("a dev build reached for the release repository")
		return updater.Client{}
	}

	status := app.CheckForUpdate()
	if !status.DevBuild || status.Available || status.Problem == "" {
		t.Fatalf("dev build answered %+v", status)
	}
	if _, err := app.DownloadUpdate(); err == nil {
		t.Fatal("a dev build was offered a download")
	}
}
