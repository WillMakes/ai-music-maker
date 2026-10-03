import {useCallback, useEffect, useMemo, useState} from 'react'
import {ChevronDown, ChevronRight} from 'lucide-react'
import {
  ConnectMCPClient,
  DisconnectMCPClient,
  GetMCPStatus,
  ListMCPTools,
} from '../../wailsjs/go/app/App'
import {app} from '../../wailsjs/go/models'
import {TextField} from '../components/Field'
import {StatusPill} from '../components/StatusPill'
import {toMessage} from '../lib/errors'

/**
 * Settings → AI assistants: registering this app as an MCP server.
 *
 * Connecting writes one entry into the client's own config file, pointing at
 * this binary run as `aimusicmaker mcp`. Every other byte of those files is left
 * exactly as it was (see mcpconfig.go) — they hold configuration the producer
 * cares about and this app has no business rewriting.
 *
 * A client with no config file has never been run, so it is shown and not
 * offered: writing a config nothing reads would be a connection that appears to
 * work and does nothing.
 */
export function AIAssistants() {
  const [status, setStatus] = useState<app.MCPStatus | null>(null)
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

  const act = async (id: string, work: Promise<app.MCPStatus>) => {
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
          Let an assistant do everything this window does, through {status.toolCount} tools —
          the library and its files, the interviews, artwork, skills, settings and the bug queue.
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
          One entry named <span className="font-mono">aimusicmaker</span>, launching:
        </p>
        <p className="overflow-x-auto whitespace-nowrap rounded bg-surface-hover px-3 py-2 font-mono text-xs">
          {status.command} mcp
        </p>
        <p className="text-sm text-fg-muted">
          No token and no port — the server reads the database directly, so it works
          whether or not this window is open.
        </p>
      </section>

      <ToolList count={status.toolCount} />
    </div>
  )
}

/**
 * Every tool the server offers, with what it does and what it takes (#90).
 *
 * Folded away by default: it is a reference for somebody deciding what to let
 * an assistant do, not something to scroll past on the way to the Connect
 * buttons. Read from the same list the server dispatches through, so a tool
 * shown here can be called and a tool that can be called is shown.
 */
function ToolList({count}: {count: number}) {
  const [tools, setTools] = useState<app.MCPToolInfo[]>([])
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [error, setError] = useState('')

  useEffect(() => {
    ListMCPTools()
      .then((found) => setTools(found ?? []))
      .catch((err) => setError(toMessage(err)))
  }, [])

  // Grouped in the server's own order — library first, settings last — and
  // narrowed by the search across name and description, since a producer
  // looking for "the one that imports takes" has words, not a tool name.
  const groups = useMemo(() => {
    const needle = query.trim().toLowerCase()
    const out: {name: string; tools: app.MCPToolInfo[]}[] = []
    for (const tool of tools) {
      if (
        needle &&
        !tool.name.toLowerCase().includes(needle) &&
        !tool.description.toLowerCase().includes(needle)
      ) {
        continue
      }
      const last = out[out.length - 1]
      if (last && last.name === tool.group) last.tools.push(tool)
      else out.push({name: tool.group, tools: [tool]})
    }
    return out
  }, [tools, query])

  return (
    <section className="sm-panel p-4">
      <button
        className="flex w-full items-center gap-2 text-left text-sm font-semibold"
        type="button"
        aria-expanded={open}
        onClick={() => setOpen((was) => !was)}
      >
        {open ? (
          <ChevronDown size={16} aria-hidden className="shrink-0 text-fg-muted" />
        ) : (
          <ChevronRight size={16} aria-hidden className="shrink-0 text-fg-muted" />
        )}
        The {count} tools
        <span className="font-normal text-fg-muted">— what a connected assistant can do here</span>
      </button>

      {open && (
        <div className="mt-4 space-y-4">
          {error && <p className="text-sm text-danger">{error}</p>}
          <TextField
            label="Find a tool"
            value={query}
            onChange={setQuery}
            placeholder="Search by name or by what it does"
          />
          {groups.length === 0 && !error && (
            <p className="text-sm text-fg-muted">
              {tools.length === 0 ? 'Loading…' : 'Nothing matches that.'}
            </p>
          )}
          {groups.map((group) => (
            <div key={group.name}>
              <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-fg-muted">
                {group.name}
              </h3>
              <ul className="divide-y divide-edge rounded border border-edge">
                {group.tools.map((tool) => (
                  <li key={tool.name} className="px-3 py-2">
                    <p className="font-mono text-xs font-medium">{tool.name}</p>
                    <p className="mt-1 text-sm text-fg-muted">{tool.description}</p>
                    {tool.arguments.length > 0 && (
                      <p className="mt-1.5 flex flex-wrap gap-1">
                        {tool.arguments.map((arg) => (
                          <span
                            key={arg.name}
                            title={(arg.required ? 'Required — ' : 'Optional — ') + arg.description}
                            className={
                              'rounded px-1.5 py-0.5 font-mono text-xs ' +
                              (arg.required ? 'bg-surface-hover text-fg' : 'text-fg-muted')
                            }
                          >
                            {arg.name}
                            {arg.required && '*'}
                          </span>
                        ))}
                      </p>
                    )}
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>
      )}
    </section>
  )
}
