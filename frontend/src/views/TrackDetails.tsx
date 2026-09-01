import {useCallback, useEffect, useState} from 'react'
import {Check, Copy, FolderOpen, ListPlus, Pencil, Play, Plus, Trash2} from 'lucide-react'
import {
  ChooseTrackFile,
  DeleteTrack,
  GetTrack,
  ImportDroppedTrackFile,
  ImportTrackFile,
  RemoveTrackFile,
  RevealTrackFile,
  SaveTrack,
} from '../../wailsjs/go/main/App'
import {main} from '../../wailsjs/go/models'
import {CopyButton} from '../components/CopyButton'
import {StatusPill} from '../components/StatusPill'
import {TabStrip} from '../components/TabStrip'
import {useDataChanged} from '../lib/dataChanged'
import {isAudioPath, readAsDataURL} from '../lib/files'
import {duration, fileSize, parseDuration} from '../lib/format'
import {toMessage} from '../lib/errors'
import {usePlayer} from '../player/PlayerProvider'

/**
 * One track, edited in place beside the album's track list (#40).
 *
 * No page-wide draft and no Save-changes button: each field is read-only until
 * its Edit is pressed, saves alone, and flashes "Saved ✓" when it lands. The
 * old page's whole-form draft made sense when the track was a destination;
 * embedded in the album it meant clicking another track could silently drop
 * edits. A field that is either saved or visibly mid-edit cannot.
 */
type FieldId = 'title' | 'prompt' | 'lyrics' | 'notes'

export function TrackEditor({
  trackId,
  onDeleted,
}: {
  trackId: string
  onDeleted: () => void
}) {
  const [track, setTrack] = useState<main.Track | null>(null)
  const [editing, setEditing] = useState<FieldId | null>(null)
  // Which of the two Suno fields is showing (#49); the style prompt first.
  const [fieldTab, setFieldTab] = useState<'prompt' | 'lyrics'>('prompt')
  const [flashed, setFlashed] = useState<FieldId | null>(null)
  const [error, setError] = useState('')
  const [confirmDelete, setConfirmDelete] = useState(false)

  const reload = useCallback(() => {
    GetTrack(trackId)
      .then((found) => {
        setTrack(found)
        setError('')
      })
      .catch((err) => setError(toMessage(err)))
  }, [trackId])

  useEffect(() => {
    // A different track is a fresh pane: whatever edit was open belonged to
    // the previous one.
    setEditing(null)
    setFieldTab('prompt')
    setFlashed(null)
    setConfirmDelete(false)
    reload()
  }, [reload])
  useDataChanged(['tracks'], reload)

  const save = async (patch: Partial<main.Track>, field: FieldId) => {
    if (!track) return
    setError('')
    try {
      setTrack(await SaveTrack(main.Track.createFrom({...track, ...patch})))
      setEditing(null)
      setFlashed(field)
      setTimeout(() => setFlashed((f) => (f === field ? null : f)), 1600)
    } catch (err) {
      setError(toMessage(err))
    }
  }

  if (!track) {
    return <p className="text-sm text-fg-muted">{error || 'Loading…'}</p>
  }

  return (
    <div className="flex flex-col gap-3">
      {error && <p className="text-sm text-danger">{error}</p>}

      <TitleCard
        track={track}
        editing={editing === 'title'}
        onEdit={() => setEditing('title')}
        onCancel={() => setEditing(null)}
        onSave={(name, lengthText) =>
          save({name, targetSeconds: parseDuration(lengthText)}, 'title')
        }
        onDelete={() => setConfirmDelete(true)}
      />

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
                  onDeleted()
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

      {/* The two Suno fields behind tabs (#49), the style prompt first —
          it is the field written before anything exists to sing. Hidden
          rather than unmounted, so an edit in progress survives a peek at
          the other tab instead of silently losing its draft. */}
      <TabStrip
        tabs={[
          {id: 'prompt', label: 'Style prompt'},
          {id: 'lyrics', label: 'Lyrics'},
        ]}
        current={fieldTab}
        onSelect={setFieldTab}
      />
      <div className={fieldTab === 'prompt' ? '' : 'hidden'}>
      <TextCard
        field="prompt"
        label="Style prompt"
        value={track.prompt}
        emptyText="No prompt written yet."
        placeholder="Comma-led and concrete, most important terms first, ending with what to avoid."
        rows={5}
        mono
        editing={editing === 'prompt'}
        flashed={flashed === 'prompt'}
        onEdit={() => setEditing('prompt')}
        onCancel={() => setEditing(null)}
        onSave={(prompt) => save({prompt}, 'prompt')}
      />
      </div>

      <div className={fieldTab === 'lyrics' ? '' : 'hidden'}>
      <TextCard
        field="lyrics"
        label="Lyrics"
        value={track.lyrics}
        emptyText="No lyrics yet."
        placeholder="With [Section] tags. For an instrumental, the tags alone."
        rows={12}
        mono
        pre
        editing={editing === 'lyrics'}
        flashed={flashed === 'lyrics'}
        onEdit={() => setEditing('lyrics')}
        onCancel={() => setEditing(null)}
        onSave={(lyrics) => save({lyrics}, 'lyrics')}
      />
      </div>

      <TextCard
        field="notes"
        label="Notes"
        value={track.notes}
        emptyText="Anything else worth remembering about this track. Not sent to Suno."
        placeholder=""
        rows={3}
        copyable={false}
        editing={editing === 'notes'}
        flashed={flashed === 'notes'}
        onEdit={() => setEditing('notes')}
        onCancel={() => setEditing(null)}
        onSave={(notes) => save({notes}, 'notes')}
      />

      <Takes track={track} onChanged={setTrack} onError={setError} />
    </div>
  )
}

