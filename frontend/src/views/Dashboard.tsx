import {Bug, Music4, Sparkles} from 'lucide-react'
import {useArtists} from '../artists/ArtistsProvider'
import {useBugs} from '../bugs/BugsProvider'
import {PageHeader} from '../components/PageHeader'
import type {ViewId} from '../navigation'

/**
 * Where the app opens.
 *
 * Deliberately short. What somebody wants on launch is the record they were
 * working on and the way to start a new one — not a wall of statistics about a
 * library they can already see.
 */
export function Dashboard({
  onNavigate,
  onOpenArtist,
}: {
  onNavigate: (view: ViewId) => void
  onOpenArtist: (artistId: string) => void
}) {
  const {artists, loading} = useArtists()
  const {counts} = useBugs()

  const albums = artists.reduce((total, artist) => total + artist.albumCount, 0)
  // Most recently touched first, and only a handful: this is a way back into
  // work, not a second Artists page.
  const recent = [...artists]
    .sort((a, b) => (b.updatedAt || '').localeCompare(a.updatedAt || ''))
    .slice(0, 4)

  return (
    <div className="space-y-6">
      <PageHeader
        title="SunoMaker"
        subtitle="Plan the record, make it in Suno, keep what comes back."
      />

      <section className="rounded-lg bg-hero p-6 text-hero-fg">
        <h2 className="text-lg font-semibold">The workflow</h2>
        <ol className="mt-3 grid gap-2 text-sm sm:grid-cols-2 xl:grid-cols-4">
          {[
            'Interview an artist into existence — a name, and the sound that is theirs.',
            'Give them an album: what is different about this record, and a cover.',
            'Write tracks against the combined style. Titles, prompts, lyrics.',
            'Make them in Suno with the extension, then bring the files back.',
          ].map((step, index) => (
            <li key={index} className="flex gap-2">
              <span className="font-semibold tabular-nums opacity-70">{index + 1}</span>
              <span>{step}</span>
            </li>
          ))}
        </ol>
      </section>

      <section className="grid gap-3 sm:grid-cols-3">
        <Count label="Artists" value={loading ? '—' : String(artists.length)} />
        <Count label="Albums" value={loading ? '—' : String(albums)} />
        <Count label="Open bug reports" value={String(counts.open)} />
      </section>

      <section className="space-y-3">
        <div className="flex items-center gap-3">
          <h2 className="flex-1 text-sm font-semibold">Pick up where you left off</h2>
          <button
            className="sm-button sm-button-quiet"
            type="button"
            onClick={() => onNavigate('artists')}
          >
            <Music4 size={15} />
            All artists
          </button>
        </div>
        {recent.length === 0 ? (
          <div className="sm-panel flex flex-col items-center gap-3 px-6 py-10 text-center">
            <Sparkles className="text-fg-muted" size={26} />
            <p className="text-sm text-fg-muted">
              Nothing here yet. An artist is the first thing to make.
            </p>
            <button
              className="sm-button sm-button-primary"
              type="button"
              onClick={() => onNavigate('artists')}
            >
              Create an artist
            </button>
          </div>
        ) : (
          <ul className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            {recent.map((artist) => (
              <li key={artist.id}>
                <button
                  className="sm-panel flex w-full flex-col gap-1 p-4 text-left hover:bg-surface-hover"
                  type="button"
                  onClick={() => onOpenArtist(artist.id)}
                >
                  <span className="truncate font-medium">{artist.name}</span>
                  <span className="text-xs text-fg-muted">
                    {artist.albumCount === 1 ? '1 album' : artist.albumCount + ' albums'}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>

      {counts.unseen > 0 && (
        <section className="sm-panel flex items-center gap-3 p-4">
          <Bug className="text-ok" size={18} />
          <p className="flex-1 text-sm">
            {counts.unseen === 1
              ? 'A bug you reported has been fixed.'
              : counts.unseen + ' bugs you reported have been fixed.'}
          </p>
          <button
            className="sm-button sm-button-quiet"
            type="button"
            onClick={() => onNavigate('settings-bugs')}
          >
            See them
          </button>
        </section>
      )}
    </div>
  )
}

function Count({label, value}: {label: string; value: string}) {
  return (
    <div className="sm-panel p-4">
      <p className="text-2xl font-semibold tabular-nums">{value}</p>
      <p className="text-sm text-fg-muted">{label}</p>
    </div>
  )
}
