package youtube

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Video is what an upload says about the video.
type Video struct {
	Title       string
	Description string
	// Privacy is "private", "unlisted" or "public".
	Privacy string
	Tags    []string
}

// uploadClient has no timeout of its own: an album's video is hundreds of
// megabytes, and the context the caller passes is what bounds it.
var uploadClient = &http.Client{}

// Upload sends the file at path to the channel as a new video and returns its
// id (#122). progress is called with the fraction sent.
//
// A resumable upload, started and sent in one go: the session request carries
// the metadata, and the file follows as one PUT. Not resumed across a failure —
// a failed upload is started again from the album page — but the resumable
// protocol is the one Google documents for files this size, and the only one
// that takes metadata and a large body without a multipart encoding.
func Upload(ctx context.Context, access, path string, video Video, progress func(float64)) (string, error) {
	if progress == nil {
		progress = func(float64) {}
	}
	switch video.Privacy {
	case "private", "unlisted", "public":
	default:
		return "", fmt.Errorf("privacy %q is not one of private, unlisted or public", video.Privacy)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("the video file could not be opened: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}

	meta := map[string]any{
		"snippet": map[string]any{
			"title":       clipRunes(clean(video.Title), 100),
			"description": clipBytes(clean(video.Description), 5000),
			"tags":        video.Tags,
			// Music, so the video is filed where a listener looks for one.
			"categoryId": "10",
		},
		"status": map[string]any{
			"privacyStatus":           video.Privacy,
			"selfDeclaredMadeForKids": false,
		},
	}
	body, _ := json.Marshal(meta)
	start, err := http.NewRequestWithContext(ctx, http.MethodPost,
		APIBase+"/upload/youtube/v3/videos?uploadType=resumable&part=snippet,status", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	start.Header.Set("Authorization", "Bearer "+access)
	start.Header.Set("Content-Type", "application/json; charset=UTF-8")
	start.Header.Set("X-Upload-Content-Type", "video/mp4")
	start.Header.Set("X-Upload-Content-Length", strconv.FormatInt(info.Size(), 10))
	resp, err := uploadClient.Do(start)
	if err != nil {
		return "", fmt.Errorf("could not reach YouTube: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", apiError(resp)
	}
	session := resp.Header.Get("Location")
	if session == "" {
		return "", errors.New("YouTube did not start an upload session")
	}

	put, err := http.NewRequestWithContext(ctx, http.MethodPut, session,
		&counting{r: file, total: info.Size(), progress: progress})
	if err != nil {
		return "", err
	}
	put.ContentLength = info.Size()
	put.Header.Set("Authorization", "Bearer "+access)
	put.Header.Set("Content-Type", "video/mp4")
	resp, err = uploadClient.Do(put)
	if err != nil {
		return "", fmt.Errorf("the upload was cut off: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", apiError(resp)
	}
	var made struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&made); err != nil || made.ID == "" {
		return "", errors.New("YouTube took the upload but did not say which video it made")
	}
	progress(1)
	return made.ID, nil
}

// WatchURL is where a video is watched.
func WatchURL(id string) string {
	if id == "" {
		return ""
	}
	return "https://www.youtube.com/watch?v=" + id
}

// counting reports how much of the body has been read.
type counting struct {
	r        io.Reader
	total    int64
	sent     int64
	progress func(float64)
}

func (c *counting) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.sent += int64(n)
	if c.total > 0 {
		// Short of 1 until YouTube answers: the bytes are sent, but the video
		// is not made until the response says so.
		c.progress(min(0.99, float64(c.sent)/float64(c.total)))
	}
	return n, err
}

// clean drops the angle brackets YouTube refuses in a title or description —
// a song called "<3" would otherwise fail the whole upload.
func clean(s string) string {
	return strings.NewReplacer("<", "", ">", "").Replace(s)
}

// clipRunes keeps a title within YouTube's 100 characters.
func clipRunes(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return string([]rune(s)[:limit])
}

// clipBytes keeps a description within YouTube's 5000 bytes, cutting at a
// line where it can so a timestamp is not left half-written, and never in
// the middle of a character.
func clipBytes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	if line := strings.LastIndexByte(s[:cut], '\n'); line > limit/2 {
		cut = line
	}
	return s[:cut]
}
