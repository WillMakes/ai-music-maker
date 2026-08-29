# Working the bug queue

SunoMaker keeps its own bug queue. Reports are filed from the bug button in the
top bar; an agent works each one to resolution through `bugs` subcommands on the
app binary. The queue is the source of truth: a report exists because something
is broken, and it leaves the queue only once that has stopped being true.

Run every command from inside this checkout. In a development tree that is
`go run . bugs <command>`; against an installed build it is
`suno-maker bugs <command>`. Everything prints JSON on stdout, and anything that
went wrong on stderr with a non-zero exit code.

## The queue is the repository's issue list

**A report is a GitHub issue.** Filing one opens an issue on the repository
`bugs repo` names; the queue you list, search and count is that issue list;
`bugs resolve` closes the issue, and closing the issue on github.com resolves the
report. There is no step where you open a tracker issue — the report arrived as
one.

**Every open issue is a report**, however it was opened and whoever opened it. An
issue written on github.com the ordinary way is in this queue at the next read
and is worked like any other. A queue you could only add to from inside the app
would be a local queue with extra steps.

Two things are not in it, and neither is a judgement about the issue:

- **pull requests**, which the same endpoint returns and which are not reports;
- **issues labelled `parked`**, which is how somebody says an issue is not work
  to pick up — an epic, a discussion, a question. Parking keeps an issue *out*;
  it does not take one out. A report already in the queue leaves it by being
  resolved or by its issue being closed.

The `bug-queue` label is provenance rather than a filter: it is written on the
issues the app files, so `gh issue list --label bug-queue` still separates those
from the ones somebody wrote.

An issue nobody filed through the app has no report id of its own, so it gets
`issue_<number>` — and since a report has no title field, its description is the
issue's title with the body under it. The title is where a hand-written issue
puts most of the bug, and reading only the body would leave the queue listing it
by its second paragraph.

What `bugs list` prints is a local projection of that list, reconciled against
the forge at the start of every read. The projection exists for two reasons and
neither is a compromise about where the truth lives: the bug button must go on
working with no remote, no credential and no network, and the window watches the
queue — which is a rate limit spent on a poll if it goes to the API. A report
filed while the forge was unreachable has `issueNumber` 0 and gets its issue on
the next sweep.

## The work happens here; the tracking happens in Jax

Every report in this queue gets a counterpart report in Jax, and the two are
resolved together. The fix, the tests, the commit and the push are all in this
repository — but the producer watches the queue from Jax, so a report that never
reaches it is work nobody can see.

**All three calls are made by the app, not by hand.** Filing a report calls
Jax's `save_debug_report` and writes the id that comes back onto the record;
`bugs claim` calls `check_out_debug_report`; `bugs resolve` calls
`delete_debug_report`. So a report has normally reached Jax before any agent has
seen it, and the row there moves as the work moves.

None of it can fail the work. Jax is a separate application and is very often
simply closed: a claim refused, or a resolve refused, because a tracker was not
running would be this queue failing at its job to keep a second one tidy.

**But a refusal is no longer silent.** Jax can accept the call and decline to act
on it — most often because the report is not the oldest workable one in *its*
queue, which spans every application it tracks and not just this one. That
arrives as HTTP 200 with `isError` set in the body, which is the same shape a
success takes, so the app used to read it as agreement and the two queues drifted
apart with nothing saying so.

Now whatever Jax said is written onto the report as `jaxNote`, shown in
Settings → Bug reports, and printed to stderr by `bugs claim` and `bugs resolve`.
The exit code stays 0 and the claim or the resolve still stands here — the fix is
on `main` either way — but you can see that Jax did not record it.

If you see one, it is usually Jax telling you to work a different report first.
That is Jax's ordering across all its apps, not this repository's; `bugs list` is
still the order to work *this* queue in. Either take the report Jax names, or
carry on and know the two queues disagree until it is cleared.

The pairing is stored on the report as `jaxReportId` rather than remembered, so
which reports are already in Jax survives one agent stopping and another
starting. It stays 0 when Jax was closed at the moment of filing, which costs the
link and nothing else — the next launch sweeps for unlinked reports and files
them.

`bugs list --unlinked` is what is missing a counterpart, and it should normally
be empty while Jax is running. A report that stays on it is one Jax keeps
refusing, and filing it by hand is the same two steps it always was:

```bash
go run . bugs list --unlinked          # local reports with no Jax counterpart
# → save_debug_report in Jax, which answers with an id
go run . bugs link <id> --jax <jaxId>  # write the pairing down
```

## The rules

**One report, one issue, one commit.** Every report is its own unit of work.
Never fold several into one issue or one commit.

**Work the oldest first.** `bugs list` is in that order deliberately. Being
oldest is what decides which report you may claim.

**Claim before you start.** `bugs claim <id>` marks it here, labels the issue
`being-worked`, and tells Jax. Two agents on the same report is the failure this
prevents.

**A report is not done until its fix is on `main`.** Fixing, committing and
pushing are all part of resolving it. `bugs resolve <id> --sha <sha> --url <url>`
records where the fix landed, closes the issue with a comment pointing at the
commit, and clears the report from Jax.

**Reproduce before you fix.** A report says what somebody saw, not what is
wrong. If you cannot reproduce it, say so on the issue and leave it open rather
than closing it as unclear.

**Write the test first where there is one to write.** The backend is
`go test ./...`; a bug in a store method or a binding should have a test that
fails before the fix and passes after it.

## The loop

```bash
go run . bugs list                     # oldest first
go run . bugs claim <id>               # here, on the issue, in Jax
# reproduce, write the failing test, fix it
go vet ./... && go test ./...          # backend
cd frontend && npm run build           # tsc is strict; unused locals fail
git commit -am "…"                     # one report, one commit
git push
go run . bugs resolve <id> --sha "$(git rev-parse HEAD)" \
  --url "https://github.com/<slug>/commit/$(git rev-parse HEAD)"
```

## Credentials

Filing needs a GitHub token, from `GITHUB_TOKEN`, `GH_TOKEN`, or `gh auth token`
— in that order. Nothing is stored by this app: a token it kept a copy of would
be one that outlives the sign-in it came from, and `gh` refreshes its own.

Reaching Jax needs its loopback endpoint (`~/.jax/mcp.json`, written while Jax is
running) and its bearer token (from whichever MCP client config Jax registered
itself with). Neither is configured here.
