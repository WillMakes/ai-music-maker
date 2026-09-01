import {createContext, useContext, useRef, useState, type ReactNode} from 'react'
import {EvolveAlbum} from '../../wailsjs/go/main/App'
import {toMessage} from '../lib/errors'

/**
 * One evolution, from the press of the button to somebody acting on the
 * outcome. `done` and `failed` entries stay until dismissed — an outcome that
 * vanished with the page that started it is the bug this provider exists to
 * fix (#82).
 */
export interface Evolution {
  /** The source album — the one the button was pressed on. */
  albumId: string
  albumName: string
  artistId: string
  state: 'running' | 'done' | 'failed'
  /** What went wrong, for the album page to show where the button is. */
  error: string
  /** The successor, once there is one. */
  nextAlbumId: string
  nextAlbumName: string
}

const EvolutionsContext = createContext<{
  evolutions: Evolution[]
  evolve: (album: {id: string; name: string; artistId: string}) => void
  dismiss: (albumId: string) => void
}>({evolutions: [], evolve: () => {}, dismiss: () => {}})

/**
 * The evolutions in flight, held above the pages (#82).
 *
 * The Wails call itself never needed the album page: a binding runs to
 * completion whether or not the component that started it is still mounted.
 * What was lost on navigation was the only *record* of it — a `useState` in
 * the page that unmounted. So the state moves up here, where the status bar
 * can show it from anywhere and the album page finds it again on return; the
 * call is otherwise the same one the button always made.
 */
export function EvolutionsProvider({children}: {children: ReactNode}) {
  const [evolutions, setEvolutions] = useState<Evolution[]>([])
  // The in-flight sources, in a ref rather than derived from state: two
  // presses in the same tick would both read the pre-press state and start
  // twin calls.
  const runningRef = useRef(new Set<string>())

  const upsert = (entry: Evolution) =>
    setEvolutions((current) => [
      ...current.filter((e) => e.albumId !== entry.albumId),
      entry,
    ])

  const evolve = (album: {id: string; name: string; artistId: string}) => {
    // One evolution per source at a time: a second press while the first is
    // in flight is a double-click, not a request for twin successors.
    if (runningRef.current.has(album.id)) return
    runningRef.current.add(album.id)
    upsert({
      albumId: album.id,
      albumName: album.name,
      artistId: album.artistId,
      state: 'running',
      error: '',
      nextAlbumId: '',
      nextAlbumName: '',
    })
    EvolveAlbum(album.id)
      .finally(() => runningRef.current.delete(album.id))
      .then((next) =>
        upsert({
          albumId: album.id,
          albumName: album.name,
          artistId: album.artistId,
          state: 'done',
          error: '',
          nextAlbumId: next.id,
          nextAlbumName: next.name,
        }),
      )
      .catch((err) =>
        upsert({
          albumId: album.id,
          albumName: album.name,
          artistId: album.artistId,
          state: 'failed',
          error: toMessage(err),
          nextAlbumId: '',
          nextAlbumName: '',
        }),
      )
  }

  const dismiss = (albumId: string) =>
    setEvolutions((current) => current.filter((e) => e.albumId !== albumId))

  return (
    <EvolutionsContext.Provider value={{evolutions, evolve, dismiss}}>
      {children}
    </EvolutionsContext.Provider>
  )
}

export const useEvolutions = () => useContext(EvolutionsContext)
