import {Sparkles, Trash2} from 'lucide-react'
import {main} from '../../wailsjs/go/models'
import {Modal} from '../components/Modal'
import {InterviewConversation, useInterview} from './Interview'

/**
 * An interview in a dialog, opened from the record it belongs to.
 *
 * Albums and tracks are made this way: the conversation is short, it is always
 * about a record already on screen, and coming back to the page behind it is the
 * point. Creating an artist is not — it has a page of its own (#17, see
 * views/NewArtist.tsx) — so this is now one of two shells around the same
 * conversation rather than the only way to have one.
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
  subjectKind: string
  subjectId: string
  opening: string
  finishLabel: string
  onFinish: (messages: main.InterviewMessage[]) => Promise<string>
  onClose: () => void
}) {
  const interview = useInterview({subjectKind, subjectId, onFinish})
  const {messages, busy, finishing, finish, clear} = interview

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
      {/* A fixed panel: a dialog that grew with the transcript would walk its own
          buttons off the bottom of the window. */}
      <div className="flex h-[26rem] flex-col gap-3">
        <InterviewConversation interview={interview} opening={opening} />
      </div>
    </Modal>
  )
}
