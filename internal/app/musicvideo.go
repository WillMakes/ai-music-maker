package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

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
// faded in and out with the gap from Settings between songs. Filed beside the
// takes in the album's folder, and recorded on the album.
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
	_, err = renderVideo(context.Background(), spec, func(fraction float64) {
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
	if err := a.store.setAlbumVideo(albumID, spec.Out); err != nil {
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
		Out:    filepath.Join(folder, album.Slug+"-music-video.mp4"),
	}, nil
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

// setAlbumVideo records where the album's video was filed.
func (s *Store) setAlbumVideo(id, path string) error {
	if s == nil || s.db == nil {
		return errNoStore
	}
	if _, err := s.db.Exec(`UPDATE albums SET video_file = ?, updated_at = ? WHERE id = ?`,
		path, nowStamp(), id); err != nil {
		return err
	}
	s.changed(scopeAlbums)
	return nil
}
