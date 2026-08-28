package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Album artwork
//
// A cover is generated from the album's name, the artist, the combined technical
// style and the Album artwork skill — or dropped in from a file the producer
// already has.
//
// Both ChatGPT connection modes work, and the difference is what gets billed:
//
//	API key   the Images API directly (gpt-image-1), which is the path that
//	          behaves the same on every machine.
//	account   Codex headless in a scratch workspace, whose image_generation tool
//	          writes cover.png there — billed to the ChatGPT subscription, no API
//	          key involved.
//
// Covers live as PNGs under ~/.sunomaker/artwork and are served to the window by
// the extension bridge (see extensionbridge.go); the album row stores only the
// file name. A webview cannot read a file:// path out of the app's data
// directory, and a base64 column would be a payload every album listing carried
// around to draw a thumbnail.
//
// Only the current cover is kept. What was *asked for* survives instead — the
// prompt is stored on the album — so the decision is reproducible even though a
// particular image is not. An album with forty abandoned attempts beside it is a
// folder nobody can read.
// ---------------------------------------------------------------------------

const (
	openAIImagesURL  = "https://api.openai.com/v1/images/generations"
	openAIImageEdit  = "https://api.openai.com/v1/images/edits"
	openAIImageModel = "gpt-image-1"
	// Square, because that is what a record cover is. The Images API's only
	// square size at this model.
	coverSize = "1024x1024"
)

// artworkHTTP is its own client with its own patience: generating an image takes
// far longer than answering a question, and the chat client's three minutes is
// close enough to the edge that a slow generation would fail after paying for
// itself.
var artworkHTTP = &http.Client{Timeout: 6 * time.Minute}

// maxUploadedArtwork bounds an imported cover. A cover is a square image; ten
// megabytes is a photograph nobody has resized, and accepting a hundred would be
// storing a RAW file as album art.
const maxUploadedArtwork = 10 << 20

// artworkExtensions are the image formats a cover may be imported from.
var artworkExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".webp": true,
}

// artworkURL is where the window loads a cover from: the bridge, with the
// bridge's own token as a query parameter.
//
// A query parameter rather than a header because this is an <img src>, and an
// image element cannot send one. The token is the same one the extension holds,
// the server is bound to loopback alone, and the thing being protected is an
// album cover — so this is the weaker half of a defence that is already stronger
// than what it guards.
func (a *App) artworkURL(file string) string {
	if file == "" {
		return ""
	}
	token, err := a.extensionToken()
	if err != nil {
		return ""
	}
	return fmt.Sprintf("http://127.0.0.1:%d/artwork/%s?t=%s",
		extensionBridgePort, url.PathEscape(file), url.QueryEscape(token))
}

// GenerateAlbumArtwork makes a cover for an album and stores it.
//
// feedback is what to change about the current one — "make it bluer" — and is
// empty for a first generation. When there is a current cover and feedback, the
// existing image is sent along so the model revises it rather than starting
// again, which is what "make it bluer" actually means.
func (a *App) GenerateAlbumArtwork(albumID, feedback string) (Album, error) {
	if a.store == nil {
		return Album{}, errNoStore
	}
	album, err := a.store.album(albumID)
	if err != nil {
		return Album{}, err
	}
	artist, err := a.store.artist(album.ArtistID)
	if err != nil {
		return Album{}, err
	}

	prompt := a.artworkPrompt(artist, album, feedback)

	var current []byte
	if album.ArtworkFile != "" && strings.TrimSpace(feedback) != "" {
		if dir, err := artworkDir(); err == nil {
			// Best effort: a cover that cannot be read is one this revises from
			// nothing, which is worse than revising from it and better than
			// failing.
			current, _ = os.ReadFile(filepath.Join(dir, album.ArtworkFile))
		}
	}

	png, err := a.renderCover(a.interviewContext(), prompt, current)
	if err != nil {
		return Album{}, err
	}

	file, err := writeArtwork(album.ID, png)
	if err != nil {
		return Album{}, err
	}
	// The brief is stored rather than the feedback, so the next revision revises
	// the whole description and not the last complaint about it.
	if err := a.store.setAlbumArtwork(album.ID, file, prompt); err != nil {
		return Album{}, err
	}
	return a.GetAlbum(album.ID)
}

