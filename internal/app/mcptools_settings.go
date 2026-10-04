package app

import "errors"

// secretSettings are the settings keys the raw setting tools refuse, with
// where they are read and written instead: each holds credentials, and an
// agent reading the blob would be handed the producer's keys and tokens.
var secretSettings = map[string]string{
	keyAIServices: "the AI connections are read through list_ai_services and written through connect_ai_key and connect_ai_account",
	keyYouTube:    "the YouTube connection is read through get_youtube and made in Settings → YouTube",
}

// ---------------------------------------------------------------------------
// Settings: the skills, the raw settings, the extension, the clients, About
// ---------------------------------------------------------------------------

func settingsTools() []mcpTool {
	return []mcpTool{
		// -- Skills ---------------------------------------------------------------
		{
			name: "list_skills",
			description: "The app's own briefs — how it interviews, how it writes a Suno prompt, " +
				"what a cover should look like — and the producer's own.",
			binding: "ListSkills",
			schema:  object(map[string]any{}),
			run: with(func(app *App, _ noArgs) map[string]any {
				return answer(app.ListSkills())
			}),
		},
		{
			name: "get_skill",
			description: "One skill's full text. Read track-generation before writing a prompt, " +
				"and bug-reports before working the queue.",
			binding: "GetSkill",
			schema:  object(map[string]any{"id": text("the skill id")}, "id"),
			run: with(func(app *App, in idArg) map[string]any {
				return answer(app.GetSkill(in.ID))
			}),
		},
		{
			name: "save_skill",
			description: "Write a skill, or override a built-in by passing its id. A built-in's " +
				"title and description stay the build's — only the body is yours.",
			binding: "SaveSkill",
			schema: object(map[string]any{
				"id":          text("the skill id; a built-in's id overrides it; omit to write a new one"),
				"title":       text("what to call it"),
				"description": text("one line on what it is for"),
				"body":        text("the skill itself, in markdown"),
				"tags":        array("words to find it by", text("a tag")),
			}, "body"),
			run: with(func(app *App, skill Skill) map[string]any {
				return answer(app.SaveSkill(skill))
			}),
		},
		{
			name: "reset_skill",
			description: "Throw a built-in's override away, so it goes back to the build's own " +
				"text. Refused for a written skill — delete_skill is for those.",
			binding: "ResetSkill",
			schema:  object(map[string]any{"id": text("the built-in skill id")}, "id"),
			run: with(func(app *App, in idArg) map[string]any {
				return answer(app.ResetSkill(in.ID))
			}),
		},
		{
			name:        "delete_skill",
			description: "Delete one of the producer's own skills. Refused for a built-in.",
			binding:     "DeleteSkill",
			schema:      object(map[string]any{"id": text("the skill id")}, "id"),
			run: with(func(app *App, in idArg) map[string]any {
				return done(app.DeleteSkill(in.ID))
			}),
		},

		// -- Raw settings ---------------------------------------------------------
		//
		// The key/value door the window uses for its small preferences: theme,
		// last_route, default_track_length. Open, with one key closed: the AI
		// connections blob holds API keys, and aiservices.go's promise that a key
		// never leaves the backend has to hold at this door too — the providers
		// are read masked through list_ai_services and written through
		// connect_ai_key, and nothing else may hand a key out or a blob in.
		{
			name: "get_setting",
			description: "Read one raw setting by key. The keys the window uses: theme, " +
				"last_route, default_track_length (seconds a new track aims at), " +
				"music_root, suno_capture (\"off\" stops the extension filing the songs it " +
				"records on Suno). Empty when unset.",
			binding: "GetSetting",
			schema:  object(map[string]any{"key": text("the setting key")}, "key"),
			run: with(func(app *App, in struct {
				Key string `json:"key"`
			}) map[string]any {
				if why, secret := secretSettings[in.Key]; secret {
					return mcpFailure(errors.New(why))
				}
				value, err := app.GetSetting(in.Key)
				if err != nil {
					return mcpFailure(err)
				}
				return mcpResult(map[string]any{"key": in.Key, "value": value})
			}),
		},
		{
			name: "set_setting",
			description: "Write one raw setting. For the music folder prefer set_music_root, " +
				"which creates the folder and checks the path.",
			binding: "SetSetting",
			schema: object(map[string]any{
				"key":   text("the setting key"),
				"value": text("the value; empty to unset"),
			}, "key"),
			run: with(func(app *App, in struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			}) map[string]any {
				if why, secret := secretSettings[in.Key]; secret {
					return mcpFailure(errors.New(why))
				}
				return done(app.SetSetting(in.Key, in.Value))
			}),
		},

		// -- YouTube (#121) -----------------------------------------------------
		{
			name: "get_youtube",
			description: "Whether the producer's YouTube channel is connected, and which channel. " +
				"Never the OAuth secret or tokens. Connecting is done in Settings → YouTube.",
			binding: "GetYouTube",
			schema:  object(map[string]any{}),
			run: with(func(app *App, _ struct{}) map[string]any {
				return mcpResult(app.GetYouTube())
			}),
		},
		{
			name:        "disconnect_youtube",
			description: "Forget the YouTube connection and revoke it with Google, keeping the OAuth client so connecting again is one press in Settings.",
			binding:     "DisconnectYouTube",
			schema:      object(map[string]any{}),
			run: with(func(app *App, _ struct{}) map[string]any {
				return answer(app.DisconnectYouTube())
			}),
		},

		// -- The Chrome extension -------------------------------------------------
		{
			name: "get_extension_status",
			description: "Whether the Chrome extension has been exported, where to, which port it " +
				"reaches the app on and which site it runs on.",
			binding: "GetExtensionStatus",
			schema:  object(map[string]any{}),
			run: with(func(app *App, _ noArgs) map[string]any {
				return mcpResult(app.GetExtensionStatus())
			}),
		},
		{
			name: "export_extension",
			description: "Write the Chrome extension to disk with this app's bridge token in it, " +
				"for 'Load unpacked'. Re-exporting is how a regenerated token reaches it.",
			binding: "ExportExtension",
			schema:  object(map[string]any{}),
			run: with(func(app *App, _ noArgs) map[string]any {
				return answer(app.ExportExtension())
			}),
		},
		{
			name:        "reveal_extension",
			description: "Open the exported extension's folder in the desktop's file manager, on the producer's screen.",
			binding:     "RevealExtension",
			schema:      object(map[string]any{}),
			run: with(func(app *App, _ noArgs) map[string]any {
				return done(app.RevealExtension())
			}),
		},
		{
			name: "bridge_library",
			description: "The library exactly as the Chrome extension's picker sees it: every " +
				"artist, album and track with the title, prompt and lyrics it can put into " +
				"Suno's fields.",
			binding: "BridgeLibrary",
			schema:  object(map[string]any{}),
			run: with(func(app *App, _ noArgs) map[string]any {
				return answer(app.BridgeLibrary())
			}),
		},

		// -- This server's own registration ---------------------------------------
		{
			name: "get_mcp_status",
			description: "Which AI clients this app is registered with as an MCP server, the " +
				"command they launch, and how many tools it offers.",
			binding: "GetMCPStatus",
			schema:  object(map[string]any{}),
			run: with(func(app *App, _ noArgs) map[string]any {
				return mcpResult(app.GetMCPStatus())
			}),
		},
		{
			name:        "connect_mcp_client",
			description: "Register this app as an MCP server in a client's own config, touching nothing else in the file.",
			binding:     "ConnectMCPClient",
			schema:      object(map[string]any{"id": oneOf("the client", mcpClientIDs...)}, "id"),
			run: with(func(app *App, in idArg) map[string]any {
				return answer(app.ConnectMCPClient(in.ID))
			}),
		},
		{
			name:        "disconnect_mcp_client",
			description: "Remove this app's entry from a client's config.",
			binding:     "DisconnectMCPClient",
			schema:      object(map[string]any{"id": oneOf("the client", mcpClientIDs...)}, "id"),
			run: with(func(app *App, in idArg) map[string]any {
				return answer(app.DisconnectMCPClient(in.ID))
			}),
		},

		// -- About ----------------------------------------------------------------
		{
			name: "describe_app",
			description: "The application's map of itself, for writing documentation: every " +
				"page with the components it renders and the functions it calls; the " +
				"shell every page shares; every binding with the pages that use it and " +
				"the tools that reach it, or why none does; and every tool with its " +
				"schema. Read off the source at build time, joined to the tool surface " +
				"at read time — nothing in it is written by hand. Large; call it once.",
			binding: "DescribeApp",
			schema:  object(map[string]any{}),
			run: with(func(app *App, _ noArgs) map[string]any {
				return answer(app.DescribeApp())
			}),
		},
		{
			name: "get_build_info",
			description: "What is running: version, commit, Go version, platform, whether the " +
				"database opened, and the repository it came from.",
			binding: "GetBuildInfo",
			schema:  object(map[string]any{}),
			run: with(func(app *App, _ noArgs) map[string]any {
				return mcpResult(app.GetBuildInfo())
			}),
		},
	}
}
