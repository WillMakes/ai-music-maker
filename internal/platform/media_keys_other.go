//go:build !windows

package platform

// WatchMediaKeys does nothing outside Windows. The #101 problem is a WebView2
// one; elsewhere the player's Media Session hookup is left to the webview, and
// taking the keys here as well would answer each press twice.
func WatchMediaKeys(on func(MediaKey)) (stop func(), err error) {
	return func() {}, nil
}
