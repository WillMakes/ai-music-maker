import {useCallback, useEffect, useState} from 'react'
import {Download, FolderOpen} from 'lucide-react'
import {ExportExtension, GetExtensionStatus, RevealExtension} from '../../wailsjs/go/main/App'
import {main} from '../../wailsjs/go/models'
import {CopyButton} from '../components/CopyButton'
import {StatusPill} from '../components/StatusPill'
import {toMessage} from '../lib/errors'

/**
 * Settings → Chrome extension.
 *
 * The extension is exported rather than downloaded: it has to carry this app's
 * bridge token, which is generated on this machine and belongs to this
 * installation. A copy from a store would have no way to reach the app.
 *
 * Re-exporting is also the fix for an extension that has stopped working, so the
 * button stays available after the first time.
 */
export function ExtensionPanel() {
  const [status, setStatus] = useState<main.ExtensionStatus | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const reload = useCallback(() => {
    GetExtensionStatus()
      .then(setStatus)
      .catch((err) => setError(toMessage(err)))
  }, [])

  useEffect(reload, [reload])

  if (!status) {
    return <p className="text-sm text-fg-muted">{error || 'Loading…'}</p>
  }

  return (
    <div className="space-y-4">
      <div>
        <h2 className="flex items-center gap-2 text-sm font-semibold">
          Chrome extension
          {status.exported ? (
            <StatusPill tone="ok">Exported</StatusPill>
          ) : (
            <StatusPill tone="muted">Not exported</StatusPill>
          )}
        </h2>
        <p className="mt-1 text-sm text-fg-muted">
          Puts a button on Suno's Lyrics, Style and Title fields. Press one, pick a track,
          and the field is filled from this app.
        </p>
      </div>

      {error && <p className="text-sm text-danger">{error}</p>}

      <section className="sm-panel space-y-3 p-4">
        <div className="flex flex-wrap gap-2">
          <button
            className="sm-button sm-button-primary"
            type="button"
            disabled={busy}
            onClick={async () => {
              setBusy(true)
              setError('')
              try {
                setStatus(await ExportExtension())
              } catch (err) {
                setError(toMessage(err))
              } finally {
                setBusy(false)
              }
            }}
          >
            <Download size={15} />
            {status.exported ? 'Export again' : 'Export the extension'}
          </button>
          {status.exported && (
            <button
              className="sm-button sm-button-quiet"
              type="button"
              onClick={() => void RevealExtension().catch((err) => setError(toMessage(err)))}
            >
              <FolderOpen size={15} />
              Show the folder
            </button>
          )}
        </div>

        {status.exported && (
          <div className="flex items-center gap-1 rounded bg-surface-hover px-3 py-2">
            {/* Copyable (#78): Chrome's Load-unpacked dialog wants this path
                typed or pasted, and the quick copy beats retyping a deep
                AppData path character by character. */}
            <p className="min-w-0 flex-1 overflow-x-auto whitespace-nowrap font-mono text-xs">
              {status.dir}
            </p>
            <CopyButton text={status.dir} label="Copy the extension folder path" />
          </div>
        )}
      </section>

      <section className="sm-panel space-y-2 p-4">
        <h3 className="text-sm font-semibold">Installing it</h3>
        <ol className="ml-4 list-decimal space-y-1 text-sm text-fg-muted">
          <li>Export it, above.</li>
          <li>
            Open <span className="font-mono">chrome://extensions</span> and turn on
            Developer mode.
          </li>
          <li>Choose "Load unpacked" and point it at the folder.</li>
          <li>
            Open <span className="font-mono">{status.matchUrl.replace('/*', '')}</span> —
            the buttons appear on the create form while this app is running.
          </li>
        </ol>
      </section>

      <section className="sm-panel space-y-2 p-4">
        <h3 className="text-sm font-semibold">How it reaches this app</h3>
        <p className="text-sm text-fg-muted">
          Over <span className="font-mono">127.0.0.1:{status.port}</span> — loopback only,
          so nothing off this machine can see it — and gated by a token written into the
          exported copy. It reads your library and nothing else; it never writes.
        </p>
        <p className="text-sm text-fg-muted">
          The buttons appear only while SunoMaker is open. On a Suno page with the app
          closed, the extension stays silent.
        </p>
      </section>
    </div>
  )
}
