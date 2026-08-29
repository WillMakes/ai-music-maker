import {useEffect} from 'react'
import {ExternalLink} from 'lucide-react'
import {MarkFixesSeen, OpenURL} from '../../wailsjs/go/main/App'
import {useBugs} from '../bugs/BugsProvider'
import {StatusPill} from '../components/StatusPill'
import {when} from '../lib/format'

/**
 * Settings → Bug reports: the open queue and what has been fixed.
 *
 * Read-only, deliberately. Claiming and resolving are an agent's job and happen
 * through the CLI or MCP, where the commit that fixed the bug is at hand; a
 * Resolve button in the window would record a fix with nothing to point at.
 *
 * Opening this page marks the fixes as seen, which is what clears the dashboard
 * notice — the producer has now been shown them, which is the whole claim the
 * flag makes.
 */
export function BugsPanel() {
  const {reports, resolved, counts, loading, error, reload} = useBugs()

  useEffect(() => {
    reload()
  }, [reload])

  useEffect(() => {
    if (counts.unseen > 0) void MarkFixesSeen().catch(() => {})
  }, [counts.unseen])

  return (
    <div className="space-y-5">
      <div>
        <h2 className="text-sm font-semibold">Bug reports</h2>
        <p className="mt-1 text-sm text-fg-muted">
          Every report is an issue on this app's repository and a report in Jax. Work one
          with <span className="font-mono">suno-maker bugs claim &lt;id&gt;</span>.
        </p>
      </div>

      {error && <p className="text-sm text-danger">{error}</p>}

      <section className="space-y-2">
        <h3 className="text-sm font-semibold">
          Open{counts.open > 0 ? ' — ' + counts.open : ''}
        </h3>
        {loading ? (
          <p className="text-sm text-fg-muted">Loading…</p>
        ) : reports.length === 0 ? (
          <p className="sm-panel px-4 py-6 text-center text-sm text-fg-muted">
            Nothing is known to be broken.
          </p>
        ) : (
          <ul className="sm-panel divide-y divide-edge overflow-hidden">
            {reports.map((report) => (
              <li key={report.id} className="flex items-start gap-3 px-4 py-3">
                <span className="min-w-0 flex-1">
                  <span className="block whitespace-pre-wrap text-sm">
                    {report.description}
                  </span>
                  <span className="mt-1 flex flex-wrap items-center gap-2 text-xs text-fg-muted">
                    <span className="font-mono">{report.id}</span>
                    {report.global ? (
                      <span>whole app</span>
                    ) : report.route ? (
                      <span className="font-mono">{report.route}</span>
                    ) : null}
                    {report.subjectName && <span>{report.subjectName}</span>}
                    <span>{when(report.createdAt)}</span>
                    {report.jaxReportId === 0 && (
                      <StatusPill tone="warn">Not in Jax</StatusPill>
                    )}
                    {report.checkedOut && <StatusPill tone="ok">Being worked</StatusPill>}
                    {report.jaxNote && <StatusPill tone="warn">Jax disagrees</StatusPill>}
                  </span>
                  {/* What Jax said when it declined to record the claim (#15).
                      Shown rather than swallowed: the claim stands here either
                      way, and the whole failure being fixed is the two queues
                      disagreeing with nothing on screen to say so. */}
                  {report.jaxNote && (
                    <span className="mt-1 block text-xs text-warn">{report.jaxNote}</span>
                  )}
                </span>
                {report.issueNumber > 0 && (
                  <button
                    className="sm-button sm-button-quiet shrink-0"
                    type="button"
                    onClick={() => void OpenURL(report.issueUrl).catch(() => {})}
                  >
                    <ExternalLink size={14} />#{report.issueNumber}
                  </button>
                )}
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="space-y-2">
        <h3 className="text-sm font-semibold">Fixed</h3>
        {resolved.length === 0 ? (
          <p className="sm-panel px-4 py-6 text-center text-sm text-fg-muted">
            Nothing yet.
          </p>
        ) : (
          <ul className="sm-panel divide-y divide-edge overflow-hidden">
            {resolved.slice(0, 25).map((report) => (
              <li key={report.id} className="flex items-start gap-3 px-4 py-3">
                <span className="min-w-0 flex-1">
                  <span className="block whitespace-pre-wrap text-sm">
                    {report.description}
                  </span>
                  <span className="mt-1 flex flex-wrap items-center gap-2 text-xs text-fg-muted">
                    <span>fixed {when(report.resolvedAt)}</span>
                    {report.commitSha && (
                      <span className="font-mono">{report.commitSha.slice(0, 8)}</span>
                    )}
                  </span>
                  {report.jaxNote && (
                    <span className="mt-1 block text-xs text-warn">{report.jaxNote}</span>
                  )}
                </span>
                {report.commitUrl && (
                  <button
                    className="sm-button sm-button-quiet shrink-0"
                    type="button"
                    onClick={() => void OpenURL(report.commitUrl).catch(() => {})}
                  >
                    <ExternalLink size={14} />
                    The fix
                  </button>
                )}
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  )
}
