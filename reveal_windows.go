//go:build windows

package main

import (
	"os/exec"
	"path/filepath"
)

// revealInFileManager opens Explorer with the file selected.
//
// /select, takes the file itself rather than its folder, which is what makes it
// highlight the file rather than merely open the directory. The path is passed
// as one argument and not shell-quoted: exec.Command does not go through cmd, so
// a space in "My Music" needs no handling here.
//
// The error is ignored deliberately. Explorer returns a non-zero exit code on
// success often enough that reporting it would put a false failure in front of
// the producer while the window they asked for is already open.
func revealInFileManager(path string) error {
	_ = exec.Command("explorer.exe", "/select,"+filepath.Clean(path)).Run()
	return nil
}
