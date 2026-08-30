import {ListMusic, Play, Trash2, Volume2} from 'lucide-react'
import {EmptyState} from '../components/EmptyState'
import {PageHeader} from '../components/PageHeader'
import {usePlayer} from '../player/PlayerProvider'
import {Cover} from './ArtistDetails'

/**
 * The play queue as a page (#45): what is lined up, where playback is in it,
 * and a click to jump anywhere. The queue is playback state, not library
 * state — clearing it deletes nothing but the plan.
 */
export function Queue() {
  const player = usePlayer()

  return (
    <div className="space-y-5">
      <PageHeader
        title="Queue"
        subtitle={
          player.queue.length === 0
            ? 'Nothing lined up.'
            : player.queue.length + (player.queue.length === 1 ? ' track' : ' tracks')
        }
        actions={
          player.queue.length > 0 && (
            <button
              className="sm-button sm-button-quiet"
              type="button"
              onClick={player.clear}
            >
              <Trash2 size={15} />
              Clear the queue
            </button>
          )
        }
      />

      {player.queue.length === 0 ? (
        <EmptyState
          icon={ListMusic}
          title="The queue is empty"
          body="Press play on a track, an album or an artist and it lines up here."
        />
      ) : (
        <ul className="sm-panel divide-y divide-edge overflow-hidden">
          {player.queue.map((track, i) => (
            <li key={track.trackId + '-' + i}>
              <button
                className={
                  'group flex w-full items-center gap-3 px-4 py-2.5 text-left ' +
                  (i === player.index ? 'bg-surface-hover' : 'hover:bg-surface-hover')
                }
                type="button"
                onClick={() => player.jumpTo(i)}
              >
                <span className="flex w-6 shrink-0 items-center justify-center text-xs tabular-nums text-fg-muted">
                  {i === player.index && player.playing ? (
                    <Volume2 size={14} className="text-accent" />
                  ) : (
                    <>
                      <span className="group-hover:hidden">{i + 1}</span>
                      <Play size={13} className="hidden group-hover:block" />
                    </>
                  )}
                </span>
                <Cover url={track.artworkUrl} name={track.albumName} size={32} />
                <span className="min-w-0 flex-1">
                  <span
                    className={
                      'block truncate text-sm ' +
                      (i === player.index ? 'font-semibold' : 'font-medium')
                    }
                  >
                    {track.name}
                  </span>
                  <span className="block truncate text-xs text-fg-muted">
                    {track.artistName} — {track.albumName}
                  </span>
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
