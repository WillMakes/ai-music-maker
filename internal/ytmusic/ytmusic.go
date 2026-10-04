// Package ytmusic uploads songs to the producer's YouTube Music library
// (#124), the way the Python ytmusicapi's upload_song does — ported here so
// the app needs no Python beside it.
//
// YouTube Music has no public upload API. What the web app itself does is:
// signed in by the browser's cookies, with an Authorization header derived
// from one of them (SAPISIDHASH, below), it starts an upload with one POST to
// upload.youtube.com and sends the file with a second. The song lands in the
// account's own library — private to it, not a release — and YouTube Music
// builds the entry from the file's tags, which is why the caller tags each
// take before it comes here.
//
// Unofficial, so it can break whenever Google changes the web app. Every
// refusal is answered in words that say so, rather than as a bare status.
package ytmusic

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Endpoints, variables so a test can stand in for Google.
var (
	Origin    = "https://music.youtube.com"
	UploadURL = "https://upload.youtube.com/upload/usermusic/http"
	APIBase   = "https://music.youtube.com/youtubei/v1"
)

// MaxBytes is YouTube Music's limit on one uploaded file.
const MaxBytes = 300 << 20

// Extensions is what YouTube Music accepts.
var Extensions = map[string]bool{".mp3": true, ".m4a": true, ".wma": true, ".flac": true, ".ogg": true}

// defaultUserAgent stands in when the pasted headers carry none: a request
// without one is answered as a bot.
const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36"

// Auth is the signed-in browser session the requests ride on.
type Auth struct {
	Cookie    string `json:"cookie"`
	AuthUser  string `json:"authUser"`
	UserAgent string `json:"userAgent"`
}

// ParseHeaders reads the request headers the producer copied from a request
// music.youtube.com made in their signed-in browser — ytmusicapi's
// "browser" setup. Both shapes the browsers copy are read: "Name: value" per
// line (Firefox, and Chrome's raw view), and Chrome's name and value on
// alternate lines.
func ParseHeaders(raw string) (Auth, error) {
	headers := map[string]string{}
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if name, value, ok := strings.Cut(line, ": "); ok && !strings.HasPrefix(line, ":") {
			headers[strings.ToLower(strings.TrimSpace(name))] = strings.TrimSpace(value)
			continue
		}
		// Chrome's copy: "cookie:" on one line and its value on the next.
		if strings.HasSuffix(line, ":") && i+1 < len(lines) {
			headers[strings.ToLower(strings.TrimSuffix(line, ":"))] = strings.TrimSpace(lines[i+1])
			i++
		}
	}
	auth := Auth{
		Cookie:    headers["cookie"],
		AuthUser:  headers["x-goog-authuser"],
		UserAgent: headers["user-agent"],
	}
	if auth.Cookie == "" {
		return Auth{}, errors.New("there is no cookie header in what was pasted — copy the request headers of a request to music.youtube.com while signed in")
	}
	if sapisid(auth.Cookie) == "" {
		return Auth{}, errors.New("the cookie has no SAPISID — it is not from a signed-in YouTube Music tab")
	}
	if auth.AuthUser == "" {
		auth.AuthUser = "0"
	}
	return auth, nil
}

// sapisid is the cookie the Authorization header is derived from: the
// __Secure-3PAPISID value where there is one, as ytmusicapi reads it, and
// SAPISID otherwise — they carry the same value.
func sapisid(cookie string) string {
	values := map[string]string{}
	for _, part := range strings.Split(cookie, ";") {
		if name, value, ok := strings.Cut(strings.TrimSpace(part), "="); ok {
			values[name] = value
		}
	}
	if v := values["__Secure-3PAPISID"]; v != "" {
		return v
	}
	return values["SAPISID"]
}

// authorization is the SAPISIDHASH header the web app sends: a timestamp and
// the SHA-1 of the timestamp, the cookie and the origin. now is a parameter
// so a test can check a known value.
func authorization(cookie string, now time.Time) string {
	ts := strconv.FormatInt(now.Unix(), 10)
	sum := sha1.Sum([]byte(ts + " " + sapisid(cookie) + " " + Origin))
	return "SAPISIDHASH " + ts + "_" + hex.EncodeToString(sum[:])
}

