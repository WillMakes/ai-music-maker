import {useCallback, useEffect, useRef, useState} from 'react'
import {AudioLines, Disc3, ExternalLink, FolderInput, ListPlus, Loader2, Music4, Play, Plus, Sparkles, Trash2, Wand2} from 'lucide-react'
import {
  AnalyzeMusicURL,
  ClearChat,
  MergeStyleWithAnalysis,
  DeleteArtist,
  GenerateAlbumArtwork,
  GetArtist,
  ImportAlbum,
  ImportAlbumArtwork,
  ImportTrackFile,
  ListAlbums,
  OpenURL,
  PlanAlbum,
  SaveAlbum,
  SaveTrack,
  SaveArtist,
} from '../../wailsjs/go/app/App'
import {app} from '../../wailsjs/go/models'
import {EmptyState} from '../components/EmptyState'
import {PageHeader} from '../components/PageHeader'
import {TextField} from '../components/Field'
import {MarkdownField} from '../components/MarkdownField'
import {InterviewDialog} from '../chat/InterviewDialog'
import {StyleEditAssistant} from '../artists/StyleEditAssistant'
import {Modal} from '../components/Modal'
import {TabStrip} from '../components/TabStrip'
import {useDataChanged} from '../lib/dataChanged'
import {toMessage} from '../lib/errors'
import {attachmentPathsByName, isImagePath} from '../lib/files'
import {usePlayer} from '../player/PlayerProvider'

