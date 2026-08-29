import {useEffect, useRef, useState} from 'react'
import {Loader2, Send, Sparkles} from 'lucide-react'
import {AskSkillEdit, ExtractSkillEdit} from '../../wailsjs/go/main/App'
import {main} from '../../wailsjs/go/models'
import {Markdown} from '../components/Markdown'
import {Modal} from '../components/Modal'
import {toMessage} from '../lib/errors'

/**
 * Revising a skill by talking about it (#23).
 *
 * The producer says what should change, the model discusses the wording, and
 * Apply runs the separate extraction pass that writes the whole revised
 * document into the edit form — where it is still every bit as editable by
 * hand as if it had been typed. The AI writes a draft; it does not take the
 * pen, and nothing reaches the database until the producer presses Save.
 *
 * The transcript lives here and nowhere else. It ends in a draft in the form
 * or in nothing, and either way there is nothing worth keeping — which is why
 * this holds plain state rather than going through GetChat/keepChat the way
 * the interviews do.
 */
export function SkillEditAssistant({
  skillId,
  onApply,
  onClose,
}: {
  skillId: string
  /** Receives the revised draft; the caller drops it into the edit form. */
  onApply: (draft: main.SkillEditDraft) => void
  onClose: () => void
}) {
  const [transcript, setTranscript] = useState<main.InterviewMessage[]>([])
  const [input, setInput] = useState('')
  const [thinking, setThinking] = useState(false)
  const [applying, setApplying] = useState(false)
  const [error, setError] = useState('')
  const scroller = useRef<HTMLDivElement>(null)

  // Follow the newest turn, the way a chat does.
  useEffect(() => {
    scroller.current?.scrollTo({top: scroller.current.scrollHeight})
  }, [transcript, thinking])

  const send = async () => {
    const text = input.trim()
    if (!text || thinking) return
    setInput('')
    setError('')
    const sent = [
      ...transcript,
      main.InterviewMessage.createFrom({role: 'user', text}),
    ]
    setTranscript(sent)
    setThinking(true)
    try {
      // A failed turn comes back as a note on the thread, not as an error —
      // the same shape AskInterview uses — so the failure is shown where the
      // answer would have been and the message can be sent again.
      setTranscript(await AskSkillEdit(skillId, sent))
    } catch (err) {
      setError(toMessage(err))
    } finally {
      setThinking(false)
    }
  }

  const apply = async () => {
    setApplying(true)
    setError('')
    try {
      const draft = await ExtractSkillEdit(skillId, transcript)
      onApply(draft)
      onClose()
    } catch (err) {
      setError(toMessage(err))
      setApplying(false)
    }
  }

  // Nothing to extract until the model has answered at least once — a request
  // nobody has discussed yet is a request, not an agreed revision.
  const canApply =
    transcript.some((message) => message.role === 'assistant' && message.text) &&
    !thinking &&
    !applying

  return (
    <Modal
      title="Edit this skill with AI"
      onClose={onClose}
      wide
      footer={
        <>
          <button className="sm-button sm-button-quiet" type="button" onClick={onClose}>
            Cancel
          </button>
          <button
            className="sm-button sm-button-primary"
            type="button"
            onClick={apply}
            disabled={!canApply}
          >
            <Sparkles size={15} />
            {applying ? 'Writing…' : 'Apply to the draft'}
          </button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <div
          ref={scroller}
          className="flex max-h-[50vh] min-h-[14rem] flex-col gap-3 overflow-y-auto pr-1"
        >
          {transcript.length === 0 && !thinking && (
            <p className="text-sm text-fg-muted">
              Say what should change — a section to tighten, a rule to add, a
              tone to shift. The assistant discusses the wording with you;
              Apply writes the revised skill into the form, where you can still
              edit it by hand before saving.
            </p>
          )}
          {transcript.map((message, index) => (
            <div
              key={index}
              className={message.role === 'user' ? 'flex justify-end' : 'flex justify-start'}
            >
              <div
                className={
                  message.role === 'user'
                    ? 'max-w-[85%] rounded-2xl rounded-br-sm bg-accent px-3 py-2 text-sm text-accent-fg'
                    : message.note
                      ? 'max-w-[85%] rounded-2xl rounded-bl-sm border border-edge bg-surface px-3 py-2 text-sm text-danger'
                      : 'max-w-[85%] rounded-2xl rounded-bl-sm border border-edge bg-surface px-3 py-2 text-sm'
                }
              >
                {message.role === 'user' ? (
                  <p className="whitespace-pre-wrap">{message.text}</p>
                ) : message.note ? (
                  <p>{message.note}</p>
                ) : (
                  <Markdown>{message.text}</Markdown>
                )}
              </div>
            </div>
          ))}
          {thinking && (
            <div className="flex justify-start">
              <div className="inline-flex items-center gap-2 rounded-2xl rounded-bl-sm border border-edge bg-surface px-3 py-2 text-sm text-fg-muted">
                <Loader2 size={13} className="animate-spin" aria-hidden />
                Thinking…
              </div>
            </div>
          )}
        </div>

        {error && <p className="text-sm text-danger">{error}</p>}

        <div className="flex items-end gap-2">
          <textarea
            className="sm-input min-h-0 flex-1 resize-none"
            rows={2}
            autoFocus
            value={input}
            placeholder="What would you like to change?"
            onChange={(event) => setInput(event.target.value)}
            onKeyDown={(event) => {
              // Enter sends; Shift+Enter is a newline — the convention every
              // chat uses.
              if (event.key === 'Enter' && !event.shiftKey) {
                event.preventDefault()
                void send()
              }
            }}
          />
          <button
            className="sm-button sm-button-primary shrink-0"
            type="button"
            onClick={send}
            disabled={!input.trim() || thinking}
            aria-label="Send"
          >
            <Send size={15} />
          </button>
        </div>
      </div>
    </Modal>
  )
}
