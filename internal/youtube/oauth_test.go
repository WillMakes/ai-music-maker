package youtube

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// fakeGoogle stands in for the token endpoint and the channels API, and
// checks what a real one would: the verifier matches the challenge, the
// client is the one that asked.
func fakeGoogle(t *testing.T) (challenges map[string]string) {
	t.Helper()
	challenges = map[string]string{} // code -> challenge it was issued against
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = r.ParseForm()
			if r.Form.Get("client_id") != "cid" || r.Form.Get("client_secret") != "secret" {
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_client"})
				return
			}
			switch r.Form.Get("grant_type") {
			case "authorization_code":
				sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
				if challenges[r.Form.Get("code")] != base64.RawURLEncoding.EncodeToString(sum[:]) {
					w.WriteHeader(http.StatusBadRequest)
					_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"access_token": "access-1", "refresh_token": "refresh-1", "expires_in": 3600})
			case "refresh_token":
				_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access-2", "expires_in": 3600})
			}
		case "/youtube/v3/channels":
			if r.Header.Get("Authorization") != "Bearer access-1" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{
				map[string]any{"id": "UC123", "snippet": map[string]any{"title": "Cassette Motel"}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	previous := []string{TokenURL, APIBase}
	TokenURL, APIBase = server.URL+"/token", server.URL
	t.Cleanup(func() { TokenURL, APIBase = previous[0], previous[1] })
	return challenges
}

// browser follows the sign-in the way the producer's would: Google approves
// and redirects to the loopback address with a code (or with what tamper
// does to the answer).
func browser(t *testing.T, challenges map[string]string, tamper func(url.Values)) func(string) {
	return func(signIn string) {
		u, err := url.Parse(signIn)
		if err != nil {
			t.Error(err)
			return
		}
		q := u.Query()
		for _, scope := range Scopes {
			if !strings.Contains(q.Get("scope"), scope) {
				t.Errorf("the sign-in does not ask for %s", scope)
			}
		}
		if q.Get("access_type") != "offline" || q.Get("code_challenge_method") != "S256" {
			t.Errorf("the sign-in asks for no refresh token or no PKCE: %v", q)
		}
		challenges["code-1"] = q.Get("code_challenge")
		back := url.Values{"code": {"code-1"}, "state": {q.Get("state")}}
		if tamper != nil {
			tamper(back)
		}
		go func() {
			resp, err := http.Get(q.Get("redirect_uri") + "?" + back.Encode())
			if err == nil {
				resp.Body.Close()
			}
		}()
	}
}

func TestAuthorize(t *testing.T) {
	challenges := fakeGoogle(t)
	client := Client{ID: "cid", Secret: "secret"}

	token, err := Authorize(context.Background(), client, browser(t, challenges, nil))
	if err != nil {
		t.Fatal(err)
	}
	if token.Access != "access-1" || token.Refresh != "refresh-1" || !token.Valid() {
		t.Fatalf("token = %+v", token)
	}
	channel, err := MyChannel(context.Background(), token.Access)
	if err != nil || channel.ID != "UC123" || channel.Title != "Cassette Motel" {
		t.Fatalf("channel = %+v, %v", channel, err)
	}

	// A refresh answers without a refresh token; the one held is kept.
	again, err := Refreshed(context.Background(), client, token.Refresh)
	if err != nil || again.Access != "access-2" || again.Refresh != "refresh-1" {
		t.Fatalf("refreshed = %+v, %v", again, err)
	}
}

// A callback whose state is not the one sent is somebody else's sign-in.
func TestAuthorizeRefusesAForeignState(t *testing.T) {
	challenges := fakeGoogle(t)
	_, err := Authorize(context.Background(), Client{ID: "cid", Secret: "secret"},
		browser(t, challenges, func(v url.Values) { v.Set("state", "forged") }))
	if err == nil || !strings.Contains(err.Error(), "different request") {
		t.Fatalf("err = %v", err)
	}
}

// The producer pressed Cancel on Google's consent screen.
func TestAuthorizeReportsARefusal(t *testing.T) {
	challenges := fakeGoogle(t)
	_, err := Authorize(context.Background(), Client{ID: "cid", Secret: "secret"},
		browser(t, challenges, func(v url.Values) { v.Del("code"); v.Set("error", "access_denied") }))
	if err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("err = %v", err)
	}
}
