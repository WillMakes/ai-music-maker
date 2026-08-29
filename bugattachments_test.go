package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pngDataURL is a one-pixel paste, the shape the page hands over.
func pngDataURL(t *testing.T) string {
	t.Helper()
	// Not a real PNG — the backend keys on the declared MIME, not the bytes,
	// because the bytes are for the human who opens the file.
	return "data:image/png;base64," +
		base64.StdEncoding.EncodeToString([]byte("png-bytes"))
}

func TestAttachmentsFollowTheReportAndLeaveWithIt(t *testing.T) {
	app := newTestApp(t)

	// Pasted before the report exists, into a draft folder (#20): the usual
	// order, because a screenshot lands before the sentence is finished.
	path, err := app.AttachBugMedia("draft-abc123", pngDataURL(t))
	if err != nil {
		t.Fatalf("attach to the draft: %v", err)
	}
	if !strings.Contains(path, "draft-abc123") {
		t.Fatalf("the draft's file is not under the draft's folder: %s", path)
	}

	report, err := app.FileBugReport(BugReport{
		Description: "the page falls over",
		Route:       "artists",
		Attachments: []string{path},
	})
	if err != nil {
		t.Fatalf("file the report: %v", err)
	}

	// Filing adopts the file under the report's own id — the path on the record
	// must be one that outlives the draft.
	if len(report.Attachments) != 1 {
		t.Fatalf("expected the one attachment, got %v", report.Attachments)
	}
	adopted := report.Attachments[0]
	if !strings.Contains(adopted, report.ID) {
		t.Fatalf("the file did not move under the report: %s", adopted)
	}
	if _, err := os.Stat(adopted); err != nil {
		t.Fatalf("the adopted file is not on disk: %v", err)
	}

	// And the path survives the round trip through the row.
	reread, err := app.store.bugReport(report.ID)
	if err != nil {
		t.Fatalf("reread the report: %v", err)
	}
	if len(reread.Attachments) != 1 || reread.Attachments[0] != adopted {
		t.Fatalf("the stored report lost its attachment: %v", reread.Attachments)
	}

	// The issue body names the file, because the agent working the report reads
	// the issue and a file nobody mentions is a file nobody opens.
	if body := issueDetails(reread); !strings.Contains(body, adopted) {
		t.Fatalf("the issue body does not name the file:\n%s", body)
	}

	// Resolving is the only way out of the queue, and the files go with it.
	if _, err := app.ResolveBugReport(report.ID, "", ""); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(adopted)); !os.IsNotExist(err) {
		t.Fatalf("the report's folder outlived the report: %v", err)
	}
}

func TestDecodeDataURLRefusesWhatItShould(t *testing.T) {
	// A type not on the list is refused rather than saved with a guessed
	// extension — the path ends up in front of a coding agent.
	if _, _, err := decodeDataURL("data:application/x-msdownload;base64,QUFB"); err == nil {
		t.Fatal("an executable's MIME was accepted")
	}
	// Oversize is turned away before the decode, so nothing this big is ever
	// held in memory.
	huge := "data:image/png;base64," + strings.Repeat("A", (attachmentMax/3*4)+8)
	if _, _, err := decodeDataURL(huge); err == nil {
		t.Fatal("an oversized image was accepted")
	}
	// A recording gets the recording's headroom, not the image's.
	video := "data:video/mp4;base64," + strings.Repeat("A", (attachmentMax/3*4)+8)
	if kind, _, err := decodeDataURL(video); err != nil || kind != ".mp4" {
		t.Fatalf("a video inside its own limit was refused: kind=%q err=%v", kind, err)
	}
}

func TestIssueDetailsKeepsThePageOnGlobalReports(t *testing.T) {
	// Global says the problem is not confined to the page; the page still says
	// where it showed up (#20). The old body dropped it.
	body := issueDetails(BugReport{
		ID:     "bug_x",
		Route:  "settings-bugs",
		Global: true,
	})
	if !strings.Contains(body, "`settings-bugs`") {
		t.Fatalf("a global report lost its page:\n%s", body)
	}
	if !strings.Contains(body, "the whole app") {
		t.Fatalf("a global report lost its scope:\n%s", body)
	}
}

func TestAttachBugMediaRefusesAnIDThatNamesAPath(t *testing.T) {
	app := newTestApp(t)
	// No backslash case: on Linux a backslash is an ordinary filename
	// character, and the check under test speaks filepath, not Windows.
	for _, id := range []string{"../elsewhere", "a/b", ".."} {
		if _, err := app.AttachBugMedia(id, pngDataURL(t)); err == nil {
			t.Fatalf("%q was allowed to name a folder", id)
		}
	}
}
