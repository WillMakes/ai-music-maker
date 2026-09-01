import {useCallback, useEffect, useRef, useState} from 'react'
import {FolderInput, ImagePlus, ListMusic, ListPlus, Loader2, Play, RefreshCw, Sparkles, Trash2, Wand2} from 'lucide-react'
import {TrackEditor} from './TrackDetails'
import {
  ClearChat,
  DeleteAlbum,
  ExtractTracks,
  GenerateAlbumStyle,
  GetAlbum,
  ImportAlbumArtwork,
  PickImageFile,
  ImportAlbumTakeDrop,
  ImportAlbumTakeFile,
  ImportTrackFile,
  ListTracks,
  RemoveAlbumArtwork,
  PickAudioFiles,
  SaveAlbum,
  SaveTrack,
  WriteAlbumTracks,
} from '../../wailsjs/go/main/App'
import {main} from '../../wailsjs/go/models'
import {CopyButton} from '../components/CopyButton'
import {EmptyState} from '../components/EmptyState'
import {PageHeader} from '../components/PageHeader'
import {TextField} from '../components/Field'
import {MarkdownField} from '../components/MarkdownField'
import {InterviewDialog} from '../chat/InterviewDialog'
import {TabStrip} from '../components/TabStrip'
import {Cover} from './ArtistDetails'
import {useDataChanged} from '../lib/dataChanged'
import {duration} from '../lib/format'
import {toMessage} from '../lib/errors'
import {attachmentPathsByName, isAudioPath, readAsDataURL} from '../lib/files'
import {usePlayer} from '../player/PlayerProvider'
import {useEvolutions} from '../evolutions/EvolutionsProvider'
import {useArtworks} from '../artworks/ArtworksProvider'

/**
 * One album: its cover, what is different about this record, its tracks, and the
 * release links that only exist once it is out.
 *
 * Artwork generation is a long call — up to several minutes on the Codex path —
 * so it has its own busy state rather than sharing the page's. A producer
 * waiting on a cover can still edit the fields beside it.
 */
