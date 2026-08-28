package main

import (
	"embed"
	"log"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// `suno-maker bugs …` is the bug queue's agent surface (see bugcli.go),
	// handled before any window exists. An agent working the queue has a
	// terminal and nothing else, and opening a webview to answer `bugs list`
	// would be the wrong shape entirely.
	if code, handled := runBugCLI(os.Args[1:]); handled {
		os.Exit(code)
	}

	// `suno-maker mcp` is the same surface offered over MCP, for the AI clients
	// Settings registers this app with (see mcpconfig.go). It speaks JSON-RPC on
	// stdio, so nothing may be written to stdout but responses — which is why
	// it, too, returns before any window exists.
	if code, handled := runMCPServer(os.Args[1:], os.Stdin, os.Stdout); handled {
		os.Exit(code)
	}

	// `suno-maker docs` prints the app's documentation of itself (see
	// appdocs.go) — the same document the MCP tool serves, for a terminal.
	if code, handled := runDocsCLI(os.Args[1:]); handled {
		os.Exit(code)
	}

	app := NewApp()

	// The window background is painted before the webview has anything in it,
	// so it matches the dark theme's --sm-bg (#0d0d0d). A light-theme user sees
	// it for the length of one frame; a dark-theme user would otherwise see a
	// white flash on every launch.
	err := wails.Run(&options.App{
		Title:     "SunoMaker",
		Width:     1280,
		Height:    840,
		MinWidth:  960,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 13, G: 13, B: 13, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
		// Per-platform options. The app is one codebase on three window
		// managers, and each needs saying to explicitly — the defaults differ in
		// ways that show (WebView2's own theme, GTK's program name in the
		// taskbar, the macOS about panel).
		Windows: &windows.Options{
			// Let WebView2 follow the OS theme rather than forcing light; the
			// in-app theme is class-based on top of it (see ThemeProvider).
			Theme: windows.SystemDefault,
		},
		Mac: &mac.Options{
			// Default rather than hidden-inset: the traffic lights would land on
			// top of the app's own top bar, and moving them is a design decision
			// rather than a scaffolding one.
			TitleBar: mac.TitleBarDefault(),
			About: &mac.AboutInfo{
				Title:   "SunoMaker",
				Message: "Plan artists, albums and tracks for Suno.",
			},
		},
		Linux: &linux.Options{
			ProgramName: "suno-maker",
			// Translucency is off deliberately: on several compositors it leaves
			// the webview compositing against the desktop and the surface tokens
			// stop reading as opaque panels.
			WindowIsTranslucent: false,
		},
	})

	if err != nil {
		log.Printf("suno-maker: %v", err)
	}
}
