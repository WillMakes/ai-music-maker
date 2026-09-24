package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// ---------------------------------------------------------------------------
// The bug queue's agent surface
//
// `suno-maker bugs …`: the same binary, short-circuited before the window is
// opened. A queue nothing can reach from outside the app is a queue only the
// person who filed into it can work, so this is the half of the feature that
// makes the other half worth having.
//
// In a development tree it is `go run . bugs <command>`; against an installed
// build, `suno-maker bugs <command>`. Everything prints JSON on stdout — an
// agent parses it, and a person reading over its shoulder can still tell what
// happened — and anything that went wrong on stderr with a non-zero exit.
// ---------------------------------------------------------------------------

// bugWorkflow is the end-to-end workflow, served by `bugs workflow`. Shipped
// with the binary rather than left in the repository for the same reason the
// commands are: an agent that has to be told the workflow every session will do
// it differently every session.
//
//go:embed skills/bug-reports.md
var bugWorkflow string

const bugUsage = `SunoMaker — bug queue

  bugs list [--unlinked]       Open reports, oldest first (the working order)
  bugs count                   Open and resolved totals
  bugs get <id>                One report
  bugs search <query>          Open reports matching a description
  bugs claim <id>              Claim a report, here, on the issue and in Jax
  bugs release <id>            Put a claimed report back
  bugs link <id> --jax <n>     Record the Jax report this one is tracked by
  bugs file -d <text>          File a report  [-r <route>] [-g]
  bugs resolve <id>            Resolve: close the issue, tell Jax, record the fix
                                              [--sha <sha>] [--url <commit url>]
  bugs history                 Reports already fixed, newest first
  bugs repo [<owner/repo>]     The repository reports are filed against; with
                               an argument, pin it (empty string unpins)
  bugs workflow                How to work a report, end to end
`

// runBugCLI handles a `bugs` invocation. It reports handled=false for anything
// else, so main falls through and opens the window as usual.
func runBugCLI(args []string) (code int, handled bool) {
	if len(args) == 0 || args[0] != "bugs" {
		return 0, false
	}
	return bugCommand(args[1:], os.Stdout, os.Stderr), true
}

// bugOut is where a command writes. A pair rather than two arguments so that
// emit can be a method: `out.emit(app.ListBugReports())` passes a two-result
// call straight through, which Go only allows when those results are the whole
// parameter list — and every call here already returns (value, error).
type bugOut struct {
	stdout io.Writer
	stderr io.Writer
}

// emit prints a value as indented JSON, or its error and a non-zero code.
func (o bugOut) emit(value any, err error) int {
	if err != nil {
		fmt.Fprintf(o.stderr, "bugs: %v\n", err)
		return 1
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fmt.Fprintf(o.stderr, "bugs: %v\n", err)
		return 1
	}
	fmt.Fprintln(o.stdout, string(encoded))
	return 0
}

