import {useEffect, useRef, useState} from 'react'
import {AudioLines, Loader2, Send, Sparkles} from 'lucide-react'
import {AskStyleEdit, ExtractStyleEdit} from '../../wailsjs/go/main/App'
import {main} from '../../wailsjs/go/models'
import {Markdown} from '../components/Markdown'
import {Modal} from '../components/Modal'
import {toMessage} from '../lib/errors'

/**
 * Refining an artist's base technical style by talking about it (#30).
 *
 * The same shape as editing a skill with AI (#23), with one addition: a
 * YouTube or YouTube Music link pasted into a turn is researched by the
 * Antigravity service (#29) before the conversation model answers, and the
 * breakdown is shown under the turn — so "make it sound more like this" comes
 * with what "this" actually sounds like.
 *
 * Apply writes the whole revised style into the edit form, where it is still
 * every bit as editable by hand; nothing reaches the database until the
 * producer presses Save. The transcript lives here and nowhere else.
 */
export function StyleEditAssistant({
  artistId,
  onApply,
  onClose,
}: {
  artistId: string
  /** Receives the revised style; the caller drops it into the draft. */
  onApply: (baseStyle: string) => void
  onClose: () => void
}) {
  const [transcript, setTranscript] = useState<main.InterviewMessage[]>([])
  const [input, setInput] = useState('')
  const [thinking, setThinking] = useState(false)
  const [applying, setApplying] = useState(false)
  const [error, setError] = useState('')
  const scroller = useRef<HTMLDivElement>(null)

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
      // A failed turn comes back as a note on the thread, not as an error, so
      // the failure shows where the answer would have been and the message can
      // be sent again.
      setTranscript(await AskStyleEdit(artistId, sent))
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
      const draft = await ExtractStyleEdit(artistId, transcript)
      onApply(draft.baseStyle)
      onClose()
    } catch (err) {
      setError(toMessage(err))
      setApplying(false)
    }
  }

  const canApply =
    transcript.some((message) => message.role === 'assistant' && message.text) &&
    !thinking &&
    !applying

  return (
    <Modal
      title="Refine the base style with AI"
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
              Say what should change — and paste a YouTube or YouTube Music link
              to have the song broken down and its traits folded in. Apply
              writes the revised style into the form, where you can still edit
              it by hand before saving.
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
                  <>
                    <p className="whitespace-pre-wrap">{message.text}</p>
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
                Working — a pasted link adds a research pass…
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
            placeholder="What should change — or paste a song link"
            onChange={(event) => setInput(event.target.value)}
            onKeyDown={(event) => {
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
