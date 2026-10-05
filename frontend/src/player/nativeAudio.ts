import {PlayerLoad, PlayerPause, PlayerPlay, PlayerSeek, PlayerStop} from '../../wailsjs/go/app/App'
import {EventsOn} from '../../wailsjs/runtime/runtime'

/** One "player:state" event from the app's speaker. */
type SpeakerState = {
  generation: number
  loaded: boolean
  playing: boolean
  position: number
  duration: number
  ended: boolean
  error: string
}

/**
 * The player's speaker (#125): the part of an <audio> element the player
 * used, with the sound made by the app's own process instead of the
 * webview's.
 *
 * An <audio> element in the webview plays out of WebView2's processes, which
 * Windows does not count as part of aimusicmaker.exe for per-application
 * capture — OBS's Application Audio Capture heard silence. This keeps the
 * element's shape (play, pause, currentTime, duration, paused, and the
 * timeupdate / loadedmetadata / play / pause / ended / seeked / error events)
 * so the queue logic around it did not have to change, and forwards each call
 * to the backend's engine.
 *
 * State comes back as events tagged with the load they belong to; a report
 * about the previous song, still in flight when the next one loads, is
 * dropped rather than shown against the new one.
 */
export class NativeAudio extends EventTarget {
  private generation = 0
  private loading: Promise<void> = Promise.resolve()
  private time = 0
  private length = 0
  private stopped = true
  paused = true

  constructor() {
    super()
    EventsOn('player:state', (s: SpeakerState) => this.onState(s))
  }

  get currentTime() {
    return this.time
  }

  set currentTime(seconds: number) {
    // Said at once, the way an element does, so the bar does not jump back
    // until the engine catches up.
    this.time = seconds
    void this.loading.then(() =>
      PlayerSeek(seconds)
        .then(() => this.emit('seeked'))
        .catch(() => this.emit('error')),
    )
  }

  get duration() {
    return this.length
  }

  /** Loads a take, paused at its start — what setting src did. */
  load(fileId: string): Promise<void> {
    this.paused = true
    this.time = 0
    this.length = 0
    this.stopped = false
    this.loading = PlayerLoad(fileId).then(
      (loaded) => {
        this.generation = loaded.generation
        this.length = loaded.duration
        this.emit('loadedmetadata')
      },
      () => this.emit('error'),
    )
    return this.loading
  }

  play(): Promise<void> {
    this.paused = false
    return this.loading.then(() =>
      PlayerPlay().then(() => {
        this.emit('play')
      }),
    )
  }

  pause() {
    if (this.paused) return
    this.paused = true
    void this.loading.then(() => PlayerPause().then(() => this.emit('pause')))
  }

  /** Unloads — what removing src did when the queue emptied. */
  stop() {
    this.paused = true
    this.stopped = true
    this.time = 0
    this.length = 0
    void this.loading.then(() => PlayerStop())
  }

  private onState(s: SpeakerState) {
    if (s.generation !== this.generation || this.stopped) return
    this.time = s.position
    if (s.duration > 0 && s.duration !== this.length) {
      this.length = s.duration
      this.emit('loadedmetadata')
    }
    this.emit('timeupdate')
    if (s.error) this.emit('error')
    if (s.ended && !this.paused) {
      this.paused = true
      this.emit('pause')
      this.emit('ended')
    }
  }

  private emit(name: string) {
    this.dispatchEvent(new Event(name))
  }
}
