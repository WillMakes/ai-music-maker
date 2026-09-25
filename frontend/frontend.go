// Package frontend embeds the built UI, frontend/dist, which the window serves
// and the app map is read from.
//
// A Go file in the Vite project because go:embed only reaches files at or
// below the embedding package's directory, and the app's code lives under
// internal/ (#99). dist must exist before the Go build: `npm run build` or
// `wails build` writes it.
package frontend

import "embed"

// Dist is the production build, with paths starting "dist/".
//
//go:embed all:dist
var Dist embed.FS
