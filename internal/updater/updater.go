// Package updater finds a newer release of the app and fetches its installer
// (#132).
//
// Releases are GitHub releases. Every one the release workflow publishes
// carries a manifest.json naming the file for each platform and its SHA-256,
// the model RapidRabbit's updater follows with a manifest beside each
// release. The manifest rather than guessing from asset names, because a
// name is a convention and a checksum is a promise: nothing this package
// downloads is handed over until it matches.
//
// The repository is private, so requests carry a GitHub credential when one
// is given. Asset downloads go through the API, which redirects to a signed
// storage URL; Go drops the Authorization header on that cross-host redirect,
// which is what the storage host requires.
package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ManifestName is the release asset that indexes the others.
const ManifestName = "manifest.json"

// Manifest is what the release workflow writes beside a release's files.
type Manifest struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	// Assets is keyed by PlatformKey: "windows-amd64", "darwin-universal",
	// "linux-amd64".
	Assets map[string]ManifestAsset `json:"assets"`
}

// ManifestAsset is one platform's file.
type ManifestAsset struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

// Release is the newest release, with the file for this platform.
type Release struct {
	Version     string
	Notes       string
	URL         string
	PublishedAt string
	// Asset is empty when the release has nothing for this platform.
	Asset Asset
}

// Asset is a downloadable file and what it must hash to.
type Asset struct {
	Name   string
	Size   int64
	SHA256 string
	// apiURL is the API address that serves the bytes.
	apiURL string
}

// Client reaches one repository's releases.
type Client struct {
	// Repo is "owner/name".
	Repo string
	// Token is a GitHub credential, or "" for a public repository.
	Token string
	// UserAgent identifies the app; GitHub refuses requests without one.
	UserAgent string
	// APIBase is the API root. Empty means api.github.com; tests point it at
	// a local server.
	APIBase string
	// HTTP is the client to use. Nil means one with a timeout on the check;
	// downloads are bounded by their context instead, since an installer on a
	// slow line legitimately takes minutes.
	HTTP *http.Client
}

// ErrNoRelease is the answer when the repository has no published release.
var ErrNoRelease = errors.New("there is no published release yet")

func (c Client) base() string {
	if c.APIBase != "" {
		return strings.TrimRight(c.APIBase, "/")
	}
	return "https://api.github.com"
}

func (c Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{}
}

func (c Client) request(ctx context.Context, url, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach GitHub: %w", err)
	}
	if resp.StatusCode == http.StatusOK {
		return resp, nil
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNotFound:
		// A private repository answers 404, not 401, to a request that may
		// not see it, so the two cannot be told apart from here.
		if c.Token == "" {
			return nil, fmt.Errorf("GitHub has no releases this app can see — sign in with `gh auth login` to check a private repository")
		}
		return nil, ErrNoRelease
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("GitHub refused the request (%s)", resp.Status)
	}
	return nil, fmt.Errorf("GitHub answered %s", resp.Status)
}

