import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from 'react'
import {GetBugCounts, ListBugReports, ListResolvedReports} from '../../wailsjs/go/app/App'
import {app} from '../../wailsjs/go/models'
import {useDataChanged} from '../lib/dataChanged'
import {toMessage} from '../lib/errors'

/**
 * The bug queue, shared by the top bar's badge and the settings tab.
 *
 * The badge reads only the counts and the tab reads the reports, which is why
 * they are separate calls: the badge is refreshed on a timer as well as on the
 * change event, and carrying every report's description with it would be a
 * poll that grows with the queue.
 *
 * The timer exists because reports arrive from outside this process — an agent
 * filing through the CLI or MCP, or the reconcile sweep taking in an issue
 * somebody wrote on github.com. Those writes emit the change event only when
 * they happen inside a running app; the sweep runs on read, so a slow poll is
 * what turns "somebody opened an issue" into a badge.
 */
interface BugsValue {
  counts: app.BugCounts
  reports: app.BugReport[]
  resolved: app.ResolvedReport[]
  loading: boolean
  error: string
  reload: () => void
  reloadCounts: () => void
}

const emptyCounts = app.BugCounts.createFrom({open: 0, resolved: 0, unseen: 0})

const BugsContext = createContext<BugsValue>({
  counts: emptyCounts,
  reports: [],
  resolved: [],
  loading: true,
  error: '',
  reload: () => {},
  reloadCounts: () => {},
})

/** How often the badge re-reads. Thirty seconds, matching the backend's own
 * reconcile interval — polling faster would only ever get the same answer. */
const COUNT_POLL_MS = 30000

export function BugsProvider({children}: {children: ReactNode}) {
  const [counts, setCounts] = useState<app.BugCounts>(emptyCounts)
  const [reports, setReports] = useState<app.BugReport[]>([])
  const [resolved, setResolved] = useState<app.ResolvedReport[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const reloadCounts = useCallback(() => {
    GetBugCounts()
      .then(setCounts)
      // Silent: the badge is ambient, and an error toast about a count nobody
      // asked for is noise on a page about something else.
      .catch(() => {})
  }, [])

  const reload = useCallback(() => {
    Promise.all([ListBugReports(), ListResolvedReports()])
      .then(([open, done]) => {
        setReports(open ?? [])
        setResolved(done ?? [])
        setError('')
      })
      .catch((err) => setError(toMessage(err)))
      .finally(() => setLoading(false))
    reloadCounts()
  }, [reloadCounts])

  // The reports themselves are loaded once at mount, not only when a page that
  // lists them opens (#19).
  //
  // They used to be, and it did not show while the Bug reports panel was the
  // only reader — it calls reload itself on the way in. The status bar reads
  // them on every page, so without this it drew "No open reports" in green
  // beside a top-bar badge saying two, which is the exact failure the tone on
  // that segment is meant to prevent: a readout that has stopped reporting.
  //
  // Only the counts stay on the timer. They are what the badge polls and they
  // are cheap; the list costs a reconcile sweep against the forge, and the
  // writes that change it — an agent claiming through the CLI, the sweep
  // adopting an issue — already announce themselves on the bug_reports scope.
  useEffect(() => {
    reload()
    const timer = setInterval(reloadCounts, COUNT_POLL_MS)
    return () => clearInterval(timer)
    // reload is stable and pulls reloadCounts in with it; listing both here
    // would re-run this effect and restart the timer for no reason.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useDataChanged(['bug_reports', 'resolved_reports'], reload)

  return (
    <BugsContext.Provider
      value={{counts, reports, resolved, loading, error, reload, reloadCounts}}
    >
      {children}
    </BugsContext.Provider>
  )
}

export const useBugs = () => useContext(BugsContext)

/**
 * A report's first line, cut to length — what the status bar shows for the one
 * being worked (#19).
 *
 * The first line only: a report is written as prose and its opening sentence is
 * the whole of what it is about, where the paragraphs under it are the detail.
 * Cut at a word so the tail is not half a word, and only when the space is far
 * enough in that cutting there does not throw most of the line away.
 */
export function summarise(description: string, limit = 80): string {
  const line = description.split('\n')[0].trim()
  if (line.length <= limit) return line
  const trimmed = line.slice(0, limit)
  const space = trimmed.lastIndexOf(' ')
  const kept = space > limit / 2 ? trimmed.slice(0, space) : trimmed
  return kept.replace(/[ ,.;:]+$/, '') + '…'
}
