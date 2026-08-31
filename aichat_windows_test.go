//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAgyEmptyAnswerSurfacesStderr is #67. The #50 answer-file instruction
// made agy reach for a tool that headless -p auto-denies, which suppressed
// every byte of stdout — the fallback was causing the failure it guarded
// against, and the engine's explanation sat unread on stderr while the app
// said "returned nothing". The instruction is gone; this pins the other half:
// a clean exit with an empty stdout must hand the producer whatever stderr
// says, because that is where the actionable reason lives.
func TestAgyEmptyAnswerSurfacesStderr(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "agy.bat")
	script := "@echo off\r\n" +
		"echo a tool required the command permission 1>&2\r\n"
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
	_, err := chatViaCLI(app.interviewContext(), serviceAntigravity, "",
		"brief", []chatMsg{{Role: "user", Text: "hello"}})
	if err == nil {
		t.Fatal("an empty answer was accepted")
	}
	if !strings.Contains(err.Error(), "command permission") {
		t.Fatalf("stderr's explanation was swallowed: %v", err)
	}
}

// TestAgyPromptCarriesNoFileInstruction pins the removal itself: the prompt
// handed to agy must be the conversation and nothing else, because the extra
// file-writing instruction is what tripped the permission denial (#67).
func TestAgyPromptCarriesNoFileInstruction(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "agy.bat")
	// The stub echoes its own prompt argument back, so the assertion can see
	// exactly what agy would have been asked.
	script := "@echo off\r\n" +
		"echo %2\r\n"
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
		t.Fatal(err)
	}
	if strings.Contains(answer, "answer.md") || strings.Contains(answer, "write the complete") {
		t.Fatalf("the prompt still asks for a file: %q", answer)
	}
}
