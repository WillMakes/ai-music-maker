//go:build !windows

package platform

import "os/exec"

// HideWindow does nothing off Windows: macOS and Linux do not give a console
// program a window of its own, so there is none to hide.
func HideWindow(*exec.Cmd) {}
