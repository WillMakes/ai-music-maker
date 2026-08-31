//go:build windows

package main

import (
	"os"
	"strings"
	"testing"
)

// TestMediaKeysArgumentIsSet is #72: WebView2 only integrates with Windows'
// media controls when Chromium's HardwareMediaKeyHandling feature is enabled
// through its environment variable, and the app must set it without erasing
// arguments the producer put there themselves.
func TestMediaKeysArgumentIsSet(t *testing.T) {
	const envName = "WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS"

	t.Setenv(envName, "")
	enableWindowsMediaKeys()
	if got := os.Getenv(envName); !strings.Contains(got, "HardwareMediaKeyHandling") {
		t.Fatalf("the feature was not enabled: %q", got)
	}

	t.Setenv(envName, "--disable-gpu")
	enableWindowsMediaKeys()
	got := os.Getenv(envName)
	if !strings.Contains(got, "--disable-gpu") {
		t.Fatalf("the producer's own argument was erased: %q", got)
	}
	if !strings.Contains(got, "HardwareMediaKeyHandling") {
		t.Fatalf("the feature was not appended: %q", got)
	}

	// Idempotent: a second call must not stack the argument.
	before := os.Getenv(envName)
	enableWindowsMediaKeys()
	if os.Getenv(envName) != before {
		t.Fatalf("a second call changed the value: %q", os.Getenv(envName))
	}
}
