package ytmusic

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const cookie = "VISITOR_INFO1_LIVE=abc; SAPISID=xyz/AAA; __Secure-3PAPISID=xyz/AAA; SID=s"

func TestParseHeaders(t *testing.T) {
	firefox := "POST /youtubei/v1/browse HTTP/2\nHost: music.youtube.com\nUser-Agent: Firefox/131\n" +
		"Cookie: " + cookie + "\nX-Goog-AuthUser: 1\n"
	chrome := ":authority:\nmusic.youtube.com\ncookie:\n" + cookie + "\nuser-agent:\nChrome/129\nx-goog-authuser:\n0\n"
	for name, raw := range map[string]string{"firefox": firefox, "chrome": chrome} {
		auth, err := ParseHeaders(raw)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if auth.Cookie != cookie || auth.UserAgent == "" || auth.AuthUser == "" {
			t.Errorf("%s: %+v", name, auth)
		}
	}
	if _, err := ParseHeaders("Cookie: SID=only"); err == nil || !strings.Contains(err.Error(), "SAPISID") {
		t.Errorf("a signed-out cookie was taken: %v", err)
	}
	if _, err := ParseHeaders("Accept: */*"); err == nil {
		t.Error("headers with no cookie were taken")
	}
}

// The header is ytmusicapi's: SAPISIDHASH <ts>_<sha1("<ts> <sapisid> <origin>")>.
func TestAuthorization(t *testing.T) {
	sum := sha1.Sum([]byte("1700000000 xyz/AAA https://music.youtube.com"))
	want := "SAPISIDHASH 1700000000_" + hex.EncodeToString(sum[:])
	if got := authorization(cookie, time.Unix(1700000000, 0)); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestUpload is #124 against a fake YouTube Music: the start request names the
// file and its length with the session's headers, and the file follows whole
// on the address the start answered with.
func TestUpload(t *testing.T) {
	content := strings.Repeat("a", 4096)
	var started http.Header
	var startBody, got string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "SAPISIDHASH ") || r.Header.Get("Cookie") != cookie {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/upload":
			started = r.Header.Clone()
			body, _ := io.ReadAll(r.Body)
			startBody = string(body)
			w.Header().Set("X-Goog-Upload-URL", server.URL+"/session")
		case "/session":
			if r.Header.Get("X-Goog-Upload-Command") != "upload, finalize" {
				t.Errorf("command = %q", r.Header.Get("X-Goog-Upload-Command"))
			}
			body, _ := io.ReadAll(r.Body)
			got = string(body)
		case "/api/account/account_menu":
			_ = json.NewEncoder(w).Encode(map[string]any{"actions": []any{map[string]any{"popup": map[string]any{
				"header": map[string]any{"accountName": map[string]any{"runs": []any{map[string]any{"text": "Will"}}}}}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	prevUpload, prevAPI := UploadURL, APIBase
	UploadURL, APIBase = server.URL+"/upload", server.URL+"/api"
	t.Cleanup(func() { UploadURL, APIBase = prevUpload, prevAPI })

	auth := Auth{Cookie: cookie, AuthUser: "0"}
	if name, err := AccountName(context.Background(), auth); err != nil || name != "Will" {
		t.Fatalf("account = %q, %v", name, err)
	}

	path := filepath.Join(t.TempDir(), "01 Neon Rain.mp3")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	var last float64
	if err := Upload(context.Background(), auth, path, func(f float64) { last = f }); err != nil {
		t.Fatal(err)
	}
	if startBody != "filename=01 Neon Rain.mp3" || started.Get("X-Goog-Upload-Header-Content-Length") != "4096" ||
		started.Get("X-Goog-Upload-Command") != "start" {
		t.Errorf("start: %q %v", startBody, started)
	}
	if got != content || last != 1 {
		t.Errorf("sent %d bytes, progress %v", len(got), last)
	}

	wav := filepath.Join(t.TempDir(), "take.wav")
	_ = os.WriteFile(wav, []byte("RIFF"), 0o600)
	if err := Upload(context.Background(), auth, wav, nil); err == nil {
		t.Error("a WAV was sent, which YouTube Music does not take")
	}

	signedOut := Auth{Cookie: "SAPISID=other", AuthUser: "0"}
	if err := Upload(context.Background(), signedOut, path, nil); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Errorf("a refused session: %v", err)
	}
}
