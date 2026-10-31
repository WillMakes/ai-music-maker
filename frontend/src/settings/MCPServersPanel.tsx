import {useCallback, useEffect, useState} from 'react'
import {ChevronDown, ChevronRight, Loader2, Plug, RefreshCw, Trash2} from 'lucide-react'
import {
  AddMCPServer,
  AuthorizeMCPServer,
  DisconnectMCPServer,
  GetMCPServers,
  RefreshMCPServerTools,
  RemoveMCPServer,
} from '../../wailsjs/go/app/App'
import {app} from '../../wailsjs/go/models'
import {TextField} from '../components/Field'
import {StatusPill} from '../components/StatusPill'
import {useDataChanged} from '../lib/dataChanged'
import {toMessage} from '../lib/errors'

/**
 * Settings → MCP servers: the remote servers this app itself signs in to.
 *
 * Suno is preset, because its server is the other way to make an album (the
 * album page's "Create via Suno MCP"). The app authorizes on its own account —
 * an assistant connected to the same server elsewhere on this machine has its
 * own sign-in, which this panel neither reads nor needs. Tokens never reach
 * the window: the panel sees a state and the tools.
 */
export function MCPServersPanel() {
  const [servers, setServers] = useState<app.MCPServerStatus[]>([])
  const [error, setError] = useState('')
  const [name, setName] = useState('')
  const [address, setAddress] = useState('')
  const [adding, setAdding] = useState(false)

  const reload = useCallback(() => {
    GetMCPServers()
      .then(setServers)
      .catch((err) => setError(toMessage(err)))
  }, [])
  useEffect(reload, [reload])
  useDataChanged(['mcp_servers'], reload)

  return (
    <div className="space-y-5">
      {servers.map((server) => (
        <ServerCard key={server.id} server={server} onChanged={reload} />
      ))}

      <section className="sm-panel space-y-3 p-4">
        <h2 className="text-sm font-semibold">Add a server</h2>
        <p className="text-xs text-fg-muted">
          A remote MCP server reached over HTTPS (Streamable HTTP). If it asks for a sign-in, the
          app registers itself with it and you approve it in your browser.
        </p>
        <div className="grid gap-3 md:grid-cols-[1fr_2fr]">
          <TextField label="Name" value={name} placeholder="Optional" onChange={setName} />
          <TextField
            label="Address"
            value={address}
            placeholder="https://example.com/mcp"
            onChange={setAddress}
          />
        </div>
        <button
          className="sm-button sm-button-quiet"
          type="button"
          disabled={adding || !address.trim()}
          onClick={async () => {
            setAdding(true)
            setError('')
            try {
              await AddMCPServer(name, address)
              setName('')
              setAddress('')
              reload()
            } catch (err) {
              setError(toMessage(err))
            } finally {
              setAdding(false)
            }
          }}
        >
          {adding && <Loader2 size={15} className="animate-spin" />}
          Add the server
        </button>
      </section>

      {error && <p className="text-sm text-danger">{error}</p>}
    </div>
  )
}

const STATE_LABEL: Record<string, {tone: 'ok' | 'warn' | 'muted'; label: string}> = {
  connected: {tone: 'ok', label: 'Connected'},
  needs_authorization: {tone: 'warn', label: 'Needs authorization'},
  not_connected: {tone: 'muted', label: 'Not connected'},
}