// ImportAlbumArtwork takes a cover the producer already has.
func (a *App) ImportAlbumArtwork(albumID, sourcePath string) (Album, error) {
	if a.store == nil {
		return Album{}, errNoStore
	}
	album, err := a.store.album(albumID)
	if err != nil {
		return Album{}, err
	}
	sourcePath = strings.TrimSpace(sourcePath)
	info, err := os.Stat(sourcePath)
	if err != nil {
		return Album{}, fmt.Errorf("could not read %s: %w", sourcePath, err)
	}
	if info.Size() > maxUploadedArtwork {
		return Album{}, fmt.Errorf("%s is larger than this app will store as a cover (%d MB)",
			filepath.Base(sourcePath), maxUploadedArtwork>>20)
	}
	ext := strings.ToLower(filepath.Ext(sourcePath))
	if !artworkExtensions[ext] {
		return Album{}, fmt.Errorf("%s is not an image this app knows", filepath.Base(sourcePath))
	}
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return Album{}, err
	}
	dir, err := artworkDir()
	if err != nil {
		return Album{}, err
	}
	// The album id and a stamp: the id ties the file to the record it belongs to
	// when somebody is looking at the folder, and the stamp is what makes the
	// name change on every replacement — a webview caches an <img src> by URL,
	// and a stable name would leave the old cover on screen after a new one was
	// made.
	file := fmt.Sprintf("%s-%d%s", album.ID, time.Now().UnixNano(), ext)
	if err := os.WriteFile(filepath.Join(dir, file), data, 0o600); err != nil {
		return Album{}, err
	}
	// The prompt is cleared: this cover was not generated from one, and leaving
	// the old brief would have the next revision revise an image that is gone.
	if err := a.store.setAlbumArtwork(album.ID, file, ""); err != nil {
		return Album{}, err
	}
	return a.GetAlbum(album.ID)
}

// RemoveAlbumArtwork clears a cover.
func (a *App) RemoveAlbumArtwork(albumID string) (Album, error) {
	if a.store == nil {
		return Album{}, errNoStore
	}
	if err := a.store.setAlbumArtwork(albumID, "", ""); err != nil {
		return Album{}, err
	}
	return a.GetAlbum(albumID)
}

// artworkPrompt assembles the brief: the skill, what the record is, and what to
// change.
func (a *App) artworkPrompt(artist Artist, album Album, feedback string) string {
	var b strings.Builder
	b.WriteString(a.skillBody(skillAlbumArtwork))
	b.WriteString("\n\n## This record\n\n")
	fmt.Fprintf(&b, "Artist: %s\n", artist.Name)
	fmt.Fprintf(&b, "Album: %s\n", album.Name)
	if style := combinedStyle(artist, album); style != "" {
		b.WriteString("\nTechnical style:\n\n")
		b.WriteString(style)
		b.WriteString("\n")
	}
	if described := strings.TrimSpace(album.ArtworkPrompt); described != "" {
		b.WriteString("\n## What the producer asked for\n\n")
		b.WriteString(described)
		b.WriteString("\n")
	}
	if change := strings.TrimSpace(feedback); change != "" {
		b.WriteString("\n## Change to make\n\n")
		b.WriteString(change)
		b.WriteString("\n\nKeep everything that was not criticised.\n")
	}
	return b.String()
}

// renderCover produces the PNG, by whichever path the ChatGPT connection allows.
func (a *App) renderCover(ctx context.Context, prompt string, current []byte) ([]byte, error) {
	conn := a.aiConnections()[serviceOpenAI]
	switch {
	case conn.Mode == modeAPIKey && conn.Key != "":
		return coverViaImagesAPI(ctx, conn.Key, prompt, current)
	case conn.Mode == modeAccount:
		return coverViaCodex(ctx, prompt, current)
	}
	return nil, fmt.Errorf(
		"artwork needs ChatGPT connected — connect it in Settings → AI, " +
			"by API key or by the signed-in Codex CLI")
}

