import {useCallback, useEffect, useRef, useState} from 'react'
import {Send, Sparkles, Trash2} from 'lucide-react'
import {
  AskInterview,
  ClearChat,
  GetAIReadiness,
  GetChat,
} from '../../wailsjs/go/main/App'
import {main} from '../../wailsjs/go/models'
import {Markdown} from '../components/Markdown'
import {Modal} from '../components/Modal'
import {toMessage} from '../lib/errors'
import {WORKING_VERBS, WORKING_VERB_MS, otherVerb} from '../lib/workingVerbs'

/**
 * The AI-led dialog behind every record in this app.
 *
 * One component for three conversations — artist, album, track — because they
 * differ only in the brief the backend puts in front of them and in what is
 * extracted at the end. The caller supplies the subject, the opening line, and
 * what to do with the transcript when the producer says it is finished.
 *
 * The transcript is stored server-side as it happens (see interview.go), so
 * closing this dialog mid-thought and coming back tomorrow resumes rather than
 * restarts. That is the whole reason the conversation is not held here.
 */
export function InterviewDialog({
  title,
  subjectKind,
  subjectId,
  opening,
  finishLabel,
  onFinish,
  onClose,
}: {
  title: string
  /** "artist", "album" or "track" — half the key the transcript is stored under. */
  subjectKind: string
  /**
   * What the conversation is keyed to: "" for an artist being created, the
   * artist's id for an album, the album's id for a track. A record has no id
   * until it exists, so the thread hangs off its parent.
   */
  subjectId: string
  /** The first thing on screen, before anybody has typed. */
  opening: string
  finishLabel: string
  /**
   * Called with the finished transcript. Returns a message to show on failure,
   * or "" once it has done its work and the dialog may close — so the extraction
   * pass, which can take several seconds and can fail, keeps the dialog open
   * while it runs.
   */
  onFinish: (messages: main.InterviewMessage[]) => Promise<string>
  onClose: () => void
}) {
  const [messages, setMessages] = useState<main.InterviewMessage[]>([])
  const [draft, setDraft] = useState('')
  const [busy, setBusy] = useState(false)
  const [finishing, setFinishing] = useState(false)
  const [error, setError] = useState('')
  const [ready, setReady] = useState<main.AIReadiness | null>(null)
  const [verb, setVerb] = useState(WORKING_VERBS[0])

  const scroller = useRef<HTMLDivElement>(null)
  const input = useRef<HTMLTextAreaElement>(null)

  // Whether an interview can run at all, asked before the producer types
  // anything. A dialog that takes a paragraph and then says "no AI provider is
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

  // Pinned to the bottom as turns arrive. Behaviour 'smooth' is deliberate on
  // the new-message case and would be wrong on the resume case — but the resume
  // lands before the first paint of the list, so there is nothing to animate.
  useEffect(() => {
    const node = scroller.current
    if (node) node.scrollTop = node.scrollHeight
  }, [messages, busy])

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
      input.current?.focus()
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

  const blocked = ready !== null && !ready.ready

  return (
    <Modal
      title={title}
      onClose={onClose}
      wide
      footer={
        <>
          {messages.length > 0 && (
            <button
              className="sm-button sm-button-quiet mr-auto"
              type="button"
              onClick={clear}
              disabled={busy || finishing}
            >
              <Trash2 size={15} />
              Start again
            </button>
          )}
          <button className="sm-button sm-button-quiet" type="button" onClick={onClose}>
            Cancel
          </button>
          <button
            className="sm-button sm-button-primary"
            type="button"
            onClick={finish}
            // Nothing to extract from an empty conversation, and the backend
            // would answer with exactly that — better not to offer it.
            disabled={messages.length === 0 || busy || finishing}
          >
            <Sparkles size={15} />
            {finishing ? 'Reading the conversation…' : finishLabel}
          </button>
        </>
      }
    >
      <div className="flex h-[26rem] flex-col gap-3">
        {blocked ? (
          <p className="text-sm text-danger">{ready?.why}</p>
        ) : (
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
                    // The producer's own words, not markdown: they typed them,
                    // and rendering an asterisk as emphasis would change what
                    // they said.
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
                  // Enter sends and Shift+Enter breaks the line. The other way
                  // round is defensible for a long-form field, but this is a
                  // conversation and the common case is one sentence.
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
        )}
      </div>
    </Modal>
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
