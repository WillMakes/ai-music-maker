// SunoMaker: plan music before Suno makes it, and keep what Suno gives back.
//
// The app lives in internal/app. This file is only the entry point Wails and
// `go run .` build, so the repository root holds no other Go code (#99).
package main

import "sunomaker/internal/app"

func main() {
	app.Main()
}
