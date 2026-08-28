import {useCallback, useEffect, useState} from 'react'
import {ConnectMCPClient, DisconnectMCPClient, GetMCPStatus} from '../../wailsjs/go/main/App'
import {main} from '../../wailsjs/go/models'
import {StatusPill} from '../components/StatusPill'
import {toMessage} from '../lib/errors'

/**
 * Settings → AI assistants: registering this app as an MCP server.
 *
 * Connecting writes one entry into the client's own config file, pointing at
 * this binary run as `suno-maker mcp`. Every other byte of those files is left
 * exactly as it was (see mcpconfig.go) — they hold configuration the producer
 * cares about and this app has no business rewriting.
 *
 * A client with no config file has never been run, so it is shown and not
 * offered: writing a config nothing reads would be a connection that appears to
 * work and does nothing.
 */
export function AIAssistants() {
  const [status, setStatus] = useState<main.MCPStatus | null>(null)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')

  const reload = useCallback(() => {
    GetMCPStatus()
      .then((found) => {
        setStatus(found)
        setError('')
      })
      .catch((err) => setError(toMessage(err)))
  }, [])

  useEffect(reload, [reload])

  if (!status) {
    return <p className="text-sm text-fg-muted">{error || 'Loading…'}</p>
  }

  const act = async (id: string, work: Promise<main.MCPStatus>) => {
    setBusy(id)
    setError('')
    try {
      setStatus(await work)
    } catch (err) {
      setError(toMessage(err))
    } finally {
      setBusy('')
    }
  }

  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-sm font-semibold">AI assistants</h2>
        <p className="mt-1 text-sm text-fg-muted">
          Let an assistant read and write your library through {status.toolCount} tools —
          artists, albums, tracks, skills and the bug queue.
        </p>
      </div>

      {error && <p className="text-sm text-danger">{error}</p>}

      <ul className="sm-panel divide-y divide-edge overflow-hidden">
        {status.clients.map((client) => (
          <li key={client.id} className="flex items-center gap-3 px-4 py-3">
            <span className="min-w-0 flex-1">
              <span className="flex items-center gap-2">
                <span className="font-medium">{client.name}</span>
                {client.problem ? (
                  <StatusPill tone="danger">Left alone</StatusPill>
                ) : !client.present ? (
                  <StatusPill tone="muted">Not installed</StatusPill>
                ) : client.connected && client.current ? (
                  <StatusPill tone="ok">Connected</StatusPill>
                ) : client.connected ? (
                  <StatusPill tone="warn">Points elsewhere</StatusPill>
                ) : (
                  <StatusPill tone="muted">Not connected</StatusPill>
                )}
              </span>
              <span className="mt-0.5 block truncate font-mono text-xs text-fg-muted">
                {client.problem || client.path}
              </span>
            </span>

            {client.present && !client.problem && (
              <>
                <button
                  className="sm-button sm-button-quiet"
                  type="button"
                  disabled={busy === client.id}
                  onClick={() => void act(client.id, ConnectMCPClient(client.id))}
                >
                  {client.connected && client.current ? 'Rewrite' : 'Connect'}
                </button>
                {client.connected && (
                  <button
                    className="sm-button sm-button-quiet"
                    type="button"
                    disabled={busy === client.id}
                    onClick={() => void act(client.id, DisconnectMCPClient(client.id))}
                  >
                    Remove
                  </button>
                )}
              </>
            )}
          </li>
        ))}
      </ul>

      <section className="sm-panel space-y-2 p-4">
        <h3 className="text-sm font-semibold">What gets written</h3>
        <p className="text-sm text-fg-muted">
          One entry named <span className="font-mono">suno-maker</span>, launching:
        </p>
        <p className="overflow-x-auto whitespace-nowrap rounded bg-surface-hover px-3 py-2 font-mono text-xs">
          {status.command} mcp
        </p>
        <p className="text-sm text-fg-muted">
          No token and no port — the server reads the database directly, so it works
          whether or not this window is open.
        </p>
      </section>
    </div>
  )
}
