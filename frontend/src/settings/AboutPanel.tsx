import {useEffect, useState} from 'react'
import {ExternalLink, RefreshCw} from 'lucide-react'
import {Documentation, GetBuildInfo, OpenURL} from '../../wailsjs/go/app/App'
import {app} from '../../wailsjs/go/models'
import {StatusPill} from '../components/StatusPill'
import logo from '../assets/logo.png'
import {toMessage} from '../lib/errors'
import {UpdateDialog} from '../updates/UpdateButton'
import {useUpdates} from '../updates/UpdatesProvider'

/**
 * Settings → About: what is running, and where it came from.
 *
 * The documentation shown here is the same text `aimusicmaker docs` prints and the
 * get_documentation MCP tool serves — generated from what the app actually holds
 * rather than written down, so there is no version of "what this app is" that
 * can drift from the others.
 */
export function AboutPanel() {
  const [info, setInfo] = useState<app.BuildInfo | null>(null)
  const [docs, setDocs] = useState('')
  const [error, setError] = useState('')

  useEffect(() => {
    GetBuildInfo()
      .then(setInfo)
      .catch((err) => setError(toMessage(err)))
    Documentation()
      .then(setDocs)
      .catch(() => setDocs(''))
  }, [])

  if (!info) {
    return <p className="text-sm text-fg-muted">{error || 'Loading…'}</p>
  }

  return (
    <div className="space-y-5">
      <section className="sm-panel space-y-3 p-4">
        {/* The mascot at full size (#21). It was a lockup with the name painted
            under it, cropped away at the rename (#103): a name in pixels is a
            name that has to be redrawn, and the heading below says it in text. */}
        <img src={logo} alt="AIMusicMaker" className="mx-auto w-52 max-w-full" />
        <h2 className="flex items-center gap-2 text-sm font-semibold">
          AIMusicMaker
          <StatusPill tone="muted">{info.version}</StatusPill>
          {info.dirty && <StatusPill tone="warn">Uncommitted changes</StatusPill>}
        </h2>
        <dl className="grid gap-x-6 gap-y-1 text-sm sm:grid-cols-[10rem_1fr]">
          <Row label="Platform" value={info.platform} />
          <Row label="Go" value={info.goVersion} />
          <Row label="Commit" value={info.commit ? info.commit.slice(0, 12) : 'unknown'} />
          <Row label="Checkout" value={info.repo.dir || 'not a checkout'} />
          <Row label="Repository" value={info.repo.slug || 'no GitHub remote'} />
        </dl>
        {info.repo.slug && (
          <button
            className="sm-button sm-button-quiet"
            type="button"
            onClick={() =>
              void OpenURL('https://github.com/' + info.repo.slug).catch(() => {})
            }
          >
            <ExternalLink size={15} />
            Open the repository
          </button>
        )}
      </section>

      <UpdatesSection />

      {docs && (
        <section className="sm-panel p-4">
          <h2 className="text-sm font-semibold">What this app is</h2>
          <p className="mt-1 text-xs text-fg-muted">
            The same document <span className="font-mono">aimusicmaker docs</span> prints
            and an assistant reads over MCP.
          </p>
          <pre className="mt-3 overflow-x-auto whitespace-pre-wrap rounded bg-surface-hover p-3 font-mono text-xs">
            {docs}
          </pre>
        </section>
      )}

      {error && <p className="text-sm text-danger">{error}</p>}
    </div>
  )
}

function Row({label, value}: {label: string; value: string}) {
  return (
    <>
      <dt className="text-fg-muted">{label}</dt>
      <dd className="truncate font-mono text-xs">{value}</dd>
    </>
  )
}

/**
 * Whether this is the newest release, and a way to ask again (#132). The top
 * bar only speaks up when there is an update; this is where "you are up to
 * date", and why a check could not answer, are said.
 */
function UpdatesSection() {
  const {status, checking, check} = useUpdates()
  const [open, setOpen] = useState(false)

  let line = 'Not checked yet.'
  if (status?.available) line = status.latest + ' is available.'
  else if (status?.problem) line = status.problem.charAt(0).toUpperCase() + status.problem.slice(1) + '.'
  else if (status?.latest) line = 'Up to date: ' + status.latest + ' is the newest release.'

  return (
    <section className="sm-panel space-y-3 p-4">
      <h2 className="text-sm font-semibold">Updates</h2>
      <p className="text-sm text-fg-muted">{line}</p>
      <div className="flex flex-wrap gap-2">
        <button
          className="sm-button sm-button-quiet"
          type="button"
          disabled={checking}
          onClick={() => void check()}
        >
          <RefreshCw size={15} />
          {checking ? 'Checking…' : 'Check for updates'}
        </button>
        {status?.available && (
          <button className="sm-button sm-button-primary" type="button" onClick={() => setOpen(true)}>
            See the update
          </button>
        )}
      </div>
      {open && <UpdateDialog onClose={() => setOpen(false)} />}
    </section>
  )
}
