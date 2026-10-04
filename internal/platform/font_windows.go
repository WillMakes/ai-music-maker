//go:build windows

package platform

import (
	"os"
	"path/filepath"
)

// FontFile is a font a music video's titles are drawn in (#118), or "" when
// none of the expected ones is installed — the video is then drawn without
// titles rather than not at all. ffmpeg's drawtext takes a file rather than a
// family name on Windows, where it has no fontconfig to ask.
func FontFile() string {
	dir := filepath.Join(os.Getenv("WINDIR"), "Fonts")
	for _, name := range []string{"segoeuib.ttf", "segoeui.ttf", "arialbd.ttf", "arial.ttf"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}
