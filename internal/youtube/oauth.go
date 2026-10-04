// Package youtube reaches the producer's YouTube channel (#121): signing in
// with Google, and — for #122 and #123 — uploading a video and reading its
// analytics. Nothing here knows about the app; the app stores what this hands
// back.
//
// The sign-in is the one Google gives installed apps, and the one Jax's Brand
// Producer uses: the producer makes an OAuth client of the "Desktop app" kind
// in their own Google Cloud project and pastes its id and secret, the browser
// signs in, and Google redirects to a loopback address this process is
// listening on. PKCE rides along, so a code caught by anything else on the
// machine is useless without the verifier only this process holds.
//
// Written against the endpoints rather than golang.org/x/oauth2: the flow is
// three requests, and the dependency would bring its own token-source
// machinery that the app's settings-stored token would have to be bent into.
package youtube

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Scopes is what the app asks for: uploading the album's video (#122),
// reading the channel and its videos, and their analytics (#123).
var Scopes = []string{
	"https://www.googleapis.com/auth/youtube.upload",
	"https://www.googleapis.com/auth/youtube.readonly",
	"https://www.googleapis.com/auth/yt-analytics.readonly",
}

// Endpoints, variables so a test can stand in for Google.
var (
	AuthURL   = "https://accounts.google.com/o/oauth2/v2/auth"
	TokenURL  = "https://oauth2.googleapis.com/token"
	RevokeURL = "https://oauth2.googleapis.com/revoke"
	APIBase   = "https://www.googleapis.com"
)

var httpClient = &http.Client{Timeout: 60 * time.Second}

// Client is the producer's OAuth client.
type Client struct {
	ID     string
	Secret string
}

// Token is what a sign-in or a refresh hands back.
type Token struct {
	Access  string
	Refresh string
	// Expiry is when Access stops working.
	Expiry time.Time
}

// Valid reports whether the access token has at least a minute left — a
// request started on a token about to expire would fail halfway.
func (t Token) Valid() bool {
	return t.Access != "" && time.Until(t.Expiry) > time.Minute
}

// SignInTimeout is how long Authorize waits for the browser.
const SignInTimeout = 5 * time.Minute

// Authorize runs the browser sign-in and returns the token. open is how the
// sign-in page reaches the producer's browser.
func Authorize(ctx context.Context, client Client, open func(string)) (Token, error) {
	if strings.TrimSpace(client.ID) == "" || strings.TrimSpace(client.Secret) == "" {
		return Token{}, errors.New("enter the OAuth client's id and secret first")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Token{}, fmt.Errorf("could not listen for the sign-in: %w", err)
	}
	defer listener.Close()
	redirect := fmt.Sprintf("http://127.0.0.1:%d/", listener.Addr().(*net.TCPAddr).Port)

	verifier := randomString(48)
	challenge := sha256.Sum256([]byte(verifier))
	state := randomString(24)

	q := url.Values{
		"client_id":             {client.ID},
		"redirect_uri":          {redirect},
		"response_type":         {"code"},
		"scope":                 {strings.Join(Scopes, " ")},
		"access_type":           {"offline"},
		"prompt":                {"consent"},
		"state":                 {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"},
	}

	type result struct {
		code string
		err  error
	}
	results := make(chan result, 1)
	server := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only the callback counts; a browser asking for /favicon.ico is not
			// a sign-in that failed.
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			got := r.URL.Query()
			var res result
			switch {
			case got.Get("state") != state:
				res.err = errors.New("the sign-in came back for a different request — try again")
			case got.Get("error") != "":
				res.err = fmt.Errorf("Google refused the sign-in: %s", got.Get("error"))
			case got.Get("code") == "":
				res.err = errors.New("the sign-in came back without a code")
			default:
				res.code = got.Get("code")
			}
			page(w, res.err)
			select {
			case results <- res:
			default:
			}
		}),
	}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	open(AuthURL + "?" + q.Encode())

	ctx, cancel := context.WithTimeout(ctx, SignInTimeout)
	defer cancel()
	var res result
	select {
	case res = <-results:
	case <-ctx.Done():
		return Token{}, errors.New("the sign-in was not finished in the browser — try again")
	}
	if res.err != nil {
		return Token{}, res.err
	}
	return exchange(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {res.code},
		"redirect_uri":  {redirect},
		"client_id":     {client.ID},
		"client_secret": {client.Secret},
		"code_verifier": {verifier},
	}, "")
}

