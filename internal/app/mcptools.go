package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// What an agent can do to this app
//
// The tool surface behind the MCP server: every operation the window has,
// reaching the same bindings (#87). An agent that can read the library, run an
// interview, write a prompt, file the takes and work the bug queue is an agent
// that can do the producer's work, and a tool that did something no button does
// would be a second implementation to keep in step.
//
// The surface is split by domain across the mcptools_*.go files, the way the
// bindings themselves are; this file holds the shared plumbing and the library
// proper. Each tool names the binding it reaches, and a test reflects over App
// to check that nothing exported is left unreached — the only bindings it may
// skip are the ones in mcpNotOffered, each with its reason.
//
// Every tool answers JSON (see mcpResult). Every failure answers as a tool error
// rather than a transport error (see mcpFailure), because "there is no album
// called that" is something the model should read and act on.
// ---------------------------------------------------------------------------

// mcpNotOffered is every binding the server deliberately does not expose, with
// the reason — and the test that checks parity holds it to exactly these. The
// reasons are data rather than a comment because describe_app (#88) reports
// them: an agent documenting the app should say why a page's button has no
// tool, not just that it has none.
//
// Six of them need a window. The server runs with no webview, and a dialog
// nobody can see is a call that never returns; an agent with a path to hand
// over has import_track_file and import_album_artwork, which take it directly.
var mcpNotOffered = map[string]string{
	"PickImageFile":    "opens the desktop's file dialog, which needs a window; import_album_artwork takes the path directly",
	"PickAudioFiles":   "opens the desktop's file dialog, which needs a window; import_track_file and import_album_take take the path directly",
	"ShowWindow":       "raises the window, which the server has none of",
	"OpenURL":          "opens a link in the producer's browser, which is theirs to do",
	"MarkFixesSeen":    "dismisses the fixed-bugs notice, which is the producer's own acknowledgement — the window shows a fix once and clears the flag when they have read it",
	"ListMCPTools":     "lists this server's tools, which is what the protocol's own tools/list already answers",
	"ReportNowPlaying": "is the window's player telling the app what it is doing; the server has no player, and now_playing reads the answer",
	"SetMediaKeys":     "holds the keyboard's media keys for the window's player, which the server has none of; the player takes and releases them as its queue fills and empties",
}

// mcpTool is one tool's definition and its implementation together.
//
// Together on purpose: a schema in one list and a switch in another is two
// places to forget, and the failure mode is a tool a client can see and cannot
// call.
type mcpTool struct {
	name        string
	description string
	// binding names the App method(s) this tool reaches, space-separated for
	// the few that compose two. Read by the parity test, and by nothing else:
	// it is documentation that is checked.
	binding string
	// schema is the JSON Schema for the arguments, as the protocol wants it.
	schema map[string]any
	run    func(app *App, args json.RawMessage) map[string]any
	// group is the domain the tool belongs to, stamped by mcpTools from the
	// slice it came in — the Settings panel lists tools under it (#90).
	group string
}

// mcpTools is the whole surface, in the order tools/list offers it: the
// library first, since that is where every task starts, then the files, the
// AI, the bug queue, and the settings.
func mcpTools() []mcpTool {
	var out []mcpTool
	for _, domain := range []struct {
		name  string
		tools []mcpTool
	}{
		{"Library", libraryTools()},
		{"Files and artwork", fileTools()},
		{"AI and interviews", aiTools()},
		{"Bug queue", bugTools()},
		{"Skills, extension and settings", settingsTools()},
	} {
		for _, t := range domain.tools {
			t.group = domain.name
			out = append(out, t)
		}
	}
	return out
}

// MCPToolInfo is one tool as Settings → AI assistants lists it (#90): what a
// connected assistant can do here, in the assistant's own terms.
type MCPToolInfo struct {
	Name        string       `json:"name"`
	Group       string       `json:"group"`
	Description string       `json:"description"`
	Arguments   []MCPToolArg `json:"arguments"`
}