function ServerCard({server, onChanged}: {server: app.MCPServerStatus; onChanged: () => void}) {
  const [busy, setBusy] = useState<'' | 'authorizing' | 'disconnecting' | 'refreshing' | 'removing'>('')
  const [error, setError] = useState('')
  const [showTools, setShowTools] = useState(false)
  const [openTool, setOpenTool] = useState('')

  const act = async (kind: typeof busy, call: () => Promise<unknown>) => {
    setBusy(kind)
    setError('')
    try {
      await call()
      onChanged()
    } catch (err) {
      setError(toMessage(err))
    } finally {
      setBusy('')
    }
  }

  const state = STATE_LABEL[server.state] ?? STATE_LABEL.not_connected
  const connected = server.state === 'connected'

  return (
    <section className="sm-panel space-y-3 p-4" aria-label={server.name}>
      <h2 className="flex items-center gap-2 text-sm font-semibold">
        <Plug size={15} />
        <span className="flex-1">{server.name}</span>
        <StatusPill tone={state.tone}>{state.label}</StatusPill>
      </h2>
      <p className="break-all font-mono text-xs text-fg-muted">{server.url}</p>

      {server.preset && (
        <p className="text-xs text-fg-muted">
          Once connected, every album offers <strong>Create via Suno MCP</strong> beside the
          Chrome extension: the tracks are generated on your Suno account and their takes filed
          into the album folder. Generating spends Suno credits; filing a take spends a download
          credit, and the album asks before it does.
        </p>
      )}

      {server.state === 'needs_authorization' && (
        <p className="text-xs text-warn">
          The server no longer accepts this app&apos;s sign-in. Authorize it again.
        </p>
      )}

      {connected && (
        <p className="text-xs text-fg-muted">
          {server.serverName ? <>{server.serverName} · </> : null}
          Signed in {server.connectedAt ? new Date(server.connectedAt).toLocaleString() : ''}
          {server.scopes.length > 0 && <> · {server.scopes.join(', ')}</>}
        </p>
      )}

      <div className="flex flex-wrap gap-2">
        {!connected && (
          <button
            className="sm-button sm-button-primary"
            type="button"
            disabled={busy !== ''}
            onClick={() => void act('authorizing', () => AuthorizeMCPServer(server.id))}
          >
            {busy === 'authorizing' ? <Loader2 size={15} className="animate-spin" /> : <Plug size={15} />}
            {busy === 'authorizing'
              ? 'Waiting for the browser…'
              : server.state === 'needs_authorization'
                ? 'Authorize again'
                : 'Connect and authorize'}
          </button>
        )}
        {connected && (
          <button
            className="sm-button sm-button-quiet"
            type="button"
            disabled={busy !== ''}
            onClick={() => void act('refreshing', () => RefreshMCPServerTools(server.id))}
          >
            {busy === 'refreshing' ? <Loader2 size={15} className="animate-spin" /> : <RefreshCw size={15} />}
            Check the connection
          </button>
        )}
        {server.state !== 'not_connected' && (
          <button
            className="sm-button sm-button-quiet"
            type="button"
            disabled={busy !== ''}
            onClick={() => void act('disconnecting', () => DisconnectMCPServer(server.id))}
          >
            Disconnect
          </button>
        )}
        {!server.preset && (
          <button
            className="sm-button sm-button-quiet"
            type="button"
            disabled={busy !== ''}
            onClick={() => void act('removing', () => RemoveMCPServer(server.id))}
          >
            <Trash2 size={15} />
            Remove
          </button>
        )}
      </div>
      {busy === 'authorizing' && (
        <p className="text-xs text-fg-muted">
          Approve the sign-in in the browser tab that just opened, then come back here.
        </p>
      )}

      {server.tools.length > 0 && (
        <div>
          <button
            className="flex items-center gap-1 text-xs text-accent hover:underline"
            type="button"
            onClick={() => setShowTools(!showTools)}
          >
            {showTools ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
            {server.tools.length} tools
            {server.toolsAt && (
              <span className="text-fg-muted"> · read {new Date(server.toolsAt).toLocaleString()}</span>
            )}
          </button>
          {showTools && (
            <ul className="mt-2 divide-y divide-edge rounded-md border border-edge">
              {server.tools.map((tool) => (
                <li key={tool.name} className="px-3 py-2">
                  <button
                    className="flex w-full items-baseline gap-2 text-left"
                    type="button"
                    onClick={() => setOpenTool(openTool === tool.name ? '' : tool.name)}
                  >
                    <span className="font-mono text-xs">{tool.name}</span>
                    <span className="truncate text-xs text-fg-muted">
                      {tool.title || tool.description.split('\n')[0]}
                    </span>
                  </button>
                  {openTool === tool.name && (
                    <div className="mt-2 space-y-2">
                      <p className="whitespace-pre-wrap text-xs text-fg-muted">{tool.description}</p>
                      <pre className="max-h-64 overflow-auto rounded-md border border-edge bg-surface px-3 py-2 font-mono text-xs text-fg-muted">
                        {prettySchema(tool.schema)}
                      </pre>
                    </div>
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      {(error || (server.lastError && server.state !== 'connected')) && (
        <p className="text-sm text-danger">{error || server.lastError}</p>
      )}
    </section>
  )
}

function prettySchema(schema: string) {
  try {
    return JSON.stringify(JSON.parse(schema), null, 2)
  } catch {
    return schema
  }
}