/**
 * One artist: who they are, what they sound like, what they were listening to,
 * and their records.
 *
 * The fields save themselves (#52): edits land in a local draft on every
 * keystroke and a debounce writes the draft down once typing pauses, so
 * nothing is lost to a forgotten Save button. The draft still exists — it is
 * what keeps a background reload from re-rendering under the cursor — but it
 * is a buffer now, not a decision.
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
  const [artist, setArtist] = useState<app.Artist | null>(null)
  const [albums, setAlbums] = useState<app.Album[]>([])
  const [draft, setDraft] = useState<app.Artist | null>(null)
  const [saveState, setSaveState] = useState<'idle' | 'saving' | 'saved'>('idle')
  // Only a draft the producer has touched is auto-saved. Without this gate, a
  // background write (MCP, the bridge) that reloads `artist` would make the
  // untouched draft look dirty, and the debounce would quietly revert it.
  const editedRef = useRef(false)
  const [error, setError] = useState('')
  const [interviewing, setInterviewing] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  // The typed-back name that arms the Delete button (#53). Deleting an artist
  // takes their whole discography with them, so the confirmation costs the
  // same deliberate act everywhere else reserves for irreversible things.
  const [confirmName, setConfirmName] = useState('')
  // The base-style refinement chat (#30), for an artist that already exists.
  const [refining, setRefining] = useState(false)
  // Importing a record that already exists (#36).
  const [importing, setImporting] = useState(false)
  // The page's three parts as tabs (#109), the records first: they are what
  // a producer comes to an artist for, and the identity and the references
  // are written once and then mostly read.
  const [tab, setTab] = useState<'albums' | 'info' | 'inspiration'>('albums')
  const player = usePlayer()

  const reload = useCallback(() => {
    Promise.all([GetArtist(artistId), ListAlbums(artistId)])
      .then(([found, rows]) => {
        setArtist(found)
        // The draft is only seeded when there is not one, so a reload triggered
        // by a background write does not throw away what is being typed.
        setDraft((current) => current ?? app.Artist.createFrom(found))
        setAlbums(rows ?? [])
        setError('')
      })
      .catch((err) => setError(toMessage(err)))
  }, [artistId])

  useEffect(reload, [reload])
  useDataChanged(['artists', 'albums'], reload)

  const update = (patch: Partial<app.Artist>) => {
    editedRef.current = true
    setDraft((current) => (current ? app.Artist.createFrom({...current, ...patch}) : current))
  }
  // The draft as of the last render, for work that outlives the render that
  // started it (#112) — an analysis finishes minutes later, and folding it
  // into the draft it began with would undo everything typed since.
  const draftRef = useRef(draft)
  draftRef.current = draft
  const updateInspirations = (change: (rows: app.Inspiration[]) => app.Inspiration[]) => {
    editedRef.current = true
    setDraft((current) =>
      current
        ? app.Artist.createFrom({...current, inspirations: change(current.inspirations ?? [])})
        : current,
    )
  }

  // The auto-save itself (#52): a debounce off the draft, so one write per
  // pause in typing rather than forty half-finished versions. The stored
  // record is adopted back into the draft only when nothing changed while the
  // save was in flight — the store assigns ids and trims, and skipping the
  // adoption when the producer kept typing lets the next debounce converge
  // instead of clobbering their keystrokes.
  useEffect(() => {
    if (!draft || !editedRef.current) return
    // An empty name would just be refused — wait for the field to be typed
    // back in rather than flashing an error per deleted character.
    if (!draft.name.trim()) return
    const sent = JSON.stringify(draft)
    const timer = setTimeout(async () => {
      setSaveState('saving')
      try {
        const saved = await SaveArtist(draft)
        setArtist(saved)
        setDraft((current) =>
          current && JSON.stringify(current) === sent
            ? app.Artist.createFrom(saved)
            : current,
        )
        // A keystroke during the flight re-armed the debounce before this
        // line runs, so clearing the flag cannot lose an edit.
        editedRef.current = false
        setSaveState('saved')
        setError('')
      } catch (err) {
        setSaveState('idle')
        setError(toMessage(err))
      }
    }, 800)
    return () => clearTimeout(timer)
  }, [draft])

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

  const heroCovers = albums.filter((album) => album.artworkUrl)

  return (
    <div className="space-y-5">
      <PageHeader
        // The draft's name, so a rename shows in the title the moment it is
        // committed rather than after the debounce lands; the stored name
        // stands in while the panel field is being typed empty (#79).
        title={draft.name.trim() ? draft.name : artist.name}
        // The rename rides the same draft-and-debounce as the panel's Name
        // field (#79) — one save path, one set of guards.
        onRename={(name) => update({name})}
        breadcrumb="Artists"
        subtitle={artist.summary || 'No description yet.'}
        icon={<Music4 size={26} />}
        // The discography as the hero's backdrop (#56): every cover the
        // artist has, tiled edge to edge behind the header. Repeated to fill
        // the strip when there are only a few, and held at opacity-15 so the
        // title and stats keep their token contrast over it in both themes —
        // the page background still does the contrast work, the collage is
        // atmosphere.
        backdrop={
          heroCovers.length > 0 ? (
            <div className="flex h-full w-full opacity-15">
              {Array.from(
                {length: Math.max(heroCovers.length, 12)},
                (_, i) => heroCovers[i % heroCovers.length],
              ).map((album, i) => (
                <img
                  key={i}
                  src={album.artworkUrl}
                  alt=""
                  className="h-full w-auto shrink-0 object-cover"
                />
              ))}
            </div>
          ) : undefined
        }
        actions={
          <>
            <button
              className="sm-button sm-button-quiet"
              type="button"
              onClick={() => void player.play('artist', artistId)}
            >
              <Play size={15} />
              Play
            </button>
            {/* Behind what is already playing rather than over it (#60). */}
            <button
              className="sm-button sm-button-quiet"
              type="button"
              onClick={() => void player.enqueue('artist', artistId)}
            >
              <ListPlus size={15} />
              Queue
            </button>
            {/* The discography's numbers at a glance (#43): albums, tracks,
                and how many are actually made - a take imported, not just
                written down. */}
            <div className="mr-2 flex items-center gap-4">
              <Stat value={albums.length} label={albums.length === 1 ? 'album' : 'albums'} />
              <Stat
                value={albums.reduce((n, album) => n + album.trackCount, 0)}
                label="tracks"
              />
              <Stat
                value={albums.reduce((n, album) => n + album.madeCount, 0)}
                label="made"
                ok
              />
            </div>
            {/* No Save button (#52) — just the word for what the debounce
                is doing, so a producer who closes the window right after
                typing knows whether it landed. */}
            {saveState !== 'idle' && (
              <span className="text-xs text-fg-muted" role="status">
                {saveState === 'saving' ? 'Saving…' : 'Saved'}
              </span>
            )}
            <button
              // Icon-only (#63): in the hero the word restated what the
              // trash can and the danger colour already say, and the real
              // decision happens in the typed-name confirmation (#53) anyway.
              className="sm-button sm-button-danger px-2"
              type="button"
              title="Delete this artist"
              aria-label="Delete this artist"
              onClick={() => {
                setConfirmName('')
                setConfirmDelete(true)
              }}
            >
              <Trash2 size={15} />
            </button>
          </>
        }
      />

      {error && <p className="text-sm text-danger">{error}</p>}

      {confirmDelete && (
        <div className="sm-panel space-y-3 border-danger p-4">
          <p className="text-sm">
            Are you sure? Deleting <strong>{artist.name}</strong> also deletes every
            record under them and every track on those records. The audio files stay
            in your music folder.
          </p>
          {/* What would actually go (#53): the discography by name, not a
              count — a number is easy to wave through, a list of records
              somebody spent evenings on is not. */}
          {albums.length === 0 ? (
            <p className="text-sm text-fg-muted">They have no albums — only the artist itself goes.</p>
          ) : (
            <ul className="space-y-1">
              {albums.map((album) => (
                <li key={album.id} className="flex items-center gap-2 text-sm">
                  <Disc3 size={14} className="shrink-0 text-fg-muted" aria-hidden />
                  <span className="truncate font-medium">{album.name}</span>
                  <span className="shrink-0 text-xs text-fg-muted">
                    {album.trackCount} track{album.trackCount === 1 ? '' : 's'}
                  </span>
                </li>
              ))}
            </ul>
          )}
          <TextField
            label="Type the artist's name to confirm"
            placeholder={artist.name}
            value={confirmName}
            onChange={setConfirmName}
          />
          <div className="flex gap-2">
            <button
              className="sm-button sm-button-danger"
              type="button"
              disabled={confirmName.trim() !== artist.name.trim()}
              onClick={remove}
            >
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

      <TabStrip
        tabs={[
          {id: 'albums', label: albums.length ? 'Albums (' + albums.length + ')' : 'Albums'},
          {id: 'info', label: 'Artist Info'},
          {
            id: 'inspiration',
            label: draft.inspirations?.length
              ? 'Inspiration (' + draft.inspirations.length + ')'
              : 'Inspiration',
          },
        ]}
        current={tab}
        onSelect={setTab}
      />

      {/* Every tab stays mounted and only the open one shows: an inspiration
          being analysed, or a style mid-edit, must survive a look at another
          tab — unmounting would drop the analysis on the floor. */}
      <section className="space-y-3" hidden={tab !== 'albums'} aria-label="Albums">
        <div className="flex items-center justify-end gap-3">
          <button
            className="sm-button sm-button-quiet"
            type="button"
            onClick={() => setImporting(true)}
          >
            <FolderInput size={15} />
            Import an album
          </button>
        </div>

        {albums.length === 0 ? (
          <div className="space-y-3">
            <EmptyState
              icon={Disc3}
              title="No albums yet"
              body="An album carries what is different about one record — the artist's base style is already inherited."
            />
            <div className="flex justify-center">
              <button
                className="sm-button sm-button-primary"
                type="button"
                onClick={() => setInterviewing(true)}
              >
                <Sparkles size={15} />
                Create an album
              </button>
            </div>
          </div>
        ) : (
          <ul className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {albums.map((album) => (
              <li key={album.id}>
                <AlbumCard album={album} onOpen={() => onOpenAlbum(album.id)} />
              </li>
            ))}
            <li>
              <button
                className="flex h-full min-h-[7rem] w-full flex-col items-center justify-center gap-1.5 rounded-lg border border-dashed border-edge text-xs font-medium text-fg-muted hover:border-accent hover:text-accent"
                type="button"
                onClick={() => setInterviewing(true)}
              >
                <Sparkles size={16} />
                Create an album
              </button>
            </li>
          </ul>
        )}
      </section>

      <section className="sm-panel space-y-4 p-4" hidden={tab !== 'info'} aria-label="Artist Info">
        <div className="flex items-center justify-end gap-3">
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
        {/* Markdown with a preview (#32), the way Site Maker Pro edits its
            documents: the style is read far more often than written, and the
            rendered form is what it is read in. */}
        <MarkdownField
          label="Base technical style"
          hint="What every record by this artist sounds like. Dense and comma-led — instruments, production, tempo range, vocal treatment, mix, and what to avoid. Combined with each album's own style when a track prompt is written."
          placeholder="Analog synths, gated reverb drums, 100–115 BPM…"
          value={draft.baseStyle}
          onChange={(baseStyle) => update({baseStyle})}
        />
      </section>

      <div hidden={tab !== 'inspiration'}>
      <Inspirations
        inspirations={draft.inspirations ?? []}
        onChange={updateInspirations}
        // Adding a reference updates the style (#31): the analysis that just
        // filled the note is folded into the base style draft, and the
        // auto-save writes it down like any other edit (#52). Against the
        // style as it is now, not as it was when the analysis began (#112).
        onMergeStyle={async (analysis) => {
          const base = draftRef.current?.baseStyle ?? ''
          const merged = await MergeStyleWithAnalysis(base, analysis)
          // The merge is a model call too; a style typed over meanwhile is
          // the producer's word and outranks it.
          if ((draftRef.current?.baseStyle ?? '') !== base) {
            throw new Error('The base style was edited while the analysis was being merged, so it was left as you wrote it.')
          }
          update({baseStyle: merged})
        }}
      />
      </div>

      {importing && (
        <ImportAlbumDialog
          artistId={artist.id}
          onDone={(albumId) => {
            setImporting(false)
            onOpenAlbum(albumId)
          }}
          onClose={() => setImporting(false)}
        />
      )}

      {refining && (
        <StyleEditAssistant
          artistId={artist.id}
          // Into the draft the producer is already editing — read it, adjust
          // it, and the auto-save takes it from there (#52).
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
              // One writing pass rather than two readers (#70): extraction
              // returned "every track the conversation settled", and an album
              // chat settles a theme and a count, not eleven songs — so the
              // album kept opening empty. PlanAlbum keeps what was agreed and
              // writes the rest, with the style built on the artist's base
              // style and references. Its failure is the dialog's error, not
              // a silently trackless record.
              const plan: app.AlbumPlan = await PlanAlbum(artist.id, messages)
              const album = await SaveAlbum(
                app.Album.createFrom({
                  artistId: artist.id,
                  name: plan.name || 'New album',
                  technicalStyle: plan.technicalStyle,
                  artworkPrompt: plan.artworkPrompt,
                }),
              )
              // Attached audio (#24) still lands on whichever track the plan
              // assigned it to.
              const attached = attachmentPathsByName(messages)
              for (const track of plan.tracks ?? []) {
                const saved = await SaveTrack(
                  app.Track.createFrom({
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
              // And the artwork (#69). An image handed to the chat is the
              // cover — the last one attached, matching how a conversation
              // corrects itself — imported before ClearChat sweeps the
              // chat's files. With no image but an artwork prompt, a cover
              // renders in the background: not awaited, because an image
              // model run is measured in tens of seconds and the album is
              // already real; if it fails, the album page's own Generate
              // button is the retry.
              const images = Object.values(attached).filter(isImagePath)
              if (images.length > 0) {
                await ImportAlbumArtwork(album.id, images[images.length - 1]).catch(
                  () => {},
                )
              } else if (plan.artworkPrompt) {
                void GenerateAlbumArtwork(album.id, '').catch(() => {})
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

/** One number of the header's cluster (#43). */
function Stat({value, label, ok = false}: {value: number; label: string; ok?: boolean}) {
  return (
    <span className="text-right">
      <span
        className={
          'block text-base font-semibold tabular-nums' + (ok ? ' text-ok' : '')
        }
      >
        {value}
      </span>
      <span className="block text-[11px] text-fg-muted">{label}</span>
    </span>
  )
}

/**
 * One record as a pipeline stage (#43): its counts, how much of it is made,
 * and the next step by name. "Written" is a track with a style prompt, "made"
 * one with a take imported - the two gates every song passes in order.
 */
function AlbumCard({album, onOpen}: {album: app.Album; onOpen: () => void}) {
  const total = album.trackCount
  const written = album.writtenCount
  const made = album.madeCount
  const released = Boolean(album.appleMusicUrl || album.spotifyUrl || album.youtubeUrl)
  const next =
    total === 0
      ? 'Next: write tracks \u2192'
      : written < total
        ? 'Next: write ' +
          (total - written) +
          (total - written === 1 ? ' prompt' : ' prompts') +
          ' \u2192'
        : made < total
          ? 'Next: make ' + (total - made) + ' in Suno \u2192'
          : released
            ? 'Released'
            : 'Next: add release links \u2192'
  const pct = total === 0 ? 0 : Math.round((made / total) * 100)

  return (
    <button
      className="sm-panel flex h-full w-full flex-col gap-2.5 p-3 text-left hover:bg-surface-hover"
      type="button"
      onClick={onOpen}
    >
      <span className="flex w-full items-center gap-3">
        <Cover url={album.artworkUrl} name={album.name} size={40} />
        <span className="min-w-0 flex-1">
          <span className="block truncate font-medium">{album.name}</span>
          <span className="block text-xs text-fg-muted">
            {total === 0
              ? 'No tracks yet'
              : (total === 1 ? '1 track' : total + ' tracks') +
                ' \u00b7 ' +
                written +
                ' written \u00b7 ' +
                made +
                ' made'}
          </span>
        </span>
      </span>
      <span className="block h-1 w-full rounded-full bg-edge">
        <span
          className="block h-1 rounded-full bg-accent"
          style={{width: pct + '%'}}
        />
      </span>
      <span className="text-xs font-medium text-accent">{next}</span>
    </button>
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
/**
 * Bringing in a record that already exists (#36): a folder of audio becomes an
 * album with a track per file, each imported into the music library — or a
 * name alone becomes an empty album, for content this app cannot read yet.
 * The counterpart of the blank artist: the interview invents, this imports.
 */
function ImportAlbumDialog({
  artistId,
  onDone,
  onClose,
}: {
  artistId: string
  onDone: (albumId: string) => void
  onClose: () => void
}) {
  const [name, setName] = useState('')
  const [dir, setDir] = useState('')
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState('')

  const run = async () => {
    if (busy || (!name.trim() && !dir.trim())) return
    setBusy(true)
    setProblem('')
    try {
      const album = await ImportAlbum(artistId, name, dir)
      onDone(album.id)
    } catch (err) {
      setProblem(toMessage(err))
      setBusy(false)
    }
  }

  return (
    <Modal
      title="Import an album"
      onClose={onClose}
      footer={
        <>
          <button className="sm-button sm-button-quiet" type="button" onClick={onClose}>
            Cancel
          </button>
          <button
            className="sm-button sm-button-primary"
            type="button"
            onClick={run}
            disabled={busy || (!name.trim() && !dir.trim())}
          >
            {busy ? 'Importing…' : 'Import'}
          </button>
        </>
      }
    >
      <div className="space-y-3">
        <TextField
          label="Folder of audio files"
          hint="Every audio file becomes a track, named and ordered from its filename, and copied into your music library. Leave it blank to create an empty album by name."
          placeholder="C:/Music/Night Drive"
          value={dir}
          onChange={setDir}
          disabled={busy}
        />
        <TextField
          label="Album name"
          hint="Blank means the folder's own name."
          value={name}
          onChange={setName}
          disabled={busy}
        />
        {problem && <p className="text-sm text-danger">{problem}</p>}
      </div>
    </Modal>
  )
}

/**
 * The style's first breath as plain text (#58): markdown markers stripped,
 * lines joined, and capped at 150 characters (#62), because a card excerpt is
 * a glance, not a document — the full rendered form lives in the card's modal.
 */
const styleBrief = (note: string) => {
  const flat = note
    .replace(/^#+\s*/gm, '')
    .replace(/[*_`>]/g, '')
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
    .join(' · ')
  return flat.length > 150 ? flat.slice(0, 150).trimEnd() + '…' : flat
}

/** Where one reference's analysis stands (#112), keyed by its link. */
type AnalysisStatus = {state: 'queued' | 'running'} | {state: 'failed'; error: string}

function Inspirations({
  inspirations,
  onChange,
  onMergeStyle,
}: {
  inspirations: app.Inspiration[]
  /** Changes the rows as they are when it lands, not as this render saw them. */
  onChange: (change: (rows: app.Inspiration[]) => app.Inspiration[]) => void
  /** Folds a fresh analysis into the base style draft (#31). */
  onMergeStyle: (analysis: string) => Promise<void>
}) {
  // Every analysis runs behind the cards rather than inside the modal (#112):
  // each is minutes of agentic CLI (#29), and holding the modal open for that
  // long held the page hostage. They are keyed by link, not by row index — a
  // row removed or added mid-run shifts every index after it, and the result
  // would land on the wrong song. One runs at a time; a second press joins
  // the queue rather than racing the first.
  const [status, setStatus] = useState<Record<string, AnalysisStatus>>({})
  const queueRef = useRef<string[]>([])
  const runningRef = useRef(false)
  // Which reference's modal is open (#58). The card is a summary; everything
  // that edits the reference lives behind the click.
  const [openIndex, setOpenIndex] = useState<number | null>(null)
  // A reference being written that is not yet in the draft (#61). A blank row
  // put straight into the draft was dropped by the store the moment the
  // auto-save (#52) fired, and the modal — keyed by index into the draft —
  // closed itself under the producer's cursor. The row joins the draft on its
  // first real content instead.
  const [pending, setPending] = useState<app.Inspiration | null>(null)

  const openRow = pending ?? (openIndex !== null ? inspirations[openIndex] : null)
  const closeModal = () => {
    setPending(null)
    setOpenIndex(null)
  }
  const editOpen = (patch: Partial<app.Inspiration>) => {
    if (pending) {
      const next = app.Inspiration.createFrom({...pending, ...patch})
      if (next.url.trim() || next.note.trim()) {
        onChange((rows) => [...rows, next])
        setPending(null)
        setOpenIndex(inspirations.length)
      } else {
        setPending(next)
      }
      return
    }
    if (openIndex !== null) {
      const index = openIndex
      onChange((rows) =>
        rows.map((row, i) => (i === index ? app.Inspiration.createFrom({...row, ...patch}) : row)),
      )
    }
  }

  const setOne = (url: string, next: AnalysisStatus | null) =>
    setStatus((current) => {
      const copy = {...current}
      if (next) copy[url] = next
      else delete copy[url]
      return copy
    })

  // Takes the next link off the queue. Everything it touches is a ref or a
  // functional update, so a run that finishes renders later still lands on
  // the rows as they are then.
  const pump = async () => {
    if (runningRef.current) return
    const url = queueRef.current.shift()
    if (url === undefined) return
    runningRef.current = true
    setOne(url, {state: 'running'})
    try {
      const analysis = await AnalyzeMusicURL(url)
      // The style lands in the note, and the song's identity — title,
      // artist, album, cover — rides along (#51), so the row says what it
      // is instead of showing a bare URL. The auto-save writes it down. A
      // row removed, or relinked, while the run was out gets nothing.
      const listed = stillListed(url)
      onChange((rows) =>
        rows.map((row) => {
          if (row.url !== url) return row
          return app.Inspiration.createFrom({
            ...row,
            note: analysis.style,
            title: analysis.title,
            artist: analysis.artist,
            album: analysis.album,
            artworkUrl: analysis.artworkUrl,
          })
        }),
      )
      // And into the base style beside it (#31) — the reference was added to
      // change how the artist sounds, and the merge is where that lands. Its
      // failure keeps the note: an analysis without a merge is still worth
      // having, and the card says why the style did not move. Nothing is
      // merged for a reference that is no longer there.
      if (listed) await onMergeStyle(analysis.style)
      setOne(url, null)
    } catch (err) {
      setOne(url, {state: 'failed', error: toMessage(err)})
    } finally {
      runningRef.current = false
      void pump()
    }
  }

  // The rows as of the latest render, for deciding whether a finished run
  // still has a reference to merge for — the functional update that fills
  // the row may not run until the next render, so it cannot answer that.
  const rowsRef = useRef(inspirations)
  rowsRef.current = inspirations
  const stillListed = (url: string) => rowsRef.current.some((row) => row.url === url)

  const analyze = (url: string) => {
    if (!url || queueRef.current.includes(url) || status[url]?.state === 'running') return
    queueRef.current.push(url)
    setOne(url, {state: 'queued'})
    void pump()
  }

  const removeAt = (index: number) => {
    const url = inspirations[index]?.url
    onChange((rows) => rows.filter((_, i) => i !== index))
    // A queued run for it would only find nothing to fill; a failure has
    // nothing left to retry.
    if (url && !inspirations.some((row, i) => i !== index && row.url === url)) {
      queueRef.current = queueRef.current.filter((queued) => queued !== url)
      if (status[url]?.state !== 'running') setOne(url, null)
    }
  }

  const openStatus = openRow?.url ? status[openRow.url] : undefined

  return (
    <section className="sm-panel space-y-4 p-4">
      <div className="flex items-center gap-3">
        <h2 className="flex-1 text-sm font-semibold">Inspiration</h2>
        <button
          className="sm-button sm-button-quiet"
          type="button"
          onClick={() =>
            // Straight into the modal (#58), but as a pending row (#61) — it
            // only enters the draft once something is written on it.
            setPending(
              app.Inspiration.createFrom({
                id: '',
                url: '',
                title: '',
                artist: '',
                album: '',
                artworkUrl: '',
                note: '',
                position: inspirations.length,
              }),
            )
          }
        >
          <Plus size={15} />
          Add a reference
        </button>
      </div>

      {inspirations.length === 0 ? (
        <p className="text-sm text-fg-muted">
          YouTube or YouTube Music links — specific tracks rather than artists. The
          waveform button researches the song: it grabs what the track is — title,
          artist, album, cover — writes the style breakdown against it, and folds the
          song&apos;s traits into the base style above.
        </p>
      ) : (
        <ul className="grid gap-3 sm:grid-cols-2">
          {inspirations.map((row, index) => {
            const rowStatus = row.url ? status[row.url] : undefined
            const failed = rowStatus?.state === 'failed' ? rowStatus : null
            return (
              <li
                key={row.id || index}
                className={
                  'flex flex-col rounded-lg border ' +
                  (failed ? 'border-danger' : 'border-edge hover:border-accent')
                }
              >
                {/* The card is the glance (#58): what the song is and the first
                    breath of its style. The full breakdown, the link and every
                    control live in the modal the click opens. */}
                <button
                  className="flex h-full w-full items-start gap-3 p-3 text-left"
                  type="button"
                  onClick={() => setOpenIndex(index)}
                >
                  {row.artworkUrl && (
                    <img
                      src={row.artworkUrl}
                      alt=""
                      className="h-12 w-12 shrink-0 rounded-md border border-edge object-cover"
                    />
                  )}
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-sm font-medium">
                      {row.title || row.url || 'New reference'}
                    </span>
                    {(row.artist || row.album) && (
                      <span className="block truncate text-xs text-fg-muted">
                        {[row.artist, row.album].filter(Boolean).join(' — ')}
                      </span>
                    )}
                    <span className="mt-1 block text-xs text-fg-muted">
                      {rowStatus?.state === 'running'
                        ? 'Analyzing…'
                        : rowStatus?.state === 'queued'
                          ? 'Waiting to be analyzed…'
                          : styleBrief(row.note) ||
                            'No style yet — open the card to write or analyze it.'}
                    </span>
                  </span>
                  {(rowStatus?.state === 'running' || rowStatus?.state === 'queued') && (
                    <Loader2 size={15} className="mt-0.5 shrink-0 animate-spin" aria-hidden />
                  )}
                </button>
                {/* A failure stays on its card with its way out (#112) — the
                    modal that used to carry the error is long closed by the
                    time a run fails. */}
                {failed && (
                  <div className="space-y-2 border-t border-edge p-3">
                    <p className="text-xs text-danger">{failed.error}</p>
                    <div className="flex gap-2">
                      <button
                        className="sm-button sm-button-quiet"
                        type="button"
                        onClick={() => analyze(row.url)}
                      >
                        <AudioLines size={15} />
                        Retry
                      </button>
                      <button
                        className="sm-button sm-button-quiet"
                        type="button"
                        onClick={() => removeAt(index)}
                      >
                        <Trash2 size={15} />
                        Remove
                      </button>
                    </div>
                  </div>
                )}
              </li>
            )
          })}
        </ul>
      )}

      {openRow && (
        <Modal
          title={openRow.title || 'Reference'}
          onClose={closeModal}
          wide
          footer={
            <>
              <button
                className="sm-button sm-button-quiet"
                type="button"
                onClick={() => {
                  if (openIndex !== null) removeAt(openIndex)
                  closeModal()
                }}
              >
                <Trash2 size={15} />
                Remove
              </button>
              <button
                className="sm-button sm-button-quiet"
                type="button"
                disabled={!openRow.url}
                onClick={() => void OpenURL(openRow.url).catch(() => {})}
              >
                <ExternalLink size={15} />
                Open the link
              </button>
              <button
                className="sm-button sm-button-primary"
                type="button"
                disabled={
                  openIndex === null ||
                  !openRow.url ||
                  openStatus?.state === 'running' ||
                  openStatus?.state === 'queued'
                }
                // The modal steps aside the moment the run starts (#112); the
                // card carries the progress, and the page stays usable.
                onClick={() => {
                  analyze(openRow.url)
                  closeModal()
                }}
              >
                {openStatus?.state === 'running' || openStatus?.state === 'queued' ? (
                  <Loader2 size={15} className="animate-spin" />
                ) : (
                  <AudioLines size={15} />
                )}
                {openStatus?.state === 'running'
                  ? 'Analyzing…'
                  : openStatus?.state === 'queued'
                    ? 'Queued'
                    : 'Analyze'}
              </button>
            </>
          }
        >
          <div className="space-y-4">
            {(openRow.artworkUrl || openRow.artist || openRow.album) && (
              <div className="flex items-center gap-3">
                {openRow.artworkUrl && (
                  <img
                    src={openRow.artworkUrl}
                    alt=""
                    className="h-16 w-16 shrink-0 rounded-md border border-edge object-cover"
                  />
                )}
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium">
                    {openRow.title || openRow.url}
                  </p>
                  <p className="truncate text-xs text-fg-muted">
                    {[openRow.artist, openRow.album].filter(Boolean).join(' — ')}
                  </p>
                </div>
              </div>
            )}
            {openStatus?.state === 'failed' && (
              <p className="text-sm text-danger">{openStatus.error}</p>
            )}
            <TextField
              label="Link"
              type="url"
              value={openRow.url}
              placeholder="https://music.youtube.com/watch?v=…"
              onChange={(url) => editOpen({url})}
            />
            {/* "Style", and markdown (#51): the analysis writes its whole
                breakdown here, and a breakdown is read rendered, not as one
                long unwrapped line in a text input. */}
            <MarkdownField
              label="Style"
              placeholder="The drum sound only — or the full breakdown the analysis writes."
              value={openRow.note}
              onChange={(note) => editOpen({note})}
            />
          </div>
        </Modal>
      )}
    </section>
  )
}
