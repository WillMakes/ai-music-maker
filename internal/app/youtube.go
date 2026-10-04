package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"aimusicmaker/internal/youtube"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// keyYouTube holds the YouTube connection (#121) as one JSON blob, the way
// keyAIServices holds the AI ones: the client, the token and the channel are
// read and written together. A secret, so the MCP setting tools refuse it.
const keyYouTube = "youtube"

// youtubeConnection is what is stored.
type youtubeConnection struct {
	ClientID     string    `json:"clientId"`
	ClientSecret string    `json:"clientSecret"`
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	Expiry       time.Time `json:"expiry"`
	ChannelID    string    `json:"channelId"`
	ChannelTitle string    `json:"channelTitle"`
	ConnectedAt  string    `json:"connectedAt"`
}

func (c youtubeConnection) client() youtube.Client {
	return youtube.Client{ID: c.ClientID, Secret: c.ClientSecret}
}

// YouTubeStatus is the connection as the window sees it: never the secret or
// the tokens.
type YouTubeStatus struct {
	// HasClient is true once a client id and secret are saved.
	HasClient bool   `json:"hasClient"`
	ClientID  string `json:"clientId"`
	Connected bool   `json:"connected"`
	// ChannelID and ChannelTitle are the channel the sign-in was for.
	ChannelID    string `json:"channelId"`
	ChannelTitle string `json:"channelTitle"`
	ConnectedAt  string `json:"connectedAt"`
}

// authorizeYouTube is youtube.Authorize, a variable so a test can sign in
// without a browser.
var authorizeYouTube = youtube.Authorize

// GetYouTube reports the connection.
func (a *App) GetYouTube() YouTubeStatus {
	c := a.youtubeConnection()
	return YouTubeStatus{
		HasClient:    c.ClientID != "" && c.ClientSecret != "",
		ClientID:     c.ClientID,
		Connected:    c.RefreshToken != "",
		ChannelID:    c.ChannelID,
		ChannelTitle: c.ChannelTitle,
		ConnectedAt:  c.ConnectedAt,
	}
}

// SaveYouTubeClient stores the producer's OAuth client. A different client
// than the one connected drops the connection: its tokens were issued to the
// other client and would be refused on the first refresh anyway.
func (a *App) SaveYouTubeClient(clientID, clientSecret string) (YouTubeStatus, error) {
	if a.store == nil {
		return YouTubeStatus{}, errNoStore
	}
	clientID, clientSecret = strings.TrimSpace(clientID), strings.TrimSpace(clientSecret)
	if clientID == "" || clientSecret == "" {
		return YouTubeStatus{}, errors.New("both the client id and the client secret are needed")
	}
	if !strings.HasSuffix(clientID, ".apps.googleusercontent.com") {
		return YouTubeStatus{}, errors.New("that is not a Google OAuth client id — it ends in .apps.googleusercontent.com")
	}
	c := a.youtubeConnection()
	if c.ClientID != clientID {
		c = youtubeConnection{}
	}
	c.ClientID, c.ClientSecret = clientID, clientSecret
	if err := a.saveYouTubeConnection(c); err != nil {
		return YouTubeStatus{}, err
	}
	return a.GetYouTube(), nil
}

// ConnectYouTube signs in through the browser and records the channel. Needs
// the window: the sign-in page is opened in the producer's browser, and the
// MCP server has no way to open one.
func (a *App) ConnectYouTube() (YouTubeStatus, error) {
	if a.store == nil {
		return YouTubeStatus{}, errNoStore
	}
	if !hasWindow(a) {
		return YouTubeStatus{}, errors.New("connect YouTube from the app's window — the sign-in opens in your browser")
	}
	c := a.youtubeConnection()
	token, err := authorizeYouTube(context.Background(), c.client(), func(link string) {
		if a.ctx != nil {
			wruntime.BrowserOpenURL(a.ctx, link)
		}
	})
	if err != nil {
		return YouTubeStatus{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	channel, err := youtubeChannel(ctx, token.Access)
	if err != nil {
		return YouTubeStatus{}, err
	}
	c.AccessToken, c.RefreshToken, c.Expiry = token.Access, token.Refresh, token.Expiry
	c.ChannelID, c.ChannelTitle, c.ConnectedAt = channel.ID, channel.Title, nowStamp()
	if err := a.saveYouTubeConnection(c); err != nil {
		return YouTubeStatus{}, err
	}
	return a.GetYouTube(), nil
}

// hasWindow reports whether this process has a window to open the sign-in
// from — a variable so a test, which has none, can stand in for one.
var hasWindow = func(a *App) bool { return a.ctx != nil }

// youtubeChannel is youtube.MyChannel, a variable for the same reason.
var youtubeChannel = youtube.MyChannel

// DisconnectYouTube forgets the connection and tells Google to, keeping the
// client so connecting again is one press. Google is told best-effort: a
// disconnect that could not reach it has still forgotten the token here.
func (a *App) DisconnectYouTube() (YouTubeStatus, error) {
	if a.store == nil {
		return YouTubeStatus{}, errNoStore
	}
	c := a.youtubeConnection()
	if c.RefreshToken != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = youtube.Revoke(ctx, c.RefreshToken)
		cancel()
	}
	kept := youtubeConnection{ClientID: c.ClientID, ClientSecret: c.ClientSecret}
	if err := a.saveYouTubeConnection(kept); err != nil {
		return YouTubeStatus{}, err
	}
	return a.GetYouTube(), nil
}

// youtubeAccess is an access token that works now, refreshed and stored when
// the held one is about to expire — for the upload (#122) and the analytics
// (#123).
func (a *App) youtubeAccess(ctx context.Context) (string, error) {
	c := a.youtubeConnection()
	if c.RefreshToken == "" {
		return "", errors.New("YouTube is not connected — connect it in Settings → YouTube")
	}
	held := youtube.Token{Access: c.AccessToken, Refresh: c.RefreshToken, Expiry: c.Expiry}
	if held.Valid() {
		return held.Access, nil
	}
	token, err := refreshYouTube(ctx, c.client(), c.RefreshToken)
	if err != nil {
		return "", err
	}
	c.AccessToken, c.RefreshToken, c.Expiry = token.Access, token.Refresh, token.Expiry
	if err := a.saveYouTubeConnection(c); err != nil {
		return "", fmt.Errorf("saving the refreshed YouTube token: %w", err)
	}
	return token.Access, nil
}

// refreshYouTube is youtube.Refreshed, a variable for the same reason.
var refreshYouTube = youtube.Refreshed

// youtubeConnection reads the stored blob — the empty connection when there
// is none or it cannot be read, for aiConnections' reason: a blob this app
// cannot decode is one it wrote wrong, and "not connected" lets the producer
// connect again.
func (a *App) youtubeConnection() youtubeConnection {
	var c youtubeConnection
	raw := a.aiSetting(keyYouTube)
	if strings.TrimSpace(raw) == "" {
		return c
	}
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return youtubeConnection{}
	}
	return c
}

