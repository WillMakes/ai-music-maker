package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAgyStdoutFallback is #50: agy has had releases where -p drops stdout
// under a pipe, exiting cleanly with nothing printed. The fix asks agy to
// also write answer.md into its private working directory and reads that
// when stdout is empty. The stub here is exactly the broken release: it
// writes the file and prints nothing.
func TestAgyStdoutFallback(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "agy.bat")
	script := "@echo off\r\n" +
		"echo synthwave, analog warmth> answer.md\r\n"
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	previous := lookUpCommand
	lookUpCommand = func(name string) (string, bool) {
		if name == "agy" {
			return stub, true
		}
		return "", false
	}
	t.Cleanup(func() { lookUpCommand = previous })

	app := newTestApp(t)
	answer, err := chatViaCLI(app.interviewContext(), serviceAntigravity, "",
		"brief", []chatMsg{{Role: "user", Text: "hello"}})
	if err != nil {
		t.Fatalf("the fallback did not rescue the answer: %v", err)
	}
	if !strings.Contains(answer, "synthwave") {
		t.Fatalf("wrong answer read back: %q", answer)
	}
}

// TestAgyAnswerFileRescue is #65: releases that hand the -p run to the
// spawned application have written the answer relative to their own workspace
// notion or under their own name, never printing to stdout and never touching
// answer.md. The stub is that release — it drops a differently named file in
// the working directory, and the rescue pass must still bring the answer home.
func TestAgyAnswerFileRescue(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "agy.bat")
	script := "@echo off\r\n" +
		"echo tape hiss, dusk chords> reply.txt\r\n"
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	previous := lookUpCommand
	lookUpCommand = func(name string) (string, bool) {
		if name == "agy" {
			return stub, true
		}
		return "", false
	}
	t.Cleanup(func() { lookUpCommand = previous })

	app := newTestApp(t)
	answer, err := chatViaCLI(app.interviewContext(), serviceAntigravity, "",
		"brief", []chatMsg{{Role: "user", Text: "hello"}})
	if err != nil {
		t.Fatalf("the rescue did not bring the answer home: %v", err)
	}
	if !strings.Contains(answer, "tape hiss") {
		t.Fatalf("wrong answer read back: %q", answer)
	}
}
