//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The #28 shape in miniature: a command that exists in a directory the
// inherited PATH does not carry. The registry itself is not faked — these
// cover the pieces around it, and the report's own reproduction (agy installed
// after launch) is what proved the whole path end to end.

func TestCommandInDirTriesTheExecutableExtensions(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "agy.exe")
	if err := os.WriteFile(target, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}

	path, found := commandInDir(dir, "agy")
	if !found || path != target {
		t.Fatalf("agy.exe not found: found=%v path=%s", found, path)
	}
	// A .cmd shim — how an npm-installed CLI arrives — is found too.
	if err := os.WriteFile(filepath.Join(dir, "gemini.cmd"), []byte("@echo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, found := commandInDir(dir, "gemini"); !found {
		t.Fatal("a .cmd shim was not found")
	}
	// A name with no executable behind it stays missing, and a directory named
	// like one does not count as one.
	if _, found := commandInDir(dir, "nothing"); found {
		t.Fatal("a missing command was found")
	}
	if err := os.Mkdir(filepath.Join(dir, "codex.exe"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, found := commandInDir(dir, "codex"); found {
		t.Fatal("a directory was mistaken for an executable")
	}
}

func TestRegistryPathDirsAnswersSomething(t *testing.T) {
	// On any real Windows machine both PATH values exist; empty here would
	// mean the registry read is broken, which is the failure this guards.
	if len(registryPathDirs()) == 0 {
		t.Fatal("the registry PATH read answered nothing")
	}
}
