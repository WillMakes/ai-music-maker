import {useCallback, useEffect, useRef, useState, type DragEvent, type ClipboardEvent} from 'react'
import {AudioLines, FileAudio, Image as ImageIcon, Paperclip, Send, X} from 'lucide-react'
import {
  AskInterview,
  AttachInterviewFile,
  ClearChat,
  GetAIReadiness,
  GetChat,
} from '../../wailsjs/go/main/App'
import {main} from '../../wailsjs/go/models'
import {Markdown} from '../components/Markdown'
import {toMessage} from '../lib/errors'
import {fileName, isAudioPath, readAsDataURL} from '../lib/files'
import {WORKING_VERBS, WORKING_VERB_MS, otherVerb} from '../lib/workingVerbs'

/**
 * The AI-led conversation behind every record in this app, with no shell around
 * it.
 *
 * One conversation for three subjects — artist, album, track — because they
 * differ only in the brief the backend puts in front of them and in what is
 * extracted at the end.
 *
 * Separated from its presentation because the two are no longer the same (#17).
 * Creating an artist is a page of its own: it is the longest conversation in the
 * app and the one somebody comes back to across sittings, and a modal is the
 * wrong container for that — it has no route, so it cannot be linked to, reopened
 * where it was left, or recorded as the page a bug was filed from. Albums and
 * tracks are still a dialog opened from the record they belong to, so both
 * shells are wanted; what neither should own is the conversation itself.
 *
 * The transcript is stored server-side as it happens (see interview.go), so
 * leaving mid-thought and coming back tomorrow resumes rather than restarts.
 * That is the whole reason the conversation is not held here.
 */

/** What useInterview hands back — the state a shell draws and the verbs it wires. */
export interface Interview {
  messages: main.InterviewMessage[]
  draft: string
  setDraft: (next: string) => void
  busy: boolean
  finishing: boolean
  error: string
  /** Null until the readiness check answers; !ready.ready means it cannot run. */
  ready: main.AIReadiness | null
  blocked: boolean
  verb: string
  send: () => Promise<void>
  finish: () => Promise<void>
  clear: () => Promise<void>
  /** Files staged for the next send (#24), as stored paths. */
  pending: string[]
  attaching: boolean
  attach: (files: File[]) => Promise<void>
  unstage: (path: string) => void
}

/**
 * Runs one interview.
 *
 * onFinish is called with the finished transcript and returns a message to show
 * on failure, or "" once it has done its work and the shell may close — so the
 * extraction pass, which takes several seconds and can fail, keeps the
 * conversation on screen while it runs.
 */
export function useInterview({
  subjectKind,
  subjectId,
  onFinish,
}: {
  /** "artist", "album" or "track" — half the key the transcript is stored under. */
  subjectKind: string
  /**
   * What the conversation is keyed to: "" for an artist being created, the
   * artist's id for an album, the album's id for a track. A record has no id
   * until it exists, so the thread hangs off its parent.
   */
  subjectId: string
  onFinish: (messages: main.InterviewMessage[]) => Promise<string>
}): Interview {
  const [messages, setMessages] = useState<main.InterviewMessage[]>([])
  const [draft, setDraft] = useState('')
  const [busy, setBusy] = useState(false)
  const [finishing, setFinishing] = useState(false)
  const [error, setError] = useState('')
  const [ready, setReady] = useState<main.AIReadiness | null>(null)
  const [verb, setVerb] = useState(WORKING_VERBS[0])
  // Files staged for the next send (#24). They ride the message they are sent
  // with — staged rather than sent at once, so the producer can drop three
  // takes and then say what they are in the same turn.
  const [pending, setPending] = useState<string[]>([])
  const [attaching, setAttaching] = useState(false)

  // Whether an interview can run at all, asked before the producer types
  // anything. A page that takes a paragraph and then says "no AI provider is
  // connected" has wasted the paragraph.
  useEffect(() => {
    GetAIReadiness()
      .then(setReady)
      .catch((err) => setError(toMessage(err)))
  }, [])

  // The stored thread, resumed.
  useEffect(() => {
    GetChat(subjectKind, subjectId)
      .then((chat) => setMessages(chat.messages ?? []))
      .catch(() => setMessages([]))
  }, [subjectKind, subjectId])

  // The working verb rotates while a turn is out. Cleared with the turn, so a
  // finished conversation does not leave a timer running.
  useEffect(() => {
    if (!busy) return
    setVerb(otherVerb(''))
    const timer = setInterval(() => setVerb((current) => otherVerb(current)), WORKING_VERB_MS)
    return () => clearInterval(timer)
  }, [busy])

  const attach = useCallback(
    async (files: File[]) => {
      const keep = files.filter(attachable)
      if (keep.length === 0) return
      setAttaching(true)
      setError('')
      try {
        // One at a time: a dropped album of ten takes would otherwise build ten
        // data URLs at once.
        for (const file of keep) {
          // eslint-disable-next-line no-await-in-loop
          const dataURL = await readAsDataURL(file)
          // eslint-disable-next-line no-await-in-loop
          const path = await AttachInterviewFile(subjectKind, subjectId, file.name, dataURL)
          setPending((all) => [...all, path])
        }
      } catch (err) {
        setError(toMessage(err))
      } finally {
        setAttaching(false)
      }
    },
    [subjectKind, subjectId],
  )

  const unstage = useCallback((path: string) => {
    // Off the next message, not off disk: the file is cheap to keep until the
    // chat is cleared, and gone for good if it is not.
    setPending((all) => all.filter((one) => one !== path))
  }, [])

  const send = useCallback(async () => {
    const text = draft.trim()
    // A turn may be files alone (#24) — dropping three takes and pressing send
    // is a complete thing to say.
    if ((!text && pending.length === 0) || busy) return
    const next = [
      ...messages,
      main.InterviewMessage.createFrom({role: 'user', text, note: '', attachments: pending}),
    ]
    setPending([])
    setMessages(next)
    setDraft('')
    setBusy(true)
    setError('')
    try {
      // The whole transcript goes over and the whole transcript comes back —
      // including the app's own note where the turn failed, which is why this
      // replaces rather than appends.
      const answered = await AskInterview(subjectKind, subjectId, next)
      setMessages(answered ?? next)
    } catch (err) {
      setError(toMessage(err))
    } finally {
      setBusy(false)
    }
  }, [busy, draft, messages, pending, subjectKind, subjectId])

  const finish = useCallback(async () => {
    setFinishing(true)
    setError('')
    const problem = await onFinish(messages)
    setFinishing(false)
    if (problem) setError(problem)
  }, [messages, onFinish])

  const clear = useCallback(async () => {
    await ClearChat(subjectKind, subjectId).catch(() => {})
    setMessages([])
    // Staged files went with the chat — ClearChat sweeps the folder they were
    // written to, so holding their paths would be holding dangling ones.
    setPending([])
    setError('')
  }, [subjectKind, subjectId])

  return {
    messages,
    draft,
    setDraft,
    busy,
    finishing,
    error,
    ready,
    blocked: ready !== null && !ready.ready,
    verb,
    send,
    finish,
    clear,
    pending,
    attaching,
    attach,
    unstage,
  }
}