func (a *App) saveYouTubeConnection(c youtubeConnection) error {
	if a.store == nil {
		return errNoStore
	}
	encoded, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if err := a.store.setSetting(keyYouTube, string(encoded)); err != nil {
		return err
	}
	a.store.changed(scopeYouTube)
	return nil
}

// uploadVideo is youtube.Upload, a variable so a test uploads nowhere.
var uploadVideo = youtube.Upload

// UploadAlbumVideo sends the album's rendered music video to the connected
// channel (#122), titled for the record and described with its timestamps
// (#120), and records the new video on the album. privacy is "private",
// "unlisted" or "public".
//
// Slow, like the render, and held by the same provider above the pages;
// "youtubeupload:progress" events carry how much has been sent.
func (a *App) UploadAlbumVideo(albumID, privacy string) (Album, error) {
	if a.store == nil {
		return Album{}, errNoStore
	}
	album, err := a.store.album(albumID)
	if err != nil {
		return Album{}, err
	}
	if album.VideoFile == "" {
		return Album{}, fmt.Errorf("render %s's music video first", album.Name)
	}
	if !fileExists(album.VideoFile) {
		return Album{}, fmt.Errorf("the music video file is gone from %s — render it again", album.VideoFile)
	}
	if _, running := musicVideoRuns.LoadOrStore("upload:"+albumID, true); running {
		return Album{}, fmt.Errorf("%s's video is already uploading", album.Name)
	}
	defer musicVideoRuns.Delete("upload:" + albumID)

	// Hours at most: a few hundred megabytes on a slow line. The context is
	// what ends a stalled upload, since the client has no timeout of its own.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Hour)
	defer cancel()
	access, err := a.youtubeAccess(ctx)
	if err != nil {
		return Album{}, err
	}
	last := -1
	id, err := uploadVideo(ctx, access, album.VideoFile, youtube.Video{
		Title:       album.ArtistName + " — " + album.Name + " (Full Album)",
		Description: album.VideoDescription,
		Privacy:     privacy,
		Tags:        []string{album.ArtistName, album.Name},
	}, func(fraction float64) {
		if pct := int(fraction * 100); pct != last {
			last = pct
			a.emit("youtubeupload:progress", map[string]any{"albumId": albumID, "fraction": fraction})
		}
	})
	if err != nil {
		return Album{}, err
	}
	if err := a.store.setAlbumYouTubeVideo(albumID, id); err != nil {
		return Album{}, err
	}
	return a.GetAlbum(albumID)
}

// setAlbumYouTubeVideo records the video an upload made.
func (s *Store) setAlbumYouTubeVideo(id, videoID string) error {
	if s == nil || s.db == nil {
		return errNoStore
	}
	if _, err := s.db.Exec(`UPDATE albums SET video_youtube_id = ?, updated_at = ? WHERE id = ?`,
		videoID, nowStamp(), id); err != nil {
		return err
	}
	s.changed(scopeAlbums)
	return nil
}

// videoStats is youtube.VideoStats, a variable so a test reads no channel.
var videoStats = youtube.VideoStats

// GetAlbumVideoStats reads how the album's uploaded video is doing (#123):
// the live counts, and the analytics report since it was published. Read on
// request rather than stored — the numbers are only worth what they are
// today, and every read costs the producer's API quota.
func (a *App) GetAlbumVideoStats(albumID string) (youtube.Stats, error) {
	if a.store == nil {
		return youtube.Stats{}, errNoStore
	}
	album, err := a.store.album(albumID)
	if err != nil {
		return youtube.Stats{}, err
	}
	if album.VideoYouTubeID == "" {
		return youtube.Stats{}, fmt.Errorf("%s's video has not been uploaded to YouTube", album.Name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	access, err := a.youtubeAccess(ctx)
	if err != nil {
		return youtube.Stats{}, err
	}
	return videoStats(ctx, access, album.VideoYouTubeID)
}
