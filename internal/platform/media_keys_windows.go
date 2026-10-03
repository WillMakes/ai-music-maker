//go:build windows

package platform

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// WatchMediaKeys registers the keyboard's media keys as system-wide hotkeys
// and calls on for each press, until stop is called (#101).
//
// The obvious route was the webview: Chromium hands media keys to a page's
// Media Session once its HardwareMediaKeyHandling feature is on, and WebView2
// takes that through WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS (#72). But that
// variable is read by Microsoft's WebView2Loader.dll, and Wails creates the
// webview with go-webview2's own Go loader, which never reads it — the running
// browser process's command line shows only the arguments Wails passed itself.
// Wails has no option for extra browser arguments, and its legacy native loader
// is on the way out, so the keys are heard here instead, outside the webview.
//
// RegisterHotKey takes a key away from every other program while it is held,
// which is why the caller holds them only while the player has a queue — the
// same thing a browser does while one of its tabs is playing. A key another
// program already holds cannot be taken; the others still are, and the error
// says which one is missing.
func WatchMediaKeys(on func(MediaKey)) (stop func(), err error) {
	type started struct {
		thread uint32
		err    error
	}
	ready := make(chan started, 1)
	done := make(chan struct{})

	go func() {
		// A hotkey belongs to the thread that registered it, and its WM_HOTKEY
		// arrives in that thread's message queue — so the registering, the
		// loop and the unregistering must all happen on one OS thread.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(done)

		var registered []int
		var held []string
		for i, k := range mediaHotkeys {
			id := i + 1
			r, _, _ := procRegisterHotKey.Call(0, uintptr(id), modNoRepeat, k.vk)
			if r == 0 {
				held = append(held, k.label)
				continue
			}
			registered = append(registered, id)
		}
		var heldErr error
		if len(held) > 0 {
			heldErr = fmt.Errorf("another program holds the %s media key", strings.Join(held, ", "))
		}
		if len(registered) == 0 {
			ready <- started{err: heldErr}
			return
		}
		// RegisterHotKey created this thread's message queue, so a quit posted
		// from now on has somewhere to land.
		ready <- started{thread: windows.GetCurrentThreadId(), err: heldErr}

		var m winMsg
		for {
			r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(r) <= 0 { // WM_QUIT, or a failure that would only repeat
				break
			}
			if m.message == wmHotkey {
				if i := int(m.wParam) - 1; i >= 0 && i < len(mediaHotkeys) {
					on(mediaHotkeys[i].key)
				}
			}
		}
		for _, id := range registered {
			procUnregisterHotKey.Call(0, uintptr(id))
		}
	}()

	s := <-ready
	if s.thread == 0 {
		return nil, s.err
	}
	var once sync.Once
	stop = func() {
		once.Do(func() {
			procPostThreadMessageW.Call(uintptr(s.thread), wmQuit, 0, 0)
			<-done
		})
	}
	return stop, s.err
}

var mediaHotkeys = []struct {
	key   MediaKey
	vk    uintptr
	label string
}{
	{MediaPlayPause, 0xB3, "Play/Pause"}, // VK_MEDIA_PLAY_PAUSE
	{MediaNext, 0xB0, "Next track"},      // VK_MEDIA_NEXT_TRACK
	{MediaPrevious, 0xB1, "Previous track"},
	{MediaStop, 0xB2, "Stop"},
}

const (
	wmHotkey = 0x0312
	wmQuit   = 0x0012
	// Holding a key down would otherwise fire it repeatedly — a held
	// Play/Pause flickering between the two.
	modNoRepeat = 0x4000
)

// winMsg is Win32's MSG.
type winMsg struct {
	hwnd     uintptr
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       struct{ x, y int32 }
	lPrivate uint32
}

var (
	user32                 = windows.NewLazySystemDLL("user32.dll")
	procRegisterHotKey     = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey   = user32.NewProc("UnregisterHotKey")
	procGetMessageW        = user32.NewProc("GetMessageW")
	procPostThreadMessageW = user32.NewProc("PostThreadMessageW")
)