type ghRelease struct {
	TagName     string    `json:"tag_name"`
	Body        string    `json:"body"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt string    `json:"published_at"`
	Assets      []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	URL  string `json:"url"`
}

// Latest reads the newest release and picks this platform's file from its
// manifest.
func (c Client) Latest(ctx context.Context) (Release, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := c.request(ctx, c.base()+"/repos/"+c.Repo+"/releases/latest", "application/vnd.github+json")
	if err != nil {
		return Release{}, err
	}
	var found ghRelease
	err = json.NewDecoder(resp.Body).Decode(&found)
	resp.Body.Close()
	if err != nil {
		return Release{}, fmt.Errorf("could not read the release: %w", err)
	}

	release := Release{
		Version:     strings.TrimPrefix(found.TagName, "v"),
		Notes:       found.Body,
		URL:         found.HTMLURL,
		PublishedAt: found.PublishedAt,
	}
	byName := map[string]ghAsset{}
	for _, asset := range found.Assets {
		byName[asset.Name] = asset
	}
	index, ok := byName[ManifestName]
	if !ok {
		// Releases from before the manifest existed. Nothing is offered from
		// them: without a checksum there is nothing to check a download by.
		return release, nil
	}
	manifest, err := c.manifest(ctx, index.URL)
	if err != nil {
		return Release{}, err
	}
	want, ok := manifest.Assets[PlatformKey(runtime.GOOS, runtime.GOARCH)]
	if !ok {
		return release, nil
	}
	file, ok := byName[want.Name]
	if !ok {
		return Release{}, fmt.Errorf("the release's manifest names %s, which is not attached to it", want.Name)
	}
	release.Asset = Asset{Name: file.Name, Size: file.Size, SHA256: strings.ToLower(want.SHA256), apiURL: file.URL}
	return release, nil
}

func (c Client) manifest(ctx context.Context, url string) (Manifest, error) {
	resp, err := c.request(ctx, url, "application/octet-stream")
	if err != nil {
		return Manifest{}, err
	}
	defer resp.Body.Close()
	var manifest Manifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("could not read the release manifest: %w", err)
	}
	return manifest, nil
}

// Download fetches the asset into dir and checks it against its SHA-256.
//
// The bytes go to a .part file that is renamed only once the hash matches, so
// a file at the returned path is always a whole, verified one, and a download
// cut off half-way never looks like a finished installer. progress, when not
// nil, is called with bytes so far and the total.
func (c Client) Download(ctx context.Context, asset Asset, dir string, progress func(done, total int64)) (string, error) {
	if asset.apiURL == "" || asset.SHA256 == "" {
		return "", errors.New("this release has no verifiable file for this platform")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	final := filepath.Join(dir, filepath.Base(asset.Name))
	partial := final + ".part"

	resp, err := c.request(ctx, asset.apiURL, "application/octet-stream")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	out, err := os.Create(partial)
	if err != nil {
		return "", err
	}
	total := asset.Size
	if total <= 0 {
		total = resp.ContentLength
	}
	hash := sha256.New()
	reader := &countingReader{r: resp.Body, total: total, progress: progress}
	_, copyErr := io.Copy(io.MultiWriter(out, hash), reader)
	closeErr := out.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		os.Remove(partial)
		return "", fmt.Errorf("the download stopped: %w", copyErr)
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != asset.SHA256 {
		os.Remove(partial)
		return "", fmt.Errorf("the download does not match the release's checksum, so it was discarded")
	}
	if err := os.Rename(partial, final); err != nil {
		os.Remove(partial)
		return "", err
	}
	return final, nil
}

type countingReader struct {
	r        io.Reader
	done     int64
	total    int64
	progress func(done, total int64)
	last     time.Time
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.done += int64(n)
	// Throttled: a call per 32 KB read would be hundreds of window events a
	// second for a bar that moves a pixel at a time.
	if c.progress != nil && (err == io.EOF || time.Since(c.last) > 100*time.Millisecond) {
		c.last = time.Now()
		c.progress(c.done, c.total)
	}
	return n, err
}

// PlatformKey is the manifest key for a GOOS/GOARCH. macOS ships one
// universal app for both architectures.
func PlatformKey(goos, goarch string) string {
	if goos == "darwin" {
		return "darwin-universal"
	}
	return goos + "-" + goarch
}

// Newer reports whether latest is a later version than current. A current
// version that is not a release number — "dev", a developer's own build — is
// never behind: it was not installed from a release, so a release is not its
// update.
func Newer(latest, current string) bool {
	l, okL := parse(latest)
	c, okC := parse(current)
	if !okL || !okC {
		return false
	}
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

// IsVersion says whether version is a release number. A release build has
// one linked in; a developer's own build says "dev".
func IsVersion(version string) bool {
	_, ok := parse(version)
	return ok
}

func parse(version string) ([3]int, bool) {
	var out [3]int
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if i := strings.IndexAny(version, "-+"); i >= 0 {
		version = version[:i]
	}
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
