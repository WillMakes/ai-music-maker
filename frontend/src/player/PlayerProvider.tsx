import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import {Playlist, SetMediaKeys} from '../../wailsjs/go/app/App'
import {app} from '../../wailsjs/go/models'
import {EventsOn} from '../../wailsjs/runtime/runtime'
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

/** What a play button can name (#107 added playlists). */
export type PlayKind = 'track' | 'album' | 'artist' | 'playlist' | 'all'

export type PlayerState = {
  queue: app.PlayerTrack[]
  index: number
  playing: boolean
  position: number
  duration: number
  error: string
  /** Builds a queue on the backend and starts it from the top. */
  play: (kind: PlayKind, id: string, startTrackId?: string) => Promise<void>
  /** Appends to the queue instead of replacing it (#60); starts an idle player. */
  enqueue: (kind: PlayKind, id: string) => Promise<void>
  toggle: () => void
  next: () => void
  previous: () => void
  jumpTo: (index: number) => void
  seek: (seconds: number) => void
  clear: () => void
  /** Loop the queue when it ends (#66); off means play once and stop. */
  repeat: boolean
  toggleRepeat: () => void
  /** Pick the next song at random instead of in order (#66). */
  shuffle: boolean
  toggleShuffle: () => void
}

const PlayerContext = createContext<PlayerState | null>(null)

export function PlayerProvider({children}: {children: React.ReactNode}) {
  const audioRef = useRef<HTMLAudioElement | null>(null)
  const [queue, setQueue] = useState<app.PlayerTrack[]>([])
  const [index, setIndex] = useState(0)
  const [playing, setPlaying] = useState(false)
  const [position, setPosition] = useState(0)
  const [duration, setDuration] = useState(0)
  const [error, setError] = useState('')
  const [repeat, setRepeat] = useState(false)
  const [shuffle, setShuffle] = useState(false)

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
    // What happens when a song runs out (#66): in order by default, at
    // random under shuffle, back to the top under repeat, and quiet at the
    // end otherwise. When the answer is "the same song again" — repeat with
    // one entry — the index does not change and the load effect will not
    // re-run, so the restart happens by hand on the element.
    const onEnded = () => {
      const len = queueRef.current.length
      const i = indexRef.current
      let next = i + 1
      if (shuffleRef.current && len > 1) {
        next = Math.floor(Math.random() * (len - 1))
        if (next >= i) next++
      } else if (next >= len) {
        next = repeatRef.current ? 0 : i
      }
      if (next === i) {
        if (repeatRef.current || shuffleRef.current) {
          el.currentTime = 0
          void el.play().catch(() => {})
        }
        return
      }
      setIndex(next)
    }
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
  const repeatRef = useRef(repeat)
  repeatRef.current = repeat
  const shuffleRef = useRef(shuffle)
  shuffleRef.current = shuffle

  const play = useCallback(async (kind: PlayKind, id: string, startTrackId?: string) => {
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
      // A song pressed inside a playlist (#107) does the same: the whole
      // playlist, from that song.
      let start = 0
      const startAt = kind === 'track' ? id : startTrackId
      if (startAt) {
        start = tracks.findIndex((t) => t.trackId === startAt)
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

  // Add rather than replace (#60): the same backend queue-building as play,
  // appended behind what is already lined up. One deliberate difference from
  // play: enqueueing a track adds just that track, not its whole record — the
  // producer is composing a queue, and the record is one press away if they
  // want it. An idle player has nothing to add behind, so enqueue starts it.
  const enqueue = useCallback(
    async (kind: PlayKind, id: string) => {
      setError('')
      try {
        let tracks = await Playlist(kind, id)
        if (kind === 'track') {
          tracks = (tracks ?? []).filter((t) => t.trackId === id)
        }
        if (!tracks?.length) {
          setError('Nothing here has a take to play yet.')
          return
        }
        if (queueRef.current.length === 0) {
          setQueue(tracks)
          setIndex(0)
          return
        }
        setQueue((current) => [...current, ...tracks])
      } catch (err) {
        setError(toMessage(err))
      }
    },
    [],
  )

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
  const toggleRepeat = useCallback(() => setRepeat((on) => !on), [])
  const toggleShuffle = useCallback(() => setShuffle((on) => !on), [])

  // The keyboard's media keys (#101). WebView2 under Wails never passes them
  // to the Media Session below, so the backend holds them itself and sends
  // each press back as an event — and holds them only while there is a queue,
  // since a held key is taken from every other program on the machine.
  const hasQueue = queue.length > 0
  useEffect(() => {
    SetMediaKeys(hasQueue).catch((err) => setError(toMessage(err)))
  }, [hasQueue])
  useEffect(
    () =>
      EventsOn('player:media-key', (key: string) => {
        if (key === 'playpause') toggle()
        else if (key === 'next') next()
        else if (key === 'previous') previous()
        else if (key === 'stop') audio().pause()
      }),
    [toggle, next, previous],
  )

  // The OS's own controls (#45): metadata for the overlay, handlers for the
  // media keys, through the Media Session API. Kept for the webviews that do
  // forward it; on Windows the keys come through the effect above.
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
      enqueue,
      toggle,
      next,
      previous,
      jumpTo,
      seek,
      clear,
      repeat,
      toggleRepeat,
      shuffle,
      toggleShuffle,
    }),
    [queue, index, playing, position, duration, error, play, enqueue, toggle, next, previous, jumpTo, seek, clear, repeat, toggleRepeat, shuffle, toggleShuffle],
  )

  return <PlayerContext.Provider value={value}>{children}</PlayerContext.Provider>
}

export function usePlayer(): PlayerState {
  const state = useContext(PlayerContext)
  if (!state) throw new Error('usePlayer outside PlayerProvider')
  return state
}
