package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// What Jax says when it refuses (#15)
//
// A refused tool call is not a failed request. MCP answers HTTP 200 and puts the
// refusal in the body as {"result":{"isError":true,"content":[…]}} — the same
// shape a success takes, with one flag flipped. So a caller that only checks the
// transport reads a refusal as a success, and the app goes on believing it told
// Jax something it never told it.
//
// The stub here answers exactly as the real Jax did: 200, isError, and the
// sentence explaining why.
// ---------------------------------------------------------------------------

// jaxStub stands in for Jax's loopback endpoint.
type jaxStub struct {
	// refuseWith, when set, is the text of a tool-level refusal.
	refuseWith string
	// calls records the tools that were invoked, so a test can assert the call
	// was actually made rather than skipped.
	calls []string
}

func (s *jaxStub) start(t *testing.T) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.calls = append(s.calls, body.Params.Name)

		w.Header().Set("Content-Type", "application/json")
		// 200 either way. That is the whole point of this file.
		if s.refuseWith != "" {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"isError":true,` +
				`"content":[{"type":"text","text":` +
				mustJSON(s.refuseWith) + `}]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":` +
			`[{"type":"text","text":"{\"id\":993}"}]}}`))
	}))
	t.Cleanup(server.Close)

	previous := findJaxTarget
	findJaxTarget = func() (jaxTarget, error) {
		return jaxTarget{url: server.URL, token: "stub-token"}, nil
	}
	t.Cleanup(func() { findJaxTarget = previous })
}

func mustJSON(s string) string {
	encoded, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(encoded)
}

// TestARefusedClaimIsReported is #15: Jax refused the check-out and the app said
// nothing, so the report read as claimed here and unworked there.
func TestARefusedClaimIsReported(t *testing.T) {
	app := newTestApp(t)
	stub := &jaxStub{refuseWith: "debug report 993 is not the oldest workable report — " +
		"report 987 was filed first, nobody is working it, so take that one instead"}
	stub.start(t)

	if err := app.store.saveBugReport(BugReport{
		ID: "bug_local", Description: "Something is broken.",
		JaxReportID: 993, CreatedAt: nowStamp(),
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	report, err := app.ClaimBugReport("bug_local")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}

	if len(stub.calls) != 1 || stub.calls[0] != jaxClaimTool {
		t.Fatalf("Jax was called with %v, want one %s", stub.calls, jaxClaimTool)
	}
	// The claim still stands locally — Jax refusing to record it must not stop
	// the work, for the reason at the top of jax.go.
	if !report.CheckedOut {
		t.Error("the local claim was abandoned because a tracker said no")
	}
	// But it must not be silent. Without this the two queues disagree and
	// nothing says so.
	if report.JaxNote == "" {
		t.Fatal("Jax refused the claim and the report carries no note about it")
	}
	if !strings.Contains(report.JaxNote, "oldest workable") {
		t.Errorf("note = %q, want Jax's own explanation", report.JaxNote)
	}
}

// TestAnAcceptedClaimLeavesNoNote: the ordinary case stays quiet.
func TestAnAcceptedClaimLeavesNoNote(t *testing.T) {
	app := newTestApp(t)
	stub := &jaxStub{}
	stub.start(t)

	if err := app.store.saveBugReport(BugReport{
		ID: "bug_local", JaxReportID: 993, CreatedAt: nowStamp(),
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	report, err := app.ClaimBugReport("bug_local")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if report.JaxNote != "" {
		t.Errorf("an accepted claim left a note: %q", report.JaxNote)
	}
}

// TestARefusedResolveIsReported: resolving has the same hole, and the same
// stakes — a report cleared here and left open in Jax.
func TestARefusedResolveIsReported(t *testing.T) {
	app := newTestApp(t)
	stub := &jaxStub{refuseWith: "debug report 993 is checked out by another agent"}
	stub.start(t)

	if err := app.store.saveBugReport(BugReport{
		ID: "bug_local", JaxReportID: 993, CreatedAt: nowStamp(),
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	resolved, err := app.ResolveBugReport("bug_local", "abc123", "https://example.test/abc123")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// Resolved locally regardless: the fix is on main and the queue must reflect
	// that whatever the tracker thinks.
	if resolved.ResolvedAt == "" {
		t.Error("the report was not resolved because a tracker said no")
	}
	if resolved.JaxNote == "" {
		t.Fatal("Jax refused the resolve and the record carries no note about it")
	}
}

// TestAClosedJaxLeavesNoNote: Jax not running is an ordinary state, not a
// refusal, and must not be reported as one.
func TestAClosedJaxLeavesNoNote(t *testing.T) {
	app := newTestApp(t)
	previous := findJaxTarget
	findJaxTarget = func() (jaxTarget, error) { return jaxTarget{}, errJaxUnavailable }
	t.Cleanup(func() { findJaxTarget = previous })

	if err := app.store.saveBugReport(BugReport{
		ID: "bug_local", JaxReportID: 993, CreatedAt: nowStamp(),
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	report, err := app.ClaimBugReport("bug_local")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if report.JaxNote != "" {
		t.Errorf("a closed Jax was reported as a refusal: %q", report.JaxNote)
	}
}

// TestJaxRefusal reads the flag and the text out of a tool result.
func TestJaxRefusal(t *testing.T) {
	refused := `{"jsonrpc":"2.0","id":1,"result":{"isError":true,` +
		`"content":[{"type":"text","text":"not the oldest workable report"}]}}`
	if note := jaxRefusal(refused); note != "not the oldest workable report" {
		t.Errorf("refusal = %q, want the content text", note)
	}

	accepted := `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"{\"id\":993}"}]}}`
	if note := jaxRefusal(accepted); note != "" {
		t.Errorf("an accepted call reported a refusal: %q", note)
	}

	// An answer this app cannot read is not evidence of a refusal. Reporting one
	// would put a permanent warning on a report that Jax accepted.
	for _, unreadable := range []string{"", "not json", "{}", `{"result":{}}`} {
		if note := jaxRefusal(unreadable); note != "" {
			t.Errorf("jaxRefusal(%q) = %q, want empty", unreadable, note)
		}
	}

	// isError with nothing to quote still has to say something, or the note
	// would be set-but-empty and read as no refusal at all.
	bare := `{"result":{"isError":true,"content":[]}}`
	if note := jaxRefusal(bare); note == "" {
		t.Error("a refusal with no text produced no note")
	}
}
