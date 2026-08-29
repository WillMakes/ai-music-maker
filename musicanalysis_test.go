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
