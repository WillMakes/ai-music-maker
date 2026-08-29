import {useCallback, useEffect, useState} from 'react'
import {AudioLines, Disc3, ExternalLink, Loader2, Plus, Sparkles, Trash2, Wand2} from 'lucide-react'
import {
  AnalyzeMusicURL,
  ClearChat,
  MergeStyleWithAnalysis,
  DeleteArtist,
  ExtractAlbum,
  ExtractTracks,
  GetArtist,
  ListAlbums,
  OpenURL,
  ImportTrackFile,
  SaveAlbum,
  SaveTrack,
  SaveArtist,
} from '../../wailsjs/go/main/App'
import {main} from '../../wailsjs/go/models'
import {EmptyState} from '../components/EmptyState'
import {PageHeader} from '../components/PageHeader'
import {TextAreaField, TextField} from '../components/Field'
import {InterviewDialog} from '../chat/InterviewDialog'
import {StyleEditAssistant} from '../artists/StyleEditAssistant'
import {useDataChanged} from '../lib/dataChanged'
import {toMessage} from '../lib/errors'
import {attachmentPathsByName, isAudioPath} from '../lib/files'

/**
 * One artist: who they are, what they sound like, what they were listening to,
 * and their records.
 *
 * The fields are edited in place and saved explicitly rather than on every
 * keystroke. A base technical style is a paragraph somebody works on for several
 * minutes, and autosaving it would write forty half-finished versions and
 * re-render the page under them each time.
 */
