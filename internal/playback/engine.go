package playback

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ebitengine/oto/v3"
)

// State is what the window's player mirrors: the element it used to read
// (#45) answered the same questions.
type State struct {
	// Generation names the load this state is about, so a report about the
	// song before is dropped rather than shown against the new one.
	Generation int     `json:"generation"`
	Loaded     bool    `json:"loaded"`
	Playing    bool    `json:"playing"`
	Position   float64 `json:"position"`
	Duration   float64 `json:"duration"`
	// Ended is true once, when the take has played to its end.
	Ended bool   `json:"ended"`
	Error string `json:"error"`
}

// tick is how often the state is reported while a take is loaded — about the
// rate an <audio> element fires timeupdate.
const tick = 250 * time.Millisecond

// Engine is the speaker: one take at a time, played from this process.
type Engine struct {
	mu      sync.Mutex
	ctx     *oto.Context
	player  *oto.Player
	src     *tracked
	gen     int
	paused  bool
	ended   bool
	onState func(State)
	stop    chan struct{}
}

// New makes an engine that reports through onState. The audio device is
// opened on the first Load, not here: an app that never plays a song should
// not hold the device, or fail to start where there is none.
func New(onState func(State)) *Engine {
	if onState == nil {
		onState = func(State) {}
	}
	return &Engine{onState: onState}
}

// tracked counts how far the output has read, for the position.
type tracked struct {
	pcm
	read atomic.Int64 // bytes
	eof  atomic.Bool
}

func (t *tracked) Read(p []byte) (int, error) {
	n, err := t.pcm.Read(p)
	t.read.Add(int64(n))
	if err == io.EOF {
		t.eof.Store(true)
	}
	return n, err
}

func (t *tracked) Seek(offset int64, whence int) (int64, error) {
	at, err := t.pcm.Seek(offset, whence)
	if err == nil {
		t.read.Store(at)
		t.eof.Store(false)
	}
	return at, err
}

// openContext opens the audio device once for the life of the process: oto
// allows one context, at one rate.
func (e *Engine) openContext() error {
	if e.ctx != nil {
		return nil
	}
	ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:      OutputRate,
		ChannelCount:    2,
		Format:          oto.FormatSignedInt16LE,
		ApplicationName: "AIMusicMaker",
	})
	if err != nil {
		return fmt.Errorf("the audio output could not be opened: %w", err)
	}
	<-ready
	e.ctx = ctx
	return nil
}

// Load replaces whatever is loaded with the take at path, paused at its
// start, and returns the new generation and the take's length in seconds.
func (e *Engine) Load(path string) (int, float64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.openContext(); err != nil {
		return 0, 0, err
	}
	e.unload()
	src, err := open(path)
	if err != nil {
		return 0, 0, err
	}
	e.gen++
	e.src = &tracked{pcm: withRate(src)}
	e.player = e.ctx.NewPlayer(e.src)
	e.paused, e.ended = true, false
	e.stop = make(chan struct{})
	go e.watch(e.gen, e.stop)
	return e.gen, e.durationLocked(), nil
}

// unload closes the current take. Called with mu held.
func (e *Engine) unload() {
	if e.stop != nil {
		close(e.stop)
		e.stop = nil
	}
	if e.player != nil {
		e.player.Pause()
		_ = e.player.Close()
		e.player = nil
	}
	if e.src != nil {
		_ = e.src.Close()
		e.src = nil
	}
}

var errNothingLoaded = errors.New("nothing is loaded to play")

// Play starts or resumes. At the end of a take it starts it again, the way
// play on an ended <audio> element does.
func (e *Engine) Play() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.player == nil {
		return errNothingLoaded
	}
	if e.ended {
		if _, err := e.player.Seek(0, io.SeekStart); err != nil {
			return err
		}
		e.ended = false
	}
	e.paused = false
	e.player.Play()
	e.reportLocked()
	return nil
}

// Pause holds the take where it is.
func (e *Engine) Pause() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.player == nil {
		return
	}
	e.paused = true
	e.player.Pause()
	e.reportLocked()
}

// Seek moves to seconds into the take, playing on if it was playing.
func (e *Engine) Seek(seconds float64) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.player == nil {
		return errNothingLoaded
	}
	frame := int64(max(0, seconds) * OutputRate)
	if total := e.src.Frames(); total > 0 && frame > total {
		frame = total
	}
	if _, err := e.player.Seek(frame*bytesPerFrame, io.SeekStart); err != nil {
		return fmt.Errorf("could not seek in the take: %w", err)
	}
	e.ended = false
	if !e.paused {
		e.player.Play()
	}
	e.reportLocked()
	return nil
}

// Stop unloads the take, leaving the device open for the next.
func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.unload()
	e.onState(State{Generation: e.gen})
}

// Close releases everything, for shutdown.
func (e *Engine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.unload()
}

// watch reports the state every tick for one load, and notices the end.
func (e *Engine) watch(gen int, stop chan struct{}) {
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		e.mu.Lock()
		if e.gen != gen || e.player == nil {
			e.mu.Unlock()
			return
		}
		// The output stops on its own once the source is spent and its
		// buffer drained; a pause is ours and is not an ending.
		if !e.paused && !e.ended && e.src.eof.Load() && !e.player.IsPlaying() {
			e.ended, e.paused = true, true
		}
		e.reportLocked()
		e.mu.Unlock()
	}
}

// reportLocked sends the current state. Called with mu held; the end is
// reported once, on the tick that noticed it.
func (e *Engine) reportLocked() {
	s := State{Generation: e.gen, Loaded: e.player != nil}
	if e.player != nil {
		s.Playing = !e.paused && e.player.IsPlaying()
		s.Position = e.positionLocked()
		s.Duration = e.durationLocked()
		s.Ended = e.ended
		if err := e.player.Err(); err != nil {
			s.Error = err.Error()
		}
	}
	e.onState(s)
}

func (e *Engine) positionLocked() float64 {
	played := e.src.read.Load() - int64(e.player.BufferedSize())
	if e.ended {
		played = e.src.read.Load()
	}
	return float64(max(0, played)) / (OutputRate * bytesPerFrame)
}

func (e *Engine) durationLocked() float64 {
	if e.src == nil {
		return 0
	}
	return float64(e.src.Frames()) / OutputRate
}
