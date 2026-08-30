import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import {Playlist} from '../../wailsjs/go/main/App'
import {main} from '../../wailsjs/go/models'
import {toMessage} from '../lib/errors'

/**
 * The player (#45): one audio element for the whole app, a queue, and the
 * Media Session hookup that hands the OS its play/pause/next keys.
 *
 * A provider rather than per-view state for the same reason the library tree
 * is one: the bar at the bottom, the Queue page and the Now Playing page are
 * three windows onto the same playback, and three copies of it would be three
 * songs at once. The <audio> lives outside React entirely — it must survive
 * every navigation, and no view owns it.
 */

export type PlayerState = {
  queue: main.PlayerTrack[]
  index: number
  playing: boolean
  position: number
  duration: number
  error: string
  /** Builds a queue on the backend and starts it from the top. */
  play: (kind: 'track' | 'album' | 'artist' | 'all', id: string) => Promise<void>
  toggle: () => void
  next: () => void
  previous: () => void
  jumpTo: (index: number) => void
  seek: (seconds: number) => void
  clear: () => void
}

const PlayerContext = createContext<PlayerState | null>(null)

export function PlayerProvider({children}: {children: React.ReactNode}) {
  const audioRef = useRef<HTMLAudioElement | null>(null)
  const [queue, setQueue] = useState<main.PlayerTrack[]>([])
  const [index, setIndex] = useState(0)
  const [playing, setPlaying] = useState(false)
  const [position, setPosition] = useState(0)
  const [duration, setDuration] = useState(0)
  const [error, setError] = useState('')

  const audio = () => {
    if (!audioRef.current) {
      audioRef.current = new Audio()
      audioRef.current.preload = 'auto'
    }
    return audioRef.current
  }

  const current = queue[index]

  // Load and start whenever the queue entry changes. Play is called from the
  // same effect that set the source: the browser allows it because the chain
  // began with a click, and splitting them apart would lose that.
  useEffect(() => {
    const el = audio()
    if (!current) {
      el.pause()
      el.removeAttribute('src')
      setPlaying(false)
      setPosition(0)
      setDuration(0)
      return
    }
    el.src = current.audioUrl
    el.play().catch((err) => setError(toMessage(err)))
  }, [current?.audioUrl])

  // The element is the source of truth for time and play state; React only
  // mirrors it, so a seek from the OS overlay shows up in the bar too.
  useEffect(() => {
    const el = audio()
    const onTime = () => setPosition(el.currentTime)
    const onMeta = () => setDuration(el.duration || 0)
    const onPlay = () => setPlaying(true)
    const onPause = () => setPlaying(false)
    const onEnded = () => setIndex((i) => (i + 1 < queueRef.current.length ? i + 1 : i))
    const onError = () => setError('This take could not be played.')
    el.addEventListener('timeupdate', onTime)
    el.addEventListener('loadedmetadata', onMeta)
    el.addEventListener('play', onPlay)
    el.addEventListener('pause', onPause)
    el.addEventListener('ended', onEnded)
    el.addEventListener('error', onError)
    return () => {
      el.removeEventListener('timeupdate', onTime)
      el.removeEventListener('loadedmetadata', onMeta)
      el.removeEventListener('play', onPlay)
      el.removeEventListener('pause', onPause)
      el.removeEventListener('ended', onEnded)
      el.removeEventListener('error', onError)
    }
  }, [])

  // 'ended' fires from a listener registered once, so it reads the queue
  // through a ref rather than a stale closure; previous reads the index the
  // same way.
  const queueRef = useRef(queue)
  queueRef.current = queue
  const indexRef = useRef(index)
  indexRef.current = index

  const play = useCallback(async (kind: 'track' | 'album' | 'artist' | 'all', id: string) => {
    setError('')
    try {
      const tracks = await Playlist(kind, id)
      if (!tracks?.length) {
        setError('Nothing here has a take to play yet.')
        return
      }
      // Playing one track queues its whole record (#55), started at the
      // track that was pressed — a single-entry queue left next and previous
      // with nowhere to go.
      let start = 0
      if (kind === 'track') {
        start = tracks.findIndex((t) => t.trackId === id)
        if (start < 0) {
          setError('This track has no take to play yet.')
          return
        }
      }
      setQueue(tracks)
      setIndex(start)
    } catch (err) {
      setError(toMessage(err))
    }
  }, [])

  const toggle = useCallback(() => {
    const el = audio()
    if (el.paused) {
      el.play().catch((err) => setError(toMessage(err)))
    } else {
      el.pause()
    }
  }, [])

  const next = useCallback(
    () => setIndex((i) => Math.min(i + 1, Math.max(queueRef.current.length - 1, 0))),
    [],
  )
  const previous = useCallback(() => {
    // The universal convention: early in a song, previous means the previous
    // song; later, it means the top of this one. On the first song there is
    // no previous one, so early presses restart it too — a button that does
    // nothing at all reads as broken (#55).
    const el = audio()
    if (el.currentTime > 3 || indexRef.current === 0) {
      el.currentTime = 0
      return
    }
    setIndex((i) => Math.max(i - 1, 0))
  }, [])
  const jumpTo = useCallback((i: number) => setIndex(i), [])
  const seek = useCallback((seconds: number) => {
    audio().currentTime = seconds
  }, [])
  const clear = useCallback(() => {
    setQueue([])
    setIndex(0)
  }, [])

  // The OS's own controls (#45): metadata for the overlay, handlers for the
  // media keys. WebView2 forwards both through the Media Session API.
  useEffect(() => {
    if (!('mediaSession' in navigator)) return
    const session = navigator.mediaSession
    if (current) {
      session.metadata = new MediaMetadata({
        title: current.name,
        artist: current.artistName,
        album: current.albumName,
        artwork: current.artworkUrl ? [{src: current.artworkUrl}] : [],
      })
    } else {
      session.metadata = null
    }
    session.setActionHandler('play', toggle)
    session.setActionHandler('pause', toggle)
    session.setActionHandler('previoustrack', previous)
    session.setActionHandler('nexttrack', next)
    session.setActionHandler('seekto', (details) => {
      if (details.seekTime != null) seek(details.seekTime)
    })
    return () => {
      session.setActionHandler('play', null)
      session.setActionHandler('pause', null)
      session.setActionHandler('previoustrack', null)
      session.setActionHandler('nexttrack', null)
      session.setActionHandler('seekto', null)
    }
  }, [current, toggle, previous, next, seek])

  const value = useMemo(
    () => ({
      queue,
      index,
      playing,
      position,
      duration,
      error,
      play,
      toggle,
      next,
      previous,
      jumpTo,
      seek,
      clear,
    }),
    [queue, index, playing, position, duration, error, play, toggle, next, previous, jumpTo, seek, clear],
  )

  return <PlayerContext.Provider value={value}>{children}</PlayerContext.Provider>
}

export function usePlayer(): PlayerState {
  const state = useContext(PlayerContext)
  if (!state) throw new Error('usePlayer outside PlayerProvider')
  return state
}