export function ArtistDetails({
  artistId,
  onOpenAlbum,
  onDeleted,
}: {
  artistId: string
  onOpenAlbum: (albumId: string) => void
  onDeleted: () => void
}) {
  const [artist, setArtist] = useState<main.Artist | null>(null)
  const [albums, setAlbums] = useState<main.Album[]>([])
  const [draft, setDraft] = useState<main.Artist | null>(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [interviewing, setInterviewing] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  // The base-style refinement chat (#30), for an artist that already exists.
  const [refining, setRefining] = useState(false)

  const reload = useCallback(() => {
    Promise.all([GetArtist(artistId), ListAlbums(artistId)])
      .then(([found, rows]) => {
        setArtist(found)
        // The draft is only seeded when there is not one, so a reload triggered
        // by a background write does not throw away what is being typed.
        setDraft((current) => current ?? main.Artist.createFrom(found))
        setAlbums(rows ?? [])
        setError('')
      })
      .catch((err) => setError(toMessage(err)))
  }, [artistId])

  useEffect(reload, [reload])
  useDataChanged(['artists', 'albums'], reload)

  const update = (patch: Partial<main.Artist>) =>
    setDraft((current) => (current ? main.Artist.createFrom({...current, ...patch}) : current))

  const save = async () => {
    if (!draft) return
    setSaving(true)
    setError('')
    try {
      const saved = await SaveArtist(draft)
      setArtist(saved)
      setDraft(main.Artist.createFrom(saved))
    } catch (err) {
      setError(toMessage(err))
    } finally {
      setSaving(false)
    }
  }

  const remove = async () => {
    try {
      await DeleteArtist(artistId)
      onDeleted()
    } catch (err) {
      setError(toMessage(err))
    }
  }

  if (!artist || !draft) {
    return <p className="text-sm text-fg-muted">{error || 'Loading…'}</p>
  }

  const dirty = JSON.stringify(draft) !== JSON.stringify(artist)

  return (
    <div className="space-y-5">
      <PageHeader
        title={artist.name}
        breadcrumb="Artists"
        subtitle={artist.summary || 'No description yet.'}
        actions={
          <>
            <button
              className="sm-button sm-button-danger"
              type="button"
              onClick={() => setConfirmDelete(true)}
            >
              <Trash2 size={15} />
              Delete
            </button>
            <button
              className="sm-button sm-button-primary"
              type="button"
              onClick={save}
              disabled={!dirty || saving}
            >
              {saving ? 'Saving…' : dirty ? 'Save changes' : 'Saved'}
            </button>
          </>
        }
      />

      {error && <p className="text-sm text-danger">{error}</p>}

      {confirmDelete && (
        <div className="sm-panel space-y-3 border-danger p-4">
          <p className="text-sm">
            Delete <strong>{artist.name}</strong>, their {albums.length} album
            {albums.length === 1 ? '' : 's'} and every track under them? The audio
            files stay in your music folder.
          </p>
          <div className="flex gap-2">
            <button className="sm-button sm-button-danger" type="button" onClick={remove}>
              Delete the artist
            </button>
            <button
              className="sm-button sm-button-quiet"
              type="button"
              onClick={() => setConfirmDelete(false)}
            >
              Keep it
            </button>
          </div>
        </div>
      )}

      <section className="sm-panel space-y-4 p-4">
        <div className="flex items-center gap-3">
          <h2 className="flex-1 text-sm font-semibold">The artist</h2>
          {/* The interview wrote the base style once; this is how it gets
              better afterwards (#30) — a conversation that can be handed
              YouTube references and ends in the form, not the database. */}
          <button
            className="sm-button sm-button-quiet"
            type="button"
            onClick={() => setRefining(true)}
          >
            <Wand2 size={15} />
            Refine style with AI
          </button>
        </div>
        <div className="grid gap-4 md:grid-cols-2">
          <TextField
            label="Name"
            required
            value={draft.name}
            onChange={(name) => update({name})}
          />
          <TextField
            label="Summary"
            hint="One line, for the card and the header."
            value={draft.summary}
            onChange={(summary) => update({summary})}
          />
        </div>
        <TextAreaField
          label="Base technical style"
          mono
          rows={10}
          hint="What every record by this artist sounds like. Dense and comma-led — instruments, production, tempo range, vocal treatment, mix, and what to avoid. Combined with each album's own style when a track prompt is written."
          value={draft.baseStyle}
          onChange={(baseStyle) => update({baseStyle})}
        />
      </section>

      <Inspirations
        inspirations={draft.inspirations ?? []}
        onChange={(inspirations) => update({inspirations})}
        // Adding a reference updates the style (#31): the analysis that just
        // filled the note is folded into the base style *draft*, and the
        // producer saves when it reads right — the AI writes, it does not
        // take the pen.
        onMergeStyle={async (analysis) => {
          const merged = await MergeStyleWithAnalysis(draft.baseStyle, analysis)
          update({baseStyle: merged})
        }}
      />

      <section className="space-y-3">
        <div className="flex items-center gap-3">
          <h2 className="flex-1 text-sm font-semibold">Albums</h2>
          <button
            className="sm-button sm-button-primary"
            type="button"
            onClick={() => setInterviewing(true)}
          >
            <Sparkles size={15} />
            Create an album
          </button>
        </div>

        {albums.length === 0 ? (
          <EmptyState
            icon={Disc3}
            title="No albums yet"
            body="An album carries what is different about one record — the artist's base style is already inherited."
          />
        ) : (
          <ul className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {albums.map((album) => (
              <li key={album.id}>
                <button
                  className="sm-panel flex w-full items-center gap-3 p-3 text-left hover:bg-surface-hover"
                  type="button"
                  onClick={() => onOpenAlbum(album.id)}
                >
                  <Cover url={album.artworkUrl} name={album.name} />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate font-medium">{album.name}</span>
                    <span className="block text-xs text-fg-muted">
                      {album.trackCount === 1 ? '1 track' : album.trackCount + ' tracks'}
                    </span>
                  </span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>

      {refining && (
        <StyleEditAssistant
          artistId={artist.id}
          // Into the draft the producer is already editing: they read what the
          // model wrote, adjust it, and press Save themselves.
          onApply={(baseStyle) => update({baseStyle})}
          onClose={() => setRefining(false)}
        />
      )}

      {interviewing && (
        <InterviewDialog
          title={'New album for ' + artist.name}
          subjectKind="album"
          subjectId={artist.id}
          opening={
            'A new record for **' +
            artist.name +
            '**.\n\nWhat is this one? A theme, an occasion, roughly how many tracks — and ' +
            'anything that has shifted from their usual sound. If nothing has, say so ' +
            'and we will go straight to the name.'
          }
          finishLabel="Create album"
          onFinish={async (messages) => {
            try {
              const albumDraft: main.AlbumDraft = await ExtractAlbum(messages)
              const album = await SaveAlbum(
                main.Album.createFrom({
                  artistId: artist.id,
                  name: albumDraft.name || 'New album',
                  technicalStyle: albumDraft.technicalStyle,
                  artworkPrompt: albumDraft.artworkPrompt,
                }),
              )
              // Audio dropped into this chat is finished tracks (#24): run the
              // track extraction over the same conversation and create them on
              // the album that now exists, each with its file imported. Only
              // when there is audio — an album interview with no files has not
              // discussed a running order, and extracting tracks from it would
              // invent one.
              const attached = attachmentPathsByName(messages)
              if (Object.keys(attached).some(isAudioPath)) {
                const drafts = await ExtractTracks(messages).catch(() => [])
                for (const track of drafts ?? []) {
                  const saved = await SaveTrack(
                    main.Track.createFrom({
                      albumId: album.id,
                      name: track.name || 'Untitled',
                      prompt: track.prompt,
                      lyrics: track.lyrics,
                      targetSeconds: track.targetSeconds,
                      files: [],
                    }),
                  )
                  for (const name of track.files ?? []) {
                    const path = attached[name]
                    if (path) await ImportTrackFile(saved.id, path, '')
                  }
                }
              }
              await ClearChat('album', artist.id).catch(() => {})
              setInterviewing(false)
              onOpenAlbum(album.id)
              return ''
            } catch (err) {
              return toMessage(err)
            }
          }}
          onClose={() => setInterviewing(false)}
        />
      )}
    </div>
  )
}

/**
 * The album cover at list size, or the placeholder tile when there is none.
 *
 * The tile is its own token rather than --color-surface: a cover is a picture,
 * and the ground under one wants to be a shade further from the page than a
 * panel is.
 */
export function Cover({url, name, size = 44}: {url: string; name: string; size?: number}) {
  if (!url) {
    return (
      <span
        className="flex shrink-0 items-center justify-center rounded bg-tile text-fg-muted"
        style={{width: size, height: size}}
        aria-hidden="true"
      >
        <Disc3 size={Math.round(size * 0.45)} />
      </span>
    )
  }
  return (
    <img
      className="shrink-0 rounded object-cover"
      style={{width: size, height: size}}
      src={url}
      alt={'Cover of ' + name}
    />
  )
}

/**
 * The references an artist's sound was decided from.
 *
 * Edited as a whole list and saved with the artist, which is why this writes
 * back through onChange rather than saving on its own: the rows are replaced
 * wholesale on the backend (see SaveArtist), and a row saved independently would
 * be undone by the next save of the fields above it.
 */
function Inspirations({
  inspirations,
  onChange,
  onMergeStyle,
}: {
  inspirations: main.Inspiration[]
  onChange: (next: main.Inspiration[]) => void
  /** Folds a fresh analysis into the base style draft (#31). */
  onMergeStyle: (analysis: string) => Promise<void>
}) {
  // Which reference Antigravity is researching right now (#29), and what went
  // wrong when one could not be. One at a time — each run is minutes of
  // agentic CLI, and a second press should wait its turn, not race the first.
  const [analyzing, setAnalyzing] = useState<number | null>(null)
  const [analysisError, setAnalysisError] = useState('')

  const analyze = async (index: number) => {
    if (analyzing !== null) return
    setAnalyzing(index)
    setAnalysisError('')
    try {
      const breakdown = await AnalyzeMusicURL(inspirations[index].url)
      // Into the note field of the draft, not the database: the producer reads
      // it, trims it, and presses Save themselves.
      update(index, {note: breakdown})
      // And into the base style beside it (#31) — the reference was added to
      // change how the artist sounds, and the merge is where that lands. Its
      // failure keeps the note: an analysis without a merge is still worth
      // having, and the error says why the style did not move.
      await onMergeStyle(breakdown)
    } catch (err) {
      setAnalysisError(toMessage(err))
    } finally {
      setAnalyzing(null)
    }
  }

  const update = (index: number, patch: Partial<main.Inspiration>) =>
    onChange(
      inspirations.map((row, i) =>
        i === index ? main.Inspiration.createFrom({...row, ...patch}) : row,
      ),
    )

  return (
    <section className="sm-panel space-y-4 p-4">
      <div className="flex items-center gap-3">
        <h2 className="flex-1 text-sm font-semibold">Inspiration</h2>
        <button
          className="sm-button sm-button-quiet"
          type="button"
          onClick={() =>
            onChange([
              ...inspirations,
              main.Inspiration.createFrom({
                id: '',
                url: '',
                title: '',
                note: '',
                position: inspirations.length,
              }),
            ])
          }
        >
          <Plus size={15} />
          Add a reference
        </button>
      </div>

      {analysisError && <p className="text-sm text-danger">{analysisError}</p>}

      {inspirations.length === 0 ? (
        <p className="text-sm text-fg-muted">
          YouTube or YouTube Music links — specific tracks rather than artists. The note
          against each is the part that survives; the waveform button fills it with
          Antigravity&apos;s technical breakdown and folds the song&apos;s traits into
          the base style above.
        </p>
      ) : (
        <ul className="space-y-3">
          {inspirations.map((row, index) => (
            <li key={row.id || index} className="grid gap-2 md:grid-cols-[1fr_1fr_auto]">
              <TextField
                label="Link"
                type="url"
                value={row.url}
                placeholder="https://music.youtube.com/watch?v=…"
                onChange={(url) => update(index, {url})}
              />
              <TextField
                label="What it is here for"
                value={row.note}
                placeholder="The drum sound only"
                onChange={(note) => update(index, {note})}
              />
              <div className="flex items-end gap-1 pb-1">
                <button
                  className="sm-button sm-button-quiet px-2"
                  type="button"
                  title="Break the song down with Antigravity"
                  aria-label="Analyze this song"
                  disabled={!row.url || analyzing !== null}
                  onClick={() => void analyze(index)}
                >
                  {analyzing === index ? (
                    <Loader2 size={15} className="animate-spin" />
                  ) : (
                    <AudioLines size={15} />
                  )}
                </button>
                <button
                  className="sm-button sm-button-quiet px-2"
                  type="button"
                  title="Open"
                  aria-label="Open the link"
                  disabled={!row.url}
                  onClick={() => void OpenURL(row.url).catch(() => {})}
                >
                  <ExternalLink size={15} />
                </button>
                <button
                  className="sm-button sm-button-quiet px-2"
                  type="button"
                  title="Remove"
                  aria-label="Remove this reference"
                  onClick={() => onChange(inspirations.filter((_, i) => i !== index))}
                >
                  <Trash2 size={15} />
                </button>
              </div>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
