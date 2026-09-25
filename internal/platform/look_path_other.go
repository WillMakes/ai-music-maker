//go:build !windows

package platform

// FreshPathLookup is the Windows fallback for commands installed after this
// process inherited its PATH (#28). Unix has no registry PATH to consult — a
// shell profile is the shell's own business — so a miss stays a miss.
func FreshPathLookup(string) (string, bool) {
	return "", false
}
