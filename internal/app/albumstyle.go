package app

import (
	"fmt"
	"strings"
)

// GenerateAlbumStyle amalgamates the album's track prompts into one central
// technical style and returns it (#38).
//
// The tracks are where the record's sound actually got written — each prompt
// is one song's style in full — and this reads them back the other way: what
// the model is asked for is the shared core, the traits that hold across the
// record, not a concatenation. It runs on the default chat provider like the
// other writing jobs, and the result lands in the field's draft for the
// producer to edit and save — the AI writes, it does not take the pen.
func (a *App) GenerateAlbumStyle(albumID string) (string, error) {
	if a.store == nil {
		return "", errNoStore
	}
	tracks, err := a.ListTracks(albumID)
	if err != nil {
		return "", err
	}

	var prompts []string
	for _, track := range tracks {
		if prompt := strings.TrimSpace(track.Prompt); prompt != "" {
			prompts = append(prompts, track.Name+":\n"+prompt)
		}
	}
	if len(prompts) == 0 {
		return "", fmt.Errorf(
			"no track on this album has a style prompt yet — there is nothing to amalgamate")
	}

	system := "You distil an album's central technical style from its tracks' " +
		"style prompts. Answer with the traits that hold across the record — " +
		"the shared instrumentation, production character, tempo range, vocal " +
		"treatment, mix and mood — not a concatenation and not any one track's " +
		"particulars. Dense, concrete and comma-led, six to twelve lines. " +
		"Answer with the style text and nothing else — no preamble, no fence."

	answer, err := a.askAI(a.interviewContext(), system, []chatMsg{{
		Role: "user",
		Text: "The tracks' style prompts:\n\n" + strings.Join(prompts, "\n\n") +
			"\n\nReturn the album's central technical style.",
	}})
	if err != nil {
		return "", err
	}
	style := strings.TrimSpace(unfence(answer))
	if style == "" {
		return "", fmt.Errorf("the style came back empty — try again")
	}
	return style, nil
}
