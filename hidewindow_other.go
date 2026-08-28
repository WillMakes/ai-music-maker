//go:build !windows

package main

import "os/exec"

// hideWindow does nothing off Windows: macOS and Linux do not give a console
// program a window of its own, so there is none to hide.
func hideWindow(*exec.Cmd) {}