// What a chat will take (#24), mirroring the backend's own list: audio is the
// point — finished Suno files — and images ride along for artwork references.
const attachable = (file: File) =>
  file.type.startsWith('audio/') ||
  file.type.startsWith('image/') ||
  isAudioPath(file.name)

/**
 * The conversation itself: the transcript and the composer, filling whatever it
 * is put inside.
 *
 * No height of its own — the shell decides, because a dialog wants a fixed panel
 * and a page wants the rest of the window.
 */
export function InterviewConversation({
  interview,
  opening,
}: {
  interview: Interview
  /** The first thing on screen, before anybody has typed. */
  opening: string
}) {
  const {
    messages,
    busy,
    finishing,
    error,
    ready,
    blocked,
    verb,
    draft,
    setDraft,
    send,
    pending,
    attaching,
    attach,
    unstage,
  } = interview
  const scroller = useRef<HTMLDivElement>(null)
  const input = useRef<HTMLTextAreaElement>(null)
  const picker = useRef<HTMLInputElement>(null)
  // A file is being dragged over the conversation, for the drop highlight.
  const [dragging, setDragging] = useState(false)

  const drop = async (event: DragEvent) => {
    const files = Array.from(event.dataTransfer?.files ?? [])
    setDragging(false)
    if (files.length === 0) return
    // The default is held onto only when there are files — a drop of anything
    // else is left for the OS to do whatever it would have.
    event.preventDefault()
    await attach(files)
  }

  const paste = async (event: ClipboardEvent) => {
    // A copied file reaches the webview as a file; ordinary text keeps its
    // default so this stays a paste handler rather than a paste replacement.
    const files = Array.from(event.clipboardData?.files ?? [])
    if (files.length === 0) return
    event.preventDefault()
    await attach(files)
  }

  // Pinned to the bottom as turns arrive.
  useEffect(() => {
    const node = scroller.current
    if (node) node.scrollTop = node.scrollHeight
  }, [messages, busy])

  // Focus returns to the composer when a turn lands, so answering the next
  // question is typing rather than typing after a click.
  useEffect(() => {
    if (!busy) input.current?.focus()
  }, [busy])

  if (blocked) {
    return <p className="text-sm text-danger">{ready?.why}</p>
  }

  return (
    <div
      className={
        'flex min-h-0 flex-1 flex-col gap-3 rounded-lg transition-colors ' +
        (dragging ? 'outline-dashed outline-2 outline-offset-4 outline-accent' : '')
      }
      // Drop and paste anywhere over the conversation (#24): a file being
      // dragged in should land wherever it is let go, not on a target the
      // producer has to aim for.
      onDragOver={(event) => {
        if (Array.from(event.dataTransfer?.items ?? []).some((item) => item.kind === 'file')) {
          event.preventDefault()
          setDragging(true)
        }
      }}
      onDragLeave={(event) => {
        if (event.currentTarget === event.target) setDragging(false)
      }}
      onDrop={(event) => void drop(event)}
      onPaste={(event) => void paste(event)}
    >
      <div ref={scroller} className="min-h-0 flex-1 space-y-3 overflow-y-auto pr-1">
        <Bubble role="assistant">
          <Markdown>{opening}</Markdown>
        </Bubble>
        {messages.map((message, index) => (
          <Bubble key={index} role={message.role}>
            {message.note ? (
              <p className="text-sm text-danger">{message.note}</p>
            ) : message.role === 'assistant' ? (
              <Markdown>{message.text}</Markdown>
            ) : (
              // The producer's own words, not markdown: they typed them, and
              // rendering an asterisk as emphasis would change what they said.
              <>
                {message.text && <p className="whitespace-pre-wrap text-sm">{message.text}</p>}
                {(message.attachments?.length ?? 0) > 0 && (
                  <span className="mt-1 flex flex-wrap gap-1.5">
                    {message.attachments.map((path) => (
                      <FileChip key={path} path={path} />
                    ))}
                  </span>
                )}
                {/* What the app looked up for a song link in this turn (#29),
                    shown as the app's finding rather than folded into the
                    producer's words. */}
                {message.research && (
                  <span className="mt-2 block rounded border-l-2 border-edge bg-bg px-2.5 py-2">
                    <span className="mb-1 flex items-center gap-1.5 text-xs font-medium text-fg-muted">
                      <AudioLines size={12} aria-hidden />
                      Music analysis
                    </span>
                    <span className="block whitespace-pre-wrap text-xs text-fg-muted">
                      {message.research}
                    </span>
                  </span>
                )}
              </>
            )}
          </Bubble>
        ))}
        {busy && (
          <p className="working-verb pl-1 text-sm" aria-live="polite">
            {verb}
          </p>
        )}
      </div>

      {error && <p className="text-sm text-danger">{error}</p>}

      {/* Files staged for the next send (#24), each removable until it goes. */}
      {(pending.length > 0 || attaching) && (
        <div className="flex flex-wrap gap-1.5">
          {pending.map((path) => (
            <FileChip key={path} path={path} onRemove={() => unstage(path)} />
          ))}
          {attaching && <span className="text-xs text-fg-muted">Attaching…</span>}
        </div>
      )}

      <div className="flex items-end gap-2">
        <button
          className="sm-button sm-button-quiet h-10"
          type="button"
          onClick={() => picker.current?.click()}
          disabled={busy || finishing || attaching}
          aria-label="Attach files"
          title="Attach a finished track or a reference image"
        >
          <Paperclip size={15} />
        </button>
        <input
          ref={picker}
          type="file"
          multiple
          accept="audio/*,image/*"
          className="hidden"
          onChange={(event) => {
            const files = Array.from(event.target.files ?? [])
            // Cleared so picking the same file twice fires the change again.
            event.target.value = ''
            void attach(files)
          }}
        />
        <textarea
          ref={input}
          className="sm-input max-h-32 min-h-[2.5rem] resize-y"
          rows={2}
          value={draft}
          placeholder={ready ? 'Answer, or say what you want' : 'Checking…'}
          disabled={busy || finishing}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            // Enter sends and Shift+Enter breaks the line. The other way round is
            // defensible for a long-form field, but this is a conversation and
            // the common case is one sentence.
            if (event.key === 'Enter' && !event.shiftKey) {
              event.preventDefault()
              void send()
            }
          }}
        />
        <button
          className="sm-button sm-button-primary h-10"
          type="button"
          onClick={send}
          disabled={busy || finishing || (!draft.trim() && pending.length === 0)}
          aria-label="Send"
        >
          <Send size={15} />
        </button>
      </div>
    </div>
  )
}

