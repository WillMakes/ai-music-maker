package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"aimusicmaker/internal/video"
)

// renderVideo is video.Render, a variable so a test can file a video without
// running ffmpeg for a whole record.
var renderVideo = video.Render

// musicVideoRuns holds the albums with a render in flight. Process-wide
// rather than on App: the window and the MCP server are separate processes,
// but within one of them a second press, or a tool call during a press,
// should be refused rather than race the first over the same output file.
var musicVideoRuns sync.Map

// RenderAlbumVideo renders the album's music video (#118): the official takes
// in running order, each over the cover with its title and a visualisation,
// faded in and out with the gap from Settings between songs, in the look the
// music-video skill chose (#119). Filed beside the takes in the album's
// folder, and recorded on the album.
//
// Slow — minutes of ffmpeg for a whole record — and synchronous, like the
// cover generation: the frontend holds the call in a provider above the pages
// and the status bar shows it, while "musicvideo:progress" events carry how
// far it has got.
func (a *App) RenderAlbumVideo(albumID string) (Album, error) {
	if a.store == nil {
		return Album{}, errNoStore
	}
	album, err := a.store.album(albumID)
	if err != nil {
		return Album{}, err
	}
	if _, running := musicVideoRuns.LoadOrStore(albumID, true); running {
		return Album{}, fmt.Errorf("a music video for %s is already rendering", album.Name)
	}
	defer musicVideoRuns.Delete(albumID)

	spec, err := a.musicVideoSpec(album)
	if err != nil {
		return Album{}, err
	}
	last := -1
	rendered, err := renderVideo(context.Background(), spec, func(fraction float64) {
		// Whole percents only: ffmpeg reports a dozen times a second, and the
		// status bar has nothing to show for a change it cannot draw.
		if pct := int(fraction * 100); pct != last {
			last = pct
			a.emit("musicvideo:progress", map[string]any{"albumId": albumID, "fraction": fraction})
		}
	})
	if err != nil {
		return Album{}, err
	}
	description := musicVideoDescription(album, spec, rendered.Starts)
	if err := a.store.setAlbumVideo(albumID, spec.Out, description); err != nil {
		return Album{}, err
	}
	return a.GetAlbum(albumID)
}

// musicVideoSpec checks the album is ready for a video and says what goes in
// it. Every refusal names what is missing, since each is something the
// producer can go and fix.
func (a *App) musicVideoSpec(album Album) (video.Spec, error) {
	if album.ArtworkFile == "" {
		return video.Spec{}, fmt.Errorf("%s has no cover yet — generate or add one first", album.Name)
	}
	dir, err := artworkDir()
	if err != nil {
		return video.Spec{}, err
	}
	cover := filepath.Join(dir, album.ArtworkFile)
	if _, err := os.Stat(cover); err != nil {
		return video.Spec{}, fmt.Errorf("the cover file for %s is missing — add it again", album.Name)
	}
	tracks, err := a.store.listTracks(album.ID)
	if err != nil {
		return video.Spec{}, err
	}
	if len(tracks) == 0 {
		return video.Spec{}, fmt.Errorf("%s has no tracks", album.Name)
	}
	var songs []video.Song
	var lacking []string
	for _, track := range tracks {
		take := officialTake(track)
		if take == nil {
			lacking = append(lacking, track.Name)
			continue
		}
		songs = append(songs, video.Song{Title: track.Name, Audio: take.Path})
	}
	if len(lacking) > 0 {
		return video.Spec{}, fmt.Errorf("these tracks need an official take first (star one, or add the only take): %s",
			strings.Join(lacking, ", "))
	}
	artist, err := a.store.artist(album.ArtistID)
	if err != nil {
		return video.Spec{}, err
	}
	folder, err := a.albumFolder(artist, album)
	if err != nil {
		return video.Spec{}, err
	}
	return video.Spec{
		Artist: artist.Name,
		Album:  album.Name,
		Cover:  cover,
		Songs:  songs,
		Gap:    a.musicVideoGap(),
		Style:  a.musicVideoStyle(artist, album),
		Out:    filepath.Join(folder, album.Slug+"-music-video.mp4"),
	}, nil
}

// askVideoStyle is the model call behind musicVideoStyle, a variable so a test
// can answer for the model.
var askVideoStyle = func(a *App, ctx context.Context, system string, turns []chatMsg) (string, error) {
	return a.askAI(ctx, system, turns)
}