export function AlbumDetails({
  albumId,
  trackId,
  onOpenAlbum,
  onOpenArtist,
  onDeleted,
}: {
  albumId: string
  // A track carried on the route — an old track-details link, a bug report's
  // subject — selects that track in the Tracks tab rather than opening a page
  // of its own (#40).
  trackId?: string
  onOpenAlbum: (albumId: string) => void
  onOpenArtist: (artistId: string) => void
  onDeleted: (artistId: string) => void
}) {
  const [album, setAlbum] = useState<main.Album | null>(null)
  const [tracks, setTracks] = useState<main.Track[]>([])
  const [draft, setDraft] = useState<main.Album | null>(null)
  const [saveState, setSaveState] = useState<'idle' | 'saving' | 'saved'>('idle')
  // Only a draft the producer has touched is auto-saved — same guard as the
  // artist page (#52): without it a background write reloading `album` would
  // make the untouched draft look dirty and quietly get reverted.
  const editedRef = useRef(false)
  // Drawing lives in the provider (#83), like evolving (#82): the generation
  // runs for minutes, and the producer must be free to navigate away and find
  // it again — on this page, or in the status bar.
  const {artworks, draw: drawArtwork, dismiss: dismissArtwork} = useArtworks()
  const artworkJob = artworks.find((job) => job.albumId === albumId)
  const drawing = artworkJob?.state === 'running'
  const [feedback, setFeedback] = useState('')
  const [error, setError] = useState('')
  const [interviewing, setInterviewing] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  // The page as tabs (#34): Record, Tracks, Artwork, Release. Local state
  // rather than routes — the album is the place, these are its drawers.
  const [tab, setTab] = useState<'record' | 'tracks' | 'artwork' | 'release'>('record')
  // The style distillation (#38) has its own busy state, like the artwork's.
  const [generating, setGenerating] = useState(false)
  // The Record tab's two style panes as tabs (#48), this record's own style
  // the default — it is the editable half.
  const [styleTab, setStyleTab] = useState<'style' | 'inherited'>('style')
  // The empty Tracks tab's one-press list writer (#71).
  const [writingTracks, setWritingTracks] = useState(false)
  // Whether a drag is hovering the track list (#76) — the batch drop zone.
  const [droppingTakes, setDroppingTakes] = useState(false)
  // Which track the detail pane shows (#40). Local state like the tab itself:
  // the album is the place, the selection is a drawer within it.
  const [selectedId, setSelectedId] = useState(trackId ?? '')
  // Evolving lives in the provider, not here (#82): the call runs for as long
  // as a big generation call does, and the producer must be free to navigate
  // away and find it again — on this page, or in the status bar.
  const {evolutions, evolve, dismiss} = useEvolutions()
  const evolution = evolutions.find((e) => e.albumId === albumId)
  const evolving = evolution?.state === 'running'
  // Auto-open the successor only on the transition seen from this page — the
  // old behaviour for a producer who waited here. Returning to this page
  // after the fact must not teleport them; the status bar offers the door.
  const sawRunningRef = useRef(false)
  useEffect(() => {
    if (evolution?.state === 'running') sawRunningRef.current = true
    if (evolution?.state === 'done' && sawRunningRef.current) {
      sawRunningRef.current = false
      dismiss(evolution.albumId)
      onOpenAlbum(evolution.nextAlbumId)
    }
  }, [evolution, dismiss, onOpenAlbum])
  const player = usePlayer()

  // The whole batch at once (#76): each file is routed to its track by name
  // on the backend, and every file that could not be routed or imported is
  // reported together — one stranger must not hide the nine that landed.
  const importTakeErrors = async (imports: (() => Promise<unknown>)[]) => {
    const problems: string[] = []
    for (const run of imports) {
      try {
        await run()
      } catch (err) {
        problems.push(toMessage(err))
      }
    }
    setError(problems.join(' '))
  }

  const importDroppedTakes = (dropped: FileList) =>
    importTakeErrors(
      Array.from(dropped).map((file) => async () => {
        if (!isAudioPath(file.name) && !file.type.startsWith('audio/')) {
          throw new Error(file.name + ' is not an audio file this app knows.')
        }
        await ImportAlbumTakeDrop(albumId, file.name, await readAsDataURL(file))
      }),
    )

  const pickTakes = async () => {
    try {
      const paths = await PickAudioFiles()
      if (!paths?.length) return
      await importTakeErrors(
        paths.map((path) => () => ImportAlbumTakeFile(albumId, path)),
      )
    } catch (err) {
      setError(toMessage(err))
    }
  }

  const reload = useCallback(() => {
    Promise.all([GetAlbum(albumId), ListTracks(albumId)])
      .then(([found, rows]) => {
        setAlbum(found)
        setDraft((current) => current ?? main.Album.createFrom(found))
        setTracks(rows ?? [])
        setError('')
      })
      .catch((err) => setError(toMessage(err)))
  }, [albumId])

  useEffect(reload, [reload])
  useDataChanged(['albums', 'tracks'], reload)

  useEffect(() => {
    if (trackId) {
      setSelectedId(trackId)
      setTab('tracks')
    }
  }, [trackId])

  const update = (patch: Partial<main.Album>) => {
    editedRef.current = true
    setDraft((current) => (current ? main.Album.createFrom({...current, ...patch}) : current))
  }

  // The auto-save, ported whole from the artist page (#52) because renaming
  // the record was dying to the same forgotten button (#75): a debounce off
  // the draft, the stored record adopted back only when nothing changed while
  // the save was in flight, and an empty name waiting rather than flashing a
  // refusal per deleted character.
  useEffect(() => {
    if (!draft || !editedRef.current) return
    if (!draft.name.trim()) return
    const sent = JSON.stringify(draft)
    const timer = setTimeout(async () => {
      setSaveState('saving')
      try {
        const saved = await SaveAlbum(draft)
        setAlbum(saved)
        setDraft((current) =>
          current && JSON.stringify(current) === sent
            ? main.Album.createFrom(saved)
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

  // Adopt a finished cover (#83): the generation may have ended while this
  // page was somewhere else entirely, so the adoption is an effect on the
  // job's state rather than the tail of the call. Only the artwork fields are
  // taken — the producer may have kept editing the rest — and adopting them
  // into the draft matters beyond display: the auto-save writes the whole
  // draft, and a draft still holding the old cover would quietly put it back.
  useEffect(() => {
    if (artworkJob?.state !== 'done' || !artworkJob.album) return
    const updated = artworkJob.album
    setAlbum(updated)
    setDraft((current) =>
      current
        ? main.Album.createFrom({
            ...current,
            artworkFile: updated.artworkFile,
            artworkUrl: updated.artworkUrl,
            artworkPrompt: updated.artworkPrompt,
          })
        : current,
    )
    setFeedback('')
    dismissArtwork(artworkJob.albumId)
  }, [artworkJob, dismissArtwork])

  const draw = () => {
    // A redraw after a failure is also its acknowledgement — the provider
    // replaces the failed entry with the new run's.
    drawArtwork({id: albumId, name: album?.name ?? '', artistId: album?.artistId ?? ''}, feedback)
  }

  if (!album || !draft) {
    return <p className="text-sm text-fg-muted">{error || 'Loading…'}</p>
  }

  // The selection falls back to the first track, so the pane is never blank
  // while the record has songs - and heals when the selected one is deleted.
  const selected = tracks.find((t) => t.id === selectedId) ?? tracks[0]

  return (
    /* The whole page is the batch drop zone (#84), not just the Tracks tab
       (#76): the page opens on the Record tab, and a drop that only worked
       one tab over read as no matching at all. Each file is still routed to
       its track by name, and several files for one track land as several
       takes. The per-track editor keeps its own zone by stopping the
       bubble. */
    <div
      className={'space-y-5' + (droppingTakes ? ' rounded-lg ring-2 ring-accent' : '')}
      onDragOver={(event) => {
        event.preventDefault()
        setDroppingTakes(true)
      }}
      onDragLeave={() => setDroppingTakes(false)}
      onDrop={(event) => {
        event.preventDefault()
        setDroppingTakes(false)
        if (event.dataTransfer.files.length) {
          void importDroppedTakes(event.dataTransfer.files)
        }
      }}
    >
      <PageHeader
        // The draft's name, so a rename shows in the title the moment it is
        // committed rather than after the debounce lands; the stored name
        // stands in while the Record tab's field is being typed empty (#80).
        title={draft.name.trim() ? draft.name : album.name}
        // The rename rides the same draft-and-debounce as the Record tab's
        // Name field (#80) — which is otherwise invisible from the Tracks
        // tab, the tab this page usually sits on.
        onRename={(name) => update({name})}
        breadcrumb={
          <button
            className="hover:text-fg hover:underline"
            type="button"
            onClick={() => onOpenArtist(album.artistId)}
          >
            {album.artistName}
          </button>
        }
        subtitle={tracks.length === 1 ? '1 track' : tracks.length + ' tracks'}
        // The quick copy beside the name (#74): the album title is what gets
        // typed into Suno's playlist field, and retyping a name that is
        // already on screen is the kind of friction that adds up.
        titleAccessory={<CopyButton text={album.name} label="Copy the album name" />}
        actions={
          <>
            <button
              className="sm-button sm-button-quiet"
              type="button"
              onClick={() => void player.play('album', albumId)}
            >
              <Play size={15} />
              Play
            </button>
            {/* Behind what is already playing rather than over it (#60). */}
            <button
              className="sm-button sm-button-quiet"
              type="button"
              onClick={() => void player.enqueue('album', albumId)}
            >
              <ListPlus size={15} />
              Queue
            </button>
            {/* In the header so the ability is visible from every tab (#84):
                the picked files are matched to tracks by name, the same as a
                drop anywhere on the page. */}
            <button
              className="sm-button sm-button-quiet"
              type="button"
              onClick={() => void pickTakes()}
            >
              <FolderInput size={15} />
              Import takes
            </button>
            {/* No Save button (#75) — just the word for what the debounce
                is doing, matching the artist page (#52). */}
            {saveState !== 'idle' && (
              <span className="text-xs text-fg-muted" role="status">
                {saveState === 'saving' ? 'Saving…' : 'Saved'}
              </span>
            )}
            <button
              className="sm-button sm-button-danger"
              type="button"
              onClick={() => setConfirmDelete(true)}
            >
              <Trash2 size={15} />
              Delete
            </button>
          </>
        }
      />

      {error && <p className="text-sm text-danger">{error}</p>}

      {confirmDelete && (
        <div className="sm-panel space-y-3 border-danger p-4">
          <p className="text-sm">
            Delete <strong>{album.name}</strong> and its {tracks.length} track
            {tracks.length === 1 ? '' : 's'}? The audio files stay in your music folder.
          </p>
          <div className="flex gap-2">
            <button
              className="sm-button sm-button-danger"
              type="button"
              onClick={async () => {
                try {
                  await DeleteAlbum(albumId)
                  onDeleted(album.artistId)
                } catch (err) {
                  setError(toMessage(err))
                }
              }}
            >
              Delete the album
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
          {id: 'record', label: 'Record'},
          {id: 'tracks', label: 'Tracks'},
          {id: 'artwork', label: 'Artwork'},
          {id: 'release', label: 'Release'},
        ]}
        current={tab}
        onSelect={setTab}
      />

      {/* Hidden rather than unmounted: artwork generation runs for minutes,
          and unmounting the panel mid-draw would drop its busy state while the
          call kept running. */}
      <div className={tab === 'artwork' ? 'max-w-md' : 'hidden'}>
        <section className="sm-panel space-y-3 p-4">
          <h2 className="text-sm font-semibold">Artwork</h2>
          <div className="flex justify-center">
            <Cover url={album.artworkUrl} name={album.name} size={200} />
          </div>
          <TextField
            label={album.artworkFile ? 'Change' : 'Describe the cover'}
            hint={
              album.artworkFile
                ? 'What to change about it — the rest is kept.'
                : 'Or leave it blank and let the style decide.'
            }
            value={feedback}
            placeholder={album.artworkFile ? 'Make it colder, less busy' : 'A single lit window'}
            onChange={setFeedback}
            disabled={drawing}
          />
          <div className="flex flex-wrap gap-2">
            <button
              className="sm-button sm-button-primary"
              type="button"
              onClick={draw}
              disabled={drawing}
            >
              {album.artworkFile ? <RefreshCw size={15} /> : <Wand2 size={15} />}
              {drawing ? 'Drawing…' : album.artworkFile ? 'Redraw' : 'Generate'}
            </button>
            <button
              className="sm-button sm-button-quiet"
              type="button"
              disabled={drawing}
              onClick={async () => {
                // The OS's own picker (#39): the binding owns the window
                // context the dialog needs, so the producer browses instead
                // of pasting a path. An empty path is a cancel, not an error.
                try {
                  const path = await PickImageFile()
                  if (!path) return
                  setAlbum(await ImportAlbumArtwork(albumId, path))
                } catch (err) {
                  setError(toMessage(err))
                }
              }}
            >
              <ImagePlus size={15} />
              Use a file
            </button>
            {album.artworkFile && (
              <button
                className="sm-button sm-button-quiet"
                type="button"
                disabled={drawing}
                onClick={async () => {
                  try {
                    setAlbum(await RemoveAlbumArtwork(albumId))
                  } catch (err) {
                    setError(toMessage(err))
                  }
                }}
              >
                <Trash2 size={15} />
                Remove
              </button>
            )}
          </div>
          {drawing && (
            <p className="text-xs text-fg-muted">
              This keeps running if you navigate away — watch the status bar, and come
              back any time.
            </p>
          )}
          {artworkJob?.state === 'failed' && (
            <p className="text-sm text-danger">{artworkJob.error}</p>
          )}
        </section>
      </div>

      {tab === 'record' && (
        <div className="max-w-4xl space-y-4">
          <section className="sm-panel space-y-4 p-4">
            <h2 className="text-sm font-semibold">The record</h2>
            <TextField
              label="Name"
              required
              value={draft.name}
              onChange={(name) => update({name})}
            />
            {/* Tabs rather than the old two-column grid (#48): the two
                halves of combinedStyle are read one at a time, and side by
                side they halved the room the style is actually written in.
                This record's own style is the default — the editable half. */}
            <TabStrip
              tabs={[
                {id: 'style', label: 'Technical style'},
                {id: 'inherited', label: 'Inherited from ' + album.artistName},
              ]}
              current={styleTab}
              onSelect={setStyleTab}
            />
            {styleTab === 'style' && (
              <div className="space-y-2">
                {/* Back by request (#38, reversing #37), with the button that
                    earns its place: Generate distils the tracks' own style
                    prompts into the record's shared core. Into the draft, not
                    the database — the producer edits and saves. */}
                <div className="flex justify-end">
                  <button
                    className="sm-button sm-button-quiet"
                    type="button"
                    disabled={generating}
                    onClick={async () => {
                      setGenerating(true)
                      setError('')
                      try {
                        update({technicalStyle: await GenerateAlbumStyle(albumId)})
                      } catch (err) {
                        setError(toMessage(err))
                      } finally {
                        setGenerating(false)
                      }
                    }}
                  >
                    {generating ? (
                      <Loader2 size={15} className="animate-spin" />
                    ) : (
                      <Wand2 size={15} />
                    )}
                    {generating ? 'Distilling…' : 'Generate from the tracks'}
                  </button>
                </div>
                {/* Markdown with a preview (#44), matching the artist's base
                    style: the style is read far more often than written, and
                    the rendered form is what it is read in. */}
                <MarkdownField
                  label=""
                  hint="The record's central style, distilled from its tracks — or written by hand. Joined with the artist's base style when a track prompt is written."
                  placeholder="Analog synths, colder mix than the debut, 100-110 BPM…"
                  value={draft.technicalStyle}
                  onChange={(technicalStyle) => update({technicalStyle})}
                />
              </div>
            )}
            {styleTab === 'inherited' && (
              /* The other half of combinedStyle, shown where the joining
                 happens (#40). Read-only: the base style belongs to the
                 artist, and two editable copies of it would drift. */
              <div className="space-y-1">
                <pre className="max-h-72 overflow-auto whitespace-pre-wrap rounded-md border border-edge bg-surface px-3 py-2 font-mono text-xs leading-relaxed text-fg-muted">
                  {album.artistBaseStyle || 'No base style written yet.'}
                </pre>
                <p className="text-xs text-fg-muted">Read-only here — edit it on the artist.</p>
              </div>
            )}
          </section>

          {/* The successor (#42): a new record under the same artist, the
              technical style evolved one step and a fresh track list written
              to flow. Navigation happens only once the album exists — a
              failure leaves the producer here, with the message. */}
          <section className="sm-panel space-y-3 p-4">
            <h2 className="text-sm font-semibold">The next record</h2>
            <p className="text-xs text-fg-muted">
              Create a new album that evolves this one: the technical style moved a
              step forward, and a whole new track list styled so each song flows into
              the next. Lyrics stay yours — write them per track, or through the
              interview on the new record.
            </p>
            <button
              className="sm-button sm-button-primary"
              type="button"
              disabled={evolving}
              onClick={() =>
                evolve({id: album.id, name: album.name, artistId: album.artistId})
              }
            >
              {evolving ? <Loader2 size={15} className="animate-spin" /> : <Sparkles size={15} />}
              {evolving ? 'Evolving…' : 'Create the next record'}
            </button>
            {evolving && (
              <p className="text-xs text-fg-muted">
                This keeps running if you navigate away — watch the status bar, and
                come back any time.
              </p>
            )}
            {evolution?.state === 'failed' && (
              <p className="text-sm text-danger">{evolution.error}</p>
            )}
          </section>
        </div>
      )}

      {tab === 'release' && (
        <div className="max-w-2xl">
          <section className="sm-panel space-y-4 p-4">
            <h2 className="text-sm font-semibold">Released at</h2>
            <p className="text-xs text-fg-muted">
              Filled in once the record is out — the last step of the workflow rather
              than the first.
            </p>
            <div className="grid gap-3 md:grid-cols-3">
              <TextField
                label="Apple Music"
                type="url"
                value={draft.appleMusicUrl}
                onChange={(appleMusicUrl) => update({appleMusicUrl})}
              />
              <TextField
                label="Spotify"
                type="url"
                value={draft.spotifyUrl}
                onChange={(spotifyUrl) => update({spotifyUrl})}
              />
              <TextField
                label="YouTube Music"
                type="url"
                value={draft.youtubeUrl}
                onChange={(youtubeUrl) => update({youtubeUrl})}
              />
            </div>
          </section>
        </div>
      )}

      {tab === 'tracks' &&
        (tracks.length === 0 ? (
          <div className="space-y-3">
            <EmptyState
              icon={ListMusic}
              title="No tracks yet"
              body="Write the whole list from this record's style in one press, or talk it through — the interview knows the combined style and takes requests."
            />
            <div className="flex justify-center gap-2">
              {/* The one-press repair (#71): a record with a style but no
                  tracks — an album created before the interview wrote them
                  (#70) — gets its list written directly, no second
                  conversation required. */}
              <button
                className="sm-button sm-button-primary"
                type="button"
                disabled={writingTracks}
                onClick={async () => {
                  setWritingTracks(true)
                  setError('')
                  try {
                    await WriteAlbumTracks(albumId)
                    await reload()
                  } catch (err) {
                    setError(toMessage(err))
                  } finally {
                    setWritingTracks(false)
                  }
                }}
              >
                {writingTracks ? (
                  <Loader2 size={15} className="animate-spin" />
                ) : (
                  <Wand2 size={15} />
                )}
                {writingTracks ? 'Writing…' : 'Write the track list'}
              </button>
              <button
                className="sm-button sm-button-quiet"
                type="button"
                onClick={() => setInterviewing(true)}
              >
                <Sparkles size={15} />
                Talk it through
              </button>
            </div>
          </div>
        ) : (
          /* The batch import's home row (#76). The drop zone itself moved up
             to the whole page (#84); this keeps the words that explain it,
             where the tracks the files land on are in view. */
          <div className="space-y-3">
            <div className="flex items-center gap-3">
              <p className="flex-1 text-xs text-fg-muted">
                Drop a batch of takes anywhere on this page — each file lands on the
                track it is named after, and several files for one track become its
                takes.
              </p>
              <button
                className="sm-button sm-button-quiet"
                type="button"
                onClick={() => void pickTakes()}
              >
                <FolderInput size={15} />
                Import takes
              </button>
            </div>
          {/* Master-detail (#40): the running order on the left, the selected
             track edited in place on the right. The track stopped being a
             destination - clicking through a record's songs no longer walks
             the history stack. */}
          <div className="grid items-start gap-4 md:grid-cols-[280px_minmax(0,1fr)]">
            <div className="sm-panel overflow-hidden">
              <ul className="divide-y divide-edge">
                {tracks.map((track) => (
                  <li key={track.id}>
                    <button
                      className={
                        'flex w-full items-center gap-2 px-3 py-2 text-left ' +
                        (track.id === selected?.id
                          ? 'bg-surface-hover'
                          : 'hover:bg-surface-hover')
                      }
                      type="button"
                      onClick={() => setSelectedId(track.id)}
                    >
                      <span
                        className="group/pos flex w-5 shrink-0 items-center justify-center text-xs tabular-nums text-fg-muted"
                        onClick={(e) => {
                          if (!track.files?.length) return
                          e.stopPropagation()
                          void player.play('track', track.id)
                        }}
                      >
                        {track.files?.length ? (
                          <>
                            <span className="group-hover/pos:hidden">{track.position}</span>
                            <Play
                              size={13}
                              className="hidden text-accent group-hover/pos:block"
                            />
                          </>
                        ) : (
                          track.position
                        )}
                      </span>
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-sm font-medium">{track.name}</span>
                        <span className="block text-xs text-fg-muted">
                          {track.files?.length
                            ? track.files.length +
                              (track.files.length === 1 ? ' take' : ' takes')
                            : 'not made'}
                        </span>
                      </span>
                      <span className="shrink-0 text-xs tabular-nums text-fg-muted">
                        {duration(track.targetSeconds)}
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
              <div className="p-2">
                <button
                  className="flex w-full items-center justify-center gap-2 rounded-md border border-dashed border-edge py-1.5 text-xs text-fg-muted hover:border-accent hover:text-accent"
                  type="button"
                  onClick={() => setInterviewing(true)}
                >
                  <Sparkles size={13} />
                  Write more tracks
                </button>
              </div>
            </div>
            {selected && (
              <TrackEditor
                key={selected.id}
                trackId={selected.id}
                onDeleted={() => {
                  setSelectedId('')
                  reload()
                }}
              />
            )}
          </div>
          </div>
        ))}

      {interviewing && (
        <InterviewDialog
          title={'Write tracks for ' + album.name}
          subjectKind="track"
          subjectId={album.id}
          opening={
            'Writing for **' +
            album.name +
            '**. I have the combined style and the ' +
            (tracks.length ? tracks.length + ' tracks already on it' : 'empty running order') +
            '.\n\nWhat is the next track — or tell me how many you want and what the ' +
            'record is about, and I will propose a set.'
          }
          finishLabel="Add tracks"
          onFinish={async (messages) => {
            try {
              const drafts = await ExtractTracks(messages)
              if (!drafts?.length) return 'The conversation did not settle on a track yet.'
              // Audio dropped into the chat, resolved from the names the
              // extraction assigned back to the stored paths (#24).
              const attached = attachmentPathsByName(messages)
              // Sequentially rather than in parallel: each save reads the highest
              // position to append after it, and five concurrent saves would all
              // read the same one and land on the same number.
              for (const track of drafts) {
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
                // Each assigned file lands as a variation through the same
                // import every other route uses, so it is copied into the music
                // library before ClearChat sweeps the chat's own copy.
                for (const name of track.files ?? []) {
                  const path = attached[name]
                  if (path) await ImportTrackFile(saved.id, path, '')
                }
              }
              await ClearChat('track', album.id).catch(() => {})
              setInterviewing(false)
              reload()
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
