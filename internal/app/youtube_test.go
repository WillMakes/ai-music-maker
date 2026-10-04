package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"aimusicmaker/internal/youtube"
)

// stubYouTube signs in without a browser or Google, and counts refreshes.
func stubYouTube(t *testing.T) (refreshes *int) {
	t.Helper()
	refreshes = new(int)
	prevAuth, prevChannel, prevRefresh, prevWindow := authorizeYouTube, youtubeChannel, refreshYouTube, hasWindow
	t.Cleanup(func() {
		authorizeYouTube, youtubeChannel, refreshYouTube, hasWindow = prevAuth, prevChannel, prevRefresh, prevWindow
	})
	hasWindow = func(*App) bool { return true }
	authorizeYouTube = func(_ context.Context, c youtube.Client, _ func(string)) (youtube.Token, error) {
		if c.ID == "" || c.Secret == "" {
			t.Errorf("signed in without the client: %+v", c)
		}
		return youtube.Token{Access: "access-1", Refresh: "refresh-1", Expiry: time.Now().Add(time.Hour)}, nil
	}
	youtubeChannel = func(context.Context, string) (youtube.Channel, error) {
		return youtube.Channel{ID: "UC123", Title: "Cassette Motel"}, nil
	}
	refreshYouTube = func(_ context.Context, _ youtube.Client, refresh string) (youtube.Token, error) {
		*refreshes++
		return youtube.Token{Access: "access-2", Refresh: refresh, Expiry: time.Now().Add(time.Hour)}, nil
	}
	return refreshes
}

// TestYouTubeConnection is #121: a client is saved, the sign-in records the
// channel, an expiring token is refreshed and stored, and a disconnect keeps
// the client. The window's status never carries the secret or the tokens.
func TestYouTubeConnection(t *testing.T) {
	app := newTestApp(t)
	refreshes := stubYouTube(t)

	if _, err := app.SaveYouTubeClient("not-a-client", "s"); err == nil {
		t.Fatal("a client id that is not Google's was accepted")
	}
	if _, err := app.youtubeAccess(context.Background()); err == nil {
		t.Fatal("an access token was handed out with nothing connected")
	}
	status, err := app.SaveYouTubeClient(" cid.apps.googleusercontent.com ", " secret ")
	if err != nil || !status.HasClient || status.Connected {
		t.Fatalf("status = %+v, %v", status, err)
	}

	status, err = app.ConnectYouTube()
	if err != nil {
		t.Fatal(err)
	}
	if !status.Connected || status.ChannelTitle != "Cassette Motel" || status.ChannelID != "UC123" {
		t.Fatalf("status = %+v", status)
	}
	encoded, _ := json.Marshal(status)
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "refresh-1") {
		t.Fatalf("the status carries a credential: %s", encoded)
	}

	access, err := app.youtubeAccess(context.Background())
	if err != nil || access != "access-1" || *refreshes != 0 {
		t.Fatalf("a valid token: %q, %v, %d refreshes", access, err, *refreshes)
	}
	// Expire it: the next call refreshes once and stores the new token.
	c := app.youtubeConnection()
	c.Expiry = time.Now()
	if err := app.saveYouTubeConnection(c); err != nil {
		t.Fatal(err)
	}
	if access, _ := app.youtubeAccess(context.Background()); access != "access-2" || *refreshes != 1 {
		t.Fatalf("an expired token: %q, %d refreshes", access, *refreshes)
	}
	if access, _ := app.youtubeAccess(context.Background()); access != "access-2" || *refreshes != 1 {
		t.Fatalf("the refreshed token was not kept: %q, %d refreshes", access, *refreshes)
	}

	// The MCP setting tools refuse the blob.
	for _, tool := range []string{"get_setting", "set_setting"} {
		out := callTool(t, app, tool, map[string]any{"key": keyYouTube, "value": "{}"})
		if !strings.Contains(out, "Settings → YouTube") {
			t.Errorf("%s handed the YouTube blob over: %s", tool, out)
		}
	}

	status, err = app.DisconnectYouTube()
	if err != nil || status.Connected || !status.HasClient {
		t.Fatalf("after disconnecting: %+v, %v", status, err)
	}
}

// callTool runs one MCP tool by name and answers its result as JSON text.
func callTool(t *testing.T, app *App, name string, args map[string]any) string {
	t.Helper()
	raw, _ := json.Marshal(args)
	for _, tool := range mcpTools() {
		if tool.name == name {
			out, _ := json.Marshal(tool.run(app, raw))
			return string(out)
		}
	}
	t.Fatalf("no tool %s", name)
	return ""
}
