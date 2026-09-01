import {createContext, useContext, useRef, useState, type ReactNode} from 'react'
import {GenerateAlbumArtwork} from '../../wailsjs/go/main/App'
import {main} from '../../wailsjs/go/models'
import {toMessage} from '../lib/errors'

/**
 * One artwork generation, from the press of the button to somebody acting on
 * the outcome. `done` and `failed` entries stay until dismissed — the album
 * page dismisses `done` the moment it adopts the cover, and `failed` when the
 * producer redraws (#83).
 */
export interface ArtworkJob {
  albumId: string
  albumName: string
  artistId: string
  state: 'running' | 'done' | 'failed'
  /** What went wrong, for the album page to show beside the button. */
  error: string
  /**
   * The record as the generation returned it. The page adopts only the
   * artwork fields from this — the producer may have kept editing the rest.
   */
  album: main.Album | null
}

const ArtworksContext = createContext<{
  artworks: ArtworkJob[]
  draw: (album: {id: string; name: string; artistId: string}, feedback: string) => void
  dismiss: (albumId: string) => void
}>({artworks: [], draw: () => {}, dismiss: () => {}})

/**
 * The artwork generations in flight, held above the pages (#83) — the same
 * move the evolutions made (#82), for the same reason: the call runs for
 * minutes and never needed the page, but a `useState` in the page was its only
 * record, so navigating away lost the spinner, the outcome and the error. The
 * status bar shows every one from anywhere; the album page finds its own
 * again on return.
 */
export function ArtworksProvider({children}: {children: ReactNode}) {
  const [artworks, setArtworks] = useState<ArtworkJob[]>([])
  // The in-flight sources, in a ref rather than derived from state: two
  // presses in the same tick would both read the pre-press state and start
  // twin generations.
  const runningRef = useRef(new Set<string>())

  const upsert = (entry: ArtworkJob) =>
    setArtworks((current) => [
      ...current.filter((job) => job.albumId !== entry.albumId),
      entry,
    ])

  const draw = (album: {id: string; name: string; artistId: string}, feedback: string) => {
    if (runningRef.current.has(album.id)) return
    runningRef.current.add(album.id)
    upsert({
      albumId: album.id,
      albumName: album.name,
      artistId: album.artistId,
      state: 'running',
      error: '',
      album: null,
    })
    GenerateAlbumArtwork(album.id, feedback)
      .finally(() => runningRef.current.delete(album.id))
      .then((updated) =>
        upsert({
          albumId: album.id,
          albumName: album.name,
          artistId: album.artistId,
          state: 'done',
          error: '',
          album: updated,
        }),
      )
      .catch((err) =>
        upsert({
          albumId: album.id,
          albumName: album.name,
          artistId: album.artistId,
          state: 'failed',
          error: toMessage(err),
          album: null,
        }),
      )
  }

  const dismiss = (albumId: string) =>
    setArtworks((current) => current.filter((job) => job.albumId !== albumId))

  return (
    <ArtworksContext.Provider value={{artworks, draw, dismiss}}>
      {children}
    </ArtworksContext.Provider>
  )
}

export const useArtworks = () => useContext(ArtworksContext)
