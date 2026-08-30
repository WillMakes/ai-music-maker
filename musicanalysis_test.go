package main

import (
	"strings"
	"testing"
)

func TestMusicURLsFindsTheSongLinks(t *testing.T) {
	text := "Something like https://music.youtube.com/watch?v=abc123, " +
		"and https://youtu.be/xyz789. Also https://example.com/watch?v=nope " +
		"and https://music.youtube.com/watch?v=abc123 again."
	urls := musicURLs(text)
	if len(urls) != 2 {
		t.Fatalf("want the two YouTube links once each, got %v", urls)
	}
	// The sentence's punctuation stays with the sentence.
	if urls[0] != "https://music.youtube.com/watch?v=abc123" ||
		urls[1] != "https://youtu.be/xyz789" {
		t.Fatalf("wrong or unclean urls: %v", urls)
	}
	if len(musicURLs("no links here")) != 0 {
		t.Fatal("found a link in text with none")
	}
}

func TestAnalyzeMusicURLGatesOnAntigravity(t *testing.T) {
	app := newTestApp(t)

	// Not a song link at all.
	if _, err := app.AnalyzeMusicURL("https://example.com/song"); err == nil {
		t.Fatal("a non-YouTube link was accepted")
	}
	// A song link with Antigravity not connected: refused with the pointer to
	// Settings, not run on a connection nobody made.
	_, err := app.AnalyzeMusicURL("https://youtu.be/abc")
	if err == nil || !strings.Contains(err.Error(), "Settings") {
		t.Fatalf("want the connect-Antigravity refusal, got %v", err)
	}
}

func TestResearchRidesTheTurn(t *testing.T) {
	app := newTestApp(t)

	// No links, no research — and no Antigravity call to fail.
	if got := app.researchForTurn("make it dreamier"); got != "" {
		t.Fatalf("research ran with nothing to research: %q", got)
	}

	// With Antigravity connected but its CLI unavailable, the failure lands in
	// the research block as a note rather than failing the turn: the producer
	// pasted a link mid-conversation and the conversation must go on.
	withCLIs(t, "agy")
	if _, err := app.ConnectAIAccount(serviceAntigravity); err != nil {
		t.Fatalf("connect: %v", err)
	}
	withCLIs(t) // agy vanishes before the run
	block := app.researchForTurn("listen to https://youtu.be/abc please")
	if !strings.Contains(block, "https://youtu.be/abc") ||
		!strings.Contains(block, "could not run") {
		t.Fatalf("the failed analysis did not land as a note: %q", block)
	}

	// The research a turn carries reaches both the conversation and the
	// extraction, labelled as the app's own lookup.
	messages := []InterviewMessage{{
		Role: "user", Text: "like this one",
		Research: "120 BPM, tape-saturated drums",
	}}
	turn := toChatMsgs(messages)[0].Text
	if !strings.Contains(turn, "Music analysis the app ran") ||
		!strings.Contains(turn, "120 BPM") {
		t.Fatalf("the conversation turn lost the research: %q", turn)
	}
	transcript := renderTranscript(messages)
	if !strings.Contains(transcript, "120 BPM") {
		t.Fatalf("the transcript lost the research:\n%s", transcript)
	}
}

// TestMergeStyleWithAnalysisRefusals: the merge is a model call, so the tests
// pin what must hold on either side of it — an empty analysis is refused
// before any provider is bothered, and with no provider connected the failure
// is the connect message, not a stored half-merge.
func TestMergeStyleWithAnalysisRefusals(t *testing.T) {
	app := newTestApp(t)

	if _, err := app.MergeStyleWithAnalysis("some style", "  "); err == nil {
		t.Fatal("an empty analysis was merged")
	}
	_, err := app.MergeStyleWithAnalysis("some style", "120 BPM, tape drums")
	if err == nil {
		t.Fatal("a merge ran with no provider connected")
	}
}

// TestParseAnalysis is #51's contract with agy: a JSON answer carries the
// song's identity alongside the breakdown, and anything else — narration,
// a fence, an empty style — degrades to a breakdown without identity rather
// than an error. The producer pressed the button for the style; the metadata
// is the bonus, never the gate.
func TestParseAnalysis(t *testing.T) {
	got := parseAnalysis("```json\n{\"title\": \"Night Drive\", \"artist\": \"Kavinsky\", " +
		"\"album\": \"OutRun\", \"artworkUrl\": \"https://img.example/a.jpg\", " +
		"\"style\": \"**Genre** — synthwave, 110 BPM\"}\n```")
	if got.Title != "Night Drive" || got.Artist != "Kavinsky" ||
		got.Album != "OutRun" || got.ArtworkURL != "https://img.example/a.jpg" {
		t.Fatalf("the identity was lost: %+v", got)
	}
	if got.Style != "**Genre** — synthwave, 110 BPM" {
		t.Fatalf("wrong style: %q", got.Style)
	}

	freeform := parseAnalysis("A dense synthwave cut, 110 BPM, analog pads.")
	if freeform.Style != "A dense synthwave cut, 110 BPM, analog pads." {
		t.Fatalf("a freeform answer should become the style: %+v", freeform)
	}
	if freeform.Title != "" {
		t.Fatalf("a freeform answer invented a title: %+v", freeform)
	}

	// JSON with nothing in the style is agy misfiring, not an analysis — the
	// raw answer at least shows the producer what came back.
	empty := parseAnalysis("{\"title\": \"x\", \"style\": \"  \"}")
	if empty.Style == "" {
		t.Fatalf("an empty style slipped through: %+v", empty)
	}
}
