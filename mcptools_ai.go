package main

// ---------------------------------------------------------------------------
// The AI: providers, interviews, and the writing jobs
//
// These tools run the app's own model calls — the interviews, the extractions,
// the one-press writers — on whichever provider the producer connected. An
// agent calling them is an agent asking this app to ask its model, which is
// the arrangement that keeps the producer's choices about billing and about
// the skills in force: the brief is the app's, the provider is the app's, and
// the agent gets what the window would have got.
//
// The conversation tools keep this repository's standing rule. ask_* converses
// and stores the transcript; extract_* and plan_album read a finished one and
// return fields. An agent that wants the structure calls the second after the
// first, the way the window does, and never asks a turn for JSON.
// ---------------------------------------------------------------------------

// chatArgs is a conversation keyed the way the store keys it.
type chatArgs struct {
	Kind      string `json:"kind"`
	SubjectID string `json:"subjectId"`
}

// turnArgs is one conversation turn: the key and the whole transcript.
type turnArgs struct {
	Kind      string             `json:"kind"`
	SubjectID string             `json:"subjectId"`
	Messages  []InterviewMessage `json:"messages"`
}

type messagesArg struct {
	Messages []InterviewMessage `json:"messages"`
}

type serviceArg struct {
	ID string `json:"id"`
}

var serviceSchema = oneOf("the provider", aiServiceIDs...)

// interviewKindSchema describes the subjectId rule that trips every first
// caller: the id keys the conversation to what is already decided, so an
// album interview is keyed to its artist and a track interview to its album.
var interviewKindSchema = oneOf("which conversation this is", subjectArtist, subjectAlbum, subjectTrack)

const subjectIDDescription = "what the conversation is about: empty for a new artist; the " +
	"artist id for an album being made for them; the album id for tracks being written " +
	"for it. The interviewer is briefed with that record and everything under it."