/**
 * One attached file as a chip: what it is (a note or a picture) and its name.
 * Named rather than played — the file is evidence for the conversation and the
 * import step, and a player in a chat bubble is neither.
 */
function FileChip({path, onRemove}: {path: string; onRemove?: () => void}) {
  return (
    <span className="inline-flex max-w-56 items-center gap-1.5 rounded-full border border-edge bg-bg px-2.5 py-1 text-xs">
      {isAudioPath(path) ? (
        <FileAudio size={12} className="shrink-0 text-fg-muted" aria-hidden />
      ) : (
        <ImageIcon size={12} className="shrink-0 text-fg-muted" aria-hidden />
      )}
      <span className="truncate">{fileName(path)}</span>
      {onRemove && (
        <button
          type="button"
          onClick={onRemove}
          aria-label={'Remove ' + fileName(path)}
          className="shrink-0 text-fg-muted hover:text-danger"
        >
          <X size={11} aria-hidden />
        </button>
      )}
    </span>
  )
}

function Bubble({role, children}: {role: string; children: React.ReactNode}) {
  const mine = role === 'user'
  return (
    <div className={mine ? 'flex justify-end' : 'flex justify-start'}>
      <div
        className={
          'max-w-[85%] rounded-lg px-3 py-2 ' +
          (mine ? 'bg-active text-active-fg' : 'bg-surface')
        }
      >
        {children}
      </div>
    </div>
  )
}
