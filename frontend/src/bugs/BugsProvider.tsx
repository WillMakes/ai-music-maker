import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from 'react'
import {GetBugCounts, ListBugReports, ListResolvedReports} from '../../wailsjs/go/main/App'
import {main} from '../../wailsjs/go/models'
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
  counts: main.BugCounts
  reports: main.BugReport[]
  resolved: main.ResolvedReport[]
  loading: boolean
  error: string
  reload: () => void
  reloadCounts: () => void
}

const emptyCounts = main.BugCounts.createFrom({open: 0, resolved: 0, unseen: 0})

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
  const [counts, setCounts] = useState<main.BugCounts>(emptyCounts)
  const [reports, setReports] = useState<main.BugReport[]>([])
  const [resolved, setResolved] = useState<main.ResolvedReport[]>([])
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

  useEffect(() => {
    reloadCounts()
    const timer = setInterval(reloadCounts, COUNT_POLL_MS)
    return () => clearInterval(timer)
  }, [reloadCounts])

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
