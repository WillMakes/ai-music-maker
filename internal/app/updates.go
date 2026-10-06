package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"aimusicmaker/internal/platform"
	"aimusicmaker/internal/updater"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// releaseRepo is where releases are published. A constant rather than the bug
// repository setting: that one can be pointed at a fork to file reports
// there, and a fork's releases are not this build's updates.
const releaseRepo = "WillMakes/ai-music-maker"

// UpdateStatus is everything the update button and dialog show (#132).
type UpdateStatus struct {
	Current string `json:"current"`
	// Latest is the newest release's version, "" until a check has answered.
	Latest string `json:"latest"`
	// Available is true only when Latest is newer AND has a verifiable file
	// for this platform: an update that cannot be installed is not offered.
	Available   bool   `json:"available"`
	Notes       string `json:"notes"`
	URL         string `json:"url"`
	PublishedAt string `json:"publishedAt"`
	AssetName   string `json:"assetName"`
	AssetSize   int64  `json:"assetSize"`
	CheckedAt   string `json:"checkedAt"`
	// Problem is why the check could not answer, in words. A check that fails
	// is not an error to the caller: nothing about reaching GitHub may get in
	// the way of the app's own work, and the button simply does not appear.
	Problem string `json:"problem"`
	// DevBuild says this is a developer's own build, which never updates
	// itself — it was not installed from a release.
	DevBuild bool `json:"devBuild"`
	// Downloaded is the verified installer on disk, once fetched.
	Downloaded string `json:"downloaded"`
}

// updateState is the last check and what it found, kept so the download step
// works from what the dialog showed rather than from a second, possibly
// different, answer.
type updateState struct {
	mu         sync.Mutex
	status     UpdateStatus
	release    updater.Release
	downloaded string
}

// updateClient builds the client for the release repository. A variable so a
// test can point it at a local server.
var updateClient = func() updater.Client {
	return updater.Client{
		Repo:      releaseRepo,
		Token:     githubToken(),
		UserAgent: "AIMusicMaker/" + appVersion,
	}
}

// CheckForUpdate asks GitHub for the newest release.
func (a *App) CheckForUpdate() UpdateStatus {
	status := UpdateStatus{
		Current:   appVersion,
		CheckedAt: nowStamp(),
		DevBuild:  !updater.IsVersion(appVersion),
	}
	if status.DevBuild {
		status.Problem = "this is a development build, which updates from source rather than from releases"
		return a.keepUpdateStatus(status, updater.Release{})
	}

	release, err := updateClient().Latest(context.Background())
	if err != nil {
		status.Problem = err.Error()
		return a.keepUpdateStatus(status, updater.Release{})
	}
	status.Latest = release.Version
	status.Notes = release.Notes
	status.URL = release.URL
	status.PublishedAt = release.PublishedAt
	status.AssetName = release.Asset.Name
	status.AssetSize = release.Asset.Size
	newer := updater.Newer(release.Version, appVersion)
	status.Available = newer && release.Asset.Name != ""
	if newer && release.Asset.Name == "" {
		status.Problem = "the newest release has no installer for this platform"
	}
	return a.keepUpdateStatus(status, release)
}

func (a *App) keepUpdateStatus(status UpdateStatus, release updater.Release) UpdateStatus {
	a.updates.mu.Lock()
	defer a.updates.mu.Unlock()
	// A check that could not answer changes nothing about the release last
	// found, so an installer already fetched is not forgotten over a dropped
	// connection. A different release since then does make it stale.
	if release.Version != "" && release.Version != a.updates.release.Version {
		a.updates.release = release
		a.updates.downloaded = ""
	}
	status.Downloaded = a.updates.downloaded
	a.updates.status = status
	return status
}

// DownloadUpdate fetches the installer the last check found into the
// Downloads folder, verified against the release's checksum. Progress arrives
// as "update:progress" events carrying the fraction done.
func (a *App) DownloadUpdate() (UpdateStatus, error) {
	a.updates.mu.Lock()
	status, release := a.updates.status, a.updates.release
	a.updates.mu.Unlock()
	if !status.Available {
		return status, errors.New("there is no update to download — check again first")
	}

	path, err := updateClient().Download(context.Background(), release.Asset, downloadsDir(),
		func(done, total int64) {
			if total > 0 {
				a.emit("update:progress", float64(done)/float64(total))
			}
		})
	if err != nil {
		return status, err
	}

	a.updates.mu.Lock()
	defer a.updates.mu.Unlock()
	a.updates.downloaded = path
	a.updates.status.Downloaded = path
	return a.updates.status, nil
}

// InstallUpdate opens what DownloadUpdate fetched, the way each platform
// installs.
//
// On Windows that is the setup program, which updates the installed copy in
// place (#131) — and this window closes so it can, since a running exe cannot
// be overwritten. Through cmd's start, which goes by ShellExecute: the
// installer asks for elevation, and CreateProcess refuses a program that does.
// On macOS the disk image opens, to drag the app over the old one. Linux's
// release is an archive, so its folder is shown.
func (a *App) InstallUpdate() error {
	a.updates.mu.Lock()
	path := a.updates.downloaded
	a.updates.mu.Unlock()
	if path == "" {
		return errors.New("download the update first")
	}
	if _, err := os.Stat(path); err != nil {
		return errors.New("the downloaded installer is gone — download it again")
	}

	switch runtime.GOOS {
	case "windows":
		cmd := exec.Command("cmd", "/c", "start", "", path)
		platform.HideWindow(cmd)
		if err := cmd.Start(); err != nil {
			return err
		}
		if a.ctx != nil {
			// A moment for the installer's window to come up before this one
			// goes, so the update does not look like the app just vanished.
			go func(ctx context.Context) {
				time.Sleep(1500 * time.Millisecond)
				wruntime.Quit(ctx)
			}(a.ctx)
		}
		return nil
	case "darwin":
		return exec.Command("open", path).Start()
	default:
		return platform.RevealInFileManager(path)
	}
}

// downloadsDir is where installers are saved: the producer's Downloads, where
// they would look for one, or the temp folder where there is none.
func downloadsDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		dir := filepath.Join(home, "Downloads")
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	return os.TempDir()
}
