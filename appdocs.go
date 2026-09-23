package main

import (
	"fmt"
	"os"
	"strings"
)

// ---------------------------------------------------------------------------
// The app's documentation of itself
//
// One document, served three ways: `suno-maker docs` for a terminal, the
// get_documentation MCP tool for an agent, and the About panel for a person. All
// three read this same text, so there is no version of "what this app is" that
// can drift from the others.
//
// It is generated rather than written down as a file, because half of it is
// facts the app already holds — how many skills there are, where the music goes,
// which repository reports are filed against. A hand-written README saying those
// things would be right on the day it was written.
// ---------------------------------------------------------------------------

// runDocsCLI handles a `docs` invocation, reporting handled=false for anything
// else so main falls through and opens the window.
func runDocsCLI(args []string) (code int, handled bool) {
	if len(args) == 0 || args[0] != "docs" {
		return 0, false
	}
	store, err := openStore()
	if err == nil {
		defer store.Close()
	}
	app := &App{store: store}
	fmt.Fprint(os.Stdout, app.Documentation())
	return 0, true
}

// Documentation is the whole document.
func (a *App) Documentation() string {
	var b strings.Builder

	b.WriteString(`# SunoMaker

A desktop app for planning music before Suno makes it, and for keeping what Suno
gives back.

## The model

    Artist   a name, a base technical style, and the YouTube references that
             style was decided from. The sound that is recognisably theirs
             across every record.
      Album  a name, a cover, and a technical style of its own — only what
             differs from the artist's. Plus the three release links, once the
             record is out.
        Track a title, a Suno style prompt, lyrics, a target length, and the
             files Suno gave back. One of those files is marked as the take
             that is the track.

The artist's base style and the album's own are joined when a track prompt is
written, in that order, rather than copied together when the album is saved. So
editing the artist's base style corrects every album under them instead of only
the ones made afterwards.

## The workflow

1. **Create an artist.** An AI-led interview asks what the music is, settles on
   a name, and gathers YouTube or YouTube Music references with a note against
   each saying what it is there for. The interview writes the base technical
   style from the answers.

2. **Create an album** for that artist. Another interview: what is different
   about this record, what to call it, and what the cover should look like. The
   name is the only thing required. The cover can be generated from that
   description or dropped in.

3. **Fill the album with tracks.** A third interview, briefed with the combined
   style and the tracks already on the record, writes a title, a Suno style
   prompt, lyrics and a target length — several at once where you ask for
   several.

4. **Make them in Suno.** The browser extension puts a button on Suno's Lyrics,
   Style and Title fields. Press one, pick a track, and the field is filled.

5. **Bring the files back.** Press the extension's toolbar button on the Suno
   tab to record, and a song you listen to all the way through is recorded
   onto the track with the same title. Or import the finished audio against
   its track. Files are copied into the operating
   system's music folder under SunoMaker/Artist/Album, so a music player finds
   them without being told to.

6. **Release it.** Once the record is out, its Apple Music, Spotify and YouTube
   Music links go on the album.

## Where things live

`)
	if dir, err := dataDir(); err == nil {
		fmt.Fprintf(&b, "    %s\n        the database, album covers, and the exported extension\n", dir)
	}
	if root, err := a.musicRoot(); err == nil {
		fmt.Fprintf(&b, "    %s\n        the audio, as Artist/Album/01 Track.mp3\n", root)
	}

	b.WriteString(`
## Settings

    AI          Claude, ChatGPT, Gemini, Antigravity and Grok, each connected
                either by its signed-in CLI (billed to the subscription) or by
                an API key. One is starred as the default.
    AI assistants
                registers this app as an MCP server with Claude Code, Claude
                Desktop, Codex, Gemini CLI, Antigravity and Grok, so an
                assistant can do everything the window can — see below.
    Skills      the app's own briefs — how it interviews, how it writes a Suno
                prompt, what a cover should look like. Built-ins can be edited
                and reset; you can write your own.
    Extension   exports the Chrome extension with this app's bridge token in
                it, says where to point "Load unpacked", and turns off recording
                the songs you listen to in Suno.
    Bug reports the open queue and what has been fixed.
    About       what is running, and where it came from.

`)

	if skills, err := a.ListSkills(); err == nil && len(skills) > 0 {
		b.WriteString("## Skills\n\n")
		for _, s := range skills {
			mark := ""
			if s.Overridden {
				mark = " (edited)"
			}
			fmt.Fprintf(&b, "    %-20s %s%s\n", s.ID, s.Description, mark)
		}
		b.WriteString("\n")
	}

	// The tool list is generated from the tools themselves, so this document
	// can never describe a server the binary does not have (#87). One line
	// each: the name and the first sentence, enough to find the right tool
	// before tools/list gives the whole schema.
	b.WriteString("## The MCP server\n\n")
	b.WriteString(`Run as ` + "`suno-maker mcp`" + `, speaking JSON-RPC on stdio, for the AI
clients Settings → AI assistants registers it with. It reads the database
directly, so it works with the window closed. Every operation the window has
is a tool, except the four that need a window (file dialogs, raising the
window, opening the browser) and dismissing the fixed-bugs notice, which is
the producer's to dismiss. describe_app is the map of all of it — every page,
what it renders, what it calls, and which tool reaches the same thing — read
off the source when the frontend is built, for writing documentation from.

`)
	for _, tool := range mcpTools() {
		fmt.Fprintf(&b, "    %-28s %s\n", tool.name, firstSentence(tool.description))
	}
	b.WriteString("\n")

	b.WriteString(`## The bug queue

Reports are filed from the bug button in the top bar. A report is a GitHub issue
on this repository, and it gets a counterpart in Jax, which is where the queue is
watched from. Work one with:

    suno-maker bugs list         open reports, oldest first
    suno-maker bugs claim <id>   claim it before starting
    suno-maker bugs resolve <id> --sha <sha> --url <url>
    suno-maker bugs workflow     the whole process

`)
	repo := currentBugRepo()
	if repo.Slug != "" {
		fmt.Fprintf(&b, "Reports are filed against %s.\n", repo.Slug)
	} else {
		b.WriteString("This build is not running from a GitHub checkout, so reports stay local.\n")
	}

	return b.String()
}

// firstSentence is a description cut at its first full stop, for a listing
// that wants one line per item. A description with no full stop is short
// enough to be its own line.
func firstSentence(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if i := strings.Index(s, ". "); i >= 0 {
		return s[:i+1]
	}
	return s
}
