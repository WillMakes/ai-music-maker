package main

import "testing"

// TestPickImageFileRefusesHeadless is #39's guard: the test app has no window,
// and a dialog with no window would block a caller forever. The refusal is the
// contract the MCP server and the bug CLI rely on.
func TestPickImageFileRefusesHeadless(t *testing.T) {
	app := newTestApp(t)
	if _, err := app.PickImageFile(); err == nil {
		t.Fatal("a windowless app offered a file dialog")
	}
}
