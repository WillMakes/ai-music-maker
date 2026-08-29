import {useCallback, useEffect, useRef, useState} from 'react'
import {Send} from 'lucide-react'
import {
  AskInterview,
  ClearChat,
  GetAIReadiness,
  GetChat,
} from '../../wailsjs/go/main/App'
import {main} from '../../wailsjs/go/models'
import {Markdown} from '../components/Markdown'
import {toMessage} from '../lib/errors'
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

  const send = useCallback(async () => {
    const text = draft.trim()
    if (!text || busy) return
    const next = [
      ...messages,
      main.InterviewMessage.createFrom({role: 'user', text, note: ''}),
    ]
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
  }, [busy, draft, messages, subjectKind, subjectId])

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
  }
}

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
  const {messages, busy, finishing, error, ready, blocked, verb, draft, setDraft, send} =
    interview
  const scroller = useRef<HTMLDivElement>(null)
  const input = useRef<HTMLTextAreaElement>(null)

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
    <>
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
              <p className="whitespace-pre-wrap text-sm">{message.text}</p>
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

      <div className="flex items-end gap-2">
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
          disabled={busy || finishing || !draft.trim()}
          aria-label="Send"
        >
          <Send size={15} />
        </button>
      </div>
    </>
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
