import {useEffect, useRef, useState, type ClipboardEvent, type DragEvent} from 'react'
import {ChevronRight, FileArchive, Film, Paperclip, X} from 'lucide-react'
import {AttachBugMedia, FileBugReport, ReadBugImage} from '../../wailsjs/go/app/App'
import {app} from '../../wailsjs/go/models'
import {MarkdownField} from '../components/MarkdownField'
import {Modal} from '../components/Modal'
import {toMessage} from '../lib/errors'
import {routeTrail} from '../navigation'

/**
 * The bug button's box.
 *
 * One field, a checkbox, and the files (#20). Everything else worth recording —
 * the page, the record it was showing, when — the app already knows and puts on
 * the issue itself (see issueDetails in bugissues.go). A form that asked
 * somebody who just found a bug to also summarise it, categorise it and retype
 * where they were is a form that gets closed.
 *
 * It is written to cost a sentence and a keystroke: the description takes the
 * caret, Ctrl/Cmd+Enter files from anywhere inside (#20) — a bug is usually
 * found mid-something, and reaching for the mouse is the slow part — and a
 * screenshot can be pasted, dropped or picked without saving first.
 *
 * Filing closes it, with nothing in between (#18): the queue is in Settings →
 * Bug reports if the producer wants it, and the badge in the top bar has
 * already gone up by one. The only thing that keeps this box open is an error
 * worth reading.
 */
