package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"aimusicmaker/internal/mcpclient"
)

// ---------------------------------------------------------------------------
// Making an album through Suno's MCP server
//
// The other way to make a record, beside the extension filling Suno's form in
// Chrome: the app sends each track's title, style prompt, lyrics and target
// length — exactly what the extension types — to Suno's audio-create, waits on
// audio-wait, and files the takes through importTrackFile with their clip id,
// the way a take recorded in the browser is filed. The rest of the app cannot
// tell the two apart, and a take later heard in the browser is recognised by
// its clip and not filed twice.
//
// Built against the tools in docs/suno-mcp.md, not guessed at. Two things in
// there shape this file:
//
//   - audio-retrieve answers no audio address, and Suno's CDN refuses private
//     songs, so the only way to the file is audio-download — which on a
//     suno.com sign-in answers requires_credit and spends one of the account's
//     *download* credits per take once confirmed. The tool says to ask the
//     user first, so a run downloads only when the producer approved that cost
//     when starting it. Without approval the songs stay in their Suno library,
//     titled as the tracks, where the extension's recording can still file
//     them for nothing.
//   - audio-create makes two songs from one call unless told otherwise
//     (clip_count defaults to 2 through MCP), as suno.com's Create does. The
//     run asks for what the producer chose, and says what it costs.
//
// The run lives in this process, not the database: it is minutes of polling,
// and a run the window was closed on cannot be resumed from a row — its songs
// carry on in Suno either way, and the producer can file them from there.
// ---------------------------------------------------------------------------

// sunoRunEvent carries a run's whole state to the window on every change.
const sunoRunEvent = "sunomcp:run"

// Suno's own limits on audio-create, from its input schema.
const (
	sunoTitleLimit  = 256
	sunoStyleLimit  = 1000
	sunoLyricsLimit = 5000
	sunoMinSeconds  = 15
	sunoMaxSeconds  = 360
)

// sunoWaitLimit is how long one track's songs are waited on before the run
// gives up on them. Generous: a busy Suno takes minutes, and the songs are not
// lost by giving up — they finish in the library.
var sunoWaitLimit = 20 * time.Minute

// sunoParallel is how many tracks are waited on and downloaded at once.
// Submitting stays one at a time, so running out of credits stops at the track
// that ran out rather than at whichever of several racing calls lost.
const sunoParallel = 3

// toolCaller is what the run needs of a server.
type toolCaller interface {
	CallTool(ctx context.Context, name string, args any) (mcpclient.ToolResult, error)
}

var errSunoNotConnected = errors.New("Suno is not connected — authorize it in Settings → MCP servers")

// sunoClient is the Suno server's client, a variable so a test can stand in
// for Suno.
var sunoClient = func(a *App) (toolCaller, error) {
	record, err := a.mcpServerRecord(sunoServerID)
	if err != nil {
		return nil, err
	}
	if record.state() != mcpConnected {
		return nil, errSunoNotConnected
	}
	return a.mcpClient(record), nil
}

// sunoConnected says whether Suno is signed in, from the stored record alone.
// It reaches nothing: an interview turn asks it every time, and a turn must not
// wait on Suno to learn what Settings already shows. A sign-in Suno has since
// refused is learned the first time something calls it, which marks the record.
func (a *App) sunoConnected() bool {
	record, err := a.mcpServerRecord(sunoServerID)
	return err == nil && record.state() == mcpConnected
}

// sunoDownloadHTTP fetches the files audio-download points at.
var sunoDownloadHTTP = &http.Client{Timeout: 10 * time.Minute}

// SunoAccount is account-usage, for the dialog to show before anything is spent.
type SunoAccount struct {
	Plan             string `json:"plan"`
	CreditsRemaining int    `json:"creditsRemaining"`
	MonthlyLimit     int    `json:"monthlyLimit"`
	MonthlyUsage     int    `json:"monthlyUsage"`
	// DownloadCreditsRemaining is the separate allowance audio-download spends.
	DownloadCreditsRemaining int      `json:"downloadCreditsRemaining"`
	RenewsOn                 string   `json:"renewsOn"`
	AllowedModels            []string `json:"allowedModels"`
	// Reported is false when the server answered without a suno_account, so
	// the dialog does not show zeros as if they were balances.
	Reported bool `json:"reported"`
}

