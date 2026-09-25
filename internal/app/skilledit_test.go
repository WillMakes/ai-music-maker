package app

import (
	"strings"
	"testing"
)

// The brief pattern is the same one the interviews are tested on: what the
// model is standing on has to actually be in front of it.

func TestSkillEditBriefCarriesTheDocument(t *testing.T) {
	brief := skillEditBrief(Skill{
		Title:       "Track generation",
		Description: "How prompts are written",
		Body:        "Always lead with the genre.",
	})
	for _, want := range []string{
		"Track generation",
		"How prompts are written",
		"Always lead with the genre.",
	} {
		if !strings.Contains(brief, want) {
			t.Fatalf("the brief is missing %q:\n%s", want, brief)
		}
	}
	// The conversation pass converses; the extraction pass writes the document.
	// The brief has to say so, or the chat fills with full drafts.
	if !strings.Contains(brief, "Do not output the whole revised document") {
		t.Fatalf("the brief does not hold the document back:\n%s", brief)
	}
	if strings.Contains(brief, "title and description are fixed") {
		t.Fatal("a producer's own skill was briefed as a built-in")
	}
}

func TestSkillEditBriefLocksABuiltInsIdentity(t *testing.T) {
	brief := skillEditBrief(Skill{Title: "Bug reports", Body: "…", BuiltIn: true})
	if !strings.Contains(brief, "title and description are fixed") {
		t.Fatalf("a built-in's brief does not say its identity is fixed:\n%s", brief)
	}
}

func TestAskSkillEditRefusesAnEmptyThreadAndAMissingSkill(t *testing.T) {
	app := newTestApp(t)
	if _, err := app.AskSkillEdit("anything", nil); err == nil {
		t.Fatal("an empty thread was answered")
	}
	turn := []InterviewMessage{{Role: "user", Text: "tighten it"}}
	if _, err := app.AskSkillEdit("skill_nope", turn); err == nil {
		t.Fatal("a skill that does not exist was edited")
	}
}

// A turn that fails — here because no provider is connected — comes back as a
// note on the thread rather than as an error, the same shape AskInterview
// uses, so the dialog shows the failure where the answer would have been.
func TestAskSkillEditAppendsFailuresAsNotes(t *testing.T) {
	app := newTestApp(t)
	skills, err := app.ListSkills()
	if err != nil || len(skills) == 0 {
		t.Fatalf("no built-in skills to edit: %v", err)
	}

	turn := []InterviewMessage{{Role: "user", Text: "tighten the opening"}}
	thread, err := app.AskSkillEdit(skills[0].ID, turn)
	if err != nil {
		t.Fatalf("a failed turn should not be an error: %v", err)
	}
	last := thread[len(thread)-1]
	if last.Role != "assistant" || last.Note == "" {
		t.Fatalf("the failure did not land as an assistant note: %+v", last)
	}
	// And the producer's own turn is still on the thread to send again.
	if thread[0].Text != "tighten the opening" {
		t.Fatalf("the producer's turn was lost: %+v", thread)
	}
}
