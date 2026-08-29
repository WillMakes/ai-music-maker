package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// The interview
//
// Every record in this app is made by talking to somebody about it. An artist,
// an album and a track are three different conversations run by one engine: the
// same turn loop, the same stored transcript, a different brief and a different
// extraction at the end.
//
// **Prose while it talks, structure once at the end.** The interview is a chat
// and reads like one; when the producer is ready, one further pass reads the
// whole transcript and returns the fields. Asking a model to converse and emit
// JSON in the same breath is how you get a conversation with a JSON block in the
// middle of it.
//
// **No tools and no MCP.** The interviewer has nothing to look up: everything it
// needs is either what the producer tells it or what the app puts in the brief.
// A turn that could go and read the database is a turn that might, mid-sentence,
// while somebody waits.
//
// **The transcript is stored as it happens.** Describing a record is twenty
// minutes of thinking, and twenty minutes is long enough to be interrupted.
// Closing the window used to lose the thread; now the conversation is written
// down by the two calls that are handed it anyway, and cleared only when
// somebody presses the thing that says so.
// ---------------------------------------------------------------------------

// The three conversations. The kind is half the key a transcript is stored
// under, so resuming an album's thread into a track's cannot happen — that would
// be handing somebody half a discussion they never had.
const (
	subjectArtist = "artist"
	subjectAlbum  = "album"
	subjectTrack  = "track"
)

// interviewTurnMax bounds the transcript sent. A long interview is a good one;
// past this the earliest turns are dropped rather than the model being handed a
// document that will not fit. The tail is what matters — later answers correct
// earlier ones — and the extraction reads what it is given, so the fields follow
// what was most recently said.
const interviewTurnMax = 40

// InterviewMessage is one side of one turn.
type InterviewMessage struct {
	// Role is "user" or "assistant".
	Role string `json:"role"`
	Text string `json:"text"`
	// Note is what the app has to add under a reply — a turn that failed, and
	// why. Its own field rather than a paragraph in Text, because the text goes
	// back to the model as its own words on every later turn, and this is the
	// app's.
	Note string `json:"note"`
	// Attachments are files sent with this turn (#24), as paths under this
	// app's own directory — see interviewfiles.go. The model never gets the
	// bytes; toChatMsgs names them to it instead.
	Attachments []string `json:"attachments"`
	// Research is what the app looked up for this turn (#29): the Antigravity
	// breakdown of a song link the producer shared. Its own field for the same
	// reason Note is — the text is the producer's own words, and the app's
	// findings must not be written into them.
	Research string `json:"research"`
}

// Chat is a conversation as it stands.
type Chat struct {
	SubjectKind string             `json:"subjectKind"`
	SubjectID   string             `json:"subjectId"`
	Messages    []InterviewMessage `json:"messages"`
}

// GetChat reads a conversation back to resume it.
//
// subjectID is "" for the conversation that creates a record — there is nothing
// to key it to yet — which is why the two are separate columns rather than one
// composite id: "the artist interview with no artist" is a row, and it is the
// row that gets thrown away when the artist is created.
func (a *App) GetChat(kind, subjectID string) (Chat, error) {
	if a.store == nil {
		return Chat{SubjectKind: kind, SubjectID: subjectID, Messages: []InterviewMessage{}}, nil
	}
	return a.store.chat(kind, subjectID)
}

// ClearChat throws a conversation away, having been asked to.
//
// The record stays. They are different things: the transcript is how the record
// was arrived at and the record is the record, and somebody starting the
// questions again has not said they want what they already have deleted.
func (a *App) ClearChat(kind, subjectID string) error {
	if a.store == nil {
		return errNoStore
	}
	if err := a.store.clearChat(kind, subjectID); err != nil {
		return err
	}
	// The conversation's files go with it (#24): anything worth keeping was
	// imported into the music library when the tracks were made.
	clearInterviewFiles(kind, subjectID)
	a.store.changed(scopeChats)
	return nil
}

