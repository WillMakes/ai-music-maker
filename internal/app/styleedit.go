package app

import (
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Refining an artist's base technical style (#30)
//
// The interview writes the base style once, when the artist is created. This
// is how it gets better afterwards: a conversation about the style as it
// stands, where the producer can paste YouTube or YouTube Music references and
// have them actually listened to — a song link in a turn gets the Antigravity
// breakdown the interviews get (#29) — and an Apply that writes the whole
// revised style into the edit form.
//
// The same two-pass shape as editing a skill (#23), for the same standing
// rule: the conversation converses, and a separate pass extracts. The result
// lands in the form, not the database — the AI writes a draft, it does not
// take the pen, and nothing is stored until the producer presses Save.
// ---------------------------------------------------------------------------

// StyleEditDraft is what the extraction hands back: the revised base style,
// whole. Whole rather than as a diff, because a diff from a model has to be
// applied to text the model was paraphrasing from memory, and the seams show.
type StyleEditDraft struct {
	BaseStyle string `json:"baseStyle"`
}

// AskStyleEdit answers one turn of the refinement conversation.
//
// The same error shape as the interviews: a failed turn is appended to the
// thread as a note, so the dialog shows what went wrong where the answer would
// have been and the producer can send the same message again. Nothing is
// persisted — the transcript ends in a draft in the form or in nothing.
func (a *App) AskStyleEdit(artistID string, messages []InterviewMessage) ([]InterviewMessage, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("there is nothing to answer")
	}
	artist, err := a.GetArtist(artistID)
	if err != nil {
		return nil, err
	}

	// A reference pasted into the refinement is researched the way it would be
	// in an interview (#29): the point of sharing the link is what the song
	// sounds like, and the conversation model cannot listen.
	if last := &messages[len(messages)-1]; last.Role == "user" && last.Research == "" {
		last.Research = a.researchForTurn(last.Text)
	}

	answer, askErr := a.askAI(a.interviewContext(), styleEditBrief(artist),
		toChatMsgs(trimTranscript(messages)))
	if askErr != nil {
		return append(messages, InterviewMessage{Role: "assistant", Note: askErr.Error()}), nil
	}
	return append(messages, InterviewMessage{Role: "assistant", Text: answer}), nil
}

// styleEditBrief is the standing instruction for the conversation pass.
//
// The current style rides in the brief rather than in a turn, so every turn is
// about the same document — pasted as a message it would scroll out of a
// trimmed transcript, and the model would end up revising its memory of it.
func styleEditBrief(artist Artist) string {
	var b strings.Builder
	b.WriteString("You are helping a music producer refine the base technical style of ")
	b.WriteString("one of their artists — the dense, comma-led breakdown every album ")
	b.WriteString("under the artist inherits. Discuss the change they want in plain ")
	b.WriteString("prose: propose wording, quote the lines you would touch, and ask when ")
	b.WriteString("a request is ambiguous. When a turn carries a music analysis the app ")
	b.WriteString("ran, treat it as how the reference actually sounds and say which of ")
	b.WriteString("its traits belong in the style. Keep answers short and concrete. Do ")
	b.WriteString("not output the whole revised style — the app has a separate step ")
	b.WriteString("that produces it once the producer is satisfied.\n\n")
	b.WriteString("The artist: " + artist.Name)
	if artist.Summary != "" {
		b.WriteString(" — " + artist.Summary)
	}
	b.WriteString("\n\nThe base technical style as it stands:\n\n")
	if artist.BaseStyle != "" {
		b.WriteString(artist.BaseStyle)
	} else {
		b.WriteString("(nothing written yet — this conversation is writing it)")
	}
	if len(artist.Inspirations) > 0 {
		b.WriteString("\n\nThe references already on the artist:\n")
		for _, insp := range artist.Inspirations {
			line := insp.URL
			if insp.Note != "" {
				line += " — " + insp.Note
			}
			b.WriteString("- " + line + "\n")
		}
	}
	return b.String()
}

// ExtractStyleEdit reads the conversation and returns the revised base style.
//
// The current style is handed over alongside the transcript, because the
// conversation names the changes and the style is what they apply to — an
// extraction given only the discussion would have to reinvent every line
// nobody mentioned.
func (a *App) ExtractStyleEdit(artistID string, messages []InterviewMessage) (StyleEditDraft, error) {
	artist, err := a.GetArtist(artistID)
	if err != nil {
		return StyleEditDraft{}, err
	}

	var draft StyleEditDraft
	err = a.extract(messages, `Below is a conversation about refining an artist's
base technical style, followed by the style as it currently stands. Apply the
changes that were agreed in the conversation — including traits taken from any
music analyses the conversation carries.

Return ONLY a JSON object, no prose and no code fence, of the form
{"baseStyle": …}: the complete revised style, dense and comma-led, six to
twelve lines covering instrumentation, production, tempo range, vocal
treatment, mix, and what to avoid. Keep everything the conversation did not
ask to change as it is.

The style as it currently stands:

`+artist.BaseStyle, &draft)
	if err != nil {
		return StyleEditDraft{}, err
	}
	// An extraction that came back empty is a failed one, not a revision: no
	// conversation about refining a style ends with "delete it all".
	if strings.TrimSpace(draft.BaseStyle) == "" {
		return StyleEditDraft{}, fmt.Errorf(
			"the revision came back empty — try asking for the change again")
	}
	return draft, nil
}