export function ReportBugModal({
  route,
  subjectKind,
  subjectId,
  onClose,
}: {
  /** The view the producer was on, as navigation.ts names it. */
  route: string
  /** What the page was showing, where it was about a record. */
  subjectKind: string
  subjectId: string
  onClose: () => void
}) {
  const [description, setDescription] = useState('')
  const [global, setGlobal] = useState(false)
  const [filing, setFiling] = useState(false)
  const [error, setError] = useState('')

  const [attachments, setAttachments] = useState<string[]>([])
  // The picture bytes for the thumbnails, by path. Read back through a binding
  // because a webview will not load a file:// url, and kept apart from the
  // paths so the list stays the thing that gets filed.
  const [previews, setPreviews] = useState<Record<string, string>>({})
  // One flag for all three ways in — paste, drop and pick — because to the form
  // they are the same errand: a file is being written and a tile should say so.
  const [attaching, setAttaching] = useState(false)
  // A file is being dragged over the dialog, for the drop target's highlight.
  const [dragging, setDragging] = useState(false)
  // The manual picker, opened by the Attach button. Hidden rather than styled,
  // because the button beside it is the thing worth looking at.
  const fileInputRef = useRef<HTMLInputElement>(null)

  // A folder for files pasted before the report exists. Reports are created on
  // file, and somebody pastes a screenshot before they have finished the
  // sentence — so the files go somewhere named after the attempt and the
  // backend moves them under the report once it has an id (see
  // adoptAttachments in bugattachments.go).
  const [draftID] = useState(
    () => `draft-${Math.random().toString(36).slice(2, 10)}`,
  )

  // The trail the report will record, shown rather than asked: where the
  // reporter was standing is a fact, and the moment it is choosable it becomes
  // a guess — which is the one thing this field exists not to be (#20).
  const trail = routeTrail(route)

  // The one path all three ways in share: read the file to a data URL and hand
  // it to the backend, which validates the type and writes it. One at a time,
  // so a dropped folder of six does not build six data URLs at once.
  const attach = async (files: File[]) => {
    const keep = files.filter(attachable)
    if (keep.length === 0) return
    setAttaching(true)
    setError('')
    try {
      for (const file of keep) {
        // eslint-disable-next-line no-await-in-loop
        const dataURL = await readAsDataURL(file)
        // eslint-disable-next-line no-await-in-loop
        const path = await AttachBugMedia(draftID, dataURL)
        setAttachments((all) => [...all, path])
        if (isImage(path)) {
          // The bytes are already in hand; drawing the thumbnail from them
          // saves reading the file straight back over the bindings.
          setPreviews((all) => ({...all, [path]: dataURL}))
        }
      }
    } catch (err) {
      setError(toMessage(err))
    } finally {
      setAttaching(false)
    }
  }

  // A thumbnail for any image that arrived without its bytes. attach() seeds
  // the preview from the data URL it already holds, so today this reads
  // nothing back — but the render below asks the map rather than assuming,
  // and this is what keeps that honest.
  useEffect(() => {
    for (const path of attachments) {
      if (previews[path] || !isImage(path)) continue
      ReadBugImage(path)
        .then((data) => setPreviews((all) => ({...all, [path]: data})))
        // A picture that has gone is not worth an error over the report it is
        // attached to; the tile simply stays blank.
        .catch(() => {})
    }
  }, [attachments, previews])

  const paste = async (event: ClipboardEvent) => {
    // Both ways something arrives on a clipboard. A screenshot is an inline
    // image item; a recording or a zip is almost always a copied *file*,
    // because nothing puts sixty megabytes on a clipboard as bytes — Finder
    // and Explorer both hand over a reference, which reaches the webview as a
    // file.
    const items = Array.from(event.clipboardData?.items ?? [])
    const inline = items.find((item) => item.type.startsWith('image/'))?.getAsFile()
    const copied = Array.from(event.clipboardData?.files ?? []).filter(attachable)
    const files = inline ? [inline] : copied
    if (files.length === 0) return
    // Only once there is something to attach: letting the default through for
    // ordinary text is what keeps this a paste handler rather than a paste
    // replacement.
    event.preventDefault()
    await attach(files)
  }

  const drop = async (event: DragEvent) => {
    const files = Array.from(event.dataTransfer?.files ?? [])
    setDragging(false)
    if (!files.some(attachable)) return
    // The default is held onto only when there is something to take — a drop
    // of anything else is left for the OS to do whatever it would have.
    event.preventDefault()
    await attach(files)
  }

  const file = async () => {
    if (!description.trim() || filing) return
    setFiling(true)
    setError('')
    try {
      await FileBugReport(
        app.BugReport.createFrom({
          description,
          // The route is kept whether or not the bug is app-wide (#20): global
          // says the problem is not confined to that screen; the route still
          // says where it showed up.
          route,
          global,
          subjectKind: global ? '' : subjectKind,
          subjectId: global ? '' : subjectId,
          attachments,
        }),
      )
      onClose()
    } catch (err) {
      setError(toMessage(err))
      setFiling(false)
    }
    // Deliberately no setFiling(false) on success: the box is closing, and
    // clearing the flag would flip the button back to "File it" for the frame
    // before it goes.
  }

  return (
    <Modal
      title="Report a bug"
      onClose={onClose}
      footer={
        <>
          <button className="sm-button sm-button-quiet" type="button" onClick={onClose}>
            Cancel
          </button>
          <button
            className="sm-button sm-button-primary"
            type="button"
            onClick={file}
            disabled={filing || !description.trim()}
          >
            {filing ? 'Filing…' : 'File it'}
          </button>
        </>
      }
    >
      <div
        className={
          'space-y-3 rounded-lg transition-colors ' +
          (dragging ? 'outline-dashed outline-2 outline-offset-4 outline-accent' : '')
        }
        // On the dialog rather than the textarea: somebody who has just taken
        // a screenshot presses paste, and where the caret happens to be is not
        // something they should have to think about.
        onPaste={(event) => void paste(event)}
        // Drop anywhere on the dialog, for the same reason: a file being
        // dragged in should land wherever it is let go, not only on a target
        // the reporter has to aim for. dragOver must cancel its default for a
        // drop to fire at all; dragging state drives the outline.
        onDragOver={(event) => {
          if (Array.from(event.dataTransfer?.items ?? []).some((item) => item.kind === 'file')) {
            event.preventDefault()
            setDragging(true)
          }
        }}
        onDragLeave={(event) => {
          // Only when the pointer actually leaves the dialog, not on every
          // move across a child inside it.
          if (event.currentTarget === event.target) setDragging(false)
        }}
        onDrop={(event) => void drop(event)}
        onKeyDown={(event) => {
          // Ctrl/Cmd+Enter files from anywhere inside the box (#20), rather
          // than plain Enter: the description is multi-line and a newline has
          // to stay a newline.
          if (event.key === 'Enter' && (event.ctrlKey || event.metaKey)) {
            event.preventDefault()
            void file()
          }
        }}
      >
        <div className="flex flex-col gap-1">
          {/* Markdown with a preview (#46): a report is read by whoever works
              it, and a list or a code fence survives the trip to GitHub and
              Jax as written. Paste and Ctrl/Cmd+Enter live on the container
              and keep working inside the editor. */}
          <MarkdownField
            label="What went wrong?"
            autoFocus
            value={description}
            placeholder="What you did, what happened, and what you expected instead."
            hint="Paste, drop, or attach a screenshot, a recording, or a .zip. Ctrl/Cmd+Enter files it."
            onChange={setDescription}
          />
        </div>

        {/* The attached files. Every report here is worked by an agent reading
            the description, so a screenshot is not decoration — it is the
            difference between describing a layout bug and showing one, and a
            recording is that difference for the bugs that only exist in
            motion (#20). */}
        {(attachments.length > 0 || attaching) && (
          <div className="flex flex-wrap gap-2">
            {attachments.map((path) => (
              <div key={path} className="group relative">
                {isImage(path) ? (
                  previews[path] ? (
                    <img
                      src={previews[path]}
                      alt="Attached screenshot"
                      className="h-20 w-32 rounded-lg border border-edge object-cover object-top"
                    />
                  ) : (
                    <div className="h-20 w-32 animate-pulse rounded-lg border border-edge bg-surface-hover" />
                  )
                ) : (
                  // Named rather than opened. The file is for the agent working
                  // the report, and a player — or an unzipped listing — in a
                  // stamp-sized tile is neither a preview nor a way of using it.
                  <div className="flex h-20 w-32 flex-col items-center justify-center gap-1 rounded-lg border border-edge bg-surface-hover px-2">
                    {isZip(path) ? (
                      <FileArchive size={16} className="text-fg-muted" aria-hidden />
                    ) : (
                      <Film size={16} className="text-fg-muted" aria-hidden />
                    )}
                    <span className="max-w-full truncate font-mono text-[11px] text-fg-muted">
                      {fileName(path)}
                    </span>
                  </div>
                )}
                <button
                  type="button"
                  onClick={() =>
                    setAttachments((all) => all.filter((one) => one !== path))
                  }
                  aria-label="Remove this attachment"
                  // Removed from the report rather than from disk. An abandoned
                  // file costs a few hundred kilobytes under this app's own
                  // directory; deleting one the producer might re-add mid-form
                  // costs them the screenshot.
                  className="absolute -right-1.5 -top-1.5 inline-flex h-5 w-5 items-center justify-center rounded-full border border-edge bg-bg text-fg-muted opacity-0 transition-opacity hover:text-danger focus-visible:opacity-100 group-hover:opacity-100"
                >
                  <X size={11} aria-hidden />
                </button>
              </div>
            ))}
            {attaching && (
              <div className="flex h-20 w-32 items-center justify-center rounded-lg border border-dashed border-edge text-xs text-fg-muted">
                Attaching…
              </div>
            )}
          </div>
        )}

        {/* The manual way in, beside paste and drop: a picker for somebody who
            has the file in a folder rather than on a clipboard. The input is
            hidden and driven by the button, and takes several at once. */}
        <div>
          <button
            type="button"
            className="sm-button sm-button-quiet"
            onClick={() => fileInputRef.current?.click()}
          >
            <Paperclip size={13} aria-hidden />
            Attach files
          </button>
          <input
            ref={fileInputRef}
            type="file"
            multiple
            accept="image/*,video/*,.zip,application/zip"
            className="hidden"
            onChange={(event) => {
              const files = Array.from(event.target.files ?? [])
              // Cleared so picking the same file twice fires the change again.
              event.target.value = ''
              void attach(files)
            }}
          />
        </div>

        <div>
          <p className="mb-1.5 text-sm font-medium">Where</p>
          {/* Readable steps rather than the raw route id (#20): `settings-bugs`
              is what the report stores, and "Settings › Bug reports" is what a
              person standing there calls it. Read, never chosen — see trail. */}
          <p className="flex flex-wrap items-center gap-1.5 rounded-lg border border-edge bg-bg px-3 py-2 text-sm text-fg-muted">
            {trail.length === 0 && 'Unknown'}
            {trail.map((step, index) => (
              <span key={`${index}:${step}`} className="flex items-center gap-1.5">
                {index > 0 && <ChevronRight size={12} className="shrink-0" aria-hidden />}
                {step}
              </span>
            ))}
          </p>
        </div>

        <label className="flex items-start gap-2 text-sm">
          <input
            type="checkbox"
            className="mt-0.5"
            checked={global}
            onChange={(event) => setGlobal(event.target.checked)}
          />
          <span>
            This is about the whole app, not this page
            <span className="block text-xs text-fg-muted">
              The report records where you were either way; this says the
              problem is not confined to it.
            </span>
          </span>
        </label>

        {error && <p className="text-sm text-danger">{error}</p>}
      </div>
    </Modal>
  )
}

