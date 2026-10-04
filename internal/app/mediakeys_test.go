package app

import (
	"errors"
	"testing"
	"time"

	"aimusicmaker/internal/platform"
)

// stubMediaKeys swaps the OS watcher for one that records what it was asked
// to do, so no test ever registers the producer's real media keys. It returns
// a function that presses a key, as the OS would.
func stubMediaKeys(t *testing.T, startErr error) (starts, stops *int, press func(platform.MediaKey)) {
	t.Helper()
	starts, stops = new(int), new(int)
	var handler func(platform.MediaKey)
	original := watchMediaKeys
	watchMediaKeys = func(on func(platform.MediaKey)) (func(), error) {
		*starts++
		handler = on
		return func() { *stops++ }, startErr
	}
	t.Cleanup(func() { watchMediaKeys = original })
	return starts, stops, func(k platform.MediaKey) { handler(k) }
}

// TestMediaKeysFollowTheQueue is #101. The #72 fix asked WebView2 for the OS
// media controls through WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS, but Wails
// creates the webview with its own Go loader, which never reads that variable,
// so the keys never arrived. The app now holds the keys itself while the
// player has something queued, and lets them go when the queue is cleared.
func TestMediaKeysFollowTheQueue(t *testing.T) {
	a := newTestApp(t)
	starts, stops, press := stubMediaKeys(t, nil)

	if err := a.SetMediaKeys(true); err != nil {
		t.Fatal(err)
	}
	// The player says so every time its queue changes; the keys are taken
	// once, not once per call.
	if err := a.SetMediaKeys(true); err != nil {
		t.Fatal(err)
	}
	if *starts != 1 {
		t.Fatalf("the keys were registered %d times, want once", *starts)
	}

	// A press with no window behind it (the CLI, the MCP server) must not
	// panic on the missing context.
	press(platform.MediaPlayPause)

	if err := a.SetMediaKeys(false); err != nil {
		t.Fatal(err)
	}
	if err := a.SetMediaKeys(false); err != nil {
		t.Fatal(err)
	}
	if *stops != 1 {
		t.Fatalf("the keys were released %d times, want once", *stops)
	}

	// A queue started again takes them back.
	if err := a.SetMediaKeys(true); err != nil {
		t.Fatal(err)
	}
	if *starts != 2 {
		t.Fatalf("the keys were not taken again: %d registrations", *starts)
	}
}

// TestMediaKeysPartlyHeld: another player can already hold one of the keys.
// The ones that registered still work and stay registered, and the producer
// is told which key is not theirs.
func TestMediaKeysPartlyHeld(t *testing.T) {
	a := newTestApp(t)
	held := errors.New("another program holds the Play/Pause key")
	starts, stops, _ := stubMediaKeys(t, held)

	if err := a.SetMediaKeys(true); !errors.Is(err, held) {
		t.Fatalf("err = %v, want the held-key error", err)
	}
	// Reported once, when the keys were taken; not on every queue change.
	if err := a.SetMediaKeys(true); err != nil {
		t.Fatalf("a second call repeated the error: %v", err)
	}
	if *starts != 1 {
		t.Fatalf("registered %d times", *starts)
	}
	a.shutdown(nil)
	if *stops != 1 {
		t.Fatalf("shutdown did not release the keys: %d", *stops)
	}
}

// TestDoublePlayPauseTogglesTheWindow is #113: two Play/Pause presses close
// together raise the app (or put it away), while every press still reaches
// the player, so the pair leaves playback where it was.
func TestDoublePlayPauseTogglesTheWindow(t *testing.T) {
	a := newTestApp(t)
	_, _, press := stubMediaKeys(t, nil)

	clock := time.Unix(1_000_000, 0)
	originalNow, originalToggle := mediaKeyNow, toggleWindowFromKey
	mediaKeyNow = func() time.Time { return clock }
	toggles := 0
	toggleWindowFromKey = func(*App) { toggles++ }
	t.Cleanup(func() { mediaKeyNow, toggleWindowFromKey = originalNow, originalToggle })

	if err := a.SetMediaKeys(true); err != nil {
		t.Fatal(err)
	}
	at := func(gap time.Duration, key platform.MediaKey) {
		clock = clock.Add(gap)
		press(key)
	}

	at(0, platform.MediaPlayPause)
	if toggles != 0 {
		t.Fatal("a single press toggled the window")
	}
	at(200*time.Millisecond, platform.MediaPlayPause)
	if toggles != 1 {
		t.Fatalf("a double press toggled the window %d times, want once", toggles)
	}
	// A third quick press opens a new pair; it must not flip the window back.
	at(200*time.Millisecond, platform.MediaPlayPause)
	if toggles != 1 {
		t.Fatal("a third quick press toggled the window again")
	}

	// Presses further apart are two ordinary pauses.
	at(time.Second, platform.MediaPlayPause)
	at(time.Second, platform.MediaPlayPause)
	if toggles != 1 {
		t.Fatal("presses a second apart toggled the window")
	}

	// Only Play/Pause counts: Next then Play/Pause is not a double press.
	at(time.Second, platform.MediaNext)
	at(100*time.Millisecond, platform.MediaPlayPause)
	if toggles != 1 {
		t.Fatal("Next followed by Play/Pause toggled the window")
	}
	at(100*time.Millisecond, platform.MediaPlayPause)
	if toggles != 2 {
		t.Fatalf("toggles = %d after a second double press, want 2", toggles)
	}
}
