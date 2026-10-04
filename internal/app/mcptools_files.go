package app

// ---------------------------------------------------------------------------
// The files: takes, covers, and where the music goes
//
// Two doors onto every import, for the two shapes a file arrives in. A path is
// what an agent running on this machine has — it downloaded the file, or the
// producer told it where one is — and the path tools take it as the window's
// file dialog would. A data URL is what a client that cannot see this
// machine's disk has, and it takes the same road a browser drop takes (#57):
// bytes and a name, landed in a temporary file, then the ordinary import.
// ---------------------------------------------------------------------------

func fileTools() []mcpTool {
	return []mcpTool{
		{
			name: "import_track_file",
			description: "File a finished take against a track from a path on this machine. The " +
				"file is copied into the music library under Artist/Album, numbered by " +
				"running order; the original is left where it was. The first take imported " +
				"becomes the chosen one and sets the track's duration. Returns the track.",
			binding: "ImportTrackFile",
			schema: object(map[string]any{
				"trackId":    text("the track this is a take of"),
				"sourcePath": text("an absolute path to the audio file (mp3, wav, flac, m4a, aac, ogg)"),
				"label":      text("what tells this take from the others; omit to use the file's own name"),
			}, "trackId", "sourcePath"),
			run: with(func(app *App, in struct {
				TrackID    string `json:"trackId"`
				SourcePath string `json:"sourcePath"`
				Label      string `json:"label"`
			}) map[string]any {
				return answer(app.ImportTrackFile(in.TrackID, in.SourcePath, in.Label))
			}),
		},
		{
			name: "import_track_file_data",
			description: "File a finished take against a track from its bytes — for a client that " +
				"has the file but not this machine's disk. Otherwise the same as " +
				"import_track_file. Returns the track.",
			binding: "ImportDroppedTrackFile",
			schema: object(map[string]any{
				"trackId": text("the track this is a take of"),
				"name":    text("the file's own name, with its extension"),
				"dataUrl": text("the audio as a data URL: data:audio/mpeg;base64,…"),
				"label":   text("what tells this take from the others; omit to use the name"),
			}, "trackId", "name", "dataUrl"),
			run: with(func(app *App, in struct {
				TrackID string `json:"trackId"`
				Name    string `json:"name"`
				DataURL string `json:"dataUrl"`
				Label   string `json:"label"`
			}) map[string]any {
				return answer(app.ImportDroppedTrackFile(in.TrackID, in.Name, in.DataURL, in.Label))
			}),
		},
		{
			name: "choose_track_file",
			description: "Mark one take as the track — the one the extension, the player and the " +
				"release step use — unmarking the others. The track's duration follows the " +
				"chosen take. Returns the track.",
			binding: "ChooseTrackFile",
			schema: object(map[string]any{
				"trackId": text("the track"),
				"fileId":  text("the take to choose, from the track's files"),
			}, "trackId", "fileId"),
			run: with(func(app *App, in struct {
				TrackID string `json:"trackId"`
				FileID  string `json:"fileId"`
			}) map[string]any {
				return answer(app.ChooseTrackFile(in.TrackID, in.FileID))
			}),
		},
		{
			name: "remove_track_file",
			description: "Forget a take. deleteFile also removes the audio from the music library; " +
				"off, the row goes and the file stays, which is undoable by importing it " +
				"again. Removing the chosen take promotes the oldest remaining one. Returns " +
				"the track.",
			binding: "RemoveTrackFile",
			schema: object(map[string]any{
				"trackId":    text("the track"),
				"fileId":     text("the take to remove"),
				"deleteFile": boolean("also delete the audio file from disk; default false"),
			}, "trackId", "fileId"),
			run: with(func(app *App, in struct {
				TrackID    string `json:"trackId"`
				FileID     string `json:"fileId"`
				DeleteFile bool   `json:"deleteFile"`
			}) map[string]any {
				return answer(app.RemoveTrackFile(in.TrackID, in.FileID, in.DeleteFile))
			}),
		},
		{
			name: "reveal_track_file",
			description: "Open the folder holding a take in the desktop's own file manager, with " +
				"the file selected — on the producer's screen, so use it when they asked " +
				"where a file went.",
			binding: "RevealTrackFile",
			schema:  object(map[string]any{"path": text("the take's path, from the track's files")}, "path"),
			run: with(func(app *App, in struct {
				Path string `json:"path"`
			}) map[string]any {
				return done(app.RevealTrackFile(in.Path))
			}),
		},
		{
			name: "import_album_take",
			description: "File one finished take onto an album without naming the track: the file " +
				"is matched to a track by name (a Suno download is named after its song), " +
				"and refused rather than guessed when it matches none or several. For a " +
				"whole folder of downloads, call this once per file. Returns the track it " +
				"landed on.",
			binding: "ImportAlbumTakeFile",
			schema: object(map[string]any{
				"albumId":    text("the album the take belongs to"),
				"sourcePath": text("an absolute path to the audio file"),
			}, "albumId", "sourcePath"),
			run: with(func(app *App, in struct {
				AlbumID    string `json:"albumId"`
				SourcePath string `json:"sourcePath"`
			}) map[string]any {
				return answer(app.ImportAlbumTakeFile(in.AlbumID, in.SourcePath))
			}),
		},
		{
			name: "import_album_take_data",
			description: "import_album_take from bytes rather than a path: the file's name is what " +
				"matches it to a track. Returns the track it landed on.",
			binding: "ImportAlbumTakeDrop",
			schema: object(map[string]any{
				"albumId": text("the album the take belongs to"),
				"name":    text("the file's own name, with its extension — this is what finds the track"),
				"dataUrl": text("the audio as a data URL"),
			}, "albumId", "name", "dataUrl"),
			run: with(func(app *App, in struct {
				AlbumID string `json:"albumId"`
				Name    string `json:"name"`
				DataURL string `json:"dataUrl"`
			}) map[string]any {
				return answer(app.ImportAlbumTakeDrop(in.AlbumID, in.Name, in.DataURL))
			}),
		},
		{
			name: "generate_album_artwork",
			description: "Generate the album's cover from its name, its artist, the combined " +
				"technical style, the album-artwork skill and the album's artworkPrompt. " +
				"Needs ChatGPT connected. With feedback and an existing cover, the current " +
				"image is revised rather than replaced — 'make it bluer' means that. Slow: " +
				"an image generation. Returns the album with its new artworkUrl.",
			binding: "GenerateAlbumArtwork",
			schema: object(map[string]any{
				"albumId":  text("the album"),
				"feedback": text("what to change about the current cover; omit for a first generation"),
			}, "albumId"),
			run: with(func(app *App, in struct {
				AlbumID  string `json:"albumId"`
				Feedback string `json:"feedback"`
			}) map[string]any {
				return answer(app.GenerateAlbumArtwork(in.AlbumID, in.Feedback))
			}),
		},
		{
			name: "render_album_video",
			description: "Render the album's music video (#118): every track's official take in " +
				"running order, each over the cover with its title and a visualisation of the " +
				"audio, faded in and out with the music-video gap from settings between songs. " +
				"Needs a cover, an official take on every track, and ffmpeg. Slow: minutes for " +
				"a whole record. Filed in the album's folder; returns the album with videoFile.",
			binding: "RenderAlbumVideo",
			schema: object(map[string]any{
				"albumId": text("the album"),
			}, "albumId"),
			run: with(func(app *App, in struct {
				AlbumID string `json:"albumId"`
			}) map[string]any {
				return answer(app.RenderAlbumVideo(in.AlbumID))
			}),
		},
		{
			name: "import_album_artwork",
			description: "Use an image the producer already has as the album's cover (png, jpg, " +
				"webp, up to 10 MB). Clears the generation brief, since this cover did not " +
				"come from one. Returns the album.",
			binding: "ImportAlbumArtwork",
			schema: object(map[string]any{
				"albumId":    text("the album"),
				"sourcePath": text("an absolute path to the image"),
			}, "albumId", "sourcePath"),
			run: with(func(app *App, in struct {
				AlbumID    string `json:"albumId"`
				SourcePath string `json:"sourcePath"`
			}) map[string]any {
				return answer(app.ImportAlbumArtwork(in.AlbumID, in.SourcePath))
			}),
		},
		{
			name:        "remove_album_artwork",
			description: "Take the cover off an album. The image file stays on disk. Returns the album.",
			binding:     "RemoveAlbumArtwork",
			schema:      object(map[string]any{"albumId": text("the album")}, "albumId"),
			run: with(func(app *App, in albumArg) map[string]any {
				return answer(app.RemoveAlbumArtwork(in.AlbumID))
			}),
		},
		{
			name: "get_music_root",
			description: "Where finished audio is filed: the producer's chosen folder, or the " +
				"operating system's music folder's AIMusicMaker directory. Takes land under it " +
				"as Artist/Album/01 Track.mp3.",
			binding: "MusicRoot",
			schema:  object(map[string]any{}),
			run: with(func(app *App, _ noArgs) map[string]any {
				root, err := app.MusicRoot()
				if err != nil {
					return mcpFailure(err)
				}
				return mcpResult(map[string]any{"path": root})
			}),
		},
		{
			name: "set_music_root",
			description: "Point the music library somewhere else — an external drive, say. The " +
				"folder is created now. Files already imported stay where they are; new " +
				"ones land in the new place. An empty dir restores the OS default. Returns " +
				"the root in effect.",
			binding: "SetMusicRoot",
			schema:  object(map[string]any{"dir": text("an absolute path, or empty for the default")}),
			run: with(func(app *App, in struct {
				Dir string `json:"dir"`
			}) map[string]any {
				root, err := app.SetMusicRoot(in.Dir)
				if err != nil {
					return mcpFailure(err)
				}
				return mcpResult(map[string]any{"path": root})
			}),
		},
	}
}
