package app

import (
	"encoding/json"
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

// analysisBrief is who agy is asked to be for this job. One structured call,
// not a conversation — there is nobody to converse with, so the interview rule
// does not apply and the metadata can ride along with the breakdown (#51).
const analysisBrief = `You are a music analyst with web access, working for a
producer who writes technical style prompts for AI music generation. Research
the song at the URL you are given — a YouTube or YouTube Music link — and
answer with one JSON object and nothing else, in exactly this shape:

{"title": "the song's title", "artist": "who performs it", "album": "the release it appears on, or \"\"", "artworkUrl": "a direct https URL to its cover art image, or \"\"", "style": "the technical breakdown"}

The style value is the complete breakdown, written in Markdown with a bold
lead-in per aspect: genre and era markers, instrumentation, production
character, estimated tempo and feel, song structure, vocal treatment and
effects, mix character, and what defines the sound. Dense, concrete and
comma-led, ten to sixteen lines, the language a style prompt is written in.
No preamble, no disclaimers, nothing outside the JSON object; if the song
cannot be found, say so in the style value and leave the other fields "".`

// MusicAnalysis is what one research run brings home: the song's identity —
// title, artist, album, cover — alongside the markdown style breakdown (#51).
// The identity is what keeps a list of references legible: a row of bare
// YouTube URLs says nothing a week later.
type MusicAnalysis struct {
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	Album      string `json:"album"`
	ArtworkURL string `json:"artworkUrl"`
	Style      string `json:"style"`
}

// parseAnalysis reads agy's JSON answer. When it is not JSON, or the style
// inside is empty, the whole answer becomes the style: an agentic model
// occasionally narrates instead of complying, and losing the metadata is a
// smaller failure than losing the breakdown it wrapped.
func parseAnalysis(answer string) MusicAnalysis {
	var out MusicAnalysis
	if err := json.Unmarshal([]byte(unfence(answer)), &out); err != nil ||
		strings.TrimSpace(out.Style) == "" {
		return MusicAnalysis{Style: strings.TrimSpace(answer)}
	}
	out.Title = strings.TrimSpace(out.Title)
	out.Artist = strings.TrimSpace(out.Artist)
	out.Album = strings.TrimSpace(out.Album)
	out.ArtworkURL = strings.TrimSpace(out.ArtworkURL)
	out.Style = strings.TrimSpace(out.Style)
	return out
}

// analysisURLCap bounds how many links one turn will research. Each is an
// agentic CLI run measured in minutes; a message with five links is a playlist,
// and a playlist is not a question.
const analysisURLCap = 2

// AnalyzeMusicURL researches one song and returns its technical breakdown.
//
// It requires Antigravity connected in account mode: the run bills whatever
// the CLI is signed in as, and this app never starts billing a producer on a
// connection they did not deliberately make.
func (a *App) AnalyzeMusicURL(url string) (MusicAnalysis, error) {
	url = strings.TrimSpace(url)
	if len(musicURLs(url)) == 0 {
		return MusicAnalysis{}, fmt.Errorf("that is not a YouTube or YouTube Music link")
	}
	if a.aiConnections()[serviceAntigravity].Mode != modeAccount {
		return MusicAnalysis{}, fmt.Errorf(
			"music analysis runs on Antigravity's signed-in CLI — connect it in Settings → AI")
	}
	answer, err := chatViaCLI(a.interviewContext(), serviceAntigravity,
		a.aiServiceModel(serviceAntigravity), analysisBrief,
		[]chatMsg{{Role: "user", Text: "Analyze this song: " + url}})
	if err != nil {
		return MusicAnalysis{}, err
	}
	return parseAnalysis(answer), nil
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
		analysis, err := a.AnalyzeMusicURL(url)
		breakdown := analysis.Style
		if err != nil {
			breakdown = "The analysis could not run: " + err.Error()
		}
		// The song's name over its URL when research found one — the model
		// reading this transcript writes prose, and prose quotes names.
		label := url
		if analysis.Title != "" {
			label = analysis.Title
			if analysis.Artist != "" {
				label += " — " + analysis.Artist
			}
			label += " (" + url + ")"
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(label + "\n" + breakdown)
	}
	return b.String()
}