// coverViaImagesAPI calls the Images API directly.
//
// Two endpoints, not one: a fresh cover is a generation and a revision is an
// edit, and sending a revision to the generations endpoint would produce a new
// image that merely resembles the brief rather than the same image changed.
func coverViaImagesAPI(ctx context.Context, key, prompt string, current []byte) ([]byte, error) {
	if len(current) > 0 {
		return editImage(ctx, key, prompt, current)
	}
	body, err := json.Marshal(map[string]any{
		"model": openAIImageModel, "prompt": prompt, "size": coverSize, "n": 1,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openAIImagesURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	return readImageResponse(req)
}

// editImage sends the current cover with the revised brief.
//
// multipart rather than JSON, because the edits endpoint takes the image as a
// file part. image[] rather than image: gpt-image-1 accepts several references
// and expects the array form even for one.
func editImage(ctx context.Context, key, prompt string, current []byte) ([]byte, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, field := range []struct{ name, value string }{
		{"model", openAIImageModel},
		{"prompt", prompt},
		{"size", coverSize},
	} {
		if err := writer.WriteField(field.name, field.value); err != nil {
			return nil, err
		}
	}
	part, err := writer.CreateFormFile("image[]", "cover.png")
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(current); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openAIImageEdit, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+key)
	return readImageResponse(req)
}

func readImageResponse(req *http.Request) ([]byte, error) {
	resp, err := artworkHTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach ChatGPT: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var reason bytes.Buffer
		_, _ = reason.ReadFrom(resp.Body)
		return nil, fmt.Errorf("ChatGPT answered %s: %s",
			resp.Status, strings.TrimSpace(reason.String()))
	}
	var answer struct {
		Data []struct {
			B64 string `json:"b64_json"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		return nil, err
	}
	if len(answer.Data) == 0 || answer.Data[0].B64 == "" {
		return nil, fmt.Errorf("ChatGPT returned no image")
	}
	return base64.StdEncoding.DecodeString(answer.Data[0].B64)
}

// coverViaCodex produces the cover through Codex headless, so the image bills
// the ChatGPT subscription.
//
// The run is pointed at a scratch workspace, Codex's image_generation tool
// writes cover.png there, and the file is collected afterwards. The sandbox is
// opened for that write: the prompt is the app's own and the directory is
// thrown away at the end of this function.
func coverViaCodex(ctx context.Context, prompt string, current []byte) ([]byte, error) {
	path, found := lookUpCommand("codex")
	if !found {
		return nil, fmt.Errorf(
			"codex was not found on this machine — reconnect ChatGPT in Settings → AI")
	}
	work, err := os.MkdirTemp("", "sunomaker-cover-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)

	if len(current) > 0 {
		if err := os.WriteFile(filepath.Join(work, "current.png"), current, 0o600); err != nil {
			return nil, err
		}
	}
	lastMessage := filepath.Join(work, "last-message.txt")

	var instruction strings.Builder
	instruction.WriteString(
		"Create one square 1024x1024 album cover with your image generation tool " +
			"and save it as cover.png in the current working directory. Do not write " +
			"any other files or run unrelated commands. Reply with only the file name " +
			"once it is saved.\n\n")
	if len(current) > 0 {
		instruction.WriteString(
			"current.png in the working directory is the existing cover — view it " +
				"first, then produce a revised version that applies the requested " +
				"change and keeps everything that was not criticised.\n\n")
	}
	instruction.WriteString(prompt)

	// Flags before the prompt positional, so "-" (read the prompt from stdin)
	// stays last.
	cmd := exec.CommandContext(ctx, path,
		"exec", "--skip-git-repo-check",
		"--sandbox", "danger-full-access",
		"--output-last-message", lastMessage,
		"-")
	cmd.Dir = work
	cmd.Stdin = strings.NewReader(instruction.String())
	cmd.Env = envWithout("OPENAI_API_KEY")
	hideWindow(cmd)

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("Codex could not generate the cover: %s",
			lastLines(output.String(), 5))
	}

	png, err := os.ReadFile(filepath.Join(work, "cover.png"))
	if err != nil {
		// Codex's own last word is far more use than "no file was written": it
		// says whether the image was refused, whether the tool is unavailable, or
		// whether it simply saved the file somewhere else.
		detail := "no image was produced — try again"
		if raw, rerr := os.ReadFile(lastMessage); rerr == nil && strings.TrimSpace(string(raw)) != "" {
			detail = strings.TrimSpace(string(raw))
			if len(detail) > 300 {
				detail = detail[:300]
			}
		}
		return nil, fmt.Errorf("Codex did not save a cover: %s", detail)
	}
	return png, nil
}

// writeArtwork stores a generated cover and returns its file name. The stamp in
// the name is what defeats the webview's image cache — see ImportAlbumArtwork.
func writeArtwork(albumID string, png []byte) (string, error) {
	dir, err := artworkDir()
	if err != nil {
		return "", err
	}
	file := albumID + "-" + strconv.FormatInt(time.Now().UnixNano(), 10) + ".png"
	if err := os.WriteFile(filepath.Join(dir, file), png, 0o600); err != nil {
		return "", err
	}
	return file, nil
}

// lastLines is the tail of a command's output, for an error message. The tail
// rather than the head: a CLI writes its progress first and its complaint last.
func lastLines(output string, n int) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	detail := strings.TrimSpace(strings.Join(lines, "\n"))
	if detail == "" {
		return "no output"
	}
	return detail
}
