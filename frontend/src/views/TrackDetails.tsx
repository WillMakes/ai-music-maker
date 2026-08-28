import {useCallback, useEffect, useState} from 'react'
import {Check, Copy, FolderOpen, Plus, Trash2} from 'lucide-react'
import {
  ChooseTrackFile,
  DeleteTrack,
  GetTrack,
  ImportTrackFile,
  RemoveTrackFile,
  RevealTrackFile,
  SaveTrack,
} from '../../wailsjs/go/main/App'
import {main} from '../../wailsjs/go/models'
import {PageHeader} from '../components/PageHeader'
import {TextAreaField, TextField} from '../components/Field'
import {StatusPill} from '../components/StatusPill'
import {useDataChanged} from '../lib/dataChanged'
import {duration, fileSize, parseDuration} from '../lib/format'
import {toMessage} from '../lib/errors'

/**
 * One track: the three fields Suno's form asks for, and the takes that came
 * back.
 *
 * The prompt and the lyrics have Copy buttons as well as the browser extension,
 * because the extension only helps on suno.com — a producer on the mobile app,
 * or pasting into a note, still needs the text.
 */
export function TrackDetails({
  trackId,
  onOpenAlbum,
  onDeleted,
}: {
  trackId: string
  onOpenAlbum: (albumId: string) => void
  onDeleted: (albumId: string) => void
}) {
  const [track, setTrack] = useState<main.Track | null>(null)
  const [draft, setDraft] = useState<main.Track | null>(null)
  const [lengthText, setLengthText] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [confirmDelete, setConfirmDelete] = useState(false)

  const reload = useCallback(() => {
    GetTrack(trackId)
      .then((found) => {
        setTrack(found)
        setDraft((current) => {
          if (current) return current
          setLengthText(duration(found.targetSeconds))
          return main.Track.createFrom(found)
        })
        setError('')
      })
      .catch((err) => setError(toMessage(err)))
  }, [trackId])

  useEffect(reload, [reload])
  useDataChanged(['tracks'], reload)

  const update = (patch: Partial<main.Track>) =>
    setDraft((current) => (current ? main.Track.createFrom({...current, ...patch}) : current))

  const save = async () => {
    if (!draft) return
    setSaving(true)
    setError('')
    try {
      const saved = await SaveTrack(
        main.Track.createFrom({...draft, targetSeconds: parseDuration(lengthText)}),
      )
      setTrack(saved)
      setDraft(main.Track.createFrom(saved))
      setLengthText(duration(saved.targetSeconds))
    } catch (err) {
      setError(toMessage(err))
    } finally {
      setSaving(false)
    }
  }

  if (!track || !draft) {
    return <p className="text-sm text-fg-muted">{error || 'Loading…'}</p>
  }

  const dirty =
    JSON.stringify({...draft, targetSeconds: parseDuration(lengthText)}) !==
    JSON.stringify(track)

  return (
    <div className="space-y-5">
      <PageHeader
        title={track.name}
        breadcrumb={
          <button
            className="hover:text-fg hover:underline"
            type="button"
            onClick={() => onOpenAlbum(track.albumId)}
          >
            {track.artistName} — {track.albumName}
          </button>
        }
        subtitle={'Track ' + track.position}
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
            Delete <strong>{track.name}</strong>? The audio files stay in your music
            folder.
          </p>
          <div className="flex gap-2">
            <button
              className="sm-button sm-button-danger"
              type="button"
              onClick={async () => {
                try {
                  await DeleteTrack(trackId)
                  onDeleted(track.albumId)
                } catch (err) {
                  setError(toMessage(err))
                }
              }}
            >
              Delete the track
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
        <h2 className="text-sm font-semibold">For Suno</h2>
        <p className="text-xs text-fg-muted">
          These are the three fields Suno's create form asks for. With the extension
          installed, its buttons fill them from here.
        </p>
        <div className="grid gap-4 md:grid-cols-[1fr_9rem]">
          <TextField
            label="Title"
            required
            value={draft.name}
            onChange={(name) => update({name})}
          />
          <TextField
            label="Target length"
            hint="m:ss or seconds"
            value={lengthText}
            onChange={setLengthText}
          />
        </div>
        <Copyable label="Style prompt" value={draft.prompt}>
          <TextAreaField
            label="Style prompt"
            mono
            rows={9}
            hint="Comma-led and concrete, most important terms first, ending with what to avoid."
            value={draft.prompt}
            onChange={(prompt) => update({prompt})}
          />
        </Copyable>
        <Copyable label="Lyrics" value={draft.lyrics}>
          <TextAreaField
            label="Lyrics"
            mono
            rows={16}
            hint="With [Section] tags. For an instrumental, the tags alone."
            value={draft.lyrics}
            onChange={(lyrics) => update({lyrics})}
          />
        </Copyable>
        <TextAreaField
          label="Notes"
          rows={3}
          hint="Anything else worth remembering about this track. Not sent to Suno."
          value={draft.notes}
          onChange={(notes) => update({notes})}
        />
      </section>

      <Takes track={track} onChanged={setTrack} onError={setError} />
    </div>
  )
}

