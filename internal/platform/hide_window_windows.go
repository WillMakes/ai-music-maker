//go:build windows

package platform

import (
	"os/exec"
	"syscall"
)

// HideWindow stops a child process flashing a console window.
//
// Every CLI this app runs is a console program, and Windows gives one a window
// unless it is told not to. Without this a producer sees a black rectangle
// appear and vanish on every interview turn, which reads as something having
// gone wrong.
func HideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
