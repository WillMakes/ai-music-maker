package audio

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"aimusicmaker/internal/platform"
)

// Tags are what a library reads off a song: YouTube Music builds its entry
// for an uploaded song from these and nothing else (#124), so a take sent
// untagged arrives as its file name by an unknown artist.
type Tags struct {
	Title      string
	Artist     string
	Album      string
	Track      int
	TrackCount int
	Year       int
	// Cover is an image file to embed as the front cover; empty for none.
	Cover string
}

// TagForUpload writes a copy of src at dest, an MP3, with tags set and the
// cover embedded. An MP3 keeps its audio untouched — copied, not re-encoded —
// and anything else (a WAV kept because ffmpeg was missing at import, #93) is
// encoded to MP3 at 320 kbps on the way, the format every library takes.
//
// ffmpeg rather than a tag writer compiled in, because it is already what
// this app encodes and renders with, and the same call can convert the take
// that needs it. A variable so tests can tag without it.
var TagForUpload = func(src, dest string, tags Tags) error {
	ffmpeg, ok := platform.LookUp("ffmpeg")
	if !ok {
		return ErrNoEncoder
	}
	ctx, cancel := context.WithTimeout(context.Background(), mp3EncodeTimeout)
	defer cancel()

	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-i", src}
	if tags.Cover != "" {
		args = append(args, "-i", tags.Cover)
	}
	args = append(args, "-map", "0:a")
	if strings.HasSuffix(strings.ToLower(src), ".mp3") {
		args = append(args, "-c:a", "copy")
	} else {
		args = append(args, "-c:a", "libmp3lame", "-b:a", "320k")
	}
	if tags.Cover != "" {
		args = append(args, "-map", "1:v", "-c:v", "mjpeg", "-vf", "scale='min(1000,iw)':-2",
			"-disposition:v", "attached_pic",
			"-metadata:s:v", "title=Album cover", "-metadata:s:v", "comment=Cover (front)")
	}
	// The source's own tags are dropped, so a Suno download's title or
	// comment cannot outlive the producer's.
	args = append(args, "-map_metadata", "-1", "-id3v2_version", "3")
	meta := map[string]string{
		"title":        tags.Title,
		"artist":       tags.Artist,
		"album_artist": tags.Artist,
		"album":        tags.Album,
	}
	if tags.Track > 0 {
		track := strconv.Itoa(tags.Track)
		if tags.TrackCount > 0 {
			track += "/" + strconv.Itoa(tags.TrackCount)
		}
		meta["track"] = track
	}
	if tags.Year > 0 {
		meta["date"] = strconv.Itoa(tags.Year)
	}
	for _, key := range []string{"title", "artist", "album_artist", "album", "track", "date"} {
		if value := strings.TrimSpace(meta[key]); value != "" {
			args = append(args, "-metadata", key+"="+value)
		}
	}
	args = append(args, "-f", "mp3", dest)

	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	platform.HideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("tagging %s took longer than %s", src, mp3EncodeTimeout)
		}
		return fmt.Errorf("ffmpeg could not tag the take: %s", strings.TrimSpace(lastLine(string(out))))
	}
	return nil
}

// CurrentYear is the year a record is tagged with: the year it is sent to a
// library, which is the year it came out as far as that library knows.
func CurrentYear() int { return time.Now().Year() }

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}