// AskInterview answers one turn and stores the thread.
//
// The whole transcript is sent each time rather than a session being kept here.
// The conversation is short, it belongs to a dialog that may be closed and
// forgotten, and a stateless turn means resuming is reading a row rather than
// reviving a session that has since expired.
func (a *App) AskInterview(kind, subjectID string, messages []InterviewMessage) ([]InterviewMessage, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("there is nothing to answer")
	}
	brief, err := a.interviewBrief(kind, subjectID)
	if err != nil {
		return nil, err
	}

	// A song link in the new turn is researched before the model answers it
	// (#29): the conversation runs on whatever provider is default, and the
	// one thing that provider cannot do is listen — Antigravity's breakdown
	// rides the turn so the interview can talk about how the song actually
	// sounds. Only the newest turn, and only once: the whole transcript comes
	// back every send, and research already on a turn stays what it was.
	if last := &messages[len(messages)-1]; last.Role == "user" && last.Research == "" {
		last.Research = a.researchForTurn(last.Text)
	}

	answer, askErr := a.askAI(a.interviewContext(), brief, toChatMsgs(trimTranscript(messages)))
	if askErr != nil {
		// The failure is appended to the thread rather than returned bare, so the
		// dialog shows what went wrong in the place the answer would have been
		// and the producer can try the same message again without retyping it.
		messages = append(messages, InterviewMessage{
			Role: "assistant",
			Note: askErr.Error(),
		})
		a.keepChat(kind, subjectID, messages)
		return messages, nil
	}

	messages = append(messages, InterviewMessage{Role: "assistant", Text: answer})
	a.keepChat(kind, subjectID, messages)
	return messages, nil
}

// interviewContext is the context an interview turn runs under.
//
// The app's own, so closing the window stops a turn that is still waiting on a
// model. Falls back to Background for the MCP server and the CLI, which have no
// window and no cancellation to inherit.
func (a *App) interviewContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

// interviewBrief assembles the standing instruction for one conversation: the
// skill that says how to run it, plus what the app already knows about what is
// being talked about.
//
// The facts go *after* the skill and are labelled as facts. A model given
// "the artist is called X" inside the instructions treats it as something to do;
// given it under a heading that says what is already decided, it treats it as
// something not to ask about.
func (a *App) interviewBrief(kind, subjectID string) (string, error) {
	var b strings.Builder

	switch kind {
	case subjectArtist:
		b.WriteString(a.skillBody(skillArtistInterview))
	case subjectAlbum:
		b.WriteString(a.skillBody(skillAlbumInterview))
	case subjectTrack:
		b.WriteString(a.skillBody(skillTrackGeneration))
	default:
		return "", fmt.Errorf("unknown interview %q", kind)
	}

	facts, err := a.interviewFacts(kind, subjectID)
	if err != nil {
		return "", err
	}
	if facts != "" {
		b.WriteString("\n\n## What is already decided\n\n")
		b.WriteString(facts)
	}
	return b.String(), nil
}

// interviewFacts is what the app can tell the interviewer without asking.
//
// An album interview is handed the artist; a track interview is handed the
// artist, the album, the combined style and the track list. Being handed the
// track list is what stops it proposing a name that is already on the record —
// the one mistake that is obvious to the producer and invisible to the model.
func (a *App) interviewFacts(kind, subjectID string) (string, error) {
	if a.store == nil || subjectID == "" {
		return "", nil
	}
	var b strings.Builder
	switch kind {
	case subjectAlbum:
		// subjectID is the artist the album is being made for: an album has no id
		// until it is created, so the conversation is keyed to its artist.
		artist, err := a.store.artist(subjectID)
		if err != nil {
			return "", err
		}
		writeArtistFacts(&b, artist)
	case subjectTrack:
		// subjectID is the album the track is being written for, for the same
		// reason.
		album, err := a.store.album(subjectID)
		if err != nil {
			return "", err
		}
		artist, err := a.store.artist(album.ArtistID)
		if err != nil {
			return "", err
		}
		writeArtistFacts(&b, artist)
		fmt.Fprintf(&b, "\n**Album:** %s\n", album.Name)
		if style := combinedStyle(artist, album); style != "" {
			b.WriteString("\n**Combined technical style** (artist's base, then this record's):\n\n")
			b.WriteString(style)
			b.WriteString("\n")
		}
		tracks, err := a.store.listTracks(album.ID)
		if err != nil {
			return "", err
		}
		if len(tracks) == 0 {
			b.WriteString("\n**Tracks so far:** none — this is the first.\n")
		} else {
			b.WriteString("\n**Tracks already on this album** (do not propose these names again):\n\n")
			for _, t := range tracks {
				fmt.Fprintf(&b, "%d. %s\n", t.Position, t.Name)
			}
		}
	}
	return b.String(), nil
}

