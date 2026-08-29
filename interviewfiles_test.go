package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func audioDataURL(t *testing.T) string {
	t.Helper()
	return "data:audio/mpeg;base64," +
		base64.StdEncoding.EncodeToString([]byte("mp3-bytes"))
}

func TestAttachInterviewFileKeepsTheFilesOwnName(t *testing.T) {
	app := newTestApp(t)

	// The name is the point (#24): a Suno download is named after its track,
	// and the extraction assigns files to tracks by that name.
	path, err := app.AttachInterviewFile("track", "alb_1", "01 Neon Rain.mp3", audioDataURL(t))
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if filepath.Base(path) != "01 Neon Rain.mp3" {
		t.Fatalf("the file lost its name: %s", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the file is not on disk: %v", err)
	}

	// The same name again is uniquified, never overwritten — two takes with one
	// name are still two takes.
	second, err := app.AttachInterviewFile("track", "alb_1", "01 Neon Rain.mp3", audioDataURL(t))
	if err != nil {
		t.Fatalf("attach the second take: %v", err)
	}
	if second == path {
		t.Fatal("the second take overwrote the first")
	}
}

func TestAttachInterviewFileRefusesWhatItShould(t *testing.T) {
	app := newTestApp(t)
	// A name that is a path may not climb out of the conversation's folder.
	if _, err := app.AttachInterviewFile("track", "alb_1", "../../evil.mp3", audioDataURL(t)); err == nil {
		t.Fatal("a path-shaped name was accepted")
	}
	// A kind nothing in an interview reads is refused.
	zip := "data:application/zip;base64," + base64.StdEncoding.EncodeToString([]byte("zip"))
	if _, err := app.AttachInterviewFile("track", "alb_1", "design.zip", zip); err == nil {
		t.Fatal("an archive was accepted into a chat")
	}
	// A dangerous extension with an innocent MIME is rewritten, not reproduced.
	path, err := app.AttachInterviewFile("track", "alb_1", "track.exe", audioDataURL(t))
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if strings.HasSuffix(path, ".exe") {
		t.Fatalf("an executable extension survived: %s", path)
	}
	if !strings.HasSuffix(path, ".mp3") {
		t.Fatalf("the MIME's extension was not used: %s", path)
	}
}

func TestClearChatSweepsTheConversationsFiles(t *testing.T) {
	app := newTestApp(t)
	path, err := app.AttachInterviewFile("album", "art_1", "reference.png",
		"data:image/png;base64,"+base64.StdEncoding.EncodeToString([]byte("png")))
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := app.ClearChat("album", "art_1"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("the conversation's folder outlived the chat: %v", err)
	}
}

func TestAttachmentsAreNamedToTheModel(t *testing.T) {
	messages := []InterviewMessage{
		{Role: "user", Text: "Here are two takes.",
			Attachments: []string{filepath.Join("x", "01 Neon Rain.mp3"), filepath.Join("y", "cover.png")}},
		// Files with no words are still a turn — dropping three takes and
		// pressing send is a complete thing to say.
		{Role: "user", Attachments: []string{"/y/02 Static.mp3"}},
	}

	turns := toChatMsgs(messages)
	if len(turns) != 2 {
		t.Fatalf("expected both turns to survive, got %d", len(turns))
	}
	if !strings.Contains(turns[0].Text, "01 Neon Rain.mp3") ||
		!strings.Contains(turns[0].Text, "cover.png") {
		t.Fatalf("the first turn does not name its files: %q", turns[0].Text)
	}
	if !strings.Contains(turns[1].Text, "02 Static.mp3") {
		t.Fatalf("the file-only turn was dropped or unnamed: %q", turns[1].Text)
	}

	// The extraction reads the transcript, which must carry the same markers.
	transcript := renderTranscript(messages)
	if !strings.Contains(transcript, "01 Neon Rain.mp3") ||
		!strings.Contains(transcript, "02 Static.mp3") {
		t.Fatalf("the transcript lost the attachment markers:\n%s", transcript)
	}

	// And attachedAudioNames is the audio alone — the names the extraction is
	// told to assign, with the artwork reference left out of it.
	names := attachedAudioNames(messages)
	if len(names) != 2 || names[0] != "01 Neon Rain.mp3" || names[1] != "02 Static.mp3" {
		t.Fatalf("wrong audio names: %v", names)
	}
}
