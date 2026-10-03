// Package platform holds the few things AIMusicMaker does differently on each
// operating system: running a console program without a window, finding a
// command installed after this process started, showing a file in the file
// manager, and hearing the keyboard's media keys. Each has a Windows file and a
// file for everything else.
package platform

// MediaKey is one of the keyboard's media keys. The values are what the
// player receives in its event, so they are words rather than key codes.
type MediaKey string

const (
	MediaPlayPause MediaKey = "playpause"
	MediaNext      MediaKey = "next"
	MediaPrevious  MediaKey = "previous"
	MediaStop      MediaKey = "stop"
)
