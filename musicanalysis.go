package main

import (
	"fmt"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Music analysis as a service (#29)
//
// A producer describing a sound usually has the sound open in another tab.
// This turns the link into the description: given a YouTube or YouTube Music
// URL, Antigravity's agentic CLI researches the song and answers with the
// technical breakdown a base style is written in — instrumentation,
// production, tempo, structure, vocal treatment, mix.
//
// Antigravity specifically, because the job is research: the song has to be
// looked up, not remembered, and agy is the connected provider that browses.
// The plain chat providers stay what they are — the interviews keep running on
// Claude or ChatGPT, and reach for this service only at the moment a link
// needs analyzing (see AskInterview). One service, two doors: the interviews
// call it when a link appears in a turn, and the artist page's inspirations
// call it directly through the binding.
// ---------------------------------------------------------------------------

// musicURLPattern is what counts as a link to a song: YouTube, YouTube Music,
// and the short youtu.be form. Deliberately narrow — the report asked for
// YouTube and YouTube Music, and a pattern that also matched every other URL
// would send Antigravity researching a producer's typo.
var musicURLPattern = regexp.MustCompile(
	`https?://(?:www\.)?(?:music\.youtube\.com|youtube\.com|youtu\.be)/\S+`)

// musicURLs is every song link in a piece of text, in order, deduplicated.
func musicURLs(text string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, url := range musicURLPattern.FindAllString(text, -1) {
		// Trailing punctuation belongs to the sentence, not the URL.
		url = strings.TrimRight(url, ".,;:!?)")
		if !seen[url] {
			seen[url] = true
			out = append(out, url)
		}
	}
	return out
}

// analysisBrief is who agy is asked to be for this job.
const analysisBrief = `You are a music analyst with web access, working for a
producer who writes technical style prompts for AI music generation. Research
the song at the URL you are given — a YouTube or YouTube Music link — and
answer with its technical breakdown and nothing else: genre and era markers,
instrumentation, production character, estimated tempo and feel, song
structure, vocal treatment and effects, mix character, and what defines the
sound. Dense, concrete and comma-led, ten to sixteen lines, the language a
style prompt is written in. No preamble, no disclaimers; if the song cannot be
found, say so in one line.`

// analysisURLCap bounds how many links one turn will research. Each is an
// agentic CLI run measured in minutes; a message with five links is a playlist,
// and a playlist is not a question.
const analysisURLCap = 2

// AnalyzeMusicURL researches one song and returns its technical breakdown.
//
// It requires Antigravity connected in account mode: the run bills whatever
// the CLI is signed in as, and this app never starts billing a producer on a
// connection they did not deliberately make.
func (a *App) AnalyzeMusicURL(url string) (string, error) {
	url = strings.TrimSpace(url)
	if len(musicURLs(url)) == 0 {
		return "", fmt.Errorf("that is not a YouTube or YouTube Music link")
	}
	if a.aiConnections()[serviceAntigravity].Mode != modeAccount {
		return "", fmt.Errorf(
			"music analysis runs on Antigravity's signed-in CLI — connect it in Settings → AI")
	}
	answer, err := chatViaCLI(a.interviewContext(), serviceAntigravity,
		a.aiServiceModel(serviceAntigravity), analysisBrief,
		[]chatMsg{{Role: "user", Text: "Analyze this song: " + url}})
	if err != nil {
		return "", err
	}
	return answer, nil
}

// MergeStyleWithAnalysis folds one song's breakdown into a base technical
// style and returns the merged style (#31).
//
// This is a writing job, not a research one, so it runs on the default chat
// provider the way the interviews do — Antigravity already did the listening.
// The style passed in is the producer's current *draft*, unsaved edits and
// all, and the merge comes back to the draft: adding a reference updates the
// style on screen, and the producer presses Save when it reads right.
func (a *App) MergeStyleWithAnalysis(baseStyle, analysis string) (string, error) {
	analysis = strings.TrimSpace(analysis)
	if analysis == "" {
		return "", fmt.Errorf("there is no analysis to merge")
	}
	system := "You maintain an artist's base technical style — the dense, " +
		"comma-led breakdown every album under them inherits, six to twelve " +
		"lines covering instrumentation, production, tempo range, vocal " +
		"treatment, mix, and what to avoid. Fold a new reference's traits into " +
		"the existing style: keep what is there unless the new reference " +
		"genuinely shifts it, integrate what is new, never just append. Answer " +
		"with the merged style text and nothing else — no preamble, no fence, " +
		"no commentary."
	prompt := "The new reference's technical breakdown:\n\n" + analysis +
		"\n\nReturn the merged base style."
	if strings.TrimSpace(baseStyle) != "" {
		prompt = "The base technical style as it stands:\n\n" + baseStyle +
			"\n\n" + prompt
	} else {
		prompt = "There is no base style yet — write one from the breakdown " +
			"below, in the shape described.\n\n" + prompt
	}
	answer, err := a.askAI(a.interviewContext(), system,
		[]chatMsg{{Role: "user", Text: prompt}})
	if err != nil {
		return "", err
	}
	merged := strings.TrimSpace(unfence(answer))
	if merged == "" {
		return "", fmt.Errorf("the merge came back empty — try again")
	}
	return merged, nil
}

// researchForTurn runs the analysis for every song link in one interview turn
// and returns what the app found, "" when there is nothing to do.
//
// Nothing here fails the turn: a producer mid-conversation who pasted a link
// should get their conversation either way, with the analysis when it worked
// and a one-line note when it did not — the note is how they learn to connect
// Antigravity rather than wondering why links do nothing.
func (a *App) researchForTurn(text string) string {
	urls := musicURLs(text)
	if len(urls) == 0 {
		return ""
	}
	if len(urls) > analysisURLCap {
		urls = urls[:analysisURLCap]
	}
	var b strings.Builder
	for _, url := range urls {
		breakdown, err := a.AnalyzeMusicURL(url)
		if err != nil {
			breakdown = "The analysis could not run: " + err.Error()
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(url + "\n" + breakdown)
	}
	return b.String()
}
