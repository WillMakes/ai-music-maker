import {useCallback, useEffect, useState} from 'react'
import {ImagePlus, ListMusic, ListPlus, Loader2, Play, RefreshCw, Sparkles, Trash2, Wand2} from 'lucide-react'
import {TrackEditor} from './TrackDetails'
import {
  ClearChat,
  DeleteAlbum,
  EvolveAlbum,
  ExtractTracks,
  GenerateAlbumArtwork,
  GenerateAlbumStyle,
  GetAlbum,
  ImportAlbumArtwork,
  PickImageFile,
  ImportTrackFile,
  ListTracks,
  RemoveAlbumArtwork,
  SaveAlbum,
  SaveTrack,
} from '../../wailsjs/go/main/App'
import {main} from '../../wailsjs/go/models'
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
import {attachmentPathsByName} from '../lib/files'
import {usePlayer} from '../player/PlayerProvider'

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
  const [saving, setSaving] = useState(false)
  const [drawing, setDrawing] = useState(false)
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
  // Which track the detail pane shows (#40). Local state like the tab itself:
  // the album is the place, the selection is a drawer within it.
  const [selectedId, setSelectedId] = useState(trackId ?? '')
  // Evolving runs for as long as a big generation call does, so it gets its
  // own busy state like the artwork's (#42).
  const [evolving, setEvolving] = useState(false)
  const player = usePlayer()

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

  const update = (patch: Partial<main.Album>) =>
    setDraft((current) => (current ? main.Album.createFrom({...current, ...patch}) : current))

  const save = async () => {
    if (!draft) return
    setSaving(true)
    setError('')
    try {
      const saved = await SaveAlbum(draft)
      setAlbum(saved)
      setDraft(main.Album.createFrom(saved))
    } catch (err) {
      setError(toMessage(err))
    } finally {
      setSaving(false)
    }
  }

  const draw = async () => {
    setDrawing(true)
    setError('')
    try {
      const updated = await GenerateAlbumArtwork(albumId, feedback)
      setAlbum(updated)
      // The fields the producer may be editing are left alone; only the cover
      // and the brief behind it are taken from the answer.
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
    } catch (err) {
      setError(toMessage(err))
    } finally {
      setDrawing(false)
    }
  }

  if (!album || !draft) {
    return <p className="text-sm text-fg-muted">{error || 'Loading…'}</p>
  }

  const dirty = JSON.stringify(draft) !== JSON.stringify(album)
  // The selection falls back to the first track, so the pane is never blank
  // while the record has songs - and heals when the selected one is deleted.
  const selected = tracks.find((t) => t.id === selectedId) ?? tracks[0]

  return (
    <div className="space-y-5">
      <PageHeader
        title={album.name}
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
              onClick={async () => {
                setEvolving(true)
                setError('')
                try {
                  const next = await EvolveAlbum(albumId)
                  onOpenAlbum(next.id)
                } catch (err) {
                  setError(toMessage(err))
                } finally {
                  setEvolving(false)
                }
              }}
            >
              {evolving ? <Loader2 size={15} className="animate-spin" /> : <Sparkles size={15} />}
              {evolving ? 'Evolving…' : 'Create the next record'}
            </button>
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
              body="The interview knows this album's combined style and what is already on it. Ask for one track or for five."
            />
            <div className="flex justify-center">
              <button
                className="sm-button sm-button-primary"
                type="button"
                onClick={() => setInterviewing(true)}
              >
                <Sparkles size={15} />
                Write tracks
              </button>
            </div>
          </div>
        ) : (
          /* Master-detail (#40): the running order on the left, the selected
             track edited in place on the right. The track stopped being a
             destination - clicking through a record's songs no longer walks
             the history stack. */
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
