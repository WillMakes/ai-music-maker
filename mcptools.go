package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// What an agent can do to this app
//
// The tool surface behind the MCP server. It is deliberately the same set of
// operations the window has, reaching the same bindings: an agent that can read
// the library, write a prompt and file a bug is an agent that can do the work,
// and a tool that did something no button does would be a second implementation
// to keep in step.
//
// Every tool answers JSON (see mcpResult). Every failure answers as a tool error
// rather than a transport error (see mcpFailure), because "there is no album
// called that" is something the model should read and act on.
// ---------------------------------------------------------------------------

// mcpTool is one tool's definition and its implementation together.
//
// Together on purpose: a schema in one list and a switch in another is two
// places to forget, and the failure mode is a tool a client can see and cannot
// call.
type mcpTool struct {
	name        string
	description string
	// schema is the JSON Schema for the arguments, as the protocol wants it.
	schema map[string]any
	run    func(app *App, args json.RawMessage) map[string]any
}

// object builds an argument schema. A helper because every one of these is the
// same three keys and writing them out twenty times buries the differences.
func object(properties map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{
		"type": "object", "properties": properties, "required": required,
	}
}

func text(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func number(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}

func mcpTools() []mcpTool {
	return []mcpTool{
		{
			name: "list_artists",
			description: "Every artist in SunoMaker, newest first, with how many albums each has. " +
				"Start here: an album or a track can only be reached through its artist.",
			schema: object(map[string]any{}),
			run: func(app *App, _ json.RawMessage) map[string]any {
				return answer(app.ListArtists())
			},
		},
		{
			name: "get_artist",
			description: "One artist with its base technical style and the YouTube references " +
				"the style was decided from.",
			schema: object(map[string]any{"id": text("the artist id")}, "id"),
			run: func(app *App, args json.RawMessage) map[string]any {
				var in struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return mcpFailure(err)
				}
				return answer(app.GetArtist(in.ID))
			},
		},
		{
			name: "save_artist",
			description: "Create or update an artist. Omit id to create. baseStyle is the " +
				"technical breakdown every album under this artist inherits — dense and " +
				"comma-led, not a genre label. Returns the stored record.",
			schema: object(map[string]any{
				"id":        text("the artist id; omit to create"),
				"name":      text("the artist name"),
				"summary":   text("one line describing who this artist is"),
				"baseStyle": text("the base technical style"),
				"inspirations": map[string]any{
					"type":        "array",
					"description": "YouTube or YouTube Music references, each with what it was chosen for",
					"items": object(map[string]any{
						"url":   text("the video or track URL"),
						"title": text("what it is called"),
						"note":  text("what this reference is here for"),
					}, "url"),
				},
			}, "name"),
			run: func(app *App, args json.RawMessage) map[string]any {
				var artist Artist
				if err := json.Unmarshal(args, &artist); err != nil {
					return mcpFailure(err)
				}
				return answer(app.SaveArtist(artist))
			},
		},
		{
			name:        "list_albums",
			description: "One artist's albums, oldest first — a discography reads forwards.",
			schema:      object(map[string]any{"artistId": text("the artist id")}, "artistId"),
			run: func(app *App, args json.RawMessage) map[string]any {
				var in struct {
					ArtistID string `json:"artistId"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return mcpFailure(err)
				}
				return answer(app.ListAlbums(in.ArtistID))
			},
		},
		{
			name:        "get_album",
			description: "One album, with its own technical style and its release links.",
			schema:      object(map[string]any{"id": text("the album id")}, "id"),
			run: func(app *App, args json.RawMessage) map[string]any {
				var in struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return mcpFailure(err)
				}
				return answer(app.GetAlbum(in.ID))
			},
		},
		{
			name: "save_album",
			description: "Create or update an album. Omit id to create; a name and an artistId " +
				"are the only requirements. technicalStyle is ONLY what differs from the " +
				"artist's base style — the two are combined when a track prompt is written, " +
				"so repeating the artist's here dilutes it.",
			schema: object(map[string]any{
				"id":             text("the album id; omit to create"),
				"artistId":       text("the artist this album belongs to"),
				"name":           text("the album name"),
				"technicalStyle": text("what differs from or adds to the artist's base style"),
				"artworkPrompt":  text("what the cover should look like"),
				"appleMusicUrl":  text("the Apple Music link, once the record is out"),
				"spotifyUrl":     text("the Spotify link"),
				"youtubeUrl":     text("the YouTube Music link"),
			}, "artistId", "name"),
			run: func(app *App, args json.RawMessage) map[string]any {
				var album Album
				if err := json.Unmarshal(args, &album); err != nil {
					return mcpFailure(err)
				}
				return answer(app.SaveAlbum(album))
			},
		},
		{
			name: "get_album_style",
			description: "The combined technical style a track on this album should be written " +
				"against: the artist's base style, then the album's own. This is what to " +
				"write a Suno prompt from.",
			schema: object(map[string]any{"albumId": text("the album id")}, "albumId"),
			run: func(app *App, args json.RawMessage) map[string]any {
				var in struct {
					AlbumID string `json:"albumId"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return mcpFailure(err)
				}
				album, err := app.GetAlbum(in.AlbumID)
				if err != nil {
					return mcpFailure(err)
				}
				artist, err := app.GetArtist(album.ArtistID)
				if err != nil {
					return mcpFailure(err)
				}
				return mcpResult(map[string]any{
					"artist":        artist.Name,
					"album":         album.Name,
					"baseStyle":     artist.BaseStyle,
					"albumStyle":    album.TechnicalStyle,
					"combinedStyle": combinedStyle(artist, album),
					"inspirations":  artist.Inspirations,
				})
			},
		},
		{
			name:        "list_tracks",
			description: "One album's tracks in running order, with their prompts, lyrics and files.",
			schema:      object(map[string]any{"albumId": text("the album id")}, "albumId"),
			run: func(app *App, args json.RawMessage) map[string]any {
				var in struct {
					AlbumID string `json:"albumId"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return mcpFailure(err)
				}
				return answer(app.ListTracks(in.AlbumID))
			},
		},
		{
			name:        "get_track",
			description: "One track: its Suno style prompt, its lyrics and the files that came back.",
			schema:      object(map[string]any{"id": text("the track id")}, "id"),
			run: func(app *App, args json.RawMessage) map[string]any {
				var in struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return mcpFailure(err)
				}
				return answer(app.GetTrack(in.ID))
			},
		},
		{
			name: "save_track",
			description: "Create or update a track. Omit id to create; a new track with no " +
				"position goes on the end. prompt is what gets pasted into Suno's Style " +
				"field and lyrics into its Lyrics field, so write both to Suno's shape — " +
				"see the track-generation skill.",
			schema: object(map[string]any{
				"id":            text("the track id; omit to create"),
				"albumId":       text("the album this track belongs to"),
				"name":          text("the track title"),
				"position":      number("the running order, 1-based; omit to append"),
				"targetSeconds": number("the target length in seconds"),
				"prompt":        text("the Suno style prompt"),
				"lyrics":        text("the lyrics, with [Section] tags"),
				"notes":         text("anything else worth recording about this track"),
			}, "albumId", "name"),
			run: func(app *App, args json.RawMessage) map[string]any {
				var track Track
				if err := json.Unmarshal(args, &track); err != nil {
					return mcpFailure(err)
				}
				return answer(app.SaveTrack(track))
			},
		},
		{
			name:        "list_skills",
			description: "The app's own briefs — how it interviews, how it writes a Suno prompt, what a cover should look like.",
			schema:      object(map[string]any{}),
			run: func(app *App, _ json.RawMessage) map[string]any {
				return answer(app.ListSkills())
			},
		},
		{
			name: "get_skill",
			description: "One skill's full text. Read track-generation before writing a prompt, " +
				"and bug-reports before working the queue.",
			schema: object(map[string]any{"id": text("the skill id")}, "id"),
			run: func(app *App, args json.RawMessage) map[string]any {
				var in struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return mcpFailure(err)
				}
				return answer(app.GetSkill(in.ID))
			},
		},
		{
			name: "save_skill",
			description: "Write a skill, or override a built-in by passing its id. A built-in's " +
				"title and description stay the build's — only the body is yours.",
			schema: object(map[string]any{
				"id":          text("the skill id; a built-in's id overrides it"),
				"title":       text("what to call it"),
				"description": text("one line on what it is for"),
				"body":        text("the skill itself, in markdown"),
			}, "body"),
			run: func(app *App, args json.RawMessage) map[string]any {
				var skill Skill
				if err := json.Unmarshal(args, &skill); err != nil {
					return mcpFailure(err)
				}
				return answer(app.SaveSkill(skill))
			},
		},
		{
			name: "list_bug_reports",
			description: "The open bug queue, oldest first — the working order, and the order " +
				"that decides which report may be claimed.",
			schema: object(map[string]any{}),
			run: func(app *App, _ json.RawMessage) map[string]any {
				return answer(app.ListBugReports())
			},
		},
		{
			name:        "file_bug_report",
			description: "File a bug. Opens an issue on this repository and files a counterpart in Jax.",
			schema: object(map[string]any{
				"description": text("what is wrong"),
				"route":       text("the page it was found on"),
				"global":      map[string]any{"type": "boolean", "description": "about the whole app rather than one page"},
			}, "description"),
			run: func(app *App, args json.RawMessage) map[string]any {
				var report BugReport
				if err := json.Unmarshal(args, &report); err != nil {
					return mcpFailure(err)
				}
				return answer(app.FileBugReport(report))
			},
		},
		{
			name:        "claim_bug_report",
			description: "Claim a report before working it, so two agents do not take the same one.",
			schema:      object(map[string]any{"id": text("the report id")}, "id"),
			run: func(app *App, args json.RawMessage) map[string]any {
				var in struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return mcpFailure(err)
				}
				return answer(app.ClaimBugReport(in.ID))
			},
		},
		{
			name: "resolve_bug_report",
			description: "Resolve a report once its fix is on main: closes the issue with a " +
				"comment pointing at the commit, and clears it from Jax.",
			schema: object(map[string]any{
				"id":        text("the report id"),
				"commitSha": text("the commit the fix landed in"),
				"commitUrl": text("where that commit can be read"),
			}, "id"),
			run: func(app *App, args json.RawMessage) map[string]any {
				var in struct {
					ID        string `json:"id"`
					CommitSHA string `json:"commitSha"`
					CommitURL string `json:"commitUrl"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return mcpFailure(err)
				}
				return answer(app.ResolveBugReport(in.ID, in.CommitSHA, in.CommitURL))
			},
		},
		{
			name:        "get_documentation",
			description: "What this application is, what it holds and how its workflow runs. Read this first.",
			schema:      object(map[string]any{}),
			run: func(app *App, _ json.RawMessage) map[string]any {
				return mcpResult(map[string]any{"documentation": app.Documentation()})
			},
		},
	}
}

// answer turns a (value, error) pair into a tool result. Written this way so
// every tool above is one line rather than three, and so the error branch is
// impossible to forget.
func answer[T any](value T, err error) map[string]any {
	if err != nil {
		return mcpFailure(err)
	}
	return mcpResult(value)
}

// mcpToolDefinitions is what tools/list answers.
func mcpToolDefinitions() []map[string]any {
	tools := mcpTools()
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		out = append(out, map[string]any{
			"name":        t.name,
			"description": t.description,
			"inputSchema": t.schema,
		})
	}
	return out
}

// mcpToolCount is how many tools this server offers, for the Settings panel.
func mcpToolCount() int {
	return len(mcpTools())
}

// callMCPTool runs one by name.
func callMCPTool(app *App, name string, args json.RawMessage) map[string]any {
	// A call with no arguments object at all is legal, and every run below
	// unmarshals — so an empty body becomes an empty object rather than a decode
	// failure the model cannot act on.
	if len(strings.TrimSpace(string(args))) == 0 {
		args = json.RawMessage("{}")
	}
	for _, t := range mcpTools() {
		if t.name == name {
			return t.run(app, args)
		}
	}
	return mcpFailure(fmt.Errorf("no tool called %q", name))
}
