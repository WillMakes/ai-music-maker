package youtube

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUpload is #122 against a fake YouTube: the session request carries the
// metadata (cleaned of what YouTube refuses, in the Music category), the
// file follows whole on the session's address, and the new video's id comes
// back with progress having reached the end.
func TestUpload(t *testing.T) {
	content := strings.Repeat("v", 300_000)
	var meta map[string]map[string]any
	var received string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer access" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/upload/youtube/v3/videos":
			if r.URL.Query().Get("uploadType") != "resumable" {
				t.Errorf("not a resumable upload: %s", r.URL)
			}
			if r.Header.Get("X-Upload-Content-Length") != "300000" {
				t.Errorf("length = %q", r.Header.Get("X-Upload-Content-Length"))
			}
			_ = json.NewDecoder(r.Body).Decode(&meta)
			w.Header().Set("Location", server.URL+"/session/1")
		case r.Method == http.MethodPut && r.URL.Path == "/session/1":
			body, _ := io.ReadAll(r.Body)
			received = string(body)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "vid123"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	previous := APIBase
	APIBase = server.URL
	t.Cleanup(func() { APIBase = previous })

	path := filepath.Join(t.TempDir(), "video.mp4")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	var last float64
	id, err := Upload(context.Background(), "access", path, Video{
		Title:       "Night Drive <3",
		Description: "0:00 Neon Rain",
		Privacy:     "unlisted",
	}, func(f float64) { last = f })
	if err != nil {
		t.Fatal(err)
	}
	if id != "vid123" || received != content || last != 1 {
		t.Fatalf("id %q, %d bytes received, progress %v", id, len(received), last)
	}
	if meta["snippet"]["title"] != "Night Drive 3" || meta["snippet"]["categoryId"] != "10" {
		t.Errorf("snippet = %v", meta["snippet"])
	}
	if meta["status"]["privacyStatus"] != "unlisted" {
		t.Errorf("status = %v", meta["status"])
	}

	if _, err := Upload(context.Background(), "access", path, Video{Privacy: "friends"}, nil); err == nil {
		t.Error("a privacy YouTube does not have was sent")
	}
}

func TestClipBytes(t *testing.T) {
	long := strings.Repeat("0:00 Song é\n", 600) // well over 5000 bytes
	got := clipBytes(long, 5000)
	if len(got) > 5000 || !strings.HasSuffix(got, "Song é") {
		t.Errorf("clipped to %d bytes ending %q", len(got), got[len(got)-10:])
	}
}
