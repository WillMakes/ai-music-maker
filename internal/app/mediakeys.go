package app

import (
	"time"

	"aimusicmaker/internal/platform"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// watchMediaKeys hears the keyboard's media keys (see platform.WatchMediaKeys).
// A variable so tests substitute one: a test that registered the real keys
// would take them from whatever the producer is listening to.
var watchMediaKeys = platform.WatchMediaKeys

// SetMediaKeys takes the keyboard's media keys while the player has a queue,
// and gives them back when it is cleared (#101). Each press reaches the window
// as a "player:media-key" event carrying "playpause", "next", "previous" or
// "stop".
//
// Driven by the player rather than taken at launch: a held key is taken from
// every other program, and an app that is open but not playing has no business
// pausing the producer's browser. Calls that change nothing do nothing, so the
// player can say so on every queue change. An error means some key is held by
// another program; the rest are working, and the error is returned only by the
// call that took them.
func (a *App) SetMediaKeys(active bool) error {
	a.mediaKeysMu.Lock()
	defer a.mediaKeysMu.Unlock()

	if !active {
		if a.releaseMediaKeys != nil {
			a.releaseMediaKeys()
			a.releaseMediaKeys = nil
		}
		return nil
	}
	if a.releaseMediaKeys != nil {
		return nil
	}
	release, err := watchMediaKeys(a.mediaKeyPressed)
	a.releaseMediaKeys = release
	return err
}

// doublePressGap is how close two Play/Pause presses must be to count as one
// double press (#113) — about what an OS allows for a double click.
const doublePressGap = 400 * time.Millisecond

// mediaKeyNow and toggleWindowFromKey are variables so tests can drive the
// clock and see the window asked for without a window to ask.
var (
	mediaKeyNow         = time.Now
	toggleWindowFromKey = (*App).toggleWindow
)

// mediaKeyPressed passes every press to the player, and a second Play/Pause
// within doublePressGap of the first also raises or puts away the window
// (#113).
//
// The second press still reaches the player rather than being swallowed: the
// pair then toggles playback twice and leaves it where it was, which is what a
// producer reaching for the window expects. Holding the first press back to
// see whether a second follows would put that wait on every ordinary pause.
// A third quick press starts a new pair rather than toggling the window back.
func (a *App) mediaKeyPressed(key platform.MediaKey) {
	a.emit("player:media-key", string(key))
	if key != platform.MediaPlayPause {
		return
	}
	now := mediaKeyNow()
	a.mediaPressMu.Lock()
	double := !a.lastPlayPause.IsZero() && now.Sub(a.lastPlayPause) <= doublePressGap
	if double {
		a.lastPlayPause = time.Time{}
	} else {
		a.lastPlayPause = now
	}
	a.mediaPressMu.Unlock()
	if double {
		toggleWindowFromKey(a)
	}
}

// toggleWindow brings the app to the front, or minimises it when it is
// already the window in front.
//
// Minimised, not hidden: a hidden window has no taskbar button, and the keys
// are only held while something is queued — clear the queue while the app is
// hidden and nothing would bring it back.
func (a *App) toggleWindow() {
	if a.ctx == nil {
		return
	}
	if !wruntime.WindowIsMinimised(a.ctx) && platform.InForeground() {
		wruntime.WindowMinimise(a.ctx)
		return
	}
	a.ShowWindow()
}