// GetSunoAccount reads the plan and the credits. It spends nothing.
func (a *App) GetSunoAccount() (SunoAccount, error) {
	client, err := sunoClient(a)
	if err != nil {
		return SunoAccount{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	return a.sunoAccount(ctx, client)
}

func (a *App) sunoAccount(ctx context.Context, client toolCaller) (SunoAccount, error) {
	res, err := client.CallTool(ctx, "account-usage", map[string]any{})
	if err != nil {
		return SunoAccount{}, a.sunoError(err)
	}
	var usage struct {
		AllowedModels []string `json:"allowed_models"`
		Account       *struct {
			CreditsRemaining         int    `json:"credits_remaining"`
			DownloadCreditsRemaining int    `json:"download_credits_remaining"`
			MonthlyLimit             int    `json:"monthly_limit"`
			MonthlyUsage             int    `json:"monthly_usage"`
			RenewsOn                 string `json:"renews_on"`
			Plan                     struct {
				Name string `json:"name"`
			} `json:"plan"`
		} `json:"suno_account"`
	}
	if err := res.Decode(&usage); err != nil {
		return SunoAccount{}, fmt.Errorf("Suno's account answer could not be read: %w", err)
	}
	out := SunoAccount{AllowedModels: []string{}}
	out.AllowedModels = append(out.AllowedModels, usage.AllowedModels...)
	if acct := usage.Account; acct != nil {
		out.Reported = true
		out.Plan = acct.Plan.Name
		out.CreditsRemaining, out.DownloadCreditsRemaining = acct.CreditsRemaining, acct.DownloadCreditsRemaining
		out.MonthlyLimit, out.MonthlyUsage, out.RenewsOn = acct.MonthlyLimit, acct.MonthlyUsage, acct.RenewsOn
	}
	return out, nil
}

// SunoRunOptions is what the producer chose in the dialog.
type SunoRunOptions struct {
	// TrackIDs is which tracks to make; empty is every track with no take yet.
	TrackIDs []string `json:"trackIds"`
	// ClipCount is songs per track, 1 or 2.
	ClipCount int `json:"clipCount"`
	// Model is a label from allowed_models, "" for the plan's default.
	Model string `json:"model"`
	// Download is the producer's approval to spend a download credit per take.
	Download bool `json:"download"`
}

// Per-track states.
const (
	sunoTrackQueued      = "queued"
	sunoTrackSubmitting  = "submitting"
	sunoTrackGenerating  = "generating"
	sunoTrackDownloading = "downloading"
	sunoTrackFiled       = "filed"
	sunoTrackInLibrary   = "in_library"
	sunoTrackFailed      = "failed"
	sunoTrackSkipped     = "skipped"
)

// SunoClip is one song Suno made for a track.
type SunoClip struct {
	ID      string  `json:"id"`
	Status  string  `json:"status"`
	Seconds float64 `json:"seconds"`
	SongURL string  `json:"songUrl"`
	Error   string  `json:"error"`
	// FileID is the take it was filed as, "" until it is.
	FileID string `json:"fileId"`
}

// SunoTrackRun is one track's progress.
type SunoTrackRun struct {
	TrackID  string     `json:"trackId"`
	Name     string     `json:"name"`
	Position int        `json:"position"`
	State    string     `json:"state"`
	Error    string     `json:"error"`
	Clips    []SunoClip `json:"clips"`
}

// SunoAlbumRun is one press of the button.
type SunoAlbumRun struct {
	ID      string `json:"id"`
	AlbumID string `json:"albumId"`
	// State is running, done, failed or cancelled; "" means no run.
	State      string         `json:"state"`
	Error      string         `json:"error"`
	StartedAt  string         `json:"startedAt"`
	FinishedAt string         `json:"finishedAt"`
	Options    SunoRunOptions `json:"options"`
	Tracks     []SunoTrackRun `json:"tracks"`
	// Before and After are the account at either end, so the run says what
	// it spent; After is unreported until the run ends.
	Before SunoAccount `json:"before"`
	After  SunoAccount `json:"after"`
}

type sunoRun struct {
	mu     sync.Mutex
	state  SunoAlbumRun
	cancel context.CancelFunc
}

var (
	sunoRunsMu sync.Mutex
	sunoRuns   = map[string]*sunoRun{}
)

func (r *sunoRun) snapshot() SunoAlbumRun {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.state
	out.Tracks = make([]SunoTrackRun, len(r.state.Tracks))
	for i, t := range r.state.Tracks {
		t.Clips = append([]SunoClip{}, t.Clips...)
		out.Tracks[i] = t
	}
	out.Options.TrackIDs = append([]string{}, r.state.Options.TrackIDs...)
	return out
}

// update changes the run and tells the window.
func (a *App) updateSunoRun(r *sunoRun, change func(*SunoAlbumRun)) {
	r.mu.Lock()
	change(&r.state)
	r.mu.Unlock()
	a.emit(sunoRunEvent, r.snapshot())
}

func (a *App) updateSunoTrack(r *sunoRun, index int, change func(*SunoTrackRun)) {
	a.updateSunoRun(r, func(s *SunoAlbumRun) { change(&s.Tracks[index]) })
}

// GetSunoAlbumRun is the album's latest run in this process, or one with no
// state when there has been none.
func (a *App) GetSunoAlbumRun(albumID string) SunoAlbumRun {
	sunoRunsMu.Lock()
	r := sunoRuns[albumID]
	sunoRunsMu.Unlock()
	if r == nil {
		return SunoAlbumRun{AlbumID: albumID, Tracks: []SunoTrackRun{}, Options: SunoRunOptions{TrackIDs: []string{}}}
	}
	return r.snapshot()
}

// CancelSunoAlbumRun stops waiting. What was already sent carries on in Suno
// and lands in the library; nothing more is sent or downloaded.
func (a *App) CancelSunoAlbumRun(albumID string) SunoAlbumRun {
	sunoRunsMu.Lock()
	r := sunoRuns[albumID]
	sunoRunsMu.Unlock()
	if r != nil && r.cancel != nil {
		r.cancel()
	}
	return a.GetSunoAlbumRun(albumID)
}

// StartSunoAlbumRun begins making the album's tracks and answers at once; the
// run reports itself on sunomcp:run and through GetSunoAlbumRun.
func (a *App) StartSunoAlbumRun(albumID string, opts SunoRunOptions) (SunoAlbumRun, error) {
	if a.store == nil {
		return SunoAlbumRun{}, errNoStore
	}
	client, err := sunoClient(a)
	if err != nil {
		return SunoAlbumRun{}, err
	}
	if opts.ClipCount != 1 && opts.ClipCount != 2 {
		return SunoAlbumRun{}, errors.New("choose one or two songs per track")
	}
	tracks, err := a.ListTracks(albumID)
	if err != nil {
		return SunoAlbumRun{}, err
	}
	chosen := []Track{}
	if len(opts.TrackIDs) == 0 {
		for _, t := range tracks {
			if len(t.Files) == 0 {
				chosen = append(chosen, t)
			}
		}
	} else {
		want := map[string]bool{}
		for _, id := range opts.TrackIDs {
			want[id] = true
		}
		for _, t := range tracks {
			if want[t.ID] {
				chosen = append(chosen, t)
			}
		}
	}
	if len(chosen) == 0 {
		return SunoAlbumRun{}, errors.New("no tracks to make — every track has a take, or none was chosen")
	}
	for _, t := range chosen {
		if strings.TrimSpace(t.Prompt) == "" && strings.TrimSpace(t.Lyrics) == "" {
			return SunoAlbumRun{}, fmt.Errorf("%q has neither a style prompt nor lyrics — write one first", t.Name)
		}
	}

	sunoRunsMu.Lock()
	if existing := sunoRuns[albumID]; existing != nil && existing.snapshot().State == "running" {
		sunoRunsMu.Unlock()
		return SunoAlbumRun{}, errors.New("this album is already being made through Suno")
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &sunoRun{cancel: cancel, state: SunoAlbumRun{
		ID: newID("sunorun"), AlbumID: albumID, State: "running", StartedAt: nowStamp(),
		Options: opts, Tracks: []SunoTrackRun{},
	}}
	run.state.Options.TrackIDs = []string{}
	for _, t := range chosen {
		run.state.Options.TrackIDs = append(run.state.Options.TrackIDs, t.ID)
		run.state.Tracks = append(run.state.Tracks, SunoTrackRun{
			TrackID: t.ID, Name: t.Name, Position: t.Position, State: sunoTrackQueued, Clips: []SunoClip{},
		})
	}
	sunoRuns[albumID] = run
	sunoRunsMu.Unlock()

	// The balance before anything is spent, so the end can say what was.
	// Not fatal: a server that stops reporting usage can still make songs.
	if before, err := a.sunoAccount(ctx, client); err == nil {
		run.state.Before = before
	}
	a.emit(sunoRunEvent, run.snapshot())
	go a.runSunoAlbum(ctx, run, client, chosen)
	return run.snapshot(), nil
}

func (a *App) runSunoAlbum(ctx context.Context, run *sunoRun, client toolCaller, tracks []Track) {
	defer run.cancel()
	opts := run.snapshot().Options

	// Submitted one at a time, so the producer sees them go in order and an
	// empty balance stops the run at the track it ran out on.
	submitted := make([][]string, len(tracks))
	var stop error
	for i, t := range tracks {
		if ctx.Err() != nil {
			stop = ctx.Err()
			break
		}
		a.updateSunoTrack(run, i, func(tr *SunoTrackRun) { tr.State = sunoTrackSubmitting })
		ids, err := a.submitSunoTrack(ctx, client, run.snapshot().ID, t, opts)
		if err != nil {
			a.updateSunoTrack(run, i, func(tr *SunoTrackRun) { tr.State, tr.Error = sunoTrackFailed, err.Error() })
			if errors.Is(err, errSunoOutOfCredits) || errors.Is(err, errSunoSignIn) {
				stop = err
				break
			}
			continue
		}
		submitted[i] = ids
		a.updateSunoTrack(run, i, func(tr *SunoTrackRun) {
			tr.State = sunoTrackGenerating
			for _, id := range ids {
				tr.Clips = append(tr.Clips, SunoClip{ID: id, Status: "submitted"})
			}
		})
	}
	if stop != nil {
		for i := range tracks {
			a.updateSunoTrack(run, i, func(tr *SunoTrackRun) {
				if tr.State == sunoTrackQueued {
					tr.State = sunoTrackSkipped
				}
			})
		}
	}

	var wg sync.WaitGroup
	slots := make(chan struct{}, sunoParallel)
	for i, t := range tracks {
		if len(submitted[i]) == 0 {
			continue
		}
		wg.Add(1)
		go func(i int, t Track, ids []string) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			a.finishSunoTrack(ctx, run, client, i, t, ids, opts)
		}(i, t, submitted[i])
	}
	wg.Wait()

	after, _ := a.sunoAccount(context.Background(), client)
	a.updateSunoRun(run, func(s *SunoAlbumRun) {
		s.After, s.FinishedAt = after, nowStamp()
		failed := 0
		for _, tr := range s.Tracks {
			if tr.State == sunoTrackFailed || tr.State == sunoTrackSkipped {
				failed++
			}
		}
		switch {
		// ctx is only cancelled by the producer here: the run's own cancel is
		// deferred until after this.
		case ctx.Err() != nil:
			s.State = "cancelled"
			s.Error = "Stopped. Songs already sent to Suno carry on there and land in your Suno library."
		case stop != nil:
			s.State, s.Error = "failed", stop.Error()
		case failed == len(s.Tracks):
			s.State, s.Error = "failed", "No track was made — see each track for why."
		default:
			s.State = "done"
			if failed > 0 {
				s.Error = fmt.Sprintf("%d of %d tracks did not make it — see each track for why.", failed, len(s.Tracks))
			}
		}
	})
}

var (
	errSunoOutOfCredits = errors.New("Suno is out of credits — top up on suno.com, then make the rest")
	errSunoSignIn       = errors.New("Suno's sign-in has expired — authorize it again in Settings → MCP servers")
)

// sunoError turns what the server said into what the producer should read,
// and marks the server for a new sign-in when that is the trouble.
func (a *App) sunoError(err error) error {
	if err == nil {
		return nil
	}
	if needsSignIn(err) {
		_, _ = a.updateMCPServer(sunoServerID, func(r *mcpServerRecord) {
			r.NeedsAuth, r.LastError = true, err.Error()
		})
		return errSunoSignIn
	}
	var toolErr *mcpclient.ToolError
	if errors.As(err, &toolErr) {
		text := strings.ToLower(toolErr.Text)
		if strings.Contains(text, "402") || strings.Contains(text, "insufficient_credits") ||
			strings.Contains(text, "quota_exhausted") || strings.Contains(text, "out of credits") {
			return errSunoOutOfCredits
		}
		return fmt.Errorf("Suno refused: %s", strings.TrimSpace(toolErr.Text))
	}
	return err
}

// sunoRateLimited is a refusal worth waiting out.
func sunoRateLimited(err error) bool {
	var toolErr *mcpclient.ToolError
	if !errors.As(err, &toolErr) {
		return false
	}
	text := strings.ToLower(toolErr.Text)
	return strings.Contains(text, "429") || strings.Contains(text, "rate_limited")
}

// sunoCreateArgs is audio-create's custom mode, filled the way the extension
// fills Suno's form: title, style, lyrics and duration.
func sunoCreateArgs(t Track, opts SunoRunOptions, key string) map[string]any {
	args := map[string]any{
		"title":           clip(strings.TrimSpace(t.Name), sunoTitleLimit),
		"clip_count":      opts.ClipCount,
		"idempotency_key": key,
	}
	if style := strings.TrimSpace(t.Prompt); style != "" {
		args["style"] = clip(style, sunoStyleLimit)
	}
	if lyrics := strings.TrimSpace(t.Lyrics); lyrics != "" {
		args["lyrics"] = clip(lyrics, sunoLyricsLimit)
	} else {
		// Custom mode with no lyrics is refused unless it says it means it.
		args["instrumental"] = true
	}
	if t.TargetSeconds > 0 {
		seconds := min(max(t.TargetSeconds, sunoMinSeconds), sunoMaxSeconds)
		args["duration"] = seconds
		// A soft target, the way the form treats it. Strict caps the song at
		// the length and cuts off one that overruns mid-phrase, which is worse
		// than a song that runs a little long.
		args["duration_strict"] = false
	}
	if opts.Model != "" {
		args["model"] = opts.Model
	}
	return args
}

// clip cuts s to n characters (runes, since lyrics are not ASCII).
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// submitSunoTrack sends one track and answers the clip ids Suno made.
//
// The idempotency key is the run and the track, so a retry after a dropped
// connection gets the songs the first call made rather than paying again.
func (a *App) submitSunoTrack(ctx context.Context, client toolCaller, runID string, t Track, opts SunoRunOptions) ([]string, error) {
	args := sunoCreateArgs(t, opts, runID+":"+t.ID)
	var res mcpclient.ToolResult
	var err error
	for attempt := 0; attempt < 4; attempt++ {
		res, err = client.CallTool(ctx, "audio-create", args)
		if err == nil || ctx.Err() != nil {
			break
		}
		var toolErr *mcpclient.ToolError
		retry := sunoRateLimited(err) || (!errors.As(err, &toolErr) && !needsSignIn(err))
		if !retry {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Duration(attempt+1) * 5 * time.Second):
		}
	}
	if err != nil {
		return nil, a.sunoError(err)
	}
	var created struct {
		ID      string   `json:"id"`
		ClipIDs []string `json:"clip_ids"`
	}
	if err := res.Decode(&created); err != nil {
		return nil, fmt.Errorf("Suno's answer could not be read: %w", err)
	}
	ids := created.ClipIDs
	if len(ids) == 0 && created.ID != "" {
		ids = []string{created.ID}
	}
	if len(ids) == 0 {
		return nil, errors.New("Suno answered without a song id")
	}
	return ids, nil
}

// sunoClipAnswer is the part of a clip object the run reads.
type sunoClipAnswer struct {
	ID       string          `json:"id"`
	Status   string          `json:"status"`
	Title    string          `json:"title"`
	SongURL  string          `json:"song_url"`
	Error    json.RawMessage `json:"error"`
	Metadata struct {
		Duration float64 `json:"duration"`
	} `json:"metadata"`
}

func (c sunoClipAnswer) errorText() string {
	raw := strings.TrimSpace(string(c.Error))
	if raw == "" || raw == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(c.Error, &s) == nil {
		return s
	}
	var obj struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	}
	if json.Unmarshal(c.Error, &obj) == nil && (obj.Message != "" || obj.Code != "") {
		return strings.TrimSpace(obj.Message + " " + obj.Code)
	}
	return raw
}

