package app

import (
	"errors"
	"sync"

	"aimusicmaker/internal/playback"
)

// The window's speaker (#125). The queue, shuffle and repeat stay in the
// window's player; these bindings are what its <audio> element used to be —
// load a take, play, pause, seek — with the sound made by this process, so
// it is the app's for everything that listens per application (OBS's
// Application Audio Capture, the OS volume mixer). The engine reports its
// state as "player:state" events, about as often as an element fires
// timeupdate.

var (
	enginesMu sync.Mutex
	// newEngine is playback.New, a variable so a test never opens the
	// producer's audio device.
	newEngine = playback.New
)

// engine is the app's one speaker, made on first use: an app that never
// plays a song does not open the audio device.
func (a *App) engine() *playback.Engine {
	enginesMu.Lock()
	defer enginesMu.Unlock()
	if a.speaker == nil {
		a.speaker = newEngine(func(s playback.State) { a.emit("player:state", s) })
	}
	return a.speaker
}

// PlayerLoaded is what a load answers: the generation every state event about
// this take will carry, and its length.
type PlayerLoaded struct {
	Generation int     `json:"generation"`
	Duration   float64 `json:"duration"`
}

// PlayerLoad loads one take, paused at its start, replacing whatever was
// loaded.
func (a *App) PlayerLoad(fileID string) (PlayerLoaded, error) {
	if a.store == nil {
		return PlayerLoaded{}, errNoStore
	}
	path, err := a.store.trackFilePath(fileID)
	if err != nil || path == "" {
		return PlayerLoaded{}, errors.New("this take is not in the library any more")
	}
	gen, duration, err := a.engine().Load(path)
	if err != nil {
		return PlayerLoaded{}, err
	}
	return PlayerLoaded{Generation: gen, Duration: duration}, nil
}

// PlayerPlay starts or resumes the loaded take.
func (a *App) PlayerPlay() error { return a.engine().Play() }

// PlayerPause holds it where it is.
func (a *App) PlayerPause() { a.engine().Pause() }

// PlayerSeek moves to seconds into it.
func (a *App) PlayerSeek(seconds float64) error { return a.engine().Seek(seconds) }

// PlayerStop unloads it — the queue was cleared.
func (a *App) PlayerStop() { a.engine().Stop() }