func aiTools() []mcpTool {
	return []mcpTool{
		// -- Providers ----------------------------------------------------------
		{
			name: "get_ai_readiness",
			description: "Whether an interview can run here, which provider would answer, and " +
				"why not when it cannot. Ask before starting a conversation.",
			binding: "GetAIReadiness",
			schema:  object(map[string]any{}),
			run: with(func(app *App, _ noArgs) map[string]any {
				return mcpResult(app.GetAIReadiness())
			}),
		},
		{
			name: "list_ai_services",
			description: "Every AI provider — Claude, ChatGPT, Gemini, Antigravity, Grok — with " +
				"how each is connected (signed-in CLI or API key), which is the default, and " +
				"its chosen model. API keys are shown masked and never returned.",
			binding: "GetAIServices",
			schema:  object(map[string]any{}),
			run: with(func(app *App, _ noArgs) map[string]any {
				return mcpResult(app.GetAIServices())
			}),
		},
		{
			name:        "get_ai_service",
			description: "One provider's connection state.",
			binding:     "GetAIService",
			schema:      object(map[string]any{"id": serviceSchema}, "id"),
			run: with(func(app *App, in serviceArg) map[string]any {
				return mcpResult(app.GetAIService(in.ID))
			}),
		},
		{
			name: "set_default_ai_service",
			description: "Star a provider: the one the interviews and the writing jobs run on. " +
				"Returns every provider's state.",
			binding: "SetDefaultAIService",
			schema:  object(map[string]any{"id": serviceSchema}, "id"),
			run: with(func(app *App, in serviceArg) map[string]any {
				return answer(app.SetDefaultAIService(in.ID))
			}),
		},
		{
			name: "set_ai_service_model",
			description: "Name the model a provider runs, or empty to let the provider choose. " +
				"list_ai_models says what a keyed provider offers.",
			binding: "SetAIServiceModel",
			schema: object(map[string]any{
				"id":    serviceSchema,
				"model": text("the model id, or empty for the provider's default"),
			}, "id"),
			run: with(func(app *App, in struct {
				ID    string `json:"id"`
				Model string `json:"model"`
			}) map[string]any {
				return answer(app.SetAIServiceModel(in.ID, in.Model))
			}),
		},
		{
			name: "list_ai_models",
			description: "What a provider offers, asked of the provider with its stored key. " +
				"Empty for a provider connected through its CLI — there is no key to ask with.",
			binding: "ListAIModels",
			schema:  object(map[string]any{"id": serviceSchema}, "id"),
			run: with(func(app *App, in serviceArg) map[string]any {
				return answer(app.ListAIModels(in.ID))
			}),
		},
		{
			name: "connect_ai_account",
			description: "Connect a provider through its signed-in CLI (claude, codex, gemini, agy) " +
				"so its usage bills the subscription. The CLI must be on this machine; whether " +
				"it is signed in is only known when it is first used.",
			binding: "ConnectAIAccount",
			schema:  object(map[string]any{"id": serviceSchema}, "id"),
			run: with(func(app *App, in serviceArg) map[string]any {
				return answer(app.ConnectAIAccount(in.ID))
			}),
		},
		{
			name: "connect_ai_key",
			description: "Connect a provider with an API key. The key is checked against the " +
				"provider before it is stored, and stored only if it worked. Stored in " +
				"the app's own database; never returned.",
			binding: "ConnectAIKey",
			schema: object(map[string]any{
				"id":  serviceSchema,
				"key": text("the API key"),
			}, "id", "key"),
			run: with(func(app *App, in struct {
				ID  string `json:"id"`
				Key string `json:"key"`
			}) map[string]any {
				return answer(app.ConnectAIKey(in.ID, in.Key))
			}),
		},
		{
			name:        "disconnect_ai",
			description: "Forget a provider's connection, key and all.",
			binding:     "DisconnectAI",
			schema:      object(map[string]any{"id": serviceSchema}, "id"),
			run: with(func(app *App, in serviceArg) map[string]any {
				return answer(app.DisconnectAI(in.ID))
			}),
		},

		// -- The interviews ----------------------------------------------------
		{
			name: "get_chat",
			description: "A stored interview transcript, to resume it. Empty for a record nobody " +
				"has talked about yet.",
			binding: "GetChat",
			schema: object(map[string]any{
				"kind":      interviewKindSchema,
				"subjectId": text(subjectIDDescription),
			}, "kind"),
			run: with(func(app *App, in chatArgs) map[string]any {
				return answer(app.GetChat(in.Kind, in.SubjectID))
			}),
		},
		{
			name: "clear_chat",
			description: "Throw an interview transcript away, with any files attached to it. The " +
				"record the conversation made stays.",
			binding: "ClearChat",
			schema: object(map[string]any{
				"kind":      interviewKindSchema,
				"subjectId": text(subjectIDDescription),
			}, "kind"),
			run: with(func(app *App, in chatArgs) map[string]any {
				return done(app.ClearChat(in.Kind, in.SubjectID))
			}),
		},
		{
			name: "ask_interview",
			description: "One turn of an interview — the conversation that creates an artist, an " +
				"album or a track. Send the whole transcript ending with the producer's " +
				"message; the interviewer's reply is appended and the thread stored. A " +
				"YouTube link in the new turn is researched first when Antigravity is " +
				"connected. The reply is prose: when the conversation has settled, call " +
				"extract_artist, plan_album or extract_tracks to get the fields.",
			binding: "AskInterview",
			schema: object(map[string]any{
				"kind":      interviewKindSchema,
				"subjectId": text(subjectIDDescription),
				"messages":  messagesSchema("the whole conversation so far, ending with the producer's new message"),
			}, "kind", "messages"),
			run: with(func(app *App, in turnArgs) map[string]any {
				return answer(app.AskInterview(in.Kind, in.SubjectID, in.Messages))
			}),
		},
		{
			name: "attach_interview_file",
			description: "Put a file on an interview — a finished Suno take, or a reference image " +
				"for the artwork. Returns the path to carry in the next turn's attachments; " +
				"the extraction assigns audio files to tracks by their names, so keep the " +
				"download's own name.",
			binding: "AttachInterviewFile",
			schema: object(map[string]any{
				"kind":      interviewKindSchema,
				"subjectId": text(subjectIDDescription),
				"name":      text("the file's own name, with its extension"),
				"dataUrl":   text("the file as a data URL (audio or image)"),
			}, "kind", "name", "dataUrl"),
			run: with(func(app *App, in struct {
				Kind      string `json:"kind"`
				SubjectID string `json:"subjectId"`
				Name      string `json:"name"`
				DataURL   string `json:"dataUrl"`
			}) map[string]any {
				path, err := app.AttachInterviewFile(in.Kind, in.SubjectID, in.Name, in.DataURL)
				if err != nil {
					return mcpFailure(err)
				}
				return mcpResult(map[string]any{"path": path})
			}),
		},
		{
			name: "extract_artist",
			description: "Read a finished artist interview and return the artist it decided: name, " +
				"summary, base technical style and the references mentioned. Nothing is " +
				"saved — pass the result to save_artist once it reads right.",
			binding: "ExtractArtist",
			schema:  object(map[string]any{"messages": messagesSchema("the finished conversation")}, "messages"),
			run: with(func(app *App, in messagesArg) map[string]any {
				return answer(app.ExtractArtist(in.Messages))
			}),
		},
		{
			name: "extract_album",
			description: "Read a finished album interview and return the album's own fields: name, " +
				"technical style (only what differs from the artist's) and artwork brief. " +
				"Nothing is saved. plan_album is the fuller pass that also writes the tracks.",
			binding: "ExtractAlbum",
			schema:  object(map[string]any{"messages": messagesSchema("the finished conversation")}, "messages"),
			run: with(func(app *App, in messagesArg) map[string]any {
				return answer(app.ExtractAlbum(in.Messages))
			}),
		},
		{
			name: "plan_album",
			description: "Write the whole record an album interview described: the album's name, " +
				"a complete technical style built from the artist's own style and " +
				"references, and the full track list — every track the conversation " +
				"settled kept as agreed, the rest written to fill the plan. Nothing is " +
				"saved: create the album with save_album and the tracks with save_track.",
			binding: "PlanAlbum",
			schema: object(map[string]any{
				"artistId": text("the artist the record is for"),
				"messages": messagesSchema("the finished conversation"),
			}, "artistId", "messages"),
			run: with(func(app *App, in struct {
				ArtistID string             `json:"artistId"`
				Messages []InterviewMessage `json:"messages"`
			}) map[string]any {
				return answer(app.PlanAlbum(in.ArtistID, in.Messages))
			}),
		},
		{
			name: "extract_tracks",
			description: "Read a finished track interview and return every track it settled: " +
				"title, Suno prompt, lyrics, target length, and which attached audio files " +
				"are takes of it. Nothing is saved — save_track each, then " +
				"import_track_file the takes.",
			binding: "ExtractTracks",
			schema:  object(map[string]any{"messages": messagesSchema("the finished conversation")}, "messages"),
			run: with(func(app *App, in messagesArg) map[string]any {
				return answer(app.ExtractTracks(in.Messages))
			}),
		},

		// -- One-press writers -------------------------------------------------
		{
			name: "evolve_album",
			description: "Create the successor to an album under the same artist: its technical " +
				"style moved one step on, and a whole new track list with prompts sequenced " +
				"so each song hands off into the next. Lyrics are left for the track " +
				"interview. Saves and returns the new album.",
			binding: "EvolveAlbum",
			schema:  object(map[string]any{"albumId": text("the album to evolve from")}, "albumId"),
			run: with(func(app *App, in albumArg) map[string]any {
				return answer(app.EvolveAlbum(in.AlbumID))
			}),
		},
		{
			name: "write_album_tracks",
			description: "Write a full track list onto an album that has none, from the artist's " +
				"base style and the album's own. Refused on a record that already has " +
				"tracks — adding to one is the track interview's job. Saves the tracks and " +
				"returns the album.",
			binding: "WriteAlbumTracks",
			schema:  object(map[string]any{"albumId": text("the empty album")}, "albumId"),
			run: with(func(app *App, in albumArg) map[string]any {
				return answer(app.WriteAlbumTracks(in.AlbumID))
			}),
		},
		{
			name: "generate_album_style",
			description: "Distil an album's central technical style from its tracks' prompts: the " +
				"traits that hold across the record. Returns the style text and saves " +
				"nothing — put it on the album with save_album once it reads right.",
			binding: "GenerateAlbumStyle",
			schema:  object(map[string]any{"albumId": text("the album")}, "albumId"),
			run: with(func(app *App, in albumArg) map[string]any {
				style, err := app.GenerateAlbumStyle(in.AlbumID)
				if err != nil {
					return mcpFailure(err)
				}
				return mcpResult(map[string]any{"technicalStyle": style})
			}),
		},

		// -- Refining a style, revising a skill ---------------------------------
		{
			name: "ask_style_edit",
			description: "One turn of a conversation about refining an artist's base technical " +
				"style. Not stored: the thread lives in the call and ends in " +
				"extract_style_edit. A YouTube link in the turn is researched when " +
				"Antigravity is connected.",
			binding: "AskStyleEdit",
			schema: object(map[string]any{
				"artistId": text("the artist whose style is being refined"),
				"messages": messagesSchema("the conversation so far, ending with the producer's new message"),
			}, "artistId", "messages"),
			run: with(func(app *App, in struct {
				ArtistID string             `json:"artistId"`
				Messages []InterviewMessage `json:"messages"`
			}) map[string]any {
				return answer(app.AskStyleEdit(in.ArtistID, in.Messages))
			}),
		},
		{
			name: "extract_style_edit",
			description: "Apply what an ask_style_edit conversation agreed and return the whole " +
				"revised base style. Nothing is saved — save_artist with it once it reads right.",
			binding: "ExtractStyleEdit",
			schema: object(map[string]any{
				"artistId": text("the artist"),
				"messages": messagesSchema("the finished conversation"),
			}, "artistId", "messages"),
			run: with(func(app *App, in struct {
				ArtistID string             `json:"artistId"`
				Messages []InterviewMessage `json:"messages"`
			}) map[string]any {
				return answer(app.ExtractStyleEdit(in.ArtistID, in.Messages))
			}),
		},
		{
			name: "ask_skill_edit",
			description: "One turn of a conversation about revising a skill. Not stored; ends in " +
				"extract_skill_edit.",
			binding: "AskSkillEdit",
			schema: object(map[string]any{
				"skillId":  text("the skill being revised"),
				"messages": messagesSchema("the conversation so far, ending with the producer's new message"),
			}, "skillId", "messages"),
			run: with(func(app *App, in struct {
				SkillID  string             `json:"skillId"`
				Messages []InterviewMessage `json:"messages"`
			}) map[string]any {
				return answer(app.AskSkillEdit(in.SkillID, in.Messages))
			}),
		},
		{
			name: "extract_skill_edit",
			description: "Apply what an ask_skill_edit conversation agreed and return the whole " +
				"revised skill. A built-in keeps its title and description. Nothing is " +
				"saved — save_skill with it once it reads right.",
			binding: "ExtractSkillEdit",
			schema: object(map[string]any{
				"skillId":  text("the skill"),
				"messages": messagesSchema("the finished conversation"),
			}, "skillId", "messages"),
			run: with(func(app *App, in struct {
				SkillID  string             `json:"skillId"`
				Messages []InterviewMessage `json:"messages"`
			}) map[string]any {
				return answer(app.ExtractSkillEdit(in.SkillID, in.Messages))
			}),
		},

		// -- Music analysis ----------------------------------------------------
		{
			name: "analyze_music_url",
			description: "Research a song from its YouTube or YouTube Music link and return its " +
				"identity — title, artist, album, cover — and a technical style breakdown " +
				"in the language a base style is written in. Runs on Antigravity's signed-in " +
				"CLI, which must be connected; slow, minutes rather than seconds.",
			binding: "AnalyzeMusicURL",
			schema:  object(map[string]any{"url": text("a YouTube or YouTube Music link")}, "url"),
			run: with(func(app *App, in struct {
				URL string `json:"url"`
			}) map[string]any {
				return answer(app.AnalyzeMusicURL(in.URL))
			}),
		},
		{
			name: "merge_style_with_analysis",
			description: "Fold one song's breakdown into a base technical style — integrate its " +
				"traits, never just append — and return the merged style. Nothing is " +
				"saved; save_artist with it, and put the analysis on the reference's note.",
			binding: "MergeStyleWithAnalysis",
			schema: object(map[string]any{
				"baseStyle": text("the base style as it stands; empty to write one from the analysis alone"),
				"analysis":  text("the breakdown analyze_music_url returned as style"),
			}, "analysis"),
			run: with(func(app *App, in struct {
				BaseStyle string `json:"baseStyle"`
				Analysis  string `json:"analysis"`
			}) map[string]any {
				style, err := app.MergeStyleWithAnalysis(in.BaseStyle, in.Analysis)
				if err != nil {
					return mcpFailure(err)
				}
				return mcpResult(map[string]any{"baseStyle": style})
			}),
		},
	}
}
