package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aimusicmaker/internal/audio"
	"aimusicmaker/internal/ytmusic"
)

// keyYTMusic holds the YouTube Music session (#124): the signed-in browser's
// cookie and the account it is for. A secret, like keyYouTube — the cookie is
// the producer's Google sign-in — so the MCP setting tools refuse it.
const keyYTMusic = "ytmusic"

type ytmusicConnection struct {
	Auth        ytmusic.Auth `json:"auth"`
	Account     string       `json:"account"`
	ConnectedAt string       `json:"connectedAt"`
}

// YTMusicStatus is the session as the window sees it: never the cookie.
type YTMusicStatus struct {
	Connected   bool   `json:"connected"`
	Account     string `json:"account"`
	ConnectedAt string `json:"connectedAt"`
}

// The ytmusic calls, variables so a test reaches no Google.
var (
	ytmusicAccount = ytmusic.AccountName
	ytmusicUpload  = ytmusic.Upload
)

// GetYouTubeMusic reports the session.
func (a *App) GetYouTubeMusic() YTMusicStatus {
	c := a.ytmusicConnection()
	return YTMusicStatus{Connected: c.Auth.Cookie != "", Account: c.Account, ConnectedAt: c.ConnectedAt}
}

// ConnectYouTubeMusic takes the request headers the producer copied from a
// signed-in music.youtube.com tab, checks YouTube Music accepts them, and
// stores the session. Checked first so a bad paste is refused here, in the
// field it was pasted into, rather than at the first upload minutes later.
func (a *App) ConnectYouTubeMusic(headers string) (YTMusicStatus, error) {
	if a.store == nil {
		return YTMusicStatus{}, errNoStore
	}
	auth, err := ytmusic.ParseHeaders(headers)
	if err != nil {
		return YTMusicStatus{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	account, err := ytmusicAccount(ctx, auth)
	if err != nil {
		return YTMusicStatus{}, err
	}
	if err := a.saveYTMusicConnection(ytmusicConnection{Auth: auth, Account: account, ConnectedAt: nowStamp()}); err != nil {
		return YTMusicStatus{}, err
	}
	return a.GetYouTubeMusic(), nil
}

// DisconnectYouTubeMusic forgets the session. The browser stays signed in;
// this only stops the app using it.
func (a *App) DisconnectYouTubeMusic() (YTMusicStatus, error) {
	if a.store == nil {
		return YTMusicStatus{}, errNoStore
	}
	if err := a.saveYTMusicConnection(ytmusicConnection{}); err != nil {
		return YTMusicStatus{}, err
	}
	return a.GetYouTubeMusic(), nil
}

// UploadAlbumToYouTubeMusic sends every track's official take to the
// producer's YouTube Music library (#124), each tagged first with its title,
// the artist, the album, its place in the running order and the cover, so the
// library files them as one album. The library is the account's own — this
// is not a release, which is a distributor's to make.
//
// Track by track, stopping at the first failure and saying how far it got:
// the songs already sent are in the library, and an upload started again
// would send them twice.
func (a *App) UploadAlbumToYouTubeMusic(albumID string) (Album, error) {
	if a.store == nil {
		return Album{}, errNoStore
	}
	album, err := a.store.album(albumID)
	if err != nil {
		return Album{}, err
	}
	session := a.ytmusicConnection()
	if session.Auth.Cookie == "" {
		return Album{}, errors.New("YouTube Music is not connected — paste its headers in Settings → YouTube")
	}
	tracks, err := a.store.listTracks(album.ID)
	if err != nil {
		return Album{}, err
	}
	if len(tracks) == 0 {
		return Album{}, fmt.Errorf("%s has no tracks", album.Name)
	}
	var lacking []string
	takes := make([]*TrackFile, len(tracks))
	for i, track := range tracks {
		if takes[i] = officialTake(track); takes[i] == nil {
			lacking = append(lacking, track.Name)
		}
	}
	if len(lacking) > 0 {
		return Album{}, fmt.Errorf("these tracks need an official take first: %s", strings.Join(lacking, ", "))
	}
	if _, running := musicVideoRuns.LoadOrStore("ytmusic:"+albumID, true); running {
		return Album{}, fmt.Errorf("%s is already uploading to YouTube Music", album.Name)
	}
	defer musicVideoRuns.Delete("ytmusic:" + albumID)

	cover := ""
	if album.ArtworkFile != "" {
		if dir, err := artworkDir(); err == nil && fileExists(filepath.Join(dir, album.ArtworkFile)) {
			cover = filepath.Join(dir, album.ArtworkFile)
		}
	}
	work, err := os.MkdirTemp("", "aimusicmaker-ytmusic-")
	if err != nil {
		return Album{}, err
	}
	defer os.RemoveAll(work)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Hour)
	defer cancel()
	last := -1
	report := func(fraction float64) {
		if pct := int(fraction * 100); pct != last {
			last = pct
			a.emit("ytmusicupload:progress", map[string]any{"albumId": albumID, "fraction": fraction})
		}
	}
	for i, track := range tracks {
		// Named for the song, since the start request sends the file name and
		// YouTube Music shows it until it has read the tags.
		tagged := filepath.Join(work, fmt.Sprintf("%02d %s.mp3", i+1, safeFileName(track.Name)))
		err := audio.TagForUpload(takes[i].Path, tagged, audio.Tags{
			Title:      track.Name,
			Artist:     album.ArtistName,
			Album:      album.Name,
			Track:      i + 1,
			TrackCount: len(tracks),
			Year:       audio.CurrentYear(),
			Cover:      cover,
		})
		if errors.Is(err, audio.ErrNoEncoder) {
			return Album{}, errors.New("uploading to YouTube Music needs ffmpeg to tag the songs — install it and try again")
		}
		if err == nil {
			err = ytmusicUpload(ctx, session.Auth, tagged, func(f float64) {
				report((float64(i) + f) / float64(len(tracks)))
			})
		}
		if err != nil {
			done := ""
			if i > 0 {
				done = fmt.Sprintf(" The first %d are in your library already.", i)
			}
			return Album{}, fmt.Errorf("%q could not be uploaded: %w.%s", track.Name, err, done)
		}
	}
	report(1)
	if err := a.store.setAlbumYTMusicUploaded(albumID, nowStamp()); err != nil {
		return Album{}, err
	}
	return a.GetAlbum(albumID)
}

// safeFileName keeps a title to what every filesystem accepts.
func safeFileName(name string) string {
	clean := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return '-'
		}
		return r
	}, name)
	if strings.TrimSpace(clean) == "" {
		return "track"
	}
	return strings.TrimSpace(clean)
}

func (a *App) ytmusicConnection() ytmusicConnection {
	var c ytmusicConnection
	raw := a.aiSetting(keyYTMusic)
	if strings.TrimSpace(raw) == "" {
		return c
	}
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return ytmusicConnection{}
	}
	return c
}

func (a *App) saveYTMusicConnection(c ytmusicConnection) error {
	if a.store == nil {
		return errNoStore
	}
	encoded, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if err := a.store.setSetting(keyYTMusic, string(encoded)); err != nil {
		return err
	}
	a.store.changed(scopeYouTube)
	return nil
}

// setAlbumYTMusicUploaded records when the album went to YouTube Music.
func (s *Store) setAlbumYTMusicUploaded(id, at string) error {
	if s == nil || s.db == nil {
		return errNoStore
	}
	if _, err := s.db.Exec(`UPDATE albums SET ytmusic_uploaded_at = ?, updated_at = ? WHERE id = ?`,
		at, nowStamp(), id); err != nil {
		return err
	}
	s.changed(scopeAlbums)
	return nil
}
