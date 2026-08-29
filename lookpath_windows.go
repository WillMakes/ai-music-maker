//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// freshPathLookup finds a command on the PATH Windows would hand a *new*
// process, for commands this process cannot see (#28).
//
// A CLI installer appends its folder to the PATH kept in the registry, but a
// process only reads that at launch: an app started before the install — or
// launched from an Explorer that was — inherits the old value and
// exec.LookPath honestly reports the command missing while every freshly
// opened terminal finds it. `agy --version` working in the producer's shell
// and Settings saying "not found on this machine" were both telling the
// truth, about two different PATHs.
//
// So when the inherited PATH misses, the registry's user and machine PATH are
// read directly — the same two values a new logon combines — and only the
// directories the inherited PATH does not already have are searched, since the
// rest were just searched by exec.LookPath.
func freshPathLookup(name string) (string, bool) {
	seen := map[string]bool{}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		seen[strings.ToLower(strings.TrimSpace(dir))] = true
	}
	for _, dir := range registryPathDirs() {
		if dir == "" || seen[strings.ToLower(dir)] {
			continue
		}
		if path, ok := commandInDir(dir, name); ok {
			return path, true
		}
	}
	return "", false
}

// registryPathDirs is every directory on the registry's PATH: the machine's
// and then the user's, the order a logon combines them in.
func registryPathDirs() []string {
	dirs := []string{}
	read := func(root registry.Key, keyPath, value string) {
		key, err := registry.OpenKey(root, keyPath, registry.QUERY_VALUE)
		if err != nil {
			return
		}
		defer key.Close()
		raw, _, err := key.GetStringValue(value)
		if err != nil {
			return
		}
		// PATH is usually REG_EXPAND_SZ, full of %SystemRoot%-style holes that
		// mean nothing to a Stat call until they are filled in.
		if expanded, err := registry.ExpandString(raw); err == nil {
			raw = expanded
		}
		for _, dir := range filepath.SplitList(raw) {
			if dir = strings.TrimSpace(dir); dir != "" {
				dirs = append(dirs, dir)
			}
		}
	}
	read(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Control\Session Manager\Environment`, "Path")
	read(registry.CURRENT_USER, `Environment`, "Path")
	return dirs
}

// commandInDir looks for name in one directory, with the executable
// extensions Windows itself would try.
func commandInDir(dir, name string) (string, bool) {
	for _, ext := range []string{".exe", ".cmd", ".bat", ".com"} {
		candidate := filepath.Join(dir, name+ext)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}
