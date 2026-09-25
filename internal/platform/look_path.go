package platform

import "os/exec"

// LookUp finds a command on the PATH, and when the inherited PATH misses, on
// the PATH a new process would get (see FreshPathLookup, #28). A command
// installed after this app started exists on the machine and in every new
// terminal, and "not found on this machine" about it is wrong in the way that
// costs a producer half an hour.
func LookUp(name string) (string, bool) {
	if path, err := exec.LookPath(name); err == nil {
		return path, true
	}
	return FreshPathLookup(name)
}
