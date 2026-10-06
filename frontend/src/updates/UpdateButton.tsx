import {useState} from 'react'
import {ArrowDownCircle, Download, ExternalLink} from 'lucide-react'
import {OpenURL} from '../../wailsjs/go/app/App'
import {Modal} from '../components/Modal'
import {Markdown} from '../components/Markdown'
import {useUpdates} from './UpdatesProvider'

/**
 * The top bar's notice that a newer release is out (#132), left of Report a
 * bug. Nothing at all when there is no update: the bar is for where you are
 * and what acts on it, and "you are up to date" is neither — Settings → About
 * says that for anyone who asks.
 */
export function UpdateButton() {
  const {status} = useUpdates()
  const [open, setOpen] = useState(false)
  if (!status?.available) return null
  return (
    <>
      {/* Compact and versionless (#135): the bar only needs to say there is
          one. The version is in the tooltip and heads the dialog. */}
      <button
        className="sm-button sm-button-primary px-2 py-1 text-xs"
        type="button"
        title={'AIMusicMaker ' + status.latest + ' is available'}
        onClick={() => setOpen(true)}
      >
        <ArrowDownCircle size={13} />
        <span>Update Available</span>
      </button>
      {open && <UpdateDialog onClose={() => setOpen(false)} />}
    </>
  )
}

/**
 * What the update is and the way to it: the notes first, then one button that
 * moves from Download to Install as the file arrives. Installing on Windows
 * closes the window, so the button says so before it is pressed.
 */
export function UpdateDialog({onClose}: {onClose: () => void}) {
  const {status, progress, error, download, install} = useUpdates()
  if (!status) return null
  const downloading = progress !== null
  const windows = status.assetName.endsWith('.exe')
  const mac = status.assetName.endsWith('.dmg')

  return (
    <Modal
      title={'Update to AIMusicMaker ' + status.latest}
      wide
      onClose={onClose}
      footer={
        <>
          {status.url && (
            <button
              className="sm-button sm-button-quiet mr-auto"
              type="button"
              onClick={() => void OpenURL(status.url).catch(() => {})}
            >
              <ExternalLink size={15} />
              The release on GitHub
            </button>
          )}
          <button className="sm-button sm-button-quiet" type="button" onClick={onClose}>
            Later
          </button>
          {status.downloaded ? (
            <button className="sm-button sm-button-primary" type="button" onClick={() => void install()}>
              {windows ? 'Install and restart' : mac ? 'Open the disk image' : 'Show the download'}
            </button>
          ) : (
            <button
              className="sm-button sm-button-primary"
              type="button"
              disabled={downloading}
              onClick={() => void download()}
            >
              <Download size={15} />
              {downloading ? 'Downloading…' : 'Download ' + formatSize(status.assetSize)}
            </button>
          )}
        </>
      }
    >
      <div className="space-y-4">
        <p className="text-sm text-fg-muted">
          You have {status.current}. The download is checked against the release's checksum
          before anything runs.{' '}
          {windows
            ? 'Installing closes AIMusicMaker and updates it in place; your library is untouched.'
            : mac
              ? 'Drag AIMusicMaker into Applications to replace this copy, then reopen it.'
              : 'Unpack it over this copy, then reopen it.'}
        </p>

        {downloading && (
          <div
            className="h-2 overflow-hidden rounded-full bg-surface-hover"
            role="progressbar"
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={Math.round((progress ?? 0) * 100)}
          >
            <div
              className="h-full bg-accent transition-[width]"
              style={{width: `${Math.round((progress ?? 0) * 100)}%`}}
            />
          </div>
        )}
        {status.downloaded && (
          <p className="break-all font-mono text-xs text-fg-muted">{status.downloaded}</p>
        )}
        {error && <p className="text-sm text-danger">{error}</p>}

        {status.notes && (
          <section className="sm-panel p-4">
            <h3 className="mb-2 text-sm font-semibold">What changed</h3>
            <Markdown>{status.notes}</Markdown>
          </section>
        )}
      </div>
    </Modal>
  )
}

function formatSize(bytes: number): string {
  if (!bytes) return ''
  return '(' + (bytes / (1024 * 1024)).toFixed(1) + ' MB)'
}
