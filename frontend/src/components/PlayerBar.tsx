import {ListMusic, Pause, Play, SkipBack, SkipForward} from 'lucide-react'
import {usePlayer} from '../player/PlayerProvider'
import {Cover} from '../views/ArtistDetails'
import {duration as fmtDuration} from '../lib/format'

/**
 * The bar at the bottom of the application (#45): what is playing, the
 * transport, and the way into the Queue and Now Playing pages.
 *
 * Rendered only while there is a queue — an empty player bar is furniture,
 * and the app spent its first thirty issues earning that space back.
 */
export function PlayerBar({
  onOpenNowPlaying,
  onOpenQueue,
}: {
  onOpenNowPlaying: () => void
  onOpenQueue: () => void
}) {
  const player = usePlayer()
  const current = player.queue[player.index]
  if (!current) return null

  return (
    <div className="flex items-center gap-3 border-t border-edge bg-surface px-4 py-2">
      <button
        className="flex min-w-0 items-center gap-3 text-left"
        type="button"
        title="Open Now Playing"
        onClick={onOpenNowPlaying}
      >
        <Cover url={current.artworkUrl} name={current.albumName} size={36} />
        <span className="min-w-0">
          <span className="block truncate text-sm font-medium">{current.name}</span>
          <span className="block truncate text-xs text-fg-muted">
            {current.artistName} — {current.albumName}
          </span>
        </span>
      </button>

      <div className="flex shrink-0 items-center gap-1">
        <button
          className="sm-button sm-button-quiet px-2"
          type="button"
          title="Previous"
          aria-label="Previous"
          onClick={player.previous}
        >
          <SkipBack size={15} />
        </button>
        <button
          className="sm-button sm-button-primary px-2.5"
          type="button"
          title={player.playing ? 'Pause' : 'Play'}
          aria-label={player.playing ? 'Pause' : 'Play'}
          onClick={player.toggle}
        >
          {player.playing ? <Pause size={16} /> : <Play size={16} />}
        </button>
        <button
          className="sm-button sm-button-quiet px-2"
          type="button"
          title="Next"
          aria-label="Next"
          onClick={player.next}
        >
          <SkipForward size={15} />
        </button>
      </div>

      <div className="flex min-w-0 flex-1 items-center gap-2">
        <span className="shrink-0 text-xs tabular-nums text-fg-muted">
          {fmtDuration(Math.floor(player.position))}
        </span>
        <input
          className="min-w-0 flex-1 accent-accent"
          type="range"
          min={0}
          max={Math.max(player.duration, 1)}
          step={1}
          value={Math.min(player.position, player.duration || 0)}
          aria-label="Seek"
          onChange={(e) => player.seek(Number(e.target.value))}
        />
        <span className="shrink-0 text-xs tabular-nums text-fg-muted">
          {fmtDuration(Math.floor(player.duration))}
        </span>
      </div>

      <button
        className="sm-button sm-button-quiet px-2"
        type="button"
        title="Open the queue"
        aria-label="Open the queue"
        onClick={onOpenQueue}
      >
        <ListMusic size={15} />
        <span className="text-xs tabular-nums">
          {player.index + 1}/{player.queue.length}
        </span>
      </button>
    </div>
  )
}
