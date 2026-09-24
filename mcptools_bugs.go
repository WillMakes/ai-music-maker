package main

// ---------------------------------------------------------------------------
// The bug queue
//
// The same surface `suno-maker bugs …` offers a terminal (see bugcli.go), for
// an agent that reaches this app over MCP instead. The workflow is the
// bug-reports skill, and it holds here as it holds there: claim before
// starting, one report one commit, resolve only once the fix is on main.
// ---------------------------------------------------------------------------

func bugTools() []mcpTool {
	return []mcpTool{
		{
			name: "list_bug_reports",
			description: "The open bug queue, oldest first — the working order, and the order " +
				"that decides which report may be claimed. Reconciled against the " +
				"repository's issues first, so an issue written on GitHub is here too.",
			binding: "ListBugReports",
			schema:  object(map[string]any{}),
			run: with(func(app *App, _ noArgs) map[string]any {
				return answer(app.ListBugReports())
			}),
		},
		{
			name:        "get_bug_report",
			description: "One open report, with its attachments' paths.",
			binding:     "GetBugReport",
			schema:      object(map[string]any{"id": text("the report id")}, "id"),
			run: with(func(app *App, in idArg) map[string]any {
				return answer(app.GetBugReport(in.ID))
			}),
		},
		{
			name:        "search_bug_reports",
			description: "Open reports whose description matches, for an agent with a symptom rather than an id.",
			binding:     "SearchBugReports",
			schema:      object(map[string]any{"query": text("words from the description")}, "query"),
			run: with(func(app *App, in struct {
				Query string `json:"query"`
			}) map[string]any {
				return answer(app.SearchBugReports(in.Query))
			}),
		},
		{
			name:        "get_bug_counts",
			description: "How many reports are open, how many have been fixed, and how many fixes the producer has not been shown.",
			binding:     "GetBugCounts",
			schema:      object(map[string]any{}),
			run: with(func(app *App, _ noArgs) map[string]any {
				return answer(app.GetBugCounts())
			}),
		},
		{
			name:        "list_resolved_reports",
			description: "Reports already fixed, newest first, each with the commit its fix landed in.",
			binding:     "ListResolvedReports",
			schema:      object(map[string]any{}),
			run: with(func(app *App, _ noArgs) map[string]any {
				return answer(app.ListResolvedReports())
			}),
		},
		{
			name: "file_bug_report",
			description: "File a bug. Opens an issue on this repository and files a counterpart in " +
				"Jax, both best effort — the report exists here whatever the trackers say.",
			binding: "FileBugReport",
			schema: object(map[string]any{
				"description": text("what is wrong"),
				"route":       text("the page it was found on"),
				"global":      boolean("about the whole app rather than one page"),
				"subjectKind": oneOf("what the page was showing, if a record", "artist", "album", "track"),
				"subjectId":   text("that record's id"),
			}, "description"),
			run: with(func(app *App, report BugReport) map[string]any {
				return answer(app.FileBugReport(report))
			}),
		},
		{
			name: "update_bug_report",
			description: "Edit a report's description, route, scope or attachment list, and " +
				"correct its issue to match. The attachments given replace the list; a " +
				"path left out is off the record.",
			binding: "UpdateBugReport",
			schema: object(map[string]any{
				"id":          text("the report id"),
				"description": text("what is wrong"),
				"route":       text("the page it was found on"),
				"global":      boolean("about the whole app rather than one page"),
				"attachments": array("the paths to keep on the report", text("a path attach_bug_media returned")),
			}, "id", "description"),
			run: with(func(app *App, report BugReport) map[string]any {
				return answer(app.UpdateBugReport(report))
			}),
		},
		{
			name:        "claim_bug_report",
			description: "Claim a report before working it — here, on the issue and in Jax — so two agents do not take the same one.",
			binding:     "ClaimBugReport",
			schema:      object(map[string]any{"id": text("the report id")}, "id"),
			run: with(func(app *App, in idArg) map[string]any {
				return answer(app.ClaimBugReport(in.ID))
			}),
		},
		{
			name:        "release_bug_report",
			description: "Put a claimed report back, for somebody else to take.",
			binding:     "ReleaseBugReport",
			schema:      object(map[string]any{"id": text("the report id")}, "id"),
			run: with(func(app *App, in idArg) map[string]any {
				return answer(app.ReleaseBugReport(in.ID))
			}),
		},
		{
			name: "resolve_bug_report",
			description: "Resolve a report once its fix is on main: closes the issue with a " +
				"comment pointing at the commit, and clears it from Jax.",
			binding: "ResolveBugReport",
			schema: object(map[string]any{
				"id":        text("the report id"),
				"commitSha": text("the commit the fix landed in"),
				"commitUrl": text("where that commit can be read"),
			}, "id"),
			run: with(func(app *App, in struct {
				ID        string `json:"id"`
				CommitSHA string `json:"commitSha"`
				CommitURL string `json:"commitUrl"`
			}) map[string]any {
				return answer(app.ResolveBugReport(in.ID, in.CommitSHA, in.CommitURL))
			}),
		},
		{
			name: "attach_bug_media",
			description: "Put a screenshot, a recording or a zip on a report. Returns the path it " +
				"was written to — add it to the report's attachments with update_bug_report.",
			binding: "AttachBugMedia",
			schema: object(map[string]any{
				"reportId": text("the report"),
				"dataUrl":  text("the file as a data URL (png, jpg, gif, webp, mp4, mov, webm, zip)"),
			}, "reportId", "dataUrl"),
			run: with(func(app *App, in struct {
				ReportID string `json:"reportId"`
				DataURL  string `json:"dataUrl"`
			}) map[string]any {
				path, err := app.AttachBugMedia(in.ReportID, in.DataURL)
				if err != nil {
					return mcpFailure(err)
				}
				return mcpResult(map[string]any{"path": path})
			}),
		},
		{
			name: "read_bug_attachment",
			description: "Look at an image attached to a report. Answered as an image, so a " +
				"client that shows its model pictures shows this one. Images only; " +
				"recordings and archives are opened from their path.",
			binding: "ReadBugImage",
			schema:  object(map[string]any{"path": text("an attachment path from the report")}, "path"),
			run: with(func(app *App, in struct {
				Path string `json:"path"`
			}) map[string]any {
				dataURL, err := app.ReadBugImage(in.Path)
				if err != nil {
					return mcpFailure(err)
				}
				return mcpImage(dataURL)
			}),
		},
		{
			name: "get_bug_repo",
			description: "The repository reports are filed against: the pinned one, or the " +
				"SunoMaker checkout this runs in, which pins itself. No slug means reports stay " +
				"local. `ignored` names another project's checkout that was not used.",
			binding: "GetBugRepo",
			schema:  object(map[string]any{}),
			run: with(func(app *App, _ noArgs) map[string]any {
				return mcpResult(app.GetBugRepo())
			}),
		},
		{
			name: "set_bug_repo",
			description: "Pin the repository reports are filed against, as owner/repo or a GitHub " +
				"URL; checked on GitHub before it is kept. Empty unpins.",
			binding: "SetBugRepo",
			schema:  object(map[string]any{"repo": text("owner/repo, a GitHub URL, or empty to unpin")}, "repo"),
			run: with(func(app *App, in struct {
				Repo string `json:"repo"`
			}) map[string]any {
				return answer(app.SetBugRepo(in.Repo))
			}),
		},
	}
}