/**
 * The name and target length share a card and an edit: they read as one line
 * ("Neon Rain · 3:20") and a producer renaming a track is usually re-aiming
 * its length in the same breath.
 */
function TitleCard({
  track,
  editing,
  onEdit,
  onCancel,
  onSave,
  onDelete,
}: {
  track: main.Track
  editing: boolean
  onEdit: () => void
  onCancel: () => void
  onSave: (name: string, lengthText: string) => void
  onDelete: () => void
}) {
  const player = usePlayer()
  const [name, setName] = useState(track.name)
  const [lengthText, setLengthText] = useState(duration(track.targetSeconds))
  useEffect(() => {
    setName(track.name)
    setLengthText(duration(track.targetSeconds))
  }, [track.id, track.name, track.targetSeconds, editing])

  return (
    <div className="group flex items-center gap-3 rounded-lg border border-edge p-3 hover:bg-surface-hover">
      {editing ? (
        <>
          <input
            className="sm-input min-w-0 flex-1"
            value={name}
            autoFocus
            onChange={(e) => setName(e.target.value)}
          />
          <input
            className="sm-input w-24 tabular-nums"
            title="Target length — m:ss or seconds"
            value={lengthText}
            onChange={(e) => setLengthText(e.target.value)}
          />
          <button
            className="sm-button sm-button-primary"
            type="button"
            onClick={() => onSave(name, lengthText)}
          >
            Save
          </button>
          <button className="sm-button sm-button-quiet" type="button" onClick={onCancel}>
            Cancel
          </button>
        </>
      ) : (
        <>
          {(track.files?.length ?? 0) > 0 && (
            <>
              <button
                className="sm-button sm-button-quiet px-2"
                type="button"
                title="Play this track"
                aria-label="Play this track"
                onClick={() => void player.play('track', track.id)}
              >
                <Play size={15} />
              </button>
              {/* Just this track, behind the queue (#60) — play here queues
                  the whole record (#55); adding composes one song at a time. */}
              <button
                className="sm-button sm-button-quiet px-2"
                type="button"
                title="Add this track to the queue"
                aria-label="Add this track to the queue"
                onClick={() => void player.enqueue('track', track.id)}
              >
                <ListPlus size={15} />
              </button>
            </>
          )}
          <span className="flex min-w-0 flex-1 items-center gap-2">
            <span className="truncate text-base font-semibold">{track.name}</span>
            {/* The title is the third thing Suno asks for beside the prompt
                and lyrics, and those two already have Copy (#74). */}
            <CopyButton text={track.name} label="Copy the track title" />
            <span className="shrink-0 text-xs tabular-nums text-fg-muted">
              {duration(track.targetSeconds)}
            </span>
          </span>
          <span className="flex shrink-0 gap-1 opacity-0 transition-opacity group-focus-within:opacity-100 group-hover:opacity-100">
            <button
              className="sm-button sm-button-quiet px-2 py-1 text-xs"
              type="button"
              onClick={onEdit}
            >
              <Pencil size={12} />
              Edit
            </button>
            <button
              className="sm-button sm-button-quiet px-2 py-1 text-xs"
              type="button"
              title="Delete this track"
              aria-label="Delete this track"
              onClick={onDelete}
            >
              <Trash2 size={12} />
            </button>
          </span>
        </>
      )}
    </div>
  )
}

/**
 * One editable text field as the design's card: label, hover-revealed Edit and
 * Copy, a read view until Edit is pressed, and a "Saved ✓" that flashes when a
 * save lands and fades on its own.
 */