// What the three ways in will accept, matching the backend's own list (see
// attachmentKinds in bugattachments.go). Checked here as well so a dropped
// .docx is refused with nothing sent over the bindings at all.
const attachable = (file: File) =>
  file.type.startsWith('image/') ||
  file.type.startsWith('video/') ||
  file.type === 'application/zip' ||
  file.type === 'application/x-zip-compressed' ||
  file.name.toLowerCase().endsWith('.zip')

const isImage = (path: string) => /\.(png|jpe?g|gif|webp)$/i.test(path)
const isZip = (path: string) => /\.zip$/i.test(path)
const fileName = (path: string) => path.split(/[\\/]/).pop() ?? path

// The file as a data URL, which is the shape the binding takes: a plain string
// survives the JSON bridge where raw bytes would not.
//
// A .zip often arrives with an empty or vague type — a drag from Explorer, a
// pick on some browsers — so the data URL comes back without the MIME the
// backend keys on. When the name says zip, the prefix is rewritten to
// application/zip: the extension is the reporter's own word for what it is,
// and the backend's allow-list is what still decides whether to keep it.
const readAsDataURL = (file: File) =>
  new Promise<string>((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => {
      let url = String(reader.result)
      if (
        /\.zip$/i.test(file.name) &&
        !/^data:application\/(zip|x-zip-compressed);base64,/i.test(url)
      ) {
        url = url.replace(/^data:[^;]*;base64,/i, 'data:application/zip;base64,')
      }
      resolve(url)
    }
    reader.onerror = () => reject(new Error(`Could not read ${file.name}`))
    reader.readAsDataURL(file)
  })
