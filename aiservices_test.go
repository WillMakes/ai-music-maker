package main

import (
	"bytes"
	"io"
	"net/http"
	"testing"
)

// stubModels makes the verify client answer every request with a models list, so
// ConnectAIKey accepts a key without reaching a provider.
//
// A RoundTripper rather than an httptest server because the URL is chosen inside
// modelsRequest from the service id — there is no seam to point at a stub
// address, and inventing one only for a test would be a seam the app does not
// otherwise want.
type stubModels struct{}

func (stubModels) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(bytes.NewReader(
			[]byte(`{"data":[{"id":"grok-2-latest"}]}`))),
	}, nil
}

func stubVerify(t *testing.T) {
	t.Helper()
	previous := aiVerifyHTTP
	aiVerifyHTTP = &http.Client{Transport: stubModels{}}
	t.Cleanup(func() { aiVerifyHTTP = previous })
}

// withCLIs makes lookUpCommand report exactly the named commands as installed,
// so a test can connect a provider in account mode without depending on what
// happens to be on the machine running it.
func withCLIs(t *testing.T, present ...string) {
	t.Helper()
	found := map[string]bool{}
	for _, name := range present {
		found[name] = true
	}
	previous := lookUpCommand
	lookUpCommand = func(name string) (string, bool) {
		if found[name] {
			return "/stub/bin/" + name, true
		}
		return "", false
	}
	t.Cleanup(func() { lookUpCommand = previous })
}

// TestGetAIServicesWithOneConnected is #12.
//
// describeAIService set Default by asking defaultAIService, which asks
// connectedAIServices, which asks describeAIService — a cycle broken only by
// Go's && short-circuit. With nothing connected, status.Connected is false
// everywhere and defaultAIService is never reached, so the panel drew fine. The
// moment one provider was connected the guard opened and the call recursed until
// the stack gave out, taking the answer — and the whole AI panel — with it.
//
// This test hangs and then dies on the old code. It is the reproduction.
func TestGetAIServicesWithOneConnected(t *testing.T) {
	app := newTestApp(t)
	withCLIs(t, "claude")

	if _, err := app.ConnectAIAccount(serviceAnthropic); err != nil {
		t.Fatalf("connect: %v", err)
	}

	services := app.GetAIServices()
	if len(services) != len(aiServiceIDs) {
		t.Fatalf("%d services, want %d — the panel has nothing to draw",
			len(services), len(aiServiceIDs))
	}

	byID := map[string]AIServiceStatus{}
	for _, s := range services {
		byID[s.ID] = s
	}
	claude := byID[serviceAnthropic]
	if !claude.Connected {
		t.Error("the connected provider does not report itself connected")
	}
	if !claude.Default {
		t.Error("the only connected provider is not the default")
	}
	// The others must still be listed. "We cannot see the AI provider options" is
	// the report, and a list holding only what is connected would be the same
	// complaint with a different cause.
	if byID[serviceOpenAI].Connected || byID[serviceGrok].Connected {
		t.Error("a provider nobody connected reports itself connected")
	}
	if byID[serviceOpenAI].Default || byID[serviceGrok].Default {
		t.Error("more than one provider is starred as the default")
	}
}

// TestGetAIServicesWithEveryProviderConnected: the cycle got worse the more were
// connected, so the fixed version is checked at the other end of the range too.
func TestGetAIServicesWithEveryProviderConnected(t *testing.T) {
	app := newTestApp(t)
	withCLIs(t, "claude", "codex", "gemini", "agy")

	if _, err := app.ConnectAIAccount(serviceAnthropic); err != nil {
		t.Fatalf("claude: %v", err)
	}
	if _, err := app.ConnectAIAccount(serviceOpenAI); err != nil {
		t.Fatalf("codex: %v", err)
	}
	if _, err := app.ConnectAIAccount(serviceGemini); err != nil {
		t.Fatalf("gemini: %v", err)
	}
	if _, err := app.ConnectAIAccount(serviceAntigravity); err != nil {
		t.Fatalf("antigravity: %v", err)
	}
	// Grok has no CLI to borrow, so it is connected the only way it can be.
	stubVerify(t)
	if _, err := app.ConnectAIKey(serviceGrok, "xai-stub-key"); err != nil {
		t.Fatalf("grok: %v", err)
	}

	services := app.GetAIServices()
	if len(services) != len(aiServiceIDs) {
		t.Fatalf("%d services, want %d", len(services), len(aiServiceIDs))
	}
	defaults := 0
	for _, s := range services {
		if !s.Connected {
			t.Errorf("%s reports itself disconnected", s.ID)
		}
		if s.Default {
			defaults++
		}
	}
	// Exactly one, whatever else is true: the star is "which one new work reaches
	// for", and a list with three starred has not answered it.
	if defaults != 1 {
		t.Errorf("%d providers starred as default, want 1", defaults)
	}
}

// TestGetAIServiceMatchesTheList: the detail page reads one service through its
// own binding, which took the same path and so had the same cycle.
func TestGetAIServiceMatchesTheList(t *testing.T) {
	app := newTestApp(t)
	withCLIs(t, "claude")
	if _, err := app.ConnectAIAccount(serviceAnthropic); err != nil {
		t.Fatalf("connect: %v", err)
	}

	one := app.GetAIService(serviceAnthropic)
	if !one.Connected || !one.Default {
		t.Errorf("detail page reports connected=%v default=%v, want both true",
			one.Connected, one.Default)
	}
	for _, s := range app.GetAIServices() {
		if s.ID != serviceAnthropic {
			continue
		}
		if s.Default != one.Default || s.Connected != one.Connected {
			t.Error("the list and the detail page disagree about the same service")
		}
	}
}

// TestStarringAProviderSurvivesADisconnect: the starred service is resolved to
// one that can actually answer, so a stored choice that has since been
// disconnected must not keep the star.
func TestStarringAProviderSurvivesADisconnect(t *testing.T) {
	app := newTestApp(t)
	withCLIs(t, "claude", "codex", "gemini", "agy")

	if _, err := app.ConnectAIAccount(serviceAnthropic); err != nil {
		t.Fatalf("claude: %v", err)
	}
	if _, err := app.ConnectAIAccount(serviceOpenAI); err != nil {
		t.Fatalf("codex: %v", err)
	}
	if _, err := app.ConnectAIAccount(serviceGemini); err != nil {
		t.Fatalf("gemini: %v", err)
	}
	if _, err := app.ConnectAIAccount(serviceAntigravity); err != nil {
		t.Fatalf("antigravity: %v", err)
	}
	if _, err := app.SetDefaultAIService(serviceOpenAI); err != nil {
		t.Fatalf("star: %v", err)
	}
	if app.GetAIService(serviceOpenAI).Default != true {
		t.Fatal("starring did not take")
	}

	if _, err := app.DisconnectAI(serviceOpenAI); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	services := app.GetAIServices()
	if len(services) != len(aiServiceIDs) {
		t.Fatalf("%d services after a disconnect, want %d", len(services), len(aiServiceIDs))
	}
	stars := 0
	for _, s := range services {
		if s.Default {
			stars++
			if s.ID != serviceAnthropic {
				t.Errorf("the star went to %s, want the one still connected", s.ID)
			}
		}
	}
	if stars != 1 {
		t.Errorf("%d stars after a disconnect, want 1", stars)
	}
}