// headers are the ones every request carries.
func (a Auth) headers(req *http.Request) {
	ua := a.UserAgent
	if ua == "" {
		ua = defaultUserAgent
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Cookie", a.Cookie)
	req.Header.Set("Authorization", authorization(a.Cookie, time.Now()))
	req.Header.Set("X-Goog-AuthUser", a.AuthUser)
	req.Header.Set("Origin", Origin)
	req.Header.Set("X-Origin", Origin)
}

var httpClient = &http.Client{}

// AccountName asks YouTube Music whose session the headers are, which is how
// a paste is checked before anything is uploaded on it. An answer the app
// cannot find a name in is still a signed-in session; only a refusal is an
// error.
func AccountName(ctx context.Context, auth Auth) (string, error) {
	body, _ := json.Marshal(map[string]any{"context": map[string]any{"client": map[string]any{
		"clientName":    "WEB_REMIX",
		"clientVersion": "1." + time.Now().UTC().Format("20060102") + ".01.00",
		"hl":            "en",
	}}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, APIBase+"/account/account_menu?alt=json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	auth.headers(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not reach YouTube Music: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", errors.New("YouTube Music did not accept these headers — copy them again from a signed-in tab")
	}
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("YouTube Music answered %s", resp.Status)
	}
	var menu any
	if err := json.NewDecoder(resp.Body).Decode(&menu); err != nil {
		return "", nil
	}
	return findAccountName(menu), nil
}

// findAccountName looks for accountName.runs[0].text anywhere in the answer:
// the menu's nesting has moved before, and the name is all that is wanted.
func findAccountName(v any) string {
	switch node := v.(type) {
	case map[string]any:
		if name, ok := node["accountName"].(map[string]any); ok {
			if runs, ok := name["runs"].([]any); ok && len(runs) > 0 {
				if run, ok := runs[0].(map[string]any); ok {
					if text, ok := run["text"].(string); ok {
						return text
					}
				}
			}
		}
		for _, child := range node {
			if found := findAccountName(child); found != "" {
				return found
			}
		}
	case []any:
		for _, child := range node {
			if found := findAccountName(child); found != "" {
				return found
			}
		}
	}
	return ""
}

// Upload sends one song to the library. progress is called with the
// fraction of the file sent.
func Upload(ctx context.Context, auth Auth, path string, progress func(float64)) error {
	if progress == nil {
		progress = func(float64) {}
	}
	ext := strings.ToLower(filepath.Ext(path))
	if !Extensions[ext] {
		return fmt.Errorf("YouTube Music does not take %s files", ext)
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() > MaxBytes {
		return fmt.Errorf("%s is %d MB, over YouTube Music's 300 MB", filepath.Base(path), info.Size()>>20)
	}

	start, err := http.NewRequestWithContext(ctx, http.MethodPost,
		UploadURL+"?authuser="+auth.AuthUser,
		strings.NewReader("filename="+filepath.Base(path)))
	if err != nil {
		return err
	}
	auth.headers(start)
	start.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	start.Header.Set("X-Goog-Upload-Command", "start")
	start.Header.Set("X-Goog-Upload-Header-Content-Length", strconv.FormatInt(info.Size(), 10))
	start.Header.Set("X-Goog-Upload-Protocol", "resumable")
	resp, err := httpClient.Do(start)
	if err != nil {
		return fmt.Errorf("could not reach YouTube Music: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return errors.New("YouTube Music refused the upload — the signed-in headers have expired; paste them again in Settings → YouTube")
	}
	session := resp.Header.Get("X-Goog-Upload-URL")
	if resp.StatusCode/100 != 2 || session == "" {
		return fmt.Errorf("YouTube Music did not start the upload (%s)", resp.Status)
	}

	send, err := http.NewRequestWithContext(ctx, http.MethodPost, session,
		&counting{r: file, total: info.Size(), progress: progress})
	if err != nil {
		return err
	}
	send.ContentLength = info.Size()
	auth.headers(send)
	send.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	send.Header.Set("X-Goog-Upload-Command", "upload, finalize")
	send.Header.Set("X-Goog-Upload-Offset", "0")
	resp, err = httpClient.Do(send)
	if err != nil {
		return fmt.Errorf("the upload was cut off: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("YouTube Music refused %s (%s)", filepath.Base(path), resp.Status)
	}
	progress(1)
	return nil
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
		c.progress(min(0.99, float64(c.sent)/float64(c.total)))
	}
	return n, err
}
