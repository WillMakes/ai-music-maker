package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"0.1.10", "0.1.9", true},
		{"v0.2.0", "0.1.99", true},
		{"1.0.0", "0.9.9", true},
		{"0.1.5", "0.1.5", false},
		{"0.1.4", "0.1.5", false},
		// A developer's own build is never offered a release.
		{"0.1.5", "dev", false},
		{"garbage", "0.1.0", false},
		{"0.1.6-rc1", "0.1.5", true},
	}
	for _, c := range cases {
		if got := Newer(c.latest, c.current); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}

func TestPlatformKey(t *testing.T) {
	if got := PlatformKey("darwin", "arm64"); got != "darwin-universal" {
		t.Errorf("darwin/arm64 = %q", got)
	}
	if got := PlatformKey("windows", "amd64"); got != "windows-amd64" {
		t.Errorf("windows/amd64 = %q", got)
	}
}

// fakeGitHub serves one release whose manifest names payload for this
// platform, hashing to sum.
func fakeGitHub(t *testing.T, payload []byte, sum string, wantToken string) *httptest.Server {
	t.Helper()
	name := "AIMusicMaker-v9.9.9-" + PlatformKey(runtime.GOOS, runtime.GOARCH) + ".bin"
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if wantToken != "" && r.Header.Get("Authorization") != "Bearer "+wantToken {
			http.NotFound(w, r)
			return
		}
		switch r.URL.Path {
		case "/repos/o/r/releases/latest":
			_ = json.NewEncoder(w).Encode(ghRelease{
				TagName: "v9.9.9",
				Body:    "notes",
				Assets: []ghAsset{
					{Name: ManifestName, URL: server.URL + "/assets/1"},
					{Name: name, Size: int64(len(payload)), URL: server.URL + "/assets/2"},
				},
			})
		case "/assets/1":
			_ = json.NewEncoder(w).Encode(Manifest{
				Version: "9.9.9",
				Assets: map[string]ManifestAsset{
					PlatformKey(runtime.GOOS, runtime.GOARCH): {Name: name, SHA256: sum},
				},
			})
		case "/assets/2":
			if r.Header.Get("Accept") != "application/octet-stream" {
				t.Errorf("asset requested as %q, not octet-stream", r.Header.Get("Accept"))
			}
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestLatestAndDownloadVerifyTheFile(t *testing.T) {
	payload := []byte(strings.Repeat("installer bytes ", 1000))
	hash := sha256.Sum256(payload)
	server := fakeGitHub(t, payload, hex.EncodeToString(hash[:]), "secret")
	client := Client{Repo: "o/r", Token: "secret", APIBase: server.URL}

	release, err := client.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if release.Version != "9.9.9" || release.Notes != "notes" || release.Asset.Name == "" {
		t.Fatalf("release read as %+v", release)
	}

	dir := t.TempDir()
	var last int64
	path, err := client.Download(context.Background(), release.Asset, dir, func(done, _ int64) { last = done })
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(payload) {
		t.Fatalf("downloaded %d bytes, err %v", len(got), err)
	}
	if last != int64(len(payload)) {
		t.Errorf("progress ended at %d of %d", last, len(payload))
	}
}

func TestDownloadDiscardsAFileThatDoesNotMatch(t *testing.T) {
	payload := []byte("tampered")
	server := fakeGitHub(t, payload, strings.Repeat("0", 64), "")
	client := Client{Repo: "o/r", APIBase: server.URL}

	release, err := client.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, err := client.Download(context.Background(), release.Asset, dir, nil); err == nil {
		t.Fatal("a file with the wrong checksum was accepted")
	}
	left, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(left) != 0 {
		t.Errorf("the rejected download left %v behind", left)
	}
}

func TestPrivateRepositoryWithoutCredentialSaysSo(t *testing.T) {
	server := fakeGitHub(t, nil, "", "secret")
	_, err := Client{Repo: "o/r", APIBase: server.URL}.Latest(context.Background())
	if err == nil || !strings.Contains(err.Error(), "gh auth login") {
		t.Fatalf("got %v, want a hint to sign in", err)
	}
}