func writeArtistFacts(b *strings.Builder, artist Artist) {
	fmt.Fprintf(b, "**Artist:** %s\n", artist.Name)
	if artist.Summary != "" {
		fmt.Fprintf(b, "\n%s\n", artist.Summary)
	}
	if artist.BaseStyle != "" {
		b.WriteString("\n**Base technical style:**\n\n")
		b.WriteString(artist.BaseStyle)
		b.WriteString("\n")
	}
	if len(artist.Inspirations) > 0 {
		b.WriteString("\n**References:**\n\n")
		for _, i := range artist.Inspirations {
			label := i.Title
			if label == "" {
				label = i.URL
			}
			if i.Note != "" {
				fmt.Fprintf(b, "- %s — %s\n", label, i.Note)
			} else {
				fmt.Fprintf(b, "- %s\n", label)
			}
		}
	}
}

// trimTranscript drops the earliest turns past the bound — see
// interviewTurnMax.
func trimTranscript(messages []InterviewMessage) []InterviewMessage {
	if len(messages) <= interviewTurnMax {
		return messages
	}
	return messages[len(messages)-interviewTurnMax:]
}

// toChatMsgs converts a transcript for the provider call, dropping the app's own
// notes: a note says a turn failed, and sending it back would have the model
// apologise for an error it did not make.
func toChatMsgs(messages []InterviewMessage) []chatMsg {
	out := make([]chatMsg, 0, len(messages))
	for _, m := range messages {
		text := strings.TrimSpace(m.Text)
		// A turn's files are named to the model (#24): it is a text
		// conversation and the bytes would mean nothing to it, but the names
		// are enough to discuss — and a Suno download's name is the track it
		// belongs to, which is what the extraction leans on.
		if len(m.Attachments) > 0 {
			names := make([]string, 0, len(m.Attachments))
			for _, path := range m.Attachments {
				names = append(names, filepath.Base(path))
			}
			text = strings.TrimSpace(text + "\n\n[Attached files: " + strings.Join(names, ", ") + "]")
		}
		// The app's research rides the turn it was run for (#29), labelled as
		// the app's: unlabelled it would read as the producer's own claim
		// about the song rather than a lookup to lean on.
		if m.Research != "" {
			text = strings.TrimSpace(text +
				"\n\n[Music analysis the app ran for this turn:\n" + m.Research + "]")
		}
		if text == "" {
			continue
		}
		out = append(out, chatMsg{Role: m.Role, Text: text})
	}
	return out
}

// keepChat stores the thread a turn was taken with.
//
// Best effort and silent. A conversation that could not be written down is a
// conversation that cannot be resumed, which is what this exists to prevent —
// losing the answer as well would be making it worse to fix it.
func (a *App) keepChat(kind, subjectID string, messages []InterviewMessage) {
	if a.store == nil || len(messages) == 0 {
		return
	}
	_ = a.store.saveChat(kind, subjectID, messages)
}

// ---------------------------------------------------------------------------
// Extraction
//
// One further pass over the finished transcript, asked for JSON and nothing
// else. Separate from the conversation for the reason at the top of this file,
// and separate from the *storage* too: extraction answers what the interview
// decided, and the caller decides whether to keep it. A producer who presses
// Create and then changes a field before saving is doing the ordinary thing.
// ---------------------------------------------------------------------------

// ArtistDraft is what an artist interview decided.
type ArtistDraft struct {
	Name      string `json:"name"`
	Summary   string `json:"summary"`
	BaseStyle string `json:"baseStyle"`
	// Inspirations are the references mentioned in the conversation, so a
	// producer who pasted five links does not have to paste them again.
	Inspirations []Inspiration `json:"inspirations"`
}

// AlbumDraft is what an album interview decided.
type AlbumDraft struct {
	Name           string `json:"name"`
	TechnicalStyle string `json:"technicalStyle"`
	ArtworkPrompt  string `json:"artworkPrompt"`
}

// TrackDraft is one track a track interview decided. A list, because asking for
// five tracks at once is the normal way an album gets filled and returning them
// one at a time would mean five more model calls to say what one already said.
type TrackDraft struct {
	Name          string `json:"name"`
	Prompt        string `json:"prompt"`
	Lyrics        string `json:"lyrics"`
	TargetSeconds int    `json:"targetSeconds"`
	// Files are the attached audio files that are takes of this track (#24), by
	// base name — assigned by the extraction from the conversation, resolved
	// back to paths and imported by the caller that creates the track.
	Files []string `json:"files"`
}

