package app

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// Files on a bug report (#20)
//
// Every report in this queue is worked by an agent reading the description and
// nothing else, so a layout bug described in a sentence is a sentence somebody
// has to reconstruct a screen from. A pasted screenshot removes the
// reconstruction, and it is the one thing the reporter already has — they are
// looking at the bug when they press the button. Recordings for the bugs that
// only exist in motion, and a .zip for anything that is a folder of files
// rather than a picture.
//
// The bytes go in files rather than in the row. The reports table is read
// whole by the queue on every change, and a base64 PNG in a column is a
// payload the list carries around forever to show a thumbnail nobody has
// opened. Files under ~/.sunomaker/bug-attachments, with the paths on the
// record — which also makes them reachable: a path in the JSON an agent
// already reads is something that agent can open, where a blob in a column is
// not.
//
// This is Site Maker Pro's shape, ported whole, because the report asking for
// it asked for exactly that.
// ---------------------------------------------------------------------------

const (
	// attachmentMax bounds one image. A screenshot of a window is a few hundred
	// kilobytes; anything past this is a photograph of a monitor or a mistake,
	// and either way it is not what this is for.
	attachmentMax = 8 << 20

	// videoMax bounds one recording. Its own figure because the thing being
	// bounded is different: eight megabytes is a generous screenshot and a
	// four-second screen recording, and a bug that only shows up in motion is a
	// bug nobody can film inside that.
	videoMax = 64 << 20

	// archiveMax bounds a .zip — a folder of Suno downloads or artwork drafts
	// handed over whole runs larger than a screenshot without being a mistake,
	// so it gets the recording's headroom rather than the image's.
	archiveMax = 64 << 20

	// attachmentsPerReport caps how many one report may carry. Past a handful
	// the reporter is describing the bug in pictures, which is worth doing in
	// the description instead.
	attachmentsPerReport = 6
)

// attachmentKinds is what a pasted, dropped or picked file may be, mapped to
// the extension it is written with.
//
// A fixed list rather than whatever the clipboard claims: the extension ends
// up in a path this app hands to a coding agent, and a MIME type from a
// webview is not something to build a filename out of unchecked.
//
// The video types are the three a screen recorder on the three platforms
// produces. No audio types, despite this being a music app: track files reach
// the app through the track's own File variations, and a report about one
// should point at the track. Both MIME spellings of a .zip are here so a file
// picked on Windows and one dropped on macOS both land as .zip.
var attachmentKinds = map[string]string{
	"image/png":                    ".png",
	"image/jpeg":                   ".jpg",
	"image/gif":                    ".gif",
	"image/webp":                   ".webp",
	"video/mp4":                    ".mp4",
	"video/quicktime":              ".mov",
	"video/webm":                   ".webm",
	"application/zip":              ".zip",
	"application/x-zip-compressed": ".zip",
}

// attachmentLimit is how big one of these may be, which depends on what it is.
func attachmentLimit(mime string) int {
	m := strings.ToLower(mime)
	switch {
	case strings.HasPrefix(m, "video/"):
		return videoMax
	case m == "application/zip" || m == "application/x-zip-compressed":
		return archiveMax
	default:
		return attachmentMax
	}
}

// AttachBugMedia writes a pasted, dropped or picked file and returns the path
// it was written to.
//
// The folder is named by whatever the caller is calling this report — a real
// id when the report exists, and a draft token while it is still being typed,
// which is the usual case: somebody pastes a screenshot before they have
// finished the sentence. adoptAttachments moves the draft ones under the real
// id at save time, so the form never has to save early to have somewhere to
// put a picture.
func (a *App) AttachBugMedia(reportID, dataURL string) (string, error) {
	reportID = strings.TrimSpace(reportID)
	if reportID == "" {
		return "", fmt.Errorf("an attachment needs a report to belong to")
	}
	// The id names a directory, so it may not be allowed to climb out of one.
	// Nothing in this app sends anything but its own generated ids here, which
	// is exactly why it is worth checking — the day something else does, this
	// is the line that stops it writing wherever it likes.
	if reportID != filepath.Base(reportID) || strings.Contains(reportID, "..") {
		return "", fmt.Errorf("that is not a report id")
	}

	kind, payload, err := decodeDataURL(dataURL)
	if err != nil {
		return "", err
	}

	dir, err := attachmentsDir(reportID)
	if err != nil {
		return "", err
	}
	existing, _ := os.ReadDir(dir)
	if len(existing) >= attachmentsPerReport {
		return "", fmt.Errorf(
			"a report can carry %d files — describe the rest", attachmentsPerReport)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("could not make somewhere to keep it: %w", err)
	}

	target := filepath.Join(dir, fmt.Sprintf("%d%s", len(existing)+1, kind))
	if err := os.WriteFile(target, payload, 0o600); err != nil {
		return "", fmt.Errorf("could not save it: %w", err)
	}
	return target, nil
}

