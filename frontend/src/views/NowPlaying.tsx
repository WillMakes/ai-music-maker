import {ListMusic, Music4, Pause, Play, SkipBack, SkipForward} from 'lucide-react'
import {EmptyState} from '../components/EmptyState'
import {usePlayer} from '../player/PlayerProvider'
import {Cover} from './ArtistDetails'
import {duration as fmtDuration} from '../lib/format'

/**
 * Now Playing as a page (#45): the cover at full size, the names, and the
 * transport. The bar shows the same facts in one line; this is the view for
 * actually listening.
 */
export function NowPlaying({onOpenQueue}: {onOpenQueue: () => void}) {
  const player = usePlayer()
  const current = player.queue[player.index]

  if (!current) {
    return (
      <EmptyState
        icon={Music4}
        title="Nothing playing"
        body="Press play on a track, an album or an artist and it shows up here."
      />
    )
  }

  return (
    <div className="mx-auto flex max-w-md flex-col items-center gap-6 py-8">
      <Cover url={current.artworkUrl} name={current.albumName} size={280} />

      <div className="w-full text-center">
        <h1 className="truncate text-xl font-semibold">{current.name}</h1>
        <p className="mt-1 truncate text-sm text-fg-muted">
          {current.artistName} — {current.albumName}
        </p>
      </div>

      <div className="flex w-full items-center gap-2">
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

      <div className="flex items-center gap-3">
        <button
          className="sm-button sm-button-quiet px-3 py-2"
          type="button"
          title="Previous"
          aria-label="Previous"
          onClick={player.previous}
        >
          <SkipBack size={18} />
        </button>
        <button
          className="sm-button sm-button-primary px-4 py-2.5"
          type="button"
          title={player.playing ? 'Pause' : 'Play'}
          aria-label={player.playing ? 'Pause' : 'Play'}
          onClick={player.toggle}
        >
          {player.playing ? <Pause size={20} /> : <Play size={20} />}
        </button>
        <button
          className="sm-button sm-button-quiet px-3 py-2"
          type="button"
          title="Next"
          aria-label="Next"
          onClick={player.next}
        >
          <SkipForward size={18} />
        </button>
      </div>

      <button className="sm-button sm-button-quiet" type="button" onClick={onOpenQueue}>
        <ListMusic size={15} />
        Queue · {player.index + 1}/{player.queue.length}
      </button>

      {player.error && <p className="text-sm text-danger">{player.error}</p>}
    </div>
  )
}
