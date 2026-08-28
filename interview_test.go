package main

import (
	"strings"
	"testing"
)

func TestUnfenceStripsACodeBlock(t *testing.T) {
	// Told not to use a fence and asked for JSON, models still sometimes do.
	// Rejecting the answer would be correct and useless — the data is right
	// there.
	cases := map[string]string{
		"```json\n{\"name\":\"x\"}\n```": "{\"name\":\"x\"}",
		"```\n{\"name\":\"x\"}\n```":     "{\"name\":\"x\"}",
		"{\"name\":\"x\"}":               "{\"name\":\"x\"}",
		"  {\"name\":\"x\"}  ":           "{\"name\":\"x\"}",
	}
	for input, want := range cases {
		if got := unfence(input); got != want {
			t.Errorf("unfence(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeTurnsMakesAnAcceptableHistory(t *testing.T) {
	// Anthropic rejects a history that opens with the assistant or repeats a
	// role, and a resumed transcript can do both.
	got := normalizeTurns([]chatMsg{
		{Role: "assistant", Text: "dropped — a conversation must open with the user"},
		{Role: "user", Text: "one"},
		{Role: "user", Text: "two"},
		{Role: "assistant", Text: ""},
		{Role: "assistant", Text: "answer"},
	})

	if len(got) != 2 {
		t.Fatalf("%d turns, want 2: %+v", len(got), got)
	}
	if got[0].Role != "user" || got[0].Text != "one\n\ntwo" {
		t.Errorf("first turn = %+v, want the two user messages joined", got[0])
	}
	if got[1].Role != "assistant" || got[1].Text != "answer" {
		t.Errorf("second turn = %+v", got[1])
	}
}

func TestTrimTranscriptKeepsTheTail(t *testing.T) {
	messages := make([]InterviewMessage, interviewTurnMax+10)
	for i := range messages {
		messages[i] = InterviewMessage{Role: "user", Text: string(rune('a' + i%26))}
	}
	trimmed := trimTranscript(messages)

	if len(trimmed) != interviewTurnMax {
		t.Fatalf("%d turns kept, want %d", len(trimmed), interviewTurnMax)
	}
	// The tail, because later answers correct earlier ones and the extraction
	// reads what it is given.
	if trimmed[len(trimmed)-1].Text != messages[len(messages)-1].Text {
		t.Error("the most recent turn was dropped")
	}
}

func TestToChatMsgsDropsTheAppsOwnNotes(t *testing.T) {
	// A note says a turn failed. Sending it back would have the model apologise
	// for an error it did not make.
	got := toChatMsgs([]InterviewMessage{
		{Role: "user", Text: "what about the drums"},
		{Role: "assistant", Note: "could not reach Claude"},
		{Role: "assistant", Text: "brushed snare"},
	})
	if len(got) != 2 {
		t.Fatalf("%d messages, want 2: %+v", len(got), got)
	}
	for _, message := range got {
		if strings.Contains(message.Text, "could not reach") {
			t.Error("an app note was sent to the model as the model's own words")
		}
	}
}

func TestInterviewBriefCarriesWhatIsAlreadyDecided(t *testing.T) {
	app := newTestApp(t)
	artist, album := aRecord(t, app)
	if _, err := app.SaveTrack(Track{AlbumID: album.ID, Name: "The Opener"}); err != nil {
		t.Fatalf("track: %v", err)
	}

	brief, err := app.interviewBrief(subjectTrack, album.ID)
	if err != nil {
		t.Fatalf("brief: %v", err)
	}

	// The skill that says how to run the conversation.
	if !strings.Contains(brief, "Track generation") && !strings.Contains(brief, "style prompt") {
		t.Error("the track-generation skill is not in the brief")
	}
	// The facts, so the interviewer does not ask about what it has been told.
	for _, want := range []string{artist.Name, album.Name, artist.BaseStyle, album.TechnicalStyle} {
		if !strings.Contains(brief, want) {
			t.Errorf("the brief does not mention %q", want)
		}
	}
	// The running order, which is what stops it proposing a name that is already
	// on the record — the one mistake obvious to the producer and invisible to
	// the model.
	if !strings.Contains(brief, "The Opener") {
		t.Error("the existing tracks are not in the brief")
	}
	if !strings.Contains(brief, "do not propose these names again") {
		t.Error("the brief lists the tracks without saying what to do about them")
	}
}

func TestAlbumBriefCarriesTheArtist(t *testing.T) {
	app := newTestApp(t)
	artist, _ := aRecord(t, app)

	brief, err := app.interviewBrief(subjectAlbum, artist.ID)
	if err != nil {
		t.Fatalf("brief: %v", err)
	}
	if !strings.Contains(brief, artist.BaseStyle) {
		t.Error("the album interview was not told the artist's base style")
	}
}

func TestUnknownInterviewIsRefused(t *testing.T) {
	app := newTestApp(t)
	if _, err := app.interviewBrief("something-else", ""); err == nil {
		t.Fatal("an unknown interview kind was accepted")
	}
}

func TestChatRoundTripAndClear(t *testing.T) {
	app := newTestApp(t)

	// A record nobody has talked about is not an error, it is the normal state
	// of most of them.
	chat, err := app.GetChat(subjectArtist, "")
	if err != nil {
		t.Fatalf("read an empty chat: %v", err)
	}
	if len(chat.Messages) != 0 {
		t.Errorf("%d messages in a conversation nobody has had", len(chat.Messages))
	}

	app.keepChat(subjectArtist, "", []InterviewMessage{{Role: "user", Text: "hello"}})
	resumed, err := app.GetChat(subjectArtist, "")
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if len(resumed.Messages) != 1 || resumed.Messages[0].Text != "hello" {
		t.Errorf("resumed %+v, want the stored turn", resumed.Messages)
	}

	if err := app.ClearChat(subjectArtist, ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	cleared, _ := app.GetChat(subjectArtist, "")
	if len(cleared.Messages) != 0 {
		t.Error("the conversation survived being cleared")
	}
}

func TestChatsAreKeptApartByKind(t *testing.T) {
	app := newTestApp(t)

	// Resuming an album's thread into a track's would hand somebody half a
	// discussion they never had.
	app.keepChat(subjectAlbum, "same-id", []InterviewMessage{{Role: "user", Text: "album talk"}})
	app.keepChat(subjectTrack, "same-id", []InterviewMessage{{Role: "user", Text: "track talk"}})

	album, _ := app.GetChat(subjectAlbum, "same-id")
	track, _ := app.GetChat(subjectTrack, "same-id")
	if album.Messages[0].Text != "album talk" || track.Messages[0].Text != "track talk" {
		t.Error("two conversations under the same id ran into each other")
	}
}

func TestAIReadinessSaysWhyNot(t *testing.T) {
	app := newTestApp(t)
	// Nothing connected. The dialog has to be able to say so on the page rather
	// than after the producer has typed a paragraph.
	ready := app.GetAIReadiness()
	if ready.Ready {
		t.Error("an app with no provider connected reported itself ready")
	}
	if ready.Why == "" {
		t.Error("the refusal says nothing about how to fix it")
	}
}