// ExtractArtist reads an artist out of a finished conversation.
func (a *App) ExtractArtist(messages []InterviewMessage) (ArtistDraft, error) {
	draft := ArtistDraft{Inspirations: []Inspiration{}}
	err := a.extract(messages, `Read the conversation and return the artist it decided on.

Return ONLY a JSON object, no prose and no code fence, with these keys:
  "name"        the artist name they chose
  "summary"     one line, under fifteen words, describing who this artist is
  "baseStyle"   the base technical style: dense, comma-led, concrete, six to
                twelve lines covering instrumentation, production, tempo range,
                vocal treatment, mix character and what to avoid. Not prose and
                not a genre label.
  "inspirations" an array of {"url","title","note"} for every YouTube or
                YouTube Music link mentioned, with what each was chosen for.
                An empty array if none were given.

If the conversation did not settle a name, use your best candidate from it.`, &draft)
	if err != nil {
		return ArtistDraft{Inspirations: []Inspiration{}}, err
	}
	if draft.Inspirations == nil {
		draft.Inspirations = []Inspiration{}
	}
	return draft, nil
}

// ExtractAlbum reads an album out of a finished conversation.
func (a *App) ExtractAlbum(messages []InterviewMessage) (AlbumDraft, error) {
	var draft AlbumDraft
	err := a.extract(messages, `Read the conversation and return the album it decided on.

Return ONLY a JSON object, no prose and no code fence, with these keys:
  "name"            the album name they chose
  "technicalStyle"  this record's own technical breakdown — ONLY what differs
                    from or adds to the artist's base style, three to eight
                    lines in the same dense comma-led shape. If nothing differs,
                    one line saying so.
  "artworkPrompt"   what they said the cover should look like, written as an
                    image brief. Empty string if they did not describe one.`, &draft)
	return draft, err
}

// ExtractTracks reads the tracks out of a finished conversation.
func (a *App) ExtractTracks(messages []InterviewMessage) ([]TrackDraft, error) {
	var wrapper struct {
		Tracks []TrackDraft `json:"tracks"`
	}
	instruction := `Read the conversation and return every track it settled on.

Return ONLY a JSON object, no prose and no code fence, of the form
{"tracks":[…]}, where each entry has:
  "name"           the track title
  "prompt"         the Suno style prompt: comma-led, concrete, under about 200
                   words, most important terms first, ending with what to avoid
  "lyrics"         the full lyrics with [Section] tags. For an instrumental,
                   the tags alone.
  "targetSeconds"  the target length in seconds`

	// Attached audio is assigned here rather than matched by code (#24): the
	// conversation may have said outright which file is which track, and a
	// filename heuristic would overrule the producer's own words.
	if names := attachedAudioNames(messages); len(names) > 0 {
		instruction += `
  "files"          which of the attached audio files are takes of this track,
                   by exact filename — the conversation marks them with
                   [Attached files: …]. A file is usually named after its
                   track. Assign each audio file to at most one track, and
                   leave out any file that belongs to none.

The attached audio files are: ` + strings.Join(names, ", ")
	}
	instruction += `

Return every track that was agreed, in the order they were discussed. If only
one was settled, return an array of one.`

	err := a.extract(messages, instruction, &wrapper)
	if err != nil {
		return nil, err
	}
	if wrapper.Tracks == nil {
		wrapper.Tracks = []TrackDraft{}
	}
	for i := range wrapper.Tracks {
		if wrapper.Tracks[i].Files == nil {
			wrapper.Tracks[i].Files = []string{}
		}
	}
	return wrapper.Tracks, nil
}

// extract runs the structured pass and decodes it into out.
//
// The transcript is handed over as one user message rather than replayed as a
// conversation. This pass is not part of the conversation — it is a reader being
// asked about one — and replaying it would invite the model to continue the
// interview instead of summarising it.
func (a *App) extract(messages []InterviewMessage, instruction string, out any) error {
	transcript := renderTranscript(trimTranscript(messages))
	if strings.TrimSpace(transcript) == "" {
		return fmt.Errorf("there is no conversation to read")
	}
	system := "You extract structured data from a conversation. You answer with " +
		"JSON and nothing else: no explanation, no markdown fence, no preamble."
	answer, err := a.askAI(a.interviewContext(), system, []chatMsg{{
		Role: "user",
		Text: instruction + "\n\n---\n\n" + transcript,
	}})
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(unfence(answer)), out)
}