/**
 * A field with a Copy button over it.
 *
 * The button sits above rather than inside the textarea: inside, it covers the
 * first line of a lyric sheet, which is the line most worth reading.
 */
function Copyable({
  label,
  value,
  children,
}: {
  label: string
  value: string
  children: React.ReactNode
}) {
  const [copied, setCopied] = useState(false)
  return (
    <div className="space-y-1">
      <div className="flex justify-end">
        <button
          className="sm-button sm-button-quiet px-2 py-1 text-xs"
          type="button"
          disabled={!value}
          onClick={() => {
            void navigator.clipboard.writeText(value).then(() => {
              setCopied(true)
              // Reverts on its own: a button stuck reading "Copied" is a button
              // that stops saying anything about the next press.
              setTimeout(() => setCopied(false), 1600)
            })
          }}
        >
          {copied ? <Check size={13} /> : <Copy size={13} />}
          {copied ? 'Copied' : 'Copy ' + label.toLowerCase()}
        </button>
      </div>
      {children}
    </div>
  )
}

/**
 * The files Suno gave back.
 *
 * Exactly one is the take that is the track, which is why choosing is a radio
 * rather than a per-row toggle: "which of these is the song" has one answer, and
 * a list where three are ticked has not answered it.
 */
function Takes({
  track,
  onChanged,
  onError,
}: {
  track: main.Track
  onChanged: (next: main.Track) => void
  onError: (message: string) => void
}) {
  const files = track.files ?? []
  const act = async (work: Promise<main.Track>) => {
    try {
      onChanged(await work)
      onError('')
    } catch (err) {
      onError(toMessage(err))
    }
  }

  return (
    <section className="sm-panel space-y-3 p-4">
      <div className="flex items-center gap-3">
        <h2 className="flex-1 text-sm font-semibold">Takes</h2>
        <button
          className="sm-button sm-button-quiet"
          type="button"
          onClick={() => {
            const path = window.prompt('Path to the audio file Suno gave you')
            if (!path) return
            void act(ImportTrackFile(track.id, path, ''))
          }}
        >
          <Plus size={15} />
          Import a file
        </button>
      </div>

      {files.length === 0 ? (
        <p className="text-sm text-fg-muted">
          Nothing imported yet. Files are copied into your music folder under{' '}
          <span className="font-mono text-xs">SunoMaker/Artist/Album</span>, so a music
          player finds them.
        </p>
      ) : (
        <ul className="divide-y divide-edge">
          {files.map((file) => (
            <li key={file.id} className="flex items-center gap-3 py-2">
              <input
                type="radio"
                name={'take-' + track.id}
                checked={file.chosen}
                aria-label={'Use ' + file.label}
                onChange={() => void act(ChooseTrackFile(track.id, file.id))}
              />
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm">{file.label}</span>
                <span className="block truncate font-mono text-xs text-fg-muted">
                  {file.name}
                </span>
              </span>
              {file.chosen && <StatusPill tone="ok">The take</StatusPill>}
              <span className="shrink-0 text-xs tabular-nums text-fg-muted">
                {fileSize(file.bytes)}
              </span>
              <button
                className="sm-button sm-button-quiet px-2"
                type="button"
                title="Show in folder"
                aria-label="Show in folder"
                onClick={() => void RevealTrackFile(file.path).catch(() => {})}
              >
                <FolderOpen size={15} />
              </button>
              <button
                className="sm-button sm-button-quiet px-2"
                type="button"
                title="Forget this take"
                aria-label="Forget this take"
                // deleteFile stays false: forgetting a row is undoable by
                // importing it again, and deleting somebody's music is not.
                onClick={() => void act(RemoveTrackFile(track.id, file.id, false))}
              >
                <Trash2 size={15} />
              </button>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
