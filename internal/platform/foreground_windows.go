//go:build windows

package platform

import (
	"os"
	"unsafe"
)

// InForeground reports whether the window the producer is looking at belongs
// to this process (#113). Asked by process rather than by window handle: Wails
// does not hand the main window's handle out, and the only top-level window
// this app has is the main one.
func InForeground() bool {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return false
	}
	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return int(pid) == os.Getpid()
}

var (
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
)