// Refreshed trades the refresh token for a new access token.
func Refreshed(ctx context.Context, client Client, refresh string) (Token, error) {
	if refresh == "" {
		return Token{}, errors.New("YouTube is not connected — connect it in Settings → YouTube")
	}
	return exchange(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refresh},
		"client_id":     {client.ID},
		"client_secret": {client.Secret},
	}, refresh)
}

// Revoke tells Google to forget the grant. Best effort for the caller: a
// disconnect that could not reach Google has still forgotten the token here.
func Revoke(ctx context.Context, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, RevokeURL,
		strings.NewReader(url.Values{"token": {token}}.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// exchange posts to the token endpoint. keepRefresh is the refresh token to
// keep when the answer has none — Google sends one with the first sign-in
// only, and a refresh answers with the access token alone.
func exchange(ctx context.Context, form url.Values, keepRefresh string) (Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := httpClient.Do(req)
	if err != nil {
		return Token{}, fmt.Errorf("could not reach Google: %w", err)
	}
	defer resp.Body.Close()
	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		Description  string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Token{}, fmt.Errorf("Google's answer could not be read (%s)", resp.Status)
	}
	if body.Error != "" || body.AccessToken == "" {
		if body.Error == "invalid_grant" {
			return Token{}, errors.New("Google no longer accepts this connection — connect YouTube again")
		}
		msg := body.Description
		if msg == "" {
			msg = body.Error
		}
		if msg == "" {
			msg = resp.Status
		}
		return Token{}, fmt.Errorf("Google refused the token: %s", msg)
	}
	token := Token{
		Access:  body.AccessToken,
		Refresh: body.RefreshToken,
		Expiry:  time.Now().Add(time.Duration(body.ExpiresIn) * time.Second),
	}
	if token.Refresh == "" {
		token.Refresh = keepRefresh
	}
	return token, nil
}

// Channel is the signed-in account's channel.
type Channel struct {
	ID    string
	Title string
}

// MyChannel reads which channel the token is for.
func MyChannel(ctx context.Context, access string) (Channel, error) {
	var body struct {
		Items []struct {
			ID      string `json:"id"`
			Snippet struct {
				Title string `json:"title"`
			} `json:"snippet"`
		} `json:"items"`
	}
	if err := getJSON(ctx, access, APIBase+"/youtube/v3/channels?part=snippet&mine=true", &body); err != nil {
		return Channel{}, err
	}
	if len(body.Items) == 0 {
		return Channel{}, errors.New("this Google account has no YouTube channel — create one on YouTube, then connect again")
	}
	return Channel{ID: body.Items[0].ID, Title: body.Items[0].Snippet.Title}, nil
}

// getJSON is an authorised GET whose answer is decoded into out, with
// Google's own error message when it refuses.
func getJSON(ctx context.Context, access, address string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach YouTube: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return apiError(resp)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// apiError turns a refusal into Google's own words.
func apiError(resp *http.Response) error {
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body.Error.Message != "" {
		return fmt.Errorf("YouTube refused: %s", body.Error.Message)
	}
	return fmt.Errorf("YouTube refused: %s", resp.Status)
}

func randomString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)[:n]
}

// page is what the browser shows once the sign-in comes back, so the tab says
// what happened instead of spinning on a request nobody answered.
func page(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	msg := "YouTube is connected. You can close this tab and go back to AIMusicMaker."
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		msg = "The sign-in did not finish: " + html.EscapeString(err.Error())
	}
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>AIMusicMaker</title>`+
		`<body style="font:16px system-ui;margin:3rem;max-width:36rem">%s</body>`, msg)
}
