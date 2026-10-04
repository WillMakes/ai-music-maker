//go:build !windows

package platform

import "os"

// FontFile is a font a music video's titles are drawn in (#118), or "" when
// none of the expected ones is installed. The usual system sans on macOS and
// the common Linux distributions, in that order.
func FontFile() string {
	for _, path := range []string{
		"/System/Library/Fonts/Supplemental/Arial Bold.ttf",
		"/System/Library/Fonts/Supplemental/Arial.ttf",
		"/Library/Fonts/Arial.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf",
		"/usr/share/fonts/TTF/DejaVuSans-Bold.ttf",
		"/usr/share/fonts/dejavu/DejaVuSans-Bold.ttf",
	} {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}
