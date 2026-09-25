import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from 'react'
import {ListArtists} from '../../wailsjs/go/app/App'
import {app} from '../../wailsjs/go/models'
import {useDataChanged} from '../lib/dataChanged'
import {toMessage} from '../lib/errors'

/**
 * The artist list, loaded once and shared.
 *
 * A provider rather than a useState in each view because three pages read it —
 * the dashboard's counts, the Artists list, and every breadcrumb on a details
 * page — and three copies would be three loads on every navigation and three
 * chances to be showing different data.
 *
 * It re-reads on the "artists" scope, so an artist created through MCP or by the
 * interview appears without anybody navigating.
 */
interface ArtistsValue {
  artists: app.Artist[]
  loading: boolean
  error: string
  reload: () => void
}

const ArtistsContext = createContext<ArtistsValue>({
  artists: [],
  loading: true,
  error: '',
  reload: () => {},
})

export function ArtistsProvider({children}: {children: ReactNode}) {
  const [artists, setArtists] = useState<app.Artist[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const reload = useCallback(() => {
    ListArtists()
      .then((rows) => {
        setArtists(rows ?? [])
        setError('')
      })
      .catch((err) => setError(toMessage(err)))
      // Cleared in both branches: a failed reload that left the page saying
      // "Loading…" forever is the one state with nothing to act on.
      .finally(() => setLoading(false))
  }, [])

  useEffect(reload, [reload])
  useDataChanged(['artists', 'albums'], reload)

  return (
    <ArtistsContext.Provider value={{artists, loading, error, reload}}>
      {children}
    </ArtistsContext.Provider>
  )
}

export const useArtists = () => useContext(ArtistsContext)
