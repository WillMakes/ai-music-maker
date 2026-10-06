import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from 'react'
import {CheckForUpdate, DownloadUpdate, InstallUpdate} from '../../wailsjs/go/app/App'
import {EventsOn} from '../../wailsjs/runtime/runtime'
import {app} from '../../wailsjs/go/models'
import {toMessage} from '../lib/errors'

/**
 * Whether a newer release is out, and the download of it (#132). Shared by
 * the top bar's update button, its dialog, and Settings → About's "Check for
 * updates", which all read the one check.
 *
 * Checked at launch and then every few hours: a release is cut per merge, and
 * a producer who leaves the app open for days should still hear about one.
 * The backend never fails a check — an unreachable GitHub comes back as a
 * problem string — so nothing here can get in the way of the app's own work.
 */
interface UpdatesValue {
  status: app.UpdateStatus | null
  checking: boolean
  /** 0–1 while a download runs, null otherwise. */
  progress: number | null
  error: string
  check: () => Promise<void>
  download: () => Promise<void>
  install: () => Promise<void>
}

const UpdatesContext = createContext<UpdatesValue>({
  status: null,
  checking: false,
  progress: null,
  error: '',
  check: async () => {},
  download: async () => {},
  install: async () => {},
})

/** Four hours: frequent enough that a long-open window hears of a release the
 * same day, rare enough to be nothing against GitHub's rate limit. */
const CHECK_EVERY_MS = 4 * 60 * 60 * 1000

export function UpdatesProvider({children}: {children: ReactNode}) {
  const [status, setStatus] = useState<app.UpdateStatus | null>(null)
  const [checking, setChecking] = useState(false)
  const [progress, setProgress] = useState<number | null>(null)
  const [error, setError] = useState('')

  const check = useCallback(async () => {
    setChecking(true)
    try {
      setStatus(await CheckForUpdate())
    } catch {
      // No backend (a plain browser). Nothing to update.
    } finally {
      setChecking(false)
    }
  }, [])

  useEffect(() => {
    void check()
    const timer = setInterval(() => void check(), CHECK_EVERY_MS)
    return () => clearInterval(timer)
  }, [check])

  useEffect(() => {
    try {
      return EventsOn('update:progress', (fraction: number) => setProgress(fraction))
    } catch {
      return undefined
    }
  }, [])

  const download = useCallback(async () => {
    setError('')
    setProgress(0)
    try {
      setStatus(await DownloadUpdate())
    } catch (err) {
      setError(toMessage(err))
    } finally {
      setProgress(null)
    }
  }, [])

  const install = useCallback(async () => {
    setError('')
    try {
      await InstallUpdate()
    } catch (err) {
      setError(toMessage(err))
    }
  }, [])

  return (
    <UpdatesContext.Provider value={{status, checking, progress, error, check, download, install}}>
      {children}
    </UpdatesContext.Provider>
  )
}

export function useUpdates() {
  return useContext(UpdatesContext)
}
