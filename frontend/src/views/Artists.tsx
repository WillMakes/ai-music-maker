import {useState} from 'react'
import {Music4, Plus, Sparkles} from 'lucide-react'
import {SaveArtist} from '../../wailsjs/go/main/App'
import {main} from '../../wailsjs/go/models'
import {EmptyState} from '../components/EmptyState'
import {PageHeader} from '../components/PageHeader'
import {useArtists} from '../artists/ArtistsProvider'
import {toMessage} from '../lib/errors'

/**
 * Every artist.
 *
 * Two ways in, and the interview is the recommended one: the app's whole model
 * hangs off an artist's base technical style, and a producer who types a name
 * into a form and presses Save has an artist with no sound. The blank one exists
 * anyway, because somebody importing a project they already have should not be
 * made to describe it to a model first.
 */
export function Artists({
  onOpen,
  onCreate,
}: {
  onOpen: (artistId: string) => void
  /** Opens the Create an artist page — a route of its own since #17. */
  onCreate: () => void
}) {
  const {artists, loading, error} = useArtists()
  const [creating, setCreating] = useState(false)
  const [problem, setProblem] = useState('')

  const createBlank = async () => {
    setCreating(true)
    setProblem('')
    try {
      const artist = await SaveArtist(
        main.Artist.createFrom({name: 'New artist', inspirations: []}),
      )
      onOpen(artist.id)
    } catch (err) {
      setProblem(toMessage(err))
    } finally {
      setCreating(false)
    }
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title="Artists"
        subtitle="Each artist has a base technical style that every album under them inherits."
        actions={
          <>
            <button
              className="sm-button sm-button-quiet"
              type="button"
              onClick={createBlank}
              disabled={creating}
            >
              <Plus size={15} />
              Blank artist
            </button>
            <button className="sm-button sm-button-primary" type="button" onClick={onCreate}>
              <Sparkles size={15} />
              Create an artist
            </button>
          </>
        }
      />

      {(error || problem) && <p className="text-sm text-danger">{error || problem}</p>}

      {loading ? (
        <p className="text-sm text-fg-muted">Loading…</p>
      ) : artists.length === 0 ? (
        <EmptyState
          icon={Music4}
          title="No artists yet"
          body="Start with a conversation about what the music is. The interview settles on a name and writes the base technical style from your answers."
          action={
            <button className="sm-button sm-button-primary" type="button" onClick={onCreate}>
              <Sparkles size={15} />
              Create an artist
            </button>
          }
        />
      ) : (
        <ul className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {artists.map((artist) => (
            <li key={artist.id}>
              <button
                className="sm-panel flex w-full flex-col gap-1.5 p-4 text-left hover:bg-surface-hover"
                type="button"
                onClick={() => onOpen(artist.id)}
              >
                <span className="truncate font-medium">{artist.name}</span>
                <span className="line-clamp-2 min-h-8 text-sm text-fg-muted">
                  {artist.summary || 'No description yet.'}
                </span>
                <span className="text-xs text-fg-muted">
                  {artist.albumCount === 1 ? '1 album' : artist.albumCount + ' albums'}
                  {artist.baseStyle ? '' : ' · no style written yet'}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}

    </div>
  )
}
