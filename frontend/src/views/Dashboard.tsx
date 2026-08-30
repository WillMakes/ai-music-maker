import {Disc3, Music4, Play, Sparkles} from 'lucide-react'
import {usePlayer} from '../player/PlayerProvider'
import {useLibrary} from '../library/LibraryProvider'
import type {Route} from '../App'

/**
 * Home, as the redesign draws it (#34): a greeting, the records to pick back
 * up, and the workflow in one line with the way to start.
 *
 * Deliberately short. What somebody wants on launch is the record they were
 * working on and the way to begin a new one — the library itself now lives in
 * the rail, so this page repeats none of it.
 */
export function Dashboard({onGo}: {onGo: (route: Route) => void}) {
  const {tree} = useLibrary()
  const player = usePlayer()

  // The records to pick back up: every album, most recently touched first,
  // four at most — a way back into work, not a second library.
  const resume = tree
    .flatMap((artist) =>
      artist.albums.map((album) => ({
        artistId: artist.id,
        artistName: artist.name,
        album,
      })),
    )
    .sort((a, b) => (b.album.updatedAt || '').localeCompare(a.album.updatedAt || ''))
    .slice(0, 4)

  return (
    <div className="mx-auto flex max-w-4xl flex-col gap-6">
      <header className="flex items-start gap-3">
        <div className="flex-1">
          <h1 className="text-xl font-semibold">{greeting()}.</h1>
          <p className="mt-1 text-sm text-fg-muted">
            Plan the record, make it in Suno, keep what comes back.
          </p>
        </div>
        {/* All the music in the system (#45), oldest artist to newest. */}
        {tree.length > 0 && (
          <button
            className="sm-button sm-button-quiet"
            type="button"
            onClick={() => void player.play('all', '')}
          >
            <Play size={15} />
            Play everything
          </button>
        )}
      </header>

      {resume.length > 0 && (
        <section className="space-y-2.5">
          <h2 className="text-xs font-semibold uppercase tracking-wider text-fg-muted">
            Pick up where you left off
          </h2>
          <div className="grid gap-2.5 sm:grid-cols-2">
            {resume.map(({artistId, artistName, album}) => (
              <button
                key={album.id}
                className="sm-panel flex items-center gap-3 p-3 text-left transition-colors hover:border-accent hover:bg-surface-hover"
                type="button"
                onClick={() => onGo({view: 'album-details', albumId: album.id, artistId})}
              >
                <span
                  className="flex size-11 shrink-0 items-center justify-center rounded bg-tile text-fg-muted"
                  aria-hidden="true"
                >
                  <Disc3 size={20} />
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block truncate font-semibold">{album.name}</span>
                  <span className="mt-px block text-xs text-fg-muted">{artistName}</span>
                  <span className="mt-1 block text-xs font-medium text-accent">
                    {album.trackCount === 0
                      ? 'Write the first track →'
                      : album.trackCount + (album.trackCount === 1 ? ' track →' : ' tracks →')}
                  </span>
                </span>
              </button>
            ))}
          </div>
        </section>
      )}

      <section className="flex items-center gap-3 rounded-lg bg-hero px-4 py-3 text-hero-fg">
        <p className="flex-1 text-sm">
          <strong>The workflow.</strong> Interview an artist → give them an album → write
          tracks → make them in Suno → bring the files back.
        </p>
        <button
          className="sm-button sm-button-primary shrink-0"
          type="button"
          onClick={() => onGo({view: 'artist-new'})}
        >
          <Sparkles size={15} />
          Create an artist
        </button>
      </section>

      {tree.length === 0 && (
        <section className="sm-panel flex flex-col items-center gap-2 p-8 text-center">
          <Music4 size={22} className="text-fg-muted" aria-hidden />
          <p className="font-medium">The library is empty.</p>
          <p className="max-w-md text-sm text-fg-muted">
            Everything starts with an artist: a conversation settles on a name and writes
            the base technical style every record inherits.
          </p>
        </section>
      )}
    </div>
  )
}

/** The hour's own greeting, the way the mockup opens. */
function greeting(): string {
  const hour = new Date().getHours()
  if (hour < 5) return 'Up late'
  if (hour < 12) return 'Good morning'
  if (hour < 18) return 'Good afternoon'
  return 'Good evening'
}