// MCPToolArg is one argument of a tool, read off its schema.
type MCPToolArg struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// ListMCPTools reports every tool the server offers, from the same list the
// server dispatches through — so a tool that is listed can be called, and one
// that can be called is listed. The window's own read of the surface; an
// assistant asks tools/list.
func (a *App) ListMCPTools() []MCPToolInfo {
	out := []MCPToolInfo{}
	for _, t := range mcpTools() {
		info := MCPToolInfo{
			Name: t.name, Group: t.group, Description: t.description, Arguments: []MCPToolArg{},
		}
		required := map[string]bool{}
		if names, ok := t.schema["required"].([]string); ok {
			for _, name := range names {
				required[name] = true
			}
		}
		properties, _ := t.schema["properties"].(map[string]any)
		// Sorted, because a map's order is not one and the panel should read
		// the same way twice.
		names := make([]string, 0, len(properties))
		for name := range properties {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			property, _ := properties[name].(map[string]any)
			description, _ := property["description"].(string)
			info.Arguments = append(info.Arguments, MCPToolArg{
				Name: name, Description: description, Required: required[name],
			})
		}
		out = append(out, info)
	}
	return out
}

// ---------------------------------------------------------------------------
// Schema helpers
//
// Every argument schema is the same few shapes, and writing them out eighty
// times would bury the differences between tools under the JSON that is the
// same for all of them.
// ---------------------------------------------------------------------------

// object builds an argument schema.
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

