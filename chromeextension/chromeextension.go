// Package chromeextension ships the SunoMaker Chrome extension inside the
// binary, which Settings exports to a folder Chrome can load unpacked.
//
// A package of its own because go:embed only reaches files at or below the
// embedding package's directory, and the app's code no longer sits above this
// folder (#99). The patterns name the extension's file types so that this Go
// file is never embedded, and so never exported into the extension.
package chromeextension

import "embed"

// Files holds the extension's files, by file name, with no subfolders.
//
//go:embed *.js *.json *.html *.css *.png
var Files embed.FS