// ReadBugImage returns an attached image as a data URL, for the page to draw.
//
// Read back through a binding rather than pointed at with a file:// url,
// because the webview will not load one — and because this is the only door,
// which means the path check here is the only door too.
//
// Images only, and deliberately so. Everything through here is base64 over the
// bindings, which is a third again on top of the bytes; a sixty-megabyte
// recording would be eighty megabytes of string built in Go, parsed in
// JavaScript and held in a component, to draw a thumbnail the size of a stamp.
// The page shows recordings and archives as a chip with their name instead.
func (a *App) ReadBugImage(path string) (string, error) {
	root, err := attachmentsRoot()
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(path)
	// Inside the attachments directory or nowhere. This is handed a path that
	// came from a record, and a record is not a promise.
	if !strings.HasPrefix(clean, root+string(filepath.Separator)) {
		return "", fmt.Errorf("that is not an attachment")
	}
	payload, err := os.ReadFile(clean)
	if err != nil {
		return "", fmt.Errorf("that image is no longer there")
	}
	kind := "image/png"
	for mime, ext := range attachmentKinds {
		if strings.EqualFold(filepath.Ext(clean), ext) {
			kind = mime
			break
		}
	}
	return "data:" + kind + ";base64," + base64.StdEncoding.EncodeToString(payload), nil
}

// forgetBugAttachments removes a report's files. Called when the report is
// resolved — the only way one leaves this queue — because a folder of
// screenshots for reports nobody can name is a folder that only grows.
func forgetBugAttachments(reportID string) {
	dir, err := attachmentsDir(reportID)
	if err != nil {
		return
	}
	_ = os.RemoveAll(dir)
}

// adoptAttachments moves a draft's files under the report that has just been
// created and returns their new paths.
//
// Called on create only. A path that is already under the report's own folder,
// or that cannot be moved, is left as it is: a report saved with a picture it
// cannot point at is worse than one pointing at a file in an oddly named
// folder, and the folder name is not what anybody reads.
func adoptAttachments(reportID string, paths []string) []string {
	dir, err := attachmentsDir(reportID)
	if err != nil {
		return paths
	}
	adopted := make([]string, 0, len(paths))
	for _, path := range paths {
		if filepath.Dir(path) == dir {
			adopted = append(adopted, path)
			continue
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			adopted = append(adopted, path)
			continue
		}
		target := filepath.Join(dir, filepath.Base(path))
		if err := os.Rename(path, target); err != nil {
			adopted = append(adopted, path)
			continue
		}
		// The draft folder is this app's and now empty; leaving it would mean a
		// directory per abandoned paste.
		_ = os.Remove(filepath.Dir(path))
		adopted = append(adopted, target)
	}
	return adopted
}

func attachmentsRoot() (string, error) {
	dir, err := dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "bug-attachments"), nil
}

func attachmentsDir(reportID string) (string, error) {
	root, err := attachmentsRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, reportID), nil
}

// decodeDataURL reads what the clipboard handed the page.
//
// Strict about the shape because what comes out of it is a file this app
// writes: an unrecognised type is refused rather than saved with a guessed
// extension, and the size is checked before the decode rather than after, so a
// 200MB paste is turned away without being held in memory first.
func decodeDataURL(dataURL string) (string, []byte, error) {
	mime, encoded, err := splitDataURL(dataURL)
	if err != nil {
		return "", nil, err
	}
	kind, known := attachmentKinds[mime]
	if !known {
		return "", nil, fmt.Errorf("%s is not something this app can keep on a report", mime)
	}

	// Base64 is four characters per three bytes, so this bounds the decode
	// without doing it.
	limit := attachmentLimit(mime)
	if len(encoded)/4*3 > limit {
		return "", nil, fmt.Errorf("that file is larger than %dMB", limit>>20)
	}
	payload, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", nil, fmt.Errorf("that file could not be read")
	}
	if len(payload) == 0 {
		return "", nil, fmt.Errorf("that file is empty")
	}
	return kind, payload, nil
}

// splitDataURL parses "data:<mime>;base64,<payload>" without decoding the
// payload, so a caller can refuse an oversized one before holding its bytes.
// Shared with the interview attachments (#24), which take a different set of
// kinds through the same shape.
func splitDataURL(dataURL string) (string, string, error) {
	dataURL = strings.TrimSpace(dataURL)
	head, encoded, ok := strings.Cut(dataURL, ",")
	if !ok || !strings.HasPrefix(head, "data:") || !strings.HasSuffix(head, ";base64") {
		return "", "", fmt.Errorf("that does not look like a file this app can keep")
	}
	mime := strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(head, "data:"), ";base64"))
	return mime, encoded, nil
}

// encodePaths and decodePaths are how the list sits in its TEXT column. JSON
// rather than a delimiter, because a path is user-controlled text and any
// character chosen to separate them is a character some path may contain.
func encodePaths(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	out, err := json.Marshal(paths)
	if err != nil {
		return ""
	}
	return string(out)
}

// decodePaths reads the column back. A row whose column got mangled is still a
// report, so bad JSON decodes to no attachments rather than an error.
func decodePaths(encoded string) []string {
	if strings.TrimSpace(encoded) == "" {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal([]byte(encoded), &out); err != nil {
		return []string{}
	}
	return out
}
