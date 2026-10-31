package app

import (
	"strings"
	"testing"

	"aimusicmaker/internal/mcpclient"
)

// connectSuno stores a Suno sign-in the way AuthorizeMCPServer does, without a
// browser and without reaching Suno.
func connectSuno(t *testing.T, app *App, change func(*mcpServerRecord)) {
	t.Helper()
	if _, err := app.updateMCPServer(sunoServerID, change); err != nil {
		t.Fatalf("storing the sign-in: %v", err)
	}
}

func signedIn(r *mcpServerRecord) {
	r.Auth = &mcpclient.Authorization{Token: mcpclient.Token{Access: "token"}}
	r.NeedsAuth = false
}

func TestSunoMCPSkillIsListedAsABuiltIn(t *testing.T) {
	app := newTestApp(t)
	skill, err := app.GetSkill(skillSunoMCP)
	if err != nil {
		t.Fatalf("the suno-mcp skill is not listed: %v", err)
	}
	if !skill.BuiltIn || skill.Overridden {
		t.Errorf("built in %v, overridden %v; want a built-in at its default", skill.BuiltIn, skill.Overridden)
	}
	if !strings.Contains(skill.Description, "connected") {
		t.Errorf("the description does not say it only applies while Suno is connected: %q", skill.Description)
	}
	// Written from the tools the app really calls and Suno's real limits.
	for _, want := range []string{"audio-create", "audio-wait", "audio-download", "account-usage",
		"256", "1,000", "5,000", "15–360", "instrumental: true", "download credit"} {
		if !strings.Contains(skill.Body, want) {
			t.Errorf("the skill does not mention %q", want)
		}
	}
}

func TestSunoMCPSkillJoinsTheTrackBriefsOnlyWhileSunoIsConnected(t *testing.T) {
	app := newTestApp(t)
	artist, album := aRecord(t, app)
	marker := "# Making tracks with Suno's MCP server"

	// Not connected: the briefs are exactly the interview skills and the facts,
	// as they were before the skill existed.
	for _, c := range []struct{ kind, subject, skill string }{
		{subjectTrack, album.ID, skillTrackGeneration},
		{subjectAlbum, artist.ID, skillAlbumInterview},
	} {
		brief, err := app.interviewBrief(c.kind, c.subject)
		if err != nil {
			t.Fatal(err)
		}
		facts, err := app.interviewFacts(c.kind, c.subject)
		if err != nil {
			t.Fatal(err)
		}
		if want := app.skillBody(c.skill) + "\n\n## What is already decided\n\n" + facts; brief != want {
			t.Errorf("the %s brief changed with Suno not connected", c.kind)
		}
	}

	// A sign-in Suno has since refused is not connected either.
	connectSuno(t, app, func(r *mcpServerRecord) { signedIn(r); r.NeedsAuth = true })
	if brief, _ := app.interviewBrief(subjectTrack, album.ID); strings.Contains(brief, marker) {
		t.Error("the skill was added while Suno needs a new sign-in")
	}

	connectSuno(t, app, signedIn)
	for _, c := range []struct{ kind, subject string }{
		{subjectTrack, album.ID},
		{subjectAlbum, artist.ID},
	} {
		brief, err := app.interviewBrief(c.kind, c.subject)
		if err != nil {
			t.Fatal(err)
		}
		at := strings.Index(brief, marker)
		if at < 0 {
			t.Errorf("the %s brief has no Suno MCP section while Suno is connected", c.kind)
			continue
		}
		// Instructions first, then the facts under their own heading.
		if facts := strings.Index(brief, "## What is already decided"); facts < at {
			t.Errorf("the %s brief puts the Suno section after the facts", c.kind)
		}
	}
	// The artist interview writes nothing Suno is sent.
	if brief, _ := app.interviewBrief(subjectArtist, ""); strings.Contains(brief, marker) {
		t.Error("the artist interview was given the Suno MCP skill")
	}
}

func TestSunoMCPSkillCanBeOverriddenAndReset(t *testing.T) {
	app := newTestApp(t)
	_, album := aRecord(t, app)
	connectSuno(t, app, signedIn)

	saved, err := app.SaveSkill(Skill{ID: skillSunoMCP, Body: "Keep every style prompt under 900 characters."})
	if err != nil {
		t.Fatalf("override: %v", err)
	}
	if !saved.Overridden || !saved.BuiltIn {
		t.Errorf("overridden %v, built in %v", saved.Overridden, saved.BuiltIn)
	}
	brief, _ := app.interviewBrief(subjectTrack, album.ID)
	if !strings.Contains(brief, "under 900 characters") || strings.Contains(brief, "# Making tracks with Suno's MCP server") {
		t.Error("the track brief does not carry the producer's override instead of the default")
	}

	if err := app.DeleteSkill(skillSunoMCP); err == nil {
		t.Error("a built-in skill was deleted")
	}
	reset, err := app.ResetSkill(skillSunoMCP)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if reset.Overridden || reset.Body != defaultSkillBody("suno-mcp.md") {
		t.Error("reset did not bring back the shipped text")
	}
	brief, _ = app.interviewBrief(subjectTrack, album.ID)
	if strings.Contains(brief, "under 900 characters") {
		t.Error("the brief kept the override after reset")
	}
}
