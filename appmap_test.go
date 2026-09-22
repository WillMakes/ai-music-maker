package main

import (
	"reflect"
	"testing"
)

// findView is one page of a map, or a failure naming it.
func findView(t *testing.T, m AppMap, id string) AppView {
	t.Helper()
	for _, v := range m.Views {
		if v.ID == id {
			return v
		}
	}
	t.Fatalf("the map has no view %q", id)
	return AppView{}
}

func usesBinding(uses []AppUse, name string) bool {
	for _, u := range uses {
		if u.Name == name {
			return true
		}
	}
	return false
}

// TestAppMapIsWholeAndTrue is #88: the map the build wrote names real pages
// and real bindings, and the join gives every binding either its tools or a
// reason. Every name it carries is checked against App by reflection, so a
// stale or hand-edited map fails here rather than being served.
func TestAppMapIsWholeAndTrue(t *testing.T) {
	app := newTestApp(t)
	m, err := app.DescribeApp()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Views) == 0 {
		t.Fatal("the map has no views")
	}

	bindings := map[string]bool{}
	appType := reflect.TypeOf(app)
	for i := 0; i < appType.NumMethod(); i++ {
		bindings[appType.Method(i).Name] = true
	}
	check := func(where string, uses []AppUse) {
		for _, u := range uses {
			if !bindings[u.Name] {
				t.Errorf("%s uses %s, which is not a binding on App", where, u.Name)
			}
			if u.Offered && len(u.Tools) == 0 {
				t.Errorf("%s: %s is offered but names no tool", where, u.Name)
			}
			if !u.Offered && u.Reason == "" {
				t.Errorf("%s: %s has no tool and no reason", where, u.Name)
			}
		}
	}
	for _, v := range m.Views {
		if v.Title == "" {
			t.Errorf("view %s has no title", v.ID)
		}
		if len(v.Components) == 0 {
			t.Errorf("view %s renders nothing", v.ID)
		}
		check("view "+v.ID, v.Bindings)
	}
	check("the shell", m.Shell.Bindings)

	// The index is every binding exactly once, and it agrees with the pages.
	if len(m.Bindings) != len(bindings) {
		t.Errorf("the index lists %d bindings, App has %d", len(m.Bindings), len(bindings))
	}
	for _, b := range m.Bindings {
		if b.Offered && len(b.Tools) == 0 || !b.Offered && b.Reason == "" {
			t.Errorf("binding %s: offered=%v tools=%v reason=%q", b.Name, b.Offered, b.Tools, b.Reason)
		}
	}
	if len(m.Tools) != mcpToolCount() {
		t.Errorf("the map lists %d tools, the server has %d", len(m.Tools), mcpToolCount())
	}
}

// TestAppMapAttributesBindingsToTheRightPages pins a few facts about this
// app's own layout — the kind a documentation report would state, and the
// kind a file-level walk of the import graph got wrong.
func TestAppMapAttributesBindingsToTheRightPages(t *testing.T) {
	app := newTestApp(t)
	m, err := app.DescribeApp()
	if err != nil {
		t.Fatal(err)
	}

	album := findView(t, m, "album-details")
	if !usesBinding(album.Bindings, "GetAlbum") || !usesBinding(album.Bindings, "ListTracks") {
		t.Errorf("the album page does not read its album: %+v", album.Bindings)
	}
	// The player bar borrows one small component from the artist page's file.
	// A walk that followed the file rather than the export put the artist
	// interview on every page; it belongs to the artist page alone.
	if usesBinding(m.Shell.Bindings, "PlanAlbum") || usesBinding(m.Shell.Bindings, "DeleteArtist") {
		t.Errorf("the shell claims the artist page's own work: %+v", m.Shell.Bindings)
	}
	artist := findView(t, m, "artist-details")
	if !usesBinding(artist.Bindings, "DeleteArtist") {
		t.Errorf("the artist page cannot delete its artist: %+v", artist.Bindings)
	}
	// The rail reads the library tree on every page.
	if !usesBinding(m.Shell.Bindings, "LibraryTree") {
		t.Errorf("the shell does not read the library tree: %+v", m.Shell.Bindings)
	}
	// A settings tab reaches its own panel's bindings and is picked through
	// the Settings shell — both components are named.
	ai := findView(t, m, "settings-ai")
	if !usesBinding(ai.Bindings, "ConnectMCPClient") {
		t.Errorf("the AI tab cannot connect a client: %+v", ai.Bindings)
	}
	if len(ai.Components) < 2 {
		t.Errorf("the AI tab names only %v", ai.Components)
	}
	// The window's file pickers are on the album page and offered to no tool.
	for _, use := range album.Bindings {
		if use.Name == "PickImageFile" && (use.Offered || use.Reason == "") {
			t.Errorf("PickImageFile should be unoffered with a reason: %+v", use)
		}
	}
	// A binding nothing in the window calls is still indexed, with its tool.
	for _, b := range m.Bindings {
		if b.Name == "GetBugReport" {
			if len(b.Views) != 0 || b.Shell || !b.Offered {
				t.Errorf("GetBugReport is indexed wrong: %+v", b)
			}
		}
	}
}

func TestDescribeAppTool(t *testing.T) {
	app := newTestApp(t)
	var m AppMap
	mcpCall(t, app, "describe_app", map[string]any{}, &m)
	if len(m.Views) == 0 || len(m.Tools) == 0 || len(m.Bindings) == 0 {
		t.Fatalf("describe_app answered an empty map: %d views, %d tools, %d bindings",
			len(m.Views), len(m.Tools), len(m.Bindings))
	}
}
