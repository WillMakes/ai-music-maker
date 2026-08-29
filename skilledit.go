package main

import (
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Editing a skill with AI (#23)
//
// A skill is several hundred words of instruction, and revising one by hand
// means holding the whole document in your head while changing a paragraph of
// it. With a provider connected, the producer can instead say what should
// change and have the model do the rewriting — in two passes, because that is
// this repository's standing rule: the conversation converses, and a separate
// pass extracts. A model asked to discuss an edit and emit the full revised
// document in the same breath emits a conversation with a document in the
// middle of it.
//
// The transcript lives in the dialog and nowhere else. It ends in a revised
// draft dropped into the edit form — still every bit as editable by hand as if
// it had been typed, because the AI writes a draft, it does not take the pen —
// or it ends in nothing, and either way there is nothing worth keeping.
// ---------------------------------------------------------------------------

// SkillEditDraft is what the extraction pass hands back: the revised skill,
// whole. Whole rather than as a diff, because a diff from a model has to be
// applied to text the model was paraphrasing from memory, and the seams show.
type SkillEditDraft struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Body        string `json:"body"`
}

// AskSkillEdit answers one turn of the editing conversation.
//
// The same error shape as AskInterview: a failed turn is appended to the
// thread as a note rather than returned bare, so the dialog shows what went
// wrong in the place the answer would have been and the producer can send the
// same message again without retyping it. Unlike an interview, nothing is
// persisted — see the note at the top of the file.
func (a *App) AskSkillEdit(skillID string, messages []InterviewMessage) ([]InterviewMessage, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("there is nothing to answer")
	}
	skill, err := a.GetSkill(skillID)
	if err != nil {
		return nil, err
	}

	answer, askErr := a.askAI(a.interviewContext(), skillEditBrief(skill),
		toChatMsgs(trimTranscript(messages)))
	if askErr != nil {
		return append(messages, InterviewMessage{Role: "assistant", Note: askErr.Error()}), nil
	}
	return append(messages, InterviewMessage{Role: "assistant", Text: answer}), nil
}

// skillEditBrief is the standing instruction for the conversation pass.
//
// The current skill rides in the brief rather than in a turn, so every turn of
// the conversation is about the same document — a skill pasted as the first
// user message scrolls out of a trimmed transcript, and the model ends up
// revising its own summary of a document it can no longer see.
func skillEditBrief(skill Skill) string {
	var b strings.Builder
	b.WriteString("You are helping a music producer revise one of this app's skills — ")
	b.WriteString("a standing instruction document the app hands to a model. ")
	b.WriteString("Discuss the change they want in plain prose: propose how you would ")
	b.WriteString("word it, quote the passages you would touch, and ask when the ")
	b.WriteString("request is ambiguous. Keep answers short and concrete. Do not ")
	b.WriteString("output the whole revised document — the app has a separate step ")
	b.WriteString("that produces it once the producer is satisfied.\n\n")
	if skill.BuiltIn {
		b.WriteString("This is one of the app's own skills: its title and ")
		b.WriteString("description are fixed, so only the body may change.\n\n")
	}
	b.WriteString("The skill as it stands:\n\n")
	b.WriteString("Title: " + skill.Title + "\n")
	b.WriteString("Description: " + skill.Description + "\n\n")
	b.WriteString(skill.Body)
	return b.String()
}

// ExtractSkillEdit reads the conversation and returns the revised skill.
//
// The current skill is handed to the extraction alongside the transcript,
// because the conversation discusses the changes and the document is what they
// apply to — an extraction given only the discussion would have to reinvent
// every paragraph nobody mentioned.
func (a *App) ExtractSkillEdit(skillID string, messages []InterviewMessage) (SkillEditDraft, error) {
	skill, err := a.GetSkill(skillID)
	if err != nil {
		return SkillEditDraft{}, err
	}

	var draft SkillEditDraft
	err = a.extract(messages, `Below is a conversation about revising a skill
document, followed by the document as it currently stands. Apply the changes
that were agreed in the conversation to the document.

Return ONLY a JSON object, no prose and no code fence, of the form
{"title": …, "description": …, "body": …} — the complete revised skill.
Keep everything the conversation did not ask to change exactly as it is,
including the title and description unless they were explicitly discussed.
The body is markdown.

The document as it currently stands:

Title: `+skill.Title+`
Description: `+skill.Description+`

`+skill.Body, &draft)
	if err != nil {
		return SkillEditDraft{}, err
	}

	// A built-in's title and description name the slot the app looks it up by;
	// whatever the model returned, they stay the build's. GetSkill already
	// answers the catalogue's identity for a built-in whatever the row says —
	// this keeps the preview in the form honest too.
	if skill.BuiltIn {
		draft.Title = skill.Title
		draft.Description = skill.Description
	}
	// An extraction that came back empty-bodied is a failed one, not an edit:
	// no conversation about revising a document ends with "delete it all".
	if strings.TrimSpace(draft.Body) == "" {
		return SkillEditDraft{}, fmt.Errorf(
			"the revision came back empty — try asking for the change again")
	}
	return draft, nil
}
