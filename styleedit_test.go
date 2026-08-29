package main

import (
	"strings"
	"testing"
)

func TestStyleEditBriefCarriesTheArtist(t *testing.T) {
	brief := styleEditBrief(Artist{
		Name:      "Cassette Motel",
		Summary:   "Neon-soaked synthwave",
		BaseStyle: "Analog synths, gated reverb drums",
		Inspirations: []Inspiration{
			{URL: "https://youtu.be/abc", Note: "the drum sound"},
		},
	})
	for _, want := range []string{
		"Cassette Motel",
		"Neon-soaked synthwave",
		"Analog synths, gated reverb drums",
		"https://youtu.be/abc — the drum sound",
	} {
		if !strings.Contains(brief, want) {
			t.Fatalf("the brief is missing %q:\n%s", want, brief)
		}
	}
	// The conversation converses; the extraction writes the document.
	if !strings.Contains(brief, "Do not output the whole revised style") {
		t.Fatalf("the brief does not hold the style back:\n%s", brief)
	}
	// An artist with nothing written yet is a conversation that writes it, not
	// a brief with a hole in it.
	empty := styleEditBrief(Artist{Name: "New"})
	if !strings.Contains(empty, "nothing written yet") {
		t.Fatalf("an empty style is not admitted:\n%s", empty)
	}
}

func TestAskStyleEditRefusalsAndNotes(t *testing.T) {
	app := newTestApp(t)

	if _, err := app.AskStyleEdit("art_x", nil); err == nil {
		t.Fatal("an empty thread was answered")
	}
	turn := []InterviewMessage{{Role: "user", Text: "warmer drums"}}
	if _, err := app.AskStyleEdit("art_missing", turn); err == nil {
		t.Fatal("an artist that does not exist was refined")
	}

	artist, err := app.SaveArtist(Artist{Name: "Cassette Motel", Inspirations: []Inspiration{}})
	if err != nil {
		t.Fatalf("save artist: %v", err)
	}
	// No provider connected: the failure lands on the thread as a note, with
	// the producer's own turn still there to send again.
	thread, err := app.AskStyleEdit(artist.ID, turn)
	if err != nil {
		t.Fatalf("a failed turn should not be an error: %v", err)
	}
	last := thread[len(thread)-1]
	if last.Role != "assistant" || last.Note == "" {
		t.Fatalf("the failure did not land as a note: %+v", last)
	}
	if thread[0].Text != "warmer drums" {
		t.Fatalf("the producer's turn was lost: %+v", thread)
	}
	// No link in the turn, so no research ran and none was invented.
	if thread[0].Research != "" {
		t.Fatalf("research appeared from nowhere: %q", thread[0].Research)
	}
}