func sunoSettled(status string) bool { return status == "complete" || status == "error" }

// finishSunoTrack waits for a track's songs and files them.
func (a *App) finishSunoTrack(ctx context.Context, run *sunoRun, client toolCaller, index int, t Track, ids []string, opts SunoRunOptions) {
	clips, err := a.waitSunoClips(ctx, client, ids, func(clips []sunoClipAnswer) {
		a.updateSunoTrack(run, index, func(tr *SunoTrackRun) { mergeSunoClips(tr, clips) })
	})
	if err != nil {
		a.updateSunoTrack(run, index, func(tr *SunoTrackRun) { tr.State, tr.Error = sunoTrackFailed, err.Error() })
		return
	}
	good := []sunoClipAnswer{}
	var failures []string
	for _, c := range clips {
		if c.Status == "complete" {
			good = append(good, c)
		} else if msg := c.errorText(); msg != "" {
			failures = append(failures, msg)
		} else {
			failures = append(failures, "the song ended in "+c.Status)
		}
	}
	if len(good) == 0 {
		msg := "Suno could not make this song"
		if len(failures) > 0 {
			msg += ": " + strings.Join(failures, "; ")
		}
		a.updateSunoTrack(run, index, func(tr *SunoTrackRun) { tr.State, tr.Error = sunoTrackFailed, msg })
		return
	}
	if !opts.Download {
		a.updateSunoTrack(run, index, func(tr *SunoTrackRun) { tr.State = sunoTrackInLibrary })
		return
	}

	a.updateSunoTrack(run, index, func(tr *SunoTrackRun) { tr.State = sunoTrackDownloading })
	var problems []string
	for _, c := range good {
		fileID, err := a.fileSunoClip(ctx, client, t, c.ID)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		a.updateSunoTrack(run, index, func(tr *SunoTrackRun) {
			for i := range tr.Clips {
				if tr.Clips[i].ID == c.ID {
					tr.Clips[i].FileID = fileID
				}
			}
		})
	}
	a.updateSunoTrack(run, index, func(tr *SunoTrackRun) {
		filed := 0
		for _, c := range tr.Clips {
			if c.FileID != "" {
				filed++
			}
		}
		switch {
		case filed == 0:
			tr.State = sunoTrackFailed
		default:
			tr.State = sunoTrackFiled
		}
		if len(problems) > 0 {
			tr.Error = strings.Join(problems, "; ")
		} else if len(failures) > 0 {
			tr.Error = "One song failed: " + strings.Join(failures, "; ")
		}
	})
}

