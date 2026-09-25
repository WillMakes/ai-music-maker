package app

import "testing"

// TestBuildInfoReportsTheStore is the fact the status bar leads with (#19).
//
// A store that will not open is not fatal — NewApp carries on so there is still
// a window to read Settings in — and that is exactly what makes it dangerous:
// every read answers empty and every write returns errNoStore, which looks like
// an app with nothing in it rather than one that cannot save. Something has to
// be able to say so.
func TestBuildInfoReportsTheStore(t *testing.T) {
	app := newTestApp(t)
	if !app.GetBuildInfo().StoreOK {
		t.Error("an app with an open database reports storeOk false")
	}

	// The state a launch is in when the database will not open.
	if (&App{}).GetBuildInfo().StoreOK {
		t.Error("an app with no store at all reports storeOk true")
	}
}