// musicVideoStyle is the look the music-video skill picks for this record
// (#119): the skill is the brief, the record's sound and its cover's brief are
// what it is read against, and the answer is a handful of fixed choices.
//
// The look never fails the render. With no AI connected, a model that times
// out, or an answer that is not the JSON asked for, the video is drawn in
// video.DefaultStyle — a video in the plain look is a video, and a render
// refused because a style could not be chosen is not. Whatever comes back is
// checked by Normalized before ffmpeg sees it, so the model can only ever
// choose among the options, never write into the filtergraph.
func (a *App) musicVideoStyle(artist Artist, album Album) video.Style {
	if !a.GetAIReadiness().Ready {
		return video.DefaultStyle
	}
	brief := a.skillBody(skillMusicVideo)
	if strings.TrimSpace(brief) == "" {
		return video.DefaultStyle
	}
	var record strings.Builder
	fmt.Fprintf(&record, "Album: %s\nArtist: %s\n\nTechnical style:\n%s\n",
		album.Name, artist.Name, combinedStyle(artist, album))
	if prompt := strings.TrimSpace(album.ArtworkPrompt); prompt != "" {
		fmt.Fprintf(&record, "\nThe brief the cover was drawn from:\n%s\n", prompt)
	}
	record.WriteString("\nChoose the music video's look. Answer with the JSON object only.")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	answer, err := askVideoStyle(a, ctx, brief, []chatMsg{{Role: "user", Text: record.String()}})
	if err != nil {
		return video.DefaultStyle
	}
	var chosen struct {
		Visualizer string  `json:"visualizer"`
		Color      string  `json:"color"`
		Background string  `json:"background"`
		Layout     string  `json:"layout"`
		Fade       float64 `json:"fade"`
	}
	if err := json.Unmarshal([]byte(unfence(answer)), &chosen); err != nil {
		return video.DefaultStyle
	}
	return video.Style{
		Visualizer: chosen.Visualizer,
		Color:      chosen.Color,
		Background: chosen.Background,
		Layout:     chosen.Layout,
		Fade:       chosen.Fade,
	}.Normalized()
}

// officialTake is the take a video is cut from: the starred one, or the only
// one. A track with several takes and none starred has not been decided, and
// picking one for the producer would put a song in the video they may have
// passed over — unlike playback, which falls back to keep the song audible.
func officialTake(track Track) *TrackFile {
	var present []*TrackFile
	for i := range track.Files {
		if track.Files[i].Missing {
			continue
		}
		if track.Files[i].Chosen {
			return &track.Files[i]
		}
		present = append(present, &track.Files[i])
	}
	if len(present) == 1 {
		return present[0]
	}
	return nil
}

// musicVideoGap is the silence between songs, in seconds: the setting, or one
// second (#117's number) when it is unset or not a number.
func (a *App) musicVideoGap() float64 {
	const fallback = 1
	if a.store == nil {
		return fallback
	}
	raw, err := a.store.setting(keyMusicVideoGap)
	if err != nil || strings.TrimSpace(raw) == "" {
		return fallback
	}
	gap, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || gap < 0 || gap > 30 {
		return fallback
	}
	return gap
}

// musicVideoDescription is the video's YouTube description (#120): the record,
// a timestamp per song — YouTube makes each a link to where the song starts,
// and chapters of them once there are three — and wherever else the record
// can be heard. The times come from the render, which knows each take's real
// length and the gaps, rather than from the tracks' target lengths.
func musicVideoDescription(album Album, spec video.Spec, starts []float64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s by %s\n\n", spec.Album, spec.Artist)
	for i, song := range spec.Songs {
		if i < len(starts) {
			fmt.Fprintf(&b, "%s %s\n", video.Timestamp(starts[i]), song.Title)
		}
	}
	links := []struct{ name, url string }{
		{"Apple Music", album.AppleMusicURL},
		{"Spotify", album.SpotifyURL},
		{"YouTube Music", album.YouTubeURL},
	}
	first := true
	for _, link := range links {
		if strings.TrimSpace(link.url) == "" {
			continue
		}
		if first {
			b.WriteString("\nListen:\n")
			first = false
		}
		fmt.Fprintf(&b, "%s: %s\n", link.name, strings.TrimSpace(link.url))
	}
	return strings.TrimRight(b.String(), "\n")
}

// setAlbumVideo records where the album's video was filed and its description.
func (s *Store) setAlbumVideo(id, path, description string) error {
	if s == nil || s.db == nil {
		return errNoStore
	}
	if _, err := s.db.Exec(`UPDATE albums SET video_file = ?, video_description = ?, updated_at = ? WHERE id = ?`,
		path, description, nowStamp(), id); err != nil {
		return err
	}
	s.changed(scopeAlbums)
	return nil
}
