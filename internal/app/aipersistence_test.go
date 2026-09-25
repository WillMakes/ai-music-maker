package app

import (
	"path/filepath"
	"testing"
)

// TestAIConnectionsSurviveARestart is #22's question asked directly: connect a
// provider, close the store the way quitting does, open the same file the way
// launching does, and ask again.
//
// It passes, which is the point of keeping it: the report "configuration is not
// persisting across restarts" turned out to be the producer's row having been
// deleted from the live database during earlier verification work — a process
// failure, not a code path — and this test is what says the code path holds, so
// the next report like it starts from "what touched the file" rather than from
// re-suspecting the machinery.
func TestAIConnectionsSurviveARestart(t *testing.T) {
	app := newTestApp(t)
	withCLIs(t, "claude")

	if _, err := app.ConnectAIAccount(serviceAnthropic); err != nil {
		t.Fatalf("connect: %v", err)
	}
	dbPath := filepath.Join(mustDataDir(t), "sunomaker.db")

	// Quit.
	if err := app.store.Close(); err != nil {
		t.Fatalf("close the store: %v", err)
	}

	// Launch.
	store, err := openStoreAt(dbPath)
	if err != nil {
		t.Fatalf("reopen the store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	reopened := &App{store: store}

	conns := reopened.aiConnections()
	conn, found := conns[serviceAnthropic]
	if !found {
		t.Fatalf("the connection did not survive the restart: %v", conns)
	}
	if conn.Mode != modeAccount {
		t.Fatalf("the connection came back as %q, not account mode", conn.Mode)
	}

	// And the panel's own read agrees, since that is what the producer sees.
	for _, service := range reopened.GetAIServices() {
		if service.ID == serviceAnthropic && !service.Connected {
			t.Fatal("GetAIServices does not show the surviving connection")
		}
	}
}

// mustDataDir is the stubbed data directory newTestApp installed.
func mustDataDir(t *testing.T) string {
	t.Helper()
	dir, err := dataDir()
	if err != nil {
		t.Fatalf("dataDir: %v", err)
	}
	return dir
}
