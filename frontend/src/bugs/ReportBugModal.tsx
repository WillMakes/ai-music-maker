import {useState} from 'react'
import {ExternalLink} from 'lucide-react'
import {FileBugReport, OpenURL} from '../../wailsjs/go/main/App'
import {main} from '../../wailsjs/go/models'
import {Modal} from '../components/Modal'
import {toMessage} from '../lib/errors'

/**
 * The bug button's box.
 *
 * One field and one checkbox. Everything else worth recording — the page, the
 * record it was showing, when — the app already knows and puts on the issue
 * itself (see issueDetails in bugissues.go). A form that asked somebody who just
 * found a bug to also summarise it, categorise it and retype where they were is
 * a form that gets closed.
 *
 * Filing cannot fail for want of a network: the report is written locally
 * whatever happens, and the issue and the Jax counterpart are caught up later.
 * So this shows what it managed — an issue number when there is one, and the
 * plain fact of the report when there is not.
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
  const [filed, setFiled] = useState<main.BugReport | null>(null)
  const [error, setError] = useState('')

  const file = async () => {
    if (!description.trim()) return
    setFiling(true)
    setError('')
    try {
      const report = await FileBugReport(
        main.BugReport.createFrom({
          description,
          route,
          global,
          subjectKind: global ? '' : subjectKind,
          subjectId: global ? '' : subjectId,
        }),
      )
      setFiled(report)
    } catch (err) {
      setError(toMessage(err))
    } finally {
      setFiling(false)
    }
  }

  if (filed) {
    return (
      <Modal
        title="Reported"
        onClose={onClose}
        footer={
          <button className="sm-button sm-button-primary" type="button" onClick={onClose}>
            Done
          </button>
        }
      >
        <div className="space-y-3 text-sm">
          <p>Thanks — it is on the queue.</p>
          {filed.issueNumber ? (
            <button
              className="sm-button sm-button-quiet"
              type="button"
              onClick={() => void OpenURL(filed.issueUrl).catch(() => {})}
            >
              <ExternalLink size={15} />
              Issue #{filed.issueNumber}
            </button>
          ) : (
            <p className="text-fg-muted">
              It could not reach GitHub just now, so it has no issue yet — the next
              sweep will open one.
            </p>
          )}
        </div>
      </Modal>
    )
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
      <div className="space-y-3">
        <div className="flex flex-col gap-1">
          <label className="text-sm font-medium" htmlFor="bug-description">
            What went wrong?
          </label>
          <textarea
            id="bug-description"
            className="sm-input resize-y"
            rows={6}
            autoFocus
            value={description}
            placeholder="What you did, what happened, and what you expected instead."
            onChange={(event) => setDescription(event.target.value)}
          />
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
              Leave it off and the report records that you were on{' '}
              <span className="font-mono">{route}</span>.
            </span>
          </span>
        </label>

        {error && <p className="text-sm text-danger">{error}</p>}
      </div>
    </Modal>
  )
}