// bugCommand runs one subcommand against its own store. Output streams are
// arguments rather than os.Stdout/os.Stderr directly, so tests can read what a
// command actually printed.
func bugCommand(args []string, stdout, stderr io.Writer) int {
	out := bugOut{stdout, stderr}
	if len(args) == 0 {
		fmt.Fprint(stderr, bugUsage)
		return 2
	}

	// `workflow` and `repo` answer even when the database will not open, which
	// matters: they are the two an agent reaches for when something else is
	// already wrong.
	switch args[0] {
	case "workflow":
		store, err := openStore()
		if err == nil {
			defer store.Close()
		}
		fmt.Fprint(stdout, bugWorkflowText(store))
		return 0
	case "repo":
		// Answers without a database, from the checkout alone; with one, from
		// the pinned setting — and `bugs repo <owner/repo>` pins (#94).
		store, err := openStore()
		if err != nil {
			if len(args) > 1 {
				return out.emit(nil, err)
			}
			return out.emit(resolveBugRepo(nil), nil)
		}
		defer store.Close()
		if len(args) > 1 {
			return out.emit((&App{store: store}).SetBugRepo(args[1]))
		}
		return out.emit(resolveBugRepo(store), nil)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, bugUsage)
		return 0
	}

	store, err := openStore()
	if err != nil {
		fmt.Fprintf(stderr, "bugs: %v\n", err)
		return 1
	}
	defer store.Close()

	// A write here has to reach an open window, which is a different process
	// (see notifyRunningWindow) — an agent resolving a report from a terminal
	// should clear the badge in a window somebody is looking at.
	store.onChange = notifyRunningWindow(store)

	// One app for the whole invocation, so every command reaches the issue queue
	// and Jax the same way the window does. No context and no window of its own:
	// App.emit is written to do nothing without one.
	app := &App{store: store, toJax: fileReportInJax}

	command, rest := args[0], args[1:]
	switch command {
	case "list":
		set := flag.NewFlagSet("list", flag.ContinueOnError)
		set.SetOutput(stderr)
		unlinked := set.Bool("unlinked", false, "only reports with no Jax counterpart")
		if err := set.Parse(rest); err != nil {
			return 2
		}
		reports, err := app.ListBugReports()
		if err != nil {
			return out.emit(nil, err)
		}
		if *unlinked {
			filtered := []BugReport{}
			for _, r := range reports {
				if r.JaxReportID == 0 {
					filtered = append(filtered, r)
				}
			}
			reports = filtered
		}
		return out.emit(reports, nil)

	case "count":
		return out.emit(app.GetBugCounts())

	case "get":
		if len(rest) != 1 {
			fmt.Fprint(stderr, "bugs: get <id>\n")
			return 2
		}
		return out.emit(store.bugReport(rest[0]))

	case "search":
		if len(rest) == 0 {
			fmt.Fprint(stderr, "bugs: search <query>\n")
			return 2
		}
		return out.emit(app.SearchBugReports(strings.Join(rest, " ")))

	case "claim":
		if len(rest) != 1 {
			fmt.Fprint(stderr, "bugs: claim <id>\n")
			return 2
		}
		return out.emit(app.ClaimBugReport(rest[0]))

	case "release":
		if len(rest) != 1 {
			fmt.Fprint(stderr, "bugs: release <id>\n")
			return 2
		}
		return out.emit(app.ReleaseBugReport(rest[0]))

	case "link":
		id, flags := leadingID(rest)
		set := flag.NewFlagSet("link", flag.ContinueOnError)
		set.SetOutput(stderr)
		jax := set.Int("jax", 0, "the Jax report id")
		if err := set.Parse(flags); err != nil {
			return 2
		}
		if id == "" || *jax == 0 {
			fmt.Fprint(stderr, "bugs: link <id> --jax <n>\n")
			return 2
		}
		report, err := store.bugReport(id)
		if err != nil {
			return out.emit(nil, err)
		}
		report.JaxReportID = *jax
		if err := store.saveBugReport(report); err != nil {
			return out.emit(nil, err)
		}
		return out.emit(store.bugReport(report.ID))

	case "file":
		set := flag.NewFlagSet("file", flag.ContinueOnError)
		set.SetOutput(stderr)
		description := set.String("d", "", "what is wrong")
		route := set.String("r", "", "the page it was found on")
		global := set.Bool("g", false, "about the whole app rather than one page")
		if err := set.Parse(rest); err != nil {
			return 2
		}
		if strings.TrimSpace(*description) == "" {
			fmt.Fprint(stderr, "bugs: file -d <text>\n")
			return 2
		}
		return out.emit(app.FileBugReport(BugReport{
			Description: *description, Route: *route, Global: *global,
		}))

	case "resolve":
		id, flags := leadingID(rest)
		set := flag.NewFlagSet("resolve", flag.ContinueOnError)
		set.SetOutput(stderr)
		sha := set.String("sha", "", "the commit the fix landed in")
		url := set.String("url", "", "where that commit can be read")
		if err := set.Parse(flags); err != nil {
			return 2
		}
		if id == "" {
			fmt.Fprint(stderr, "bugs: resolve <id> [--sha <sha>] [--url <url>]\n")
			return 2
		}
		resolved, err := app.ResolveBugReport(id, *sha, *url)
		// See `claim` for why this goes to stderr and does not change the exit
		// code: the fix is on main either way, and the refusal must not be silent.
		if err == nil && resolved.JaxNote != "" {
			fmt.Fprintf(stderr, "bugs: Jax did not clear the report: %s\n", resolved.JaxNote)
		}
		return out.emit(resolved, err)

	case "history":
		return out.emit(app.ListResolvedReports())
	}

	fmt.Fprint(stderr, bugUsage)
	return 2
}

// leadingID pulls a report id off the front of an argument list, returning it
// and whatever is left for the flag set.
//
// Go's flag package stops at the first non-flag argument, so
// `bugs resolve <id> --sha abc` parses no flags at all — the id ends the flag
// section. Every other CLI in the world takes the subject first, so the fix is
// to lift the positional out here rather than to make agents write
// `bugs resolve --sha abc <id>` and be silently ignored when they do not.
//
// Only one, and only at the front: these commands take exactly one id, and
// anything after the flags begin is the flags' business.
func leadingID(args []string) (id string, flags []string) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args[0], args[1:]
	}
	return "", args
}

// bugWorkflowText is the workflow as it stands: the producer's override of the
// bug-reports skill where there is one, else the text this build shipped.
//
// An override is worth reading — the process is theirs to change — but not at
// the cost of the command that explains how to work the queue, so a store that
// will not open falls back to the default rather than failing.
func bugWorkflowText(store *Store) string {
	if store == nil {
		return bugWorkflow
	}
	app := &App{store: store}
	if body := app.skillBody(skillBugReports); body != "" {
		return body + "\n"
	}
	return bugWorkflow
}
