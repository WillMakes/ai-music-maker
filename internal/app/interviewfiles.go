package app

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// Files on an interview (#24)
//
// A producer mid-conversation about an album often has the evidence in hand: a
// reference image for the artwork, and above all the finished Suno files for
// tracks that already exist. Making them describe a file they could hand over
// is the interview equivalent of the bug form asking somebody to retype the
// page they are on — so the chat takes files, by drop, paste or the clip.
//
// The files land under this app's own directory, keyed the way the transcript
// is (kind + subject), and the paths ride on the message they were sent with.
// The model never receives the bytes — it is a text conversation — but every
// turn's attachments are named to it (see toChatMsgs), which is enough to talk
// about them and, at the end, to assign each audio file to the track it is a
// take of (see ExtractTracks). The import into the music library happens when
// the tracks are created, through the same ImportTrackFile every other route
// uses; this folder is a waiting room, and clearing the chat sweeps it.
// ---------------------------------------------------------------------------

// interviewAttachmentKinds is what a chat will take, by MIME. Audio is the
// point of the feature — finished Suno tracks — and images ride along for
// artwork references. No video and no archives: nothing in an interview reads
// them, and a file nothing will read is clutter with a path.
var interviewAttachmentKinds = map[string]string{
	"audio/mpeg":   ".mp3",
	"audio/mp3":    ".mp3",
	"audio/wav":    ".wav",
	"audio/x-wav":  ".wav",
	"audio/wave":   ".wav",
	"audio/flac":   ".flac",
	"audio/x-flac": ".flac",
	"audio/mp4":    ".m4a",
	"audio/x-m4a":  ".m4a",
	"audio/aac":    ".aac",
	"audio/ogg":    ".ogg",
	"image/png":    ".png",
	"image/jpeg":   ".jpg",
	"image/gif":    ".gif",
	"image/webp":   ".webp",
}

// interviewAudioMax bounds one audio upload. A Suno download is a few
// megabytes and a lossless master tens; the bytes cross the binding bridge as
// base64, so the track library's half-gigabyte ceiling would mean building a
// 700MB string just to say no to it.
const interviewAudioMax = 64 << 20

// AttachInterviewFile stores one file against a conversation and returns the
// path it was written to.
//
// name is the file's own name as the producer's machine knows it, and it is
// kept (uniquified, never overwritten): a Suno download is named after the
// track it is a take of, and that name is how the extraction pass assigns the
// file to the right track at the end.
func (a *App) AttachInterviewFile(kind, subjectID, name, dataURL string) (string, error) {
	base := filepath.Base(strings.TrimSpace(name))
	if base == "" || base == "." || base == ".." || base != strings.TrimSpace(name) {
		return "", fmt.Errorf("that is not a file name")
	}

	mime, encoded, err := splitDataURL(dataURL)
	if err != nil {
		return "", err
	}
	mimeExt, known := interviewAttachmentKinds[mime]
	if !known {
		return "", fmt.Errorf("%s is not something an interview can take", mime)
	}
	limit := attachmentMax
	if strings.HasPrefix(mime, "audio/") {
		limit = interviewAudioMax
	}
	// Base64 is four characters per three bytes, so this bounds the decode
	// without doing it.
	if len(encoded)/4*3 > limit {
		return "", fmt.Errorf("%s is larger than %dMB", base, limit>>20)
	}
	payload, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(payload) == 0 {
		return "", fmt.Errorf("%s could not be read", base)
	}

	// The extension the file is written with comes from its own name where that
	// name is one this app knows, and from the MIME where it is not — the path
	// ends up in the music library, and "track.exe" with an audio MIME is not a
	// name to reproduce.
	ext := strings.ToLower(filepath.Ext(base))
	if !audioExtensions[ext] && !imageExtension(ext) {
		ext = mimeExt
	}

	dir, err := interviewFilesDir(kind, subjectID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("could not make somewhere to keep it: %w", err)
	}
	target, err := uniquePath(dir, strings.TrimSuffix(base, filepath.Ext(base)), ext)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(target, payload, 0o600); err != nil {
		return "", fmt.Errorf("could not save it: %w", err)
	}
	return target, nil
}

func imageExtension(ext string) bool {
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	}
	return false
}

// clearInterviewFiles sweeps a conversation's waiting room. Called when the
// chat is cleared — by then every file worth keeping has been imported into
// the music library under its track, and what is left belongs to a
// conversation that no longer exists.
func clearInterviewFiles(kind, subjectID string) {
	dir, err := interviewFilesDir(kind, subjectID)
	if err != nil {
		return
	}
	_ = os.RemoveAll(dir)
}

// interviewFilesDir is one conversation's folder, keyed the way the chat row
// is. subjectID "" is the conversation that creates a record, which is a real
// thread and needs a real folder.
func interviewFilesDir(kind, subjectID string) (string, error) {
	dir, err := dataDir()
	if err != nil {
		return "", err
	}
	key := kind
	if subjectID != "" {
		key += "-" + subjectID
	} else {
		key += "-new"
	}
	return filepath.Join(dir, "interview-attachments", key), nil
}

// attachedAudioNames is every audio file on the transcript, by base name, in
// the order sent — what the extraction pass is told it has to assign.
func attachedAudioNames(messages []InterviewMessage) []string {
	names := []string{}
	for _, message := range messages {
		for _, path := range message.Attachments {
			if audioExtensions[strings.ToLower(filepath.Ext(path))] {
				names = append(names, filepath.Base(path))
			}
		}
	}
	return names
}
