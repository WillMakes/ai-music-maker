package app

import (
	"aimusicmaker/internal/platform"
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
	release, err := watchMediaKeys(func(key platform.MediaKey) {
		a.emit("player:media-key", string(key))
	})
	a.releaseMediaKeys = release
	return err
}