func mergeSunoClips(tr *SunoTrackRun, clips []sunoClipAnswer) {
	for _, c := range clips {
		for i := range tr.Clips {
			if tr.Clips[i].ID == c.ID {
				tr.Clips[i].Status, tr.Clips[i].SongURL = c.Status, c.SongURL
				tr.Clips[i].Seconds, tr.Clips[i].Error = c.Metadata.Duration, c.errorText()
			}
		}
	}
}

// waitSunoClips calls audio-wait until every clip is complete or failed.
// audio-wait answers after about fifty seconds with the clips still pending,
// so it is called again until the run's own limit.
func (a *App) waitSunoClips(ctx context.Context, client toolCaller, ids []string, progress func([]sunoClipAnswer)) ([]sunoClipAnswer, error) {
	deadline := time.Now().Add(sunoWaitLimit)
	for {
		res, err := client.CallTool(ctx, "audio-wait", map[string]any{"ids": ids, "until": "complete"})
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if sunoRateLimited(err) {
				time.Sleep(5 * time.Second)
				continue
			}
			return nil, a.sunoError(err)
		}
		var answer struct {
			sunoClipAnswer
			Clips []sunoClipAnswer `json:"clips"`
		}
		if err := res.Decode(&answer); err != nil {
			return nil, fmt.Errorf("Suno's answer could not be read: %w", err)
		}
		clips := answer.Clips
		if len(clips) == 0 {
			clips = []sunoClipAnswer{answer.sunoClipAnswer}
		}
		progress(clips)
		settled := len(clips) >= len(ids)
		for _, c := range clips {
			if !sunoSettled(c.Status) {
				settled = false
			}
		}
		if settled {
			return clips, nil
		}
		if time.Now().After(deadline) {
			return nil, errors.New("Suno has not finished after twenty minutes — the songs will land in your Suno library when it does")
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
}

// fileSunoClip downloads one song and files it onto the track as a take.
// A clip already filed — by an earlier run, or recorded in the browser — is
// left alone, which is what the clip id is kept for.
func (a *App) fileSunoClip(ctx context.Context, client toolCaller, t Track, clipID string) (string, error) {
	sunoCaptureMu.Lock()
	already, err := a.store.trackWithSunoClip(clipID)
	sunoCaptureMu.Unlock()
	if err != nil {
		return "", err
	}
	if already != "" {
		return a.fileIDForClip(already, clipID), nil
	}

	link, err := a.sunoDownloadURL(ctx, client, clipID)
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "aimusicmaker-sunomcp-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	name := slugify(t.Name)
	if name == "" {
		name = "take"
	}
	temp := filepath.Join(dir, name+".mp3")
	if err := downloadTo(ctx, link, temp); err != nil {
		return "", err
	}

	sunoCaptureMu.Lock()
	defer sunoCaptureMu.Unlock()
	// Checked again under the lock: the browser may have recorded the same
	// song while this one was downloading.
	if already, err := a.store.trackWithSunoClip(clipID); err == nil && already != "" {
		return a.fileIDForClip(already, clipID), nil
	}
	label := "Made through Suno MCP " + clipID[:min(8, len(clipID))]
	track, err := a.importTrackFile(t.ID, temp, label, clipID)
	if err != nil {
		return "", err
	}
	return a.fileIDForClip(track.ID, clipID), nil
}

func (a *App) fileIDForClip(trackID, clipID string) string {
	files, err := a.store.trackFiles(trackID)
	if err != nil {
		return ""
	}
	for _, f := range files {
		if f.SunoClip == clipID {
			return f.ID
		}
	}
	return ""
}

// sunoDownloadURL asks audio-download for the file. Called with confirm only
// because the producer approved the download credit when starting the run —
// the tool's own rule is to ask first, and the dialog is where that was asked.
func (a *App) sunoDownloadURL(ctx context.Context, client toolCaller, clipID string) (string, error) {
	confirm := false
	for attempt := 0; attempt < 8; attempt++ {
		args := map[string]any{"id": clipID, "format": "mp3"}
		if confirm {
			args["confirm"] = true
		}
		res, err := client.CallTool(ctx, "audio-download", args)
		if err != nil {
			if sunoRateLimited(err) {
				time.Sleep(5 * time.Second)
				continue
			}
			return "", a.sunoError(err)
		}
		var answer struct {
			Status  string `json:"status"`
			URL     string `json:"url"`
			Message string `json:"message"`
		}
		if err := res.Decode(&answer); err != nil {
			return "", fmt.Errorf("Suno's download answer could not be read: %w", err)
		}
		switch {
		case answer.URL != "":
			return answer.URL, nil
		case answer.Status == "requires_credit" && !confirm:
			confirm = true
		case answer.Status == "processing":
			// The tool has already waited about fifty seconds; ask again.
		default:
			msg := answer.Message
			if msg == "" {
				msg = answer.Status
			}
			return "", fmt.Errorf("Suno would not hand over the file: %s", msg)
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
	}
	return "", errors.New("Suno was still preparing the file after several tries — it is in your Suno library")
}

func downloadTo(ctx context.Context, link, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return err
	}
	resp, err := sunoDownloadHTTP.Do(req)
	if err != nil {
		return fmt.Errorf("could not download the song: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading the song answered %s", resp.Status)
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(out, io.LimitReader(resp.Body, maxTrackFileBytes+1))
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written > maxTrackFileBytes {
		return fmt.Errorf("the song is larger than this app will file (%d MB)", maxTrackFileBytes>>20)
	}
	if written == 0 {
		return errors.New("Suno sent an empty file")
	}
	return nil
}