// unfence strips a markdown code fence from an answer that has one.
//
// Told not to use one and asked for JSON, models still sometimes do. Rejecting
// the answer would be correct and useless — the data is right there — so the
// fence is removed and the content decoded.
func unfence(answer string) string {
	answer = strings.TrimSpace(answer)
	if !strings.HasPrefix(answer, "```") {
		return answer
	}
	// Drop the opening fence and its language tag, then everything from the
	// closing fence on. Not a general markdown parse: the only case this handles
	// is a whole answer wrapped in one fence, which is the only case that occurs.
	if newline := strings.IndexByte(answer, '\n'); newline >= 0 {
		answer = answer[newline+1:]
	}
	if close := strings.LastIndex(answer, "```"); close >= 0 {
		answer = answer[:close]
	}
	return strings.TrimSpace(answer)
}

// renderTranscript writes a conversation out for the extraction pass. Labelled
// turns, so the reader can tell which decisions were the producer's.
func renderTranscript(messages []InterviewMessage) string {
	var b strings.Builder
	for _, m := range messages {
		text := strings.TrimSpace(m.Text)
		// The same attachment markers the conversation carried (#24): the
		// extraction reads the transcript, and an assignment it was never shown
		// is an assignment it can only invent.
		if len(m.Attachments) > 0 {
			names := make([]string, 0, len(m.Attachments))
			for _, path := range m.Attachments {
				names = append(names, filepath.Base(path))
			}
			text = strings.TrimSpace(text + "\n[Attached files: " + strings.Join(names, ", ") + "]")
		}
		if m.Research != "" {
			text = strings.TrimSpace(text +
				"\n[Music analysis the app ran for this turn:\n" + m.Research + "]")
		}
		if text == "" {
			continue
		}
		who := "Producer"
		if m.Role == "assistant" {
			who = "Interviewer"
		}
		b.WriteString(who)
		b.WriteString(": ")
		b.WriteString(text)
		b.WriteString("\n\n")
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Storage
// ---------------------------------------------------------------------------

func (s *Store) chat(kind, subjectID string) (Chat, error) {
	chat := Chat{SubjectKind: kind, SubjectID: subjectID, Messages: []InterviewMessage{}}
	if s == nil || s.db == nil {
		return chat, nil
	}
	var messages string
	err := s.db.QueryRow(
		`SELECT messages FROM chats WHERE subject_kind = ? AND subject_id = ?`,
		kind, subjectID).Scan(&messages)
	if err == sql.ErrNoRows {
		// A record nobody has talked about is not an error, it is the normal
		// state of most of them.
		return chat, nil
	}
	if err != nil {
		return chat, err
	}
	if strings.TrimSpace(messages) == "" {
		return chat, nil
	}
	if err := json.Unmarshal([]byte(messages), &chat.Messages); err != nil {
		// A row this app cannot read is a row it wrote wrong, and the honest
		// answer is the empty conversation rather than a broken one.
		return Chat{SubjectKind: kind, SubjectID: subjectID, Messages: []InterviewMessage{}}, nil
	}
	if chat.Messages == nil {
		chat.Messages = []InterviewMessage{}
	}
	return chat, nil
}

// saveChat replaces the row whole. A transcript only ever grows within a
// conversation, and the newest one is the whole of it.
func (s *Store) saveChat(kind, subjectID string, messages []InterviewMessage) error {
	if s == nil || s.db == nil {
		return errNoStore
	}
	encoded, err := json.Marshal(messages)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO chats (subject_kind, subject_id, messages, at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(subject_kind, subject_id) DO UPDATE SET
		   messages = excluded.messages, at = excluded.at`,
		kind, subjectID, string(encoded), nowStamp())
	return err
}

func (s *Store) clearChat(kind, subjectID string) error {
	if s == nil || s.db == nil {
		return errNoStore
	}
	_, err := s.db.Exec(
		`DELETE FROM chats WHERE subject_kind = ? AND subject_id = ?`, kind, subjectID)
	return err
}
