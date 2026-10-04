//go:build !windows

package platform

// InForeground answers false off Windows, so a caller deciding between
// raising the window and putting it away always raises it. The media keys
// that ask (#113) are only heard on Windows anyway.
func InForeground() bool { return false }
