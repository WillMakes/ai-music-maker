//go:build !windows

package platform

import (
	"os/exec"
	"path/filepath"
	"runtime"
)

// RevealInFileManager opens the platform's file manager on the file.
//
// macOS has an exact equivalent of Explorer's /select — `open -R` reveals the
// file in Finder. Linux has none that can be relied on: the file managers that
// implement org.freedesktop.FileManager1's ShowItems are a subset of the ones
// installed, so xdg-open on the containing folder is the answer that works
// everywhere at the cost of not highlighting anything.
func RevealInFileManager(path string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", "-R", path).Run()
	}
	return exec.Command("xdg-open", filepath.Dir(path)).Run()
}
