package main

import (
	"context"
	"log"
	"sync"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the Wails binding surface: every method exported here is callable from
// the frontend, and nothing else is. Domain files hang their bindings off this
// type rather than defining types of their own, which is what keeps
// frontend/wailsjs a single generated module.
type App struct {
	ctx   context.Context
	store *Store

	// cancel stops the background supervisors started in startup. Held so
	// shutdown can end them before the window goes; a supervisor still running
	// after the webview is gone would emit events into nothing.
	cancel context.CancelFunc

	// toJax files a bug report into Jax and returns the id it was given there
	// (see jax.go). A field rather than a direct call so tests can substitute
	// one — filing reaches out to another application on loopback, and a test
	// suite must never depend on whether the producer has Jax open. app is the
	// repository the report belongs to, which is how Jax names this app (#94).
	toJax func(report BugReport, app string) (int, error)

	// emitMu serialises the "data:changed" emits. The store's onChange hook is
	// called from whichever goroutine wrote — a bridge request, an MCP call, the
	// window — and Wails' event emitter is not documented as safe under that.
	emitMu sync.Mutex
}

// NewApp builds the app and opens its database.
//
// A store that will not open is not fatal. The producer still gets a window,
// Settings still explains itself, and every read answers empty — which is a far
// better failure than a launch that does nothing and says nothing. Writes report
// errNoStore, so nothing appears to have been saved when it was not.
func NewApp() *App {
	a := &App{toJax: fileReportInJax}
	store, err := openStore()
	if err != nil {
		log.Printf("suno-maker: could not open the database: %v", err)
		return a
	}
	a.store = store
	return a
}

// startup is Wails' OnStartup. It keeps the context every binding needs to talk
// to the window, wires the store's change hook to the frontend event, and starts
// the background supervisors.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	supervised, cancel := context.WithCancel(ctx)
	a.cancel = cancel

	if a.store != nil {
		a.store.onChange = func(scope string) {
			a.emitMu.Lock()
			defer a.emitMu.Unlock()
			// Guarded because a write can land before the window is up — the
			// extension bridge answers as soon as it is listening, and Wails
			// panics on an emit with no context behind it.
			if a.ctx == nil {
				return
			}
			wruntime.EventsEmit(a.ctx, "data:changed", scope)
		}
	}

	// The loopback server the browser extension talks to (see
	// extensionbridge.go). Started here rather than lazily because the extension
	// asks whether the app is running before it draws anything, and "the app is
	// open but has not been asked anything yet" must not read as closed.
	go a.superviseExtensionBridge(supervised)

	// Reports filed while the app was closed — by an agent through the CLI, or
	// by the MCP server — have no Jax counterpart yet. Catching them up on
	// launch is the only moment this process can (see jax.go).
	go a.catchUpJaxFilings(supervised)
}

// shutdown is Wails' OnShutdown: stop the supervisors, then close the database.
// In that order — a supervisor mid-write against a closed handle would log an
// error about the shutdown rather than about anything real.
func (a *App) shutdown(_ context.Context) {
	if a.cancel != nil {
		a.cancel()
	}
	if a.store != nil {
		if err := a.store.Close(); err != nil {
			log.Printf("suno-maker: closing the database: %v", err)
		}
	}
}

// emit sends an event to the window, doing nothing when there is no window —
// which is the ordinary state of the MCP server and the bug CLI, both of which
// run this same binary with no webview at all.
func (a *App) emit(name string, data ...any) {
	if a.ctx == nil {
		return
	}
	wruntime.EventsEmit(a.ctx, name, data...)
}

// ShowWindow raises the app and brings it to the front. Called by the browser
// extension through the bridge, so a producer who found something in Suno can
// get back to the record it belongs to without hunting for the window.
func (a *App) ShowWindow() {
	if a.ctx == nil {
		return
	}
	wruntime.WindowUnminimise(a.ctx)
	wruntime.WindowShow(a.ctx)
}

// OpenURL opens a link in the producer's own browser rather than in the webview.
// Every external link in this app goes through here: a YouTube reference opened
// inside the window would replace the app with a video and leave no way back,
// the webview having no address bar and no Back button of its own.
func (a *App) OpenURL(url string) {
	if a.ctx == nil || url == "" {
		return
	}
	wruntime.BrowserOpenURL(a.ctx, url)
}
