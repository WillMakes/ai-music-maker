//go:build !windows

package platform

// WatchMediaKeys does nothing outside Windows. The #101 problem was a WebView2
// one, and elsewhere the keys were left to the webview's Media Session — which
// only answers while the webview itself is playing, and since #125 the sound
// is made by this process instead. So on macOS and Linux the keyboard's media
// keys do not reach the player until each has a native hookup of its own
// (MPRemoteCommandCenter, MPRIS).
func WatchMediaKeys(on func(MediaKey)) (stop func(), err error) {
	return func() {}, nil
}
