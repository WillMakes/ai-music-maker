import {useCallback, useEffect, useState} from 'react'
import {AlertTriangle, Check, Copy, FolderOpen, Pencil, Plus, Star, Trash2} from 'lucide-react'
import {
  ChooseTrackFile,
  GetTrack,
  ImportDroppedTrackFile,
  ImportTrackFile,
  RemoveTrackFile,
  RevealTrackFile,
  SaveTrack,
} from '../../wailsjs/go/app/App'
import {app} from '../../wailsjs/go/models'
import {TabStrip} from '../components/TabStrip'
import {useDataChanged} from '../lib/dataChanged'
import {isAudioPath, readAsDataURL} from '../lib/files'
import {fileSize} from '../lib/format'
import {toMessage} from '../lib/errors'

/**
 * One track, edited in place beside the album's track list (#40).
 *
 * No page-wide draft and no Save-changes button: each field is read-only until
 * its Edit is pressed, saves alone, and flashes "Saved ✓" when it lands. The
 * old page's whole-form draft made sense when the track was a destination;
 * embedded in the album it meant clicking another track could silently drop
 * edits. A field that is either saved or visibly mid-edit cannot.
 *
 * The title, length, and the whole-track actions are on the track's row in
 * the list (#115), not repeated in a card above this pane.
 */
type FieldId = 'prompt' | 'lyrics' | 'notes'

export function TrackEditor({trackId}: {trackId: string}) {
  const [track, setTrack] = useState<app.Track | null>(null)
  const [editing, setEditing] = useState<FieldId | null>(null)
  // Which of the two Suno fields is showing (#49); the style prompt first.
  const [fieldTab, setFieldTab] = useState<'prompt' | 'lyrics'>('prompt')
  const [flashed, setFlashed] = useState<FieldId | null>(null)
  const [error, setError] = useState('')

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
    reload()
  }, [reload])
  useDataChanged(['tracks'], reload)

  const save = async (patch: Partial<app.Track>, field: FieldId) => {
    if (!track) return
    setError('')
    try {
      setTrack(await SaveTrack(app.Track.createFrom({...track, ...patch})))
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
              Save {field}
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
 * The files Suno gave back, every track's in one list (#144, #146): a short
 * heading per track and a line per take, so the whole record's takes read at
 * a glance on the album page's Takes tab rather than as a panel per track.
 *
 * Exactly one take per track is the official one, which is why choosing is a
 * star rather than a per-row toggle: "which of these is the song" has one
 * answer, and a list where three are ticked has not answered it.
 */
export function TakesList({
  tracks,
  onChanged,
  onError,
}: {
  tracks: app.Track[]
  onChanged: () => void
  onError: (message: string) => void
}) {
  return (
    <ul className="sm-panel divide-y divide-edge">
      {tracks.map((track) => (
        <TakeGroup key={track.id} track={track} onChanged={onChanged} onError={onError} />
      ))}
    </ul>
  )
}

function TakeGroup({
  track,
  onChanged,
  onError,
}: {
  track: app.Track
  onChanged: () => void
  onError: (message: string) => void
}) {
  const files = track.files ?? []
  // Whether a drag is hovering this track's group (#57) — the ring is the
  // "drop it here" answer to the file being carried over the window.
  const [dragging, setDragging] = useState(false)
  const act = async (work: Promise<app.Track>) => {
    try {
      await work
      onChanged()
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
    <li
      className={'px-3 py-1.5' + (dragging ? ' ring-2 ring-inset ring-accent' : '')}
      // The bubble stops here (#84): the album page behind this list is a drop
      // zone of its own, and a take dropped on a specific track must land on
      // that track once — not once here and once more by name.
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
      <div className="flex items-center gap-2">
        <span className="min-w-0 flex-1 truncate text-xs font-semibold text-fg-muted">
          {track.position}. {track.name}
        </span>
        {files.length === 0 && <span className="text-xs text-fg-muted">No takes yet</span>}
        <button
          className="sm-button sm-button-quiet border-0 px-1.5 py-0.5"
          type="button"
          title="Import a file"
          aria-label={`Import a take for ${track.name}`}
          onClick={() => {
            const path = window.prompt('Path to the audio file Suno gave you')
            if (!path) return
            void act(ImportTrackFile(track.id, path, ''))
          }}
        >
          <Plus size={13} />
        </button>
      </div>
      {files.length > 0 && (
        <ul>
          {files.map((file) => (
            <li key={file.id} className="flex items-center gap-2 py-0.5">
              {/* The star is the choice (#86): the filled one is the official
                  take — the one the extension and the release act on — and
                  every other take reads as a remix of it. */}
              <button
                className={
                  'shrink-0 p-0.5 transition-colors ' +
                  (file.chosen ? 'text-warn' : 'text-fg-muted hover:text-fg')
                }
                type="button"
                title={file.chosen ? 'The official take' : 'Star as the official take'}
                aria-label={file.chosen ? 'The official take' : 'Star as the official take'}
                aria-pressed={file.chosen}
                onClick={() => {
                  if (!file.chosen) void act(ChooseTrackFile(track.id, file.id))
                }}
              >
                <Star size={14} fill={file.chosen ? 'currentColor' : 'none'} />
              </button>
              <span className="min-w-0 flex-1 truncate text-sm" title={file.name}>
                {file.label}
              </span>
              {/* Filed by path, and the file has gone from it (#105) — moved or
                  deleted outside the app. Said on the take rather than left for
                  the player to fail on. */}
              {file.missing && (
                <span className="shrink-0 text-danger" title={'The file is no longer at ' + file.path}>
                  <AlertTriangle size={13} aria-label="The file is missing" />
                </span>
              )}
              <span className="shrink-0 text-xs tabular-nums text-fg-muted">
                {fileSize(file.bytes)}
              </span>
              <button
                className="sm-button sm-button-quiet border-0 px-1.5 py-0.5"
                type="button"
                title="Show in folder"
                aria-label="Show in folder"
                onClick={() => void RevealTrackFile(file.path).catch(() => {})}
              >
                <FolderOpen size={13} />
              </button>
              <button
                className="sm-button sm-button-quiet border-0 px-1.5 py-0.5"
                type="button"
                title="Forget this take"
                aria-label="Forget this take"
                // deleteFile stays false: forgetting a row is undoable by
                // importing it again, and deleting somebody's music is not.
                onClick={() => void act(RemoveTrackFile(track.id, file.id, false))}
              >
                <Trash2 size={13} />
              </button>
            </li>
          ))}
        </ul>
      )}
    </li>
  )
}
