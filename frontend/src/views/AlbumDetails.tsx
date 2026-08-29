import {useCallback, useEffect, useState} from 'react'
import {ImagePlus, ListMusic, RefreshCw, Sparkles, Trash2, Wand2} from 'lucide-react'
import {
  ClearChat,
  DeleteAlbum,
  ExtractTracks,
  GenerateAlbumArtwork,
  GetAlbum,
  ImportAlbumArtwork,
  ImportTrackFile,
  ListTracks,
  RemoveAlbumArtwork,
  SaveAlbum,
  SaveTrack,
} from '../../wailsjs/go/main/App'
import {main} from '../../wailsjs/go/models'
import {EmptyState} from '../components/EmptyState'
import {PageHeader} from '../components/PageHeader'
import {TextAreaField, TextField} from '../components/Field'
import {InterviewDialog} from '../chat/InterviewDialog'
import {Cover} from './ArtistDetails'
import {useDataChanged} from '../lib/dataChanged'
import {duration} from '../lib/format'
import {toMessage} from '../lib/errors'
import {attachmentPathsByName} from '../lib/files'

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
  onOpenTrack,
  onOpenArtist,
  onDeleted,
}: {
  albumId: string
  onOpenTrack: (trackId: string) => void
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

      <div className="grid gap-4 lg:grid-cols-[240px_1fr]">
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
                // The path comes from the producer typing or pasting one. A file
                // picker would be better and Wails' dialog needs the window's
                // context, which this component does not have — so the binding
                // takes a path and the field says so.
                const path = window.prompt('Path to an image file')
                if (!path) return
                try {
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

        <div className="space-y-4">
          <section className="sm-panel space-y-4 p-4">
            <h2 className="text-sm font-semibold">The record</h2>
            <TextField
              label="Name"
              required
              value={draft.name}
              onChange={(name) => update({name})}
            />
            <TextAreaField
              label="Technical style"
              mono
              rows={8}
              hint={
                'Only what differs from ' +
                album.artistName +
                "'s base style — the two are joined when a track prompt is written."
              }
              value={draft.technicalStyle}
              onChange={(technicalStyle) => update({technicalStyle})}
            />
          </section>

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
      </div>

      <section className="space-y-3">
        <div className="flex items-center gap-3">
          <h2 className="flex-1 text-sm font-semibold">Tracks</h2>
          <button
            className="sm-button sm-button-primary"
            type="button"
            onClick={() => setInterviewing(true)}
          >
            <Sparkles size={15} />
            Write tracks
          </button>
        </div>

        {tracks.length === 0 ? (
          <EmptyState
            icon={ListMusic}
            title="No tracks yet"
            body="The interview knows this album's combined style and what is already on it. Ask for one track or for five."
          />
        ) : (
          <ul className="sm-panel divide-y divide-edge overflow-hidden">
            {tracks.map((track) => (
              <li key={track.id}>
                <button
                  className="flex w-full items-center gap-3 px-4 py-3 text-left hover:bg-surface-hover"
                  type="button"
                  onClick={() => onOpenTrack(track.id)}
                >
                  <span className="w-6 shrink-0 text-sm tabular-nums text-fg-muted">
                    {track.position}
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="block truncate font-medium">{track.name}</span>
                    <span className="block truncate text-xs text-fg-muted">
                      {track.prompt ? track.prompt.slice(0, 90) : 'No prompt written yet'}
                    </span>
                  </span>
                  <span className="shrink-0 text-xs tabular-nums text-fg-muted">
                    {duration(track.targetSeconds)}
                  </span>
                  <span className="shrink-0 text-xs text-fg-muted">
                    {track.files?.length
                      ? track.files.length + (track.files.length === 1 ? ' take' : ' takes')
                      : 'not made'}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>

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
