import {createContext, useContext, useEffect, useRef, useState, type ReactNode} from 'react'
import {RenderAlbumVideo, UploadAlbumToYouTubeMusic, UploadAlbumVideo} from '../../wailsjs/go/app/App'
import {EventsOn} from '../../wailsjs/runtime/runtime'
import {app} from '../../wailsjs/go/models'
import {toMessage} from '../lib/errors'

/**
 * One long job on an album's music video, from the press to somebody acting
 * on the outcome — the shape of an artwork generation (#83), plus how far it
 * is. A render (#118) makes the file; an upload (#122) sends it to YouTube;
 * a library upload (#124) sends the album's takes to YouTube Music.
 */
export interface VideoJob {
  albumId: string
  albumName: string
  artistId: string
  kind: 'render' | 'upload' | 'ytmusic'
  state: 'running' | 'done' | 'failed'
  /** 0 to 1, from the job's progress events. */
  fraction: number
  error: string
  album: app.Album | null
}

type AlbumRef = {id: string; name: string; artistId: string}

const VideosContext = createContext<{
  videos: VideoJob[]
  render: (album: AlbumRef) => void
  upload: (album: AlbumRef, privacy: string) => void
  uploadToYouTubeMusic: (album: AlbumRef) => void
  dismiss: (albumId: string, kind: VideoJob['kind']) => void
}>({
  videos: [],
  render: () => {},
  upload: () => {},
  uploadToYouTubeMusic: () => {},
  dismiss: () => {},
})

const PROGRESS_EVENTS: Record<VideoJob['kind'], string> = {
  render: 'musicvideo:progress',
  upload: 'youtubeupload:progress',
  ytmusic: 'ytmusicupload:progress',
}

/**
 * The music-video jobs in flight, above the pages for the artwork
 * generations' reason (#83): each runs for minutes and must outlive the album
 * page that started it, and the status bar shows it from anywhere.
 */
export function VideosProvider({children}: {children: ReactNode}) {
  const [videos, setVideos] = useState<VideoJob[]>([])
  // In a ref for ArtworksProvider's reason: two presses in one tick would
  // both read the state from before either.
  const runningRef = useRef(new Set<string>())

  useEffect(() => {
    const offs = (Object.keys(PROGRESS_EVENTS) as VideoJob['kind'][]).map((kind) =>
      EventsOn(PROGRESS_EVENTS[kind], (event: {albumId: string; fraction: number}) =>
        setVideos((current) =>
          current.map((job) =>
            job.albumId === event.albumId && job.kind === kind && job.state === 'running'
              ? {...job, fraction: event.fraction}
              : job,
          ),
        ),
      ),
    )
    return () => offs.forEach((off) => off())
  }, [])

  const upsert = (entry: VideoJob) =>
    setVideos((current) => [
      ...current.filter((job) => !(job.albumId === entry.albumId && job.kind === entry.kind)),
      entry,
    ])

  const start = (album: AlbumRef, kind: VideoJob['kind'], call: () => Promise<app.Album>) => {
    const key = kind + ':' + album.id
    if (runningRef.current.has(key)) return
    runningRef.current.add(key)
    const base = {albumId: album.id, albumName: album.name, artistId: album.artistId, kind}
    upsert({...base, state: 'running', fraction: 0, error: '', album: null})
    call()
      .finally(() => runningRef.current.delete(key))
      .then((updated) => upsert({...base, state: 'done', fraction: 1, error: '', album: updated}))
      .catch((err) =>
        upsert({...base, state: 'failed', fraction: 0, error: toMessage(err), album: null}),
      )
  }

  const render = (album: AlbumRef) => start(album, 'render', () => RenderAlbumVideo(album.id))
  const upload = (album: AlbumRef, privacy: string) =>
    start(album, 'upload', () => UploadAlbumVideo(album.id, privacy))
  const uploadToYouTubeMusic = (album: AlbumRef) =>
    start(album, 'ytmusic', () => UploadAlbumToYouTubeMusic(album.id))

  const dismiss = (albumId: string, kind: VideoJob['kind']) =>
    setVideos((current) => current.filter((job) => !(job.albumId === albumId && job.kind === kind)))

  return (
    <VideosContext.Provider value={{videos, render, upload, uploadToYouTubeMusic, dismiss}}>
      {children}
    </VideosContext.Provider>
  )
}

export const useVideos = () => useContext(VideosContext)