func boolean(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func array(description string, items map[string]any) map[string]any {
	return map[string]any{"type": "array", "description": description, "items": items}
}

// oneOf is a string argument with a closed set of values. Declared as an enum
// so a client can validate before calling, and so the model sees the choices
// rather than guessing at them from prose.
func oneOf(description string, values ...string) map[string]any {
	return map[string]any{"type": "string", "description": description, "enum": values}
}

// messagesSchema is a conversation as every Ask/Extract tool takes it: the
// whole transcript, oldest first, in the shape the window holds it. Sent whole
// on every turn for interview.go's reason — the turn is stateless, so resuming
// is reading a row rather than reviving a session.
func messagesSchema(description string) map[string]any {
	return array(description, object(map[string]any{
		"role": oneOf("who said it", "user", "assistant"),
		"text": text("what was said"),
		"note": text("the app's own note under a reply — a turn that failed, and why. Leave empty."),
		"attachments": array("paths of files sent with this turn, as attach_interview_file returned them",
			text("a path under this app's own directory")),
		"research": text("the music analysis the app ran for this turn. Leave empty; the app fills it."),
	}, "role", "text"))
}

// ---------------------------------------------------------------------------
// Result helpers
// ---------------------------------------------------------------------------

// with decodes the arguments into T and hands them to fn.
//
// Generic so every tool is its own two lines rather than the same five: the
// unmarshal, the error branch and the call are identical across the surface,
// and the one place they could differ — what T is — is the type parameter. A
// decode failure is a tool error, not a transport one: a wrongly typed argument
// is the model's mistake to read and correct.
func with[T any](fn func(app *App, in T) map[string]any) func(*App, json.RawMessage) map[string]any {
	return func(app *App, args json.RawMessage) map[string]any {
		var in T
		if err := json.Unmarshal(args, &in); err != nil {
			return mcpFailure(fmt.Errorf("bad arguments: %w", err))
		}
		return fn(app, in)
	}
}

// The argument shapes that recur across domains. Named once so a tool that
// takes an album id declares it the same way every other one does.
type (
	idArg struct {
		ID string `json:"id"`
	}
	artistArg struct {
		ArtistID string `json:"artistId"`
	}
	albumArg struct {
		AlbumID string `json:"albumId"`
	}
	trackArg struct {
		TrackID string `json:"trackId"`
	}
	noArgs struct{}
)

// answer turns a (value, error) pair into a tool result. Written this way so
// every tool is one line rather than three, and so the error branch is
// impossible to forget.
func answer[T any](value T, err error) map[string]any {
	if err != nil {
		return mcpFailure(err)
	}
	return mcpResult(value)
}

// done is answer for a binding that returns only an error. Something is still
// answered on success — a model reading an empty result cannot tell it from a
// tool that produced nothing.
func done(err error) map[string]any {
	if err != nil {
		return mcpFailure(err)
	}
	return mcpResult(map[string]any{"ok": true})
}

// mcpImage answers a picture as the protocol's image block, so a client that
// shows images to its model shows this one. Text would be the same base64
// wrapped in a data URL — a string the model can only describe the length of.
func mcpImage(dataURL string) map[string]any {
	mime, encoded, err := splitDataURL(dataURL)
	if err != nil {
		return mcpFailure(err)
	}
	return map[string]any{
		"content": []map[string]any{{"type": "image", "data": encoded, "mimeType": mime}},
	}
}

// ---------------------------------------------------------------------------
// The library: artists, albums, tracks, and the app's own account of itself
// ---------------------------------------------------------------------------

func libraryTools() []mcpTool {
	return []mcpTool{
		{
			name: "get_documentation",
			// "At a glance" and "overview" are in here on purpose: a model
			// asked for either searches tool descriptions for the words, and
			// this is the tool it should find (#89).
			description: "AIMusicMaker at a glance — the overview to read first: what this " +
				"application is, what it holds, how its workflow runs, where things live, " +
				"and which tools this server offers.",
			binding: "Documentation",
			schema:  object(map[string]any{}),
			run: with(func(app *App, _ noArgs) map[string]any {
				return mcpResult(map[string]any{"documentation": app.Documentation()})
			}),
		},
		{
			name: "library_tree",
			description: "The whole library in one call: every artist with their albums, each album's " +
				"track count and cover. The cheapest way to find a record by name.",
			binding: "LibraryTree",
			schema:  object(map[string]any{}),
			run: with(func(app *App, _ noArgs) map[string]any {
				return answer(app.LibraryTree())
			}),
		},
		{
			name: "list_artists",
			description: "Every artist in AIMusicMaker, newest first, with how many albums each has. " +
				"An album or a track can only be reached through its artist.",
			binding: "ListArtists",
			schema:  object(map[string]any{}),
			run: with(func(app *App, _ noArgs) map[string]any {
				return answer(app.ListArtists())
			}),
		},
		{
			name: "get_artist",
			description: "One artist with its base technical style and the YouTube references " +
				"the style was decided from.",
			binding: "GetArtist",
			schema:  object(map[string]any{"id": text("the artist id")}, "id"),
			run: with(func(app *App, in idArg) map[string]any {
				return answer(app.GetArtist(in.ID))
			}),
		},
		{
			name: "save_artist",
			description: "Create or update an artist. Omit id to create. baseStyle is the " +
				"technical breakdown every album under this artist inherits — dense and " +
				"comma-led, not a genre label. The inspirations given replace the ones " +
				"stored. Returns the stored record.",
			binding: "SaveArtist",
			schema: object(map[string]any{
				"id":        text("the artist id; omit to create"),
				"name":      text("the artist name"),
				"summary":   text("one line describing who this artist is"),
				"baseStyle": text("the base technical style"),
				"inspirations": array("YouTube or YouTube Music references, each with what it was chosen for",
					object(map[string]any{
						"url":        text("the video or track URL"),
						"title":      text("what it is called"),
						"artist":     text("who performs it, once analyzed"),
						"album":      text("the release it appears on, once analyzed"),
						"artworkUrl": text("its cover art, once analyzed"),
						"note":       text("what this reference is here for — or its full analyzed style breakdown"),
					}, "url")),
			}, "name"),
			run: with(func(app *App, artist Artist) map[string]any {
				return answer(app.SaveArtist(artist))
			}),
		},
		{
			name: "delete_artist",
			description: "Delete an artist and everything under it — albums, tracks and the rows " +
				"naming their files. The audio stays on disk. There is no undo.",
			binding: "DeleteArtist",
			schema:  object(map[string]any{"id": text("the artist id")}, "id"),
			run: with(func(app *App, in idArg) map[string]any {
				return done(app.DeleteArtist(in.ID))
			}),
		},
		{
			name:        "list_albums",
			description: "One artist's albums, oldest first — a discography reads forwards.",
			binding:     "ListAlbums",
			schema:      object(map[string]any{"artistId": text("the artist id")}, "artistId"),
			run: with(func(app *App, in artistArg) map[string]any {
				return answer(app.ListAlbums(in.ArtistID))
			}),
		},
		{
			name: "get_album",
			description: "One album, with its own technical style, its cover, its release links " +
				"and how far along it is (tracks written, tracks made).",
			binding: "GetAlbum",
			schema:  object(map[string]any{"id": text("the album id")}, "id"),
			run: with(func(app *App, in idArg) map[string]any {
				return answer(app.GetAlbum(in.ID))
			}),
		},
		{
			name: "save_album",
			description: "Create or update an album. Omit id to create; a name and an artistId " +
				"are the only requirements. technicalStyle is ONLY what differs from the " +
				"artist's base style — the two are combined when a track prompt is written, " +
				"so repeating the artist's here dilutes it.",
			binding: "SaveAlbum",
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
			run: with(func(app *App, album Album) map[string]any {
				return answer(app.SaveAlbum(album))
			}),
		},
		{
			name: "delete_album",
			description: "Delete an album, its tracks and the rows naming their files. The audio " +
				"and the cover stay on disk. There is no undo.",
			binding: "DeleteAlbum",
			schema:  object(map[string]any{"id": text("the album id")}, "id"),
			run: with(func(app *App, in idArg) map[string]any {
				return done(app.DeleteAlbum(in.ID))
			}),
		},
		{
			name: "import_album",
			description: "Create an album from a folder of finished audio: the album is named after " +
				"the folder unless a name is given, every audio file becomes a track in " +
				"filename order, and each file is filed into the library. With a name and no " +
				"folder, makes an empty album.",
			binding: "ImportAlbum",
			schema: object(map[string]any{
				"artistId": text("the artist the album belongs to"),
				"name":     text("the album name; omit to use the folder's"),
				"dir":      text("an absolute path to the folder of audio; omit for an empty album"),
			}, "artistId"),
			run: with(func(app *App, in struct {
				ArtistID string `json:"artistId"`
				Name     string `json:"name"`
				Dir      string `json:"dir"`
			}) map[string]any {
				return answer(app.ImportAlbum(in.ArtistID, in.Name, in.Dir))
			}),
		},
		{
			name: "get_album_style",
			description: "The combined technical style a track on this album should be written " +
				"against: the artist's base style, then the album's own. This is what to " +
				"write a Suno prompt from.",
			binding: "GetAlbum GetArtist",
			schema:  object(map[string]any{"albumId": text("the album id")}, "albumId"),
			run: with(func(app *App, in albumArg) map[string]any {
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
			}),
		},
		{
			name:        "list_tracks",
			description: "One album's tracks in running order, with their prompts, lyrics and files.",
			binding:     "ListTracks",
			schema:      object(map[string]any{"albumId": text("the album id")}, "albumId"),
			run: with(func(app *App, in albumArg) map[string]any {
				return answer(app.ListTracks(in.AlbumID))
			}),
		},
		{
			name:        "get_track",
			description: "One track: its Suno style prompt, its lyrics and the files that came back.",
			binding:     "GetTrack",
			schema:      object(map[string]any{"id": text("the track id")}, "id"),
			run: with(func(app *App, in idArg) map[string]any {
				return answer(app.GetTrack(in.ID))
			}),
		},
		{
			name: "save_track",
			description: "Create or update a track. Omit id to create; a new track with no " +
				"position goes on the end. prompt is what gets pasted into Suno's Style " +
				"field and lyrics into its Lyrics field, so write both to Suno's shape — " +
				"see the track-generation skill.",
			binding: "SaveTrack",
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
			run: with(func(app *App, track Track) map[string]any {
				return answer(app.SaveTrack(track))
			}),
		},
		{
			name: "delete_track",
			description: "Delete a track and the rows naming its files. The audio stays on disk. " +
				"There is no undo.",
			binding: "DeleteTrack",
			schema:  object(map[string]any{"id": text("the track id")}, "id"),
			run: with(func(app *App, in idArg) map[string]any {
				return done(app.DeleteTrack(in.ID))
			}),
		},
		{
			name: "reorder_tracks",
			description: "Write an album's running order as a whole: every track id, in the order " +
				"they should play. Returns the tracks in their new order.",
			binding: "ReorderTracks",
			schema: object(map[string]any{
				"albumId":  text("the album id"),
				"trackIds": array("every track on the album, in running order", text("a track id")),
			}, "albumId", "trackIds"),
			run: with(func(app *App, in struct {
				AlbumID  string   `json:"albumId"`
				TrackIDs []string `json:"trackIds"`
			}) map[string]any {
				return answer(app.ReorderTracks(in.AlbumID, in.TrackIDs))
			}),
		},
		{
			name: "playlist",
			description: "The play queue for one track, an album, an artist, a saved " +
				"playlist or the whole library, in playing order, with a loopback URL for each track's chosen take. " +
				"The URLs are served by the running app; a track with no takes is skipped.",
			binding: "Playlist",
			schema: object(map[string]any{
				"kind": oneOf("what to queue", "track", "album", "artist", "playlist", "all"),
				"id":   text("the track, album, artist or playlist id; omit for all"),
			}, "kind"),
			run: with(func(app *App, in struct {
				Kind string `json:"kind"`
				ID   string `json:"id"`
			}) map[string]any {
				return answer(app.Playlist(in.Kind, in.ID))
			}),
		},
		{
			name: "now_playing",
			description: "What the app's player is doing right now (#111): state (playing, paused or " +
				"stopped), the track title, artist and album, the position and length in seconds, and a " +
				"URL for the album's cover. Read from the running app; with the app closed, nothing is " +
				"playing. The same answer is served with no token at http://127.0.0.1:8799/now-playing.",
			binding: "NowPlaying",
			schema:  object(map[string]any{}),
			run: with(func(app *App, _ noArgs) map[string]any {
				// This server is its own process with no player, so it asks the
				// window's (see nowPlayingFromApp).
				return mcpResult(app.nowPlayingFromApp())
			}),
		},
		// Saved playlists (#107): named lists of tracks from any album, in the
		// rail under the artists.
		{
			name:        "list_playlists",
			description: "Every saved playlist by name, with how many tracks each holds.",
			binding:     "ListPlaylists",
			schema:      object(map[string]any{}),
			run: with(func(app *App, _ struct{}) map[string]any {
				return answer(app.ListPlaylists())
			}),
		},
		{
			name: "get_playlist",
			description: "One saved playlist with its entries in order: each track's name, " +
				"album and artist, and whether a take of it is on disk to play.",
			binding: "GetPlaylist",
			schema:  object(map[string]any{"id": text("the playlist id")}, "id"),
			run: with(func(app *App, in idArg) map[string]any {
				return answer(app.GetPlaylist(in.ID))
			}),
		},
		{
			name: "save_playlist",
			description: "Create a playlist, or rename one. Omit id to create; a name is required. " +
				"Tracks are added and removed with add_to_playlist and remove_from_playlist.",
			binding: "SavePlaylist",
			schema: object(map[string]any{
				"id":   text("the playlist id; omit to create"),
				"name": text("the playlist name"),
			}, "name"),
			run: with(func(app *App, p SavedPlaylist) map[string]any {
				return answer(app.SavePlaylist(p))
			}),
		},
		{
			name:        "delete_playlist",
			description: "Delete a playlist. Its tracks stay on their albums. There is no undo.",
			binding:     "DeletePlaylist",
			schema:      object(map[string]any{"id": text("the playlist id")}, "id"),
			run: with(func(app *App, in idArg) map[string]any {
				return done(app.DeletePlaylist(in.ID))
			}),
		},
		{
			name: "add_to_playlist",
			description: "Put a track at the end of a playlist. A track already in it stays " +
				"where it is. Returns the playlist.",
			binding: "AddToPlaylist",
			schema: object(map[string]any{
				"playlistId": text("the playlist id"),
				"trackId":    text("the track id"),
			}, "playlistId", "trackId"),
			run: with(func(app *App, in struct {
				PlaylistID string `json:"playlistId"`
				TrackID    string `json:"trackId"`
			}) map[string]any {
				return answer(app.AddToPlaylist(in.PlaylistID, in.TrackID))
			}),
		},
		{
			name: "remove_from_playlist",
			description: "Take one entry out of a playlist, by the entry id get_playlist reports " +
				"(not the track id). Returns the playlist.",
			binding: "RemoveFromPlaylist",
			schema: object(map[string]any{
				"playlistId": text("the playlist id"),
				"entryId":    text("the entry id, from get_playlist"),
			}, "playlistId", "entryId"),
			run: with(func(app *App, in struct {
				PlaylistID string `json:"playlistId"`
				EntryID    string `json:"entryId"`
			}) map[string]any {
				return answer(app.RemoveFromPlaylist(in.PlaylistID, in.EntryID))
			}),
		},
	}
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