function TextCard({
  field,
  label,
  value,
  emptyText,
  placeholder,
  rows,
  mono = false,
  pre = false,
  copyable = true,
  editing,
  flashed,
  onEdit,
  onCancel,
  onSave,
}: {
  field: FieldId
  label: string
  value: string
  emptyText: string
  placeholder: string
  rows: number
  mono?: boolean
  pre?: boolean
  copyable?: boolean
  editing: boolean
  flashed: boolean
  onEdit: () => void
  onCancel: () => void
  onSave: (next: string) => void
}) {
  const [draft, setDraft] = useState(value)
  const [copied, setCopied] = useState(false)
  useEffect(() => setDraft(value), [value, editing])

  const bodyClass =
    'whitespace-pre-wrap text-sm leading-relaxed' + (mono ? ' font-mono text-xs' : '')

  return (
    <div className="group flex flex-col gap-2 rounded-lg border border-edge p-3 hover:bg-surface-hover">
      <div className="flex items-center gap-2">
        <span className="flex-1 text-xs font-semibold uppercase tracking-wide text-fg-muted">
          {label}
        </span>
        {flashed && <span className="text-xs text-ok">Saved ✓</span>}
        {!editing && (
          <span className="flex gap-1 opacity-0 transition-opacity group-focus-within:opacity-100 group-hover:opacity-100">
            <button
              className="sm-button sm-button-quiet px-2 py-1 text-xs"
              type="button"
              onClick={onEdit}
            >
              <Pencil size={12} />
              Edit
            </button>
            {copyable && (
              <button
                className="sm-button sm-button-quiet px-2 py-1 text-xs"
                type="button"
                disabled={!value}
                onClick={() => {
                  void navigator.clipboard.writeText(value).then(() => {
                    setCopied(true)
                    // Reverts on its own: a button stuck reading "Copied" is a
                    // button that stops saying anything about the next press.
                    setTimeout(() => setCopied(false), 1600)
                  })
                }}
              >
                {copied ? <Check size={12} /> : <Copy size={12} />}
                {copied ? 'Copied' : 'Copy'}
              </button>
            )}
          </span>
        )}
      </div>

      {editing ? (
        <>
          <textarea
            className={'sm-input resize-y' + (mono ? ' font-mono text-xs' : '')}
            rows={rows}
            value={draft}
            autoFocus
            placeholder={placeholder}
            onChange={(e) => setDraft(e.target.value)}
          />
          <div className="flex justify-end gap-2">
            <button className="sm-button sm-button-quiet" type="button" onClick={onCancel}>
              Cancel
            </button>
            <button
              className="sm-button sm-button-primary"
              type="button"
              onClick={() => onSave(draft)}
            >
              Save {field === 'title' ? '' : field}
            </button>
          </div>
        </>
      ) : value ? (
        pre ? (
          <pre className={bodyClass + ' font-mono'}>{value}</pre>
        ) : (
          <p className={bodyClass}>{value}</p>
        )
      ) : (
        <p className="text-sm text-fg-muted">{emptyText}</p>
      )}
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
  // Whether a drag is hovering the panel (#57) — the ring is the "drop it
  // here" answer to the file being carried over the window.
  const [dragging, setDragging] = useState(false)
  const act = async (work: Promise<main.Track>) => {
    try {
      onChanged(await work)
      onError('')
    } catch (err) {
      onError(toMessage(err))
    }
  }

  // Dropped files arrive as bytes, not paths — the webview keeps the disk
  // location to itself — so each rides the bridge as a data URL and the
  // backend files it exactly as if it had been picked (#57). Sequential, so
  // two takes dropped together get distinct numbered names.
  const importDropped = async (dropped: FileList) => {
    for (const file of Array.from(dropped)) {
      if (!isAudioPath(file.name) && !file.type.startsWith('audio/')) {
        onError(file.name + ' is not an audio file this app knows')
        continue
      }
      await act(
        ImportDroppedTrackFile(track.id, file.name, await readAsDataURL(file), ''),
      )
    }
  }

  return (
    <section
      className={'sm-panel space-y-3 p-4' + (dragging ? ' ring-2 ring-accent' : '')}
      // The bubble stops here (#84): the album page behind this panel is a
      // drop zone of its own, and a take dropped on a specific track must land
      // on that track once — not once here and once more by name.
      onDragOver={(event) => {
        event.preventDefault()
        event.stopPropagation()
        setDragging(true)
      }}
      onDragLeave={() => setDragging(false)}
      onDrop={(event) => {
        event.preventDefault()
        event.stopPropagation()
        setDragging(false)
        if (event.dataTransfer.files.length) void importDropped(event.dataTransfer.files)
      }}
    >
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
          Nothing imported yet — drop the file here, or import it above. Files are
          copied into your music folder under{' '}
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
