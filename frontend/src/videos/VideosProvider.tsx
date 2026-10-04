import {createContext, useContext, useEffect, useRef, useState, type ReactNode} from 'react'
import {RenderAlbumVideo} from '../../wailsjs/go/app/App'
import {EventsOn} from '../../wailsjs/runtime/runtime'
import {app} from '../../wailsjs/go/models'
import {toMessage} from '../lib/errors'

/**
 * One music-video render (#118), from the press to somebody acting on the
 * outcome — the shape of an artwork generation (#83), plus how far it is.
 */
export interface VideoJob {
  albumId: string
  albumName: string
  artistId: string
  state: 'running' | 'done' | 'failed'
  /** 0 to 1, from the render's "musicvideo:progress" events. */
  fraction: number
  error: string
  album: app.Album | null
}

const VideosContext = createContext<{
  videos: VideoJob[]
  render: (album: {id: string; name: string; artistId: string}) => void
  dismiss: (albumId: string) => void
}>({videos: [], render: () => {}, dismiss: () => {}})

/**
 * The music-video renders in flight, above the pages for the artwork
 * generations' reason (#83): a render runs for minutes and must outlive the
 * album page that started it, and the status bar shows it from anywhere.
 */
export function VideosProvider({children}: {children: ReactNode}) {
  const [videos, setVideos] = useState<VideoJob[]>([])
  // In a ref for ArtworksProvider's reason: two presses in one tick would
  // both read the state from before either.
  const runningRef = useRef(new Set<string>())

  useEffect(
    () =>
      EventsOn('musicvideo:progress', (event: {albumId: string; fraction: number}) =>
        setVideos((current) =>
          current.map((job) =>
            job.albumId === event.albumId && job.state === 'running'
              ? {...job, fraction: event.fraction}
              : job,
          ),
        ),
      ),
    [],
  )

  const upsert = (entry: VideoJob) =>
    setVideos((current) => [...current.filter((job) => job.albumId !== entry.albumId), entry])

  const render = (album: {id: string; name: string; artistId: string}) => {
    if (runningRef.current.has(album.id)) return
    runningRef.current.add(album.id)
    const base = {albumId: album.id, albumName: album.name, artistId: album.artistId}
    upsert({...base, state: 'running', fraction: 0, error: '', album: null})
    RenderAlbumVideo(album.id)
      .finally(() => runningRef.current.delete(album.id))
      .then((updated) => upsert({...base, state: 'done', fraction: 1, error: '', album: updated}))
      .catch((err) =>
        upsert({...base, state: 'failed', fraction: 0, error: toMessage(err), album: null}),
      )
  }

  const dismiss = (albumId: string) =>
    setVideos((current) => current.filter((job) => job.albumId !== albumId))

  return (
    <VideosContext.Provider value={{videos, render, dismiss}}>{children}</VideosContext.Provider>
  )
}

export const useVideos = () => useContext(VideosContext)
