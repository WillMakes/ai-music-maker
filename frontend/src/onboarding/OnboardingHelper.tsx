import {useCallback, useEffect, useState} from 'react'
import {Check, ChevronLeft, ExternalLink, KeyRound, Star} from 'lucide-react'
import {
  ConnectAIAccount,
  ConnectMCPClient,
  DisconnectMCPClient,
  GetAIServices,
  GetMCPStatus,
  OpenURL,
  SetDefaultAIService,
} from '../../wailsjs/go/app/App'
import {app} from '../../wailsjs/go/models'
import {Modal} from '../components/Modal'
import {StatusPill} from '../components/StatusPill'
import {AIServiceDetail} from '../settings/AIServiceDetail'
import {MCPClientPill} from '../settings/AIAssistants'
import {ProviderLogo} from '../settings/ProviderLogo'
import {useDataChanged} from '../lib/dataChanged'
import {SETTING_KEYS, saveSetting} from '../lib/settings'
import {toMessage} from '../lib/errors'

/**
 * The setup guide that opens at launch (#127), in two steps (#129): the AI
 * providers the app's features run on, then the assistants that can drive the
 * app over MCP.
 *
 * Each card carries its own primary action, the one most likely to finish the
 * job in a click — sign in with a CLI that is already installed, connect an
 * assistant that is already installed — so setting up is pressing buttons
 * rather than reading. The API-key form is the one thing that needs more room,
 * and it is Settings → AI's own page opened in place, not a second copy of it.
 *
 * Every way out — Done, Skip, ×, Escape, the scrim — turns the helper off,
 * because a guide that comes back after being dismissed is nagging.
 * Preferences turns it back on.
 */
export function OnboardingHelper({onClose}: {onClose: () => void}) {
  const [step, setStep] = useState<'providers' | 'assistants'>('providers')
  const [serviceId, setServiceId] = useState('')

  const finish = () => {
    void saveSetting(SETTING_KEYS.onboardingHelper, 'off')
    onClose()
  }

  const steps = [
    {id: 'providers' as const, label: 'AI providers'},
    {id: 'assistants' as const, label: 'AI assistants'},
  ]

  return (
    <Modal
      title="Set up AIMusicMaker"
      wide
      onClose={finish}
      footer={
        <>
          <button className="sm-button sm-button-quiet mr-auto" type="button" onClick={finish}>
            Skip for now
          </button>
          {step === 'assistants' && (
            <button
              className="sm-button sm-button-quiet"
              type="button"
              onClick={() => setStep('providers')}
            >
              Back
            </button>
          )}
          {step === 'providers' ? (
            <button
              className="sm-button sm-button-primary"
              type="button"
              onClick={() => {
                setServiceId('')
                setStep('assistants')
              }}
            >
              Next
            </button>
          ) : (
            <button className="sm-button sm-button-primary" type="button" onClick={finish}>
              Done
            </button>
          )}
        </>
      }
    >
      <ol className="mb-5 flex items-center gap-2 text-xs" aria-label="Setup steps">
        {steps.map((one, i) => {
          const on = one.id === step
          return (
            <li key={one.id} className="flex items-center gap-2">
              {i > 0 && <span className="h-px w-6 bg-edge" aria-hidden />}
              <button
                type="button"
                aria-current={on ? 'step' : undefined}
                className={
                  'flex items-center gap-2 rounded-full border px-3 py-1 ' +
                  (on
                    ? 'border-accent bg-accent text-accent-fg'
                    : 'border-edge text-fg-muted hover:bg-surface-hover')
                }
                onClick={() => {
                  setServiceId('')
                  setStep(one.id)
                }}
              >
                <span className="tabular-nums">{i + 1}</span>
                {one.label}
              </button>
            </li>
          )
        })}
      </ol>

      {step === 'providers' &&
        (serviceId ? (
          <div className="space-y-4">
            <button
              className="sm-button sm-button-quiet"
              type="button"
              onClick={() => setServiceId('')}
            >
              <ChevronLeft size={15} />
              All providers
            </button>
            <AIServiceDetail serviceId={serviceId} />
          </div>
        ) : (
          <ProvidersStep onOpen={setServiceId} />
        ))}

      {step === 'assistants' && <AssistantsStep />}
    </Modal>
  )
}

/**
 * What a provider is the only way to do, said on its card: otherwise the
 * producer learns it from an error the first time they try. Both match the
 * backend's own gates (renderCover, AnalyzeMusicURL).
 */
const REQUIRED_FOR: Record<string, string> = {
  openai: 'Required to generate artwork',
  antigravity: 'Required to analyse online music for inspiration',
}

function ProvidersStep({onOpen}: {onOpen: (serviceId: string) => void}) {
  const [services, setServices] = useState<app.AIServiceStatus[]>([])
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')

  const reload = useCallback(() => {
    GetAIServices()
      .then((rows) => setServices(rows ?? []))
      .catch((err) => setError(toMessage(err)))
  }, [])
  useEffect(reload, [reload])
  useDataChanged(['ai_services'], reload)

  const act = async (id: string, work: () => Promise<unknown>) => {
    setBusy(id)
    setError('')
    try {
      await work()
      reload()
    } catch (err) {
      setError(toMessage(err))
    } finally {
      setBusy('')
    }
  }

  return (
    <div className="space-y-4">
      <p className="text-sm text-fg-muted">
        AIMusicMaker writes artists, albums, prompts and lyrics with the AI you already use.
        Connect at least one, and star the one the interviews should use. A signed-in CLI bills
        your subscription; an API key bills the console account.
      </p>
      {error && <p className="text-sm text-danger">{error}</p>}
      <ul className="grid gap-3 sm:grid-cols-2">
        {services.map((service) => (
          <li
            key={service.id}
            className={
              'flex flex-col gap-3 rounded-xl border p-4 transition-colors ' +
              (service.connected ? 'border-accent bg-surface' : 'border-edge bg-surface')
            }
          >
            <div className="flex items-start gap-3">
              <span className="flex h-11 w-11 shrink-0 items-center justify-center rounded-lg bg-tile">
                <ProviderLogo id={service.id} size={24} />
              </span>
              <span className="min-w-0 flex-1">
                <span className="flex flex-wrap items-center gap-2">
                  <span className="font-semibold">{service.name}</span>
                  {service.connected ? (
                    <StatusPill tone="ok">
                      {service.mode === 'account' ? 'Signed-in CLI' : 'API key'}
                    </StatusPill>
                  ) : (
                    <StatusPill tone="muted">Not connected</StatusPill>
                  )}
                </span>
                <span className="mt-0.5 block truncate text-xs text-fg-muted">
                  {service.connected ? service.account || 'Connected' : describeWay(service)}
                </span>
                {REQUIRED_FOR[service.id] && (
                  <span className="mt-1 block text-xs font-medium text-accent">
                    {REQUIRED_FOR[service.id]}
                  </span>
                )}
              </span>
              {service.connected && (
                <button
                  className="sm-button sm-button-quiet border-0 px-1.5 py-1"
                  type="button"
                  title={service.default ? 'The default' : 'Make this the default'}
                  aria-label={service.default ? 'The default provider' : 'Make default'}
                  disabled={service.default || busy === service.id}
                  onClick={() =>
                    void act(service.id, () => SetDefaultAIService(service.id))
                  }
                >
                  <Star
                    size={16}
                    className={service.default ? 'text-accent' : 'text-fg-muted'}
                    fill={service.default ? 'currentColor' : 'none'}
                  />
                </button>
              )}
            </div>
            <div className="mt-auto flex flex-wrap gap-2">
              <ProviderAction
                service={service}
                busy={busy === service.id}
                onSignIn={() => void act(service.id, () => ConnectAIAccount(service.id))}
                onOpen={() => onOpen(service.id)}
              />
            </div>
          </li>
        ))}
      </ul>
    </div>
  )
}

function describeWay(service: app.AIServiceStatus): string {
  if (service.cliName && service.cliFound) return service.cliName + ' is installed'
  if (service.cliName) return 'Sign in with ' + service.cliName + (service.keysUrl ? ', or use an API key' : '')
  return 'API key only'
}

/**
 * The card's buttons: the likeliest way to connect as the big one. A CLI that
 * is already on the machine is a single click; one that is not is a key or an
 * install page.
 */
function ProviderAction({
  service,
  busy,
  onSignIn,
  onOpen,
}: {
  service: app.AIServiceStatus
  busy: boolean
  onSignIn: () => void
  onOpen: () => void
}) {
  if (service.connected) {
    return (
      <button className="sm-button sm-button-quiet" type="button" onClick={onOpen}>
        <Check size={15} />
        Manage
      </button>
    )
  }
  const keyButton = service.keysUrl && (
    <button
      className={'sm-button ' + (service.cliFound ? 'sm-button-quiet' : 'sm-button-primary')}
      type="button"
      onClick={onOpen}
    >
      <KeyRound size={15} />
      Use an API key
    </button>
  )
  return (
    <>
      {service.cliName && service.cliFound && (
        <button className="sm-button sm-button-primary" type="button" disabled={busy} onClick={onSignIn}>
          {busy ? 'Connecting…' : 'Sign in with ' + service.cliName}
        </button>
      )}
      {keyButton}
      {service.cliName && !service.cliFound && service.installUrl && (
        <button
          className={'sm-button ' + (service.keysUrl ? 'sm-button-quiet' : 'sm-button-primary')}
          type="button"
          onClick={() => void OpenURL(service.installUrl).catch(() => {})}
        >
          <ExternalLink size={15} />
          Install {service.cliName}
        </button>
      )}
    </>
  )
}

/** The provider whose mark an MCP client wears: each client is one of theirs. */
const CLIENT_LOGO: Record<string, string> = {
  'claude-code': 'anthropic',
  'claude-desktop': 'anthropic',
  codex: 'openai',
  'gemini-cli': 'gemini',
  antigravity: 'antigravity',
  grok: 'grok',
}

function AssistantsStep() {
  const [status, setStatus] = useState<app.MCPStatus | null>(null)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')

  useEffect(() => {
    GetMCPStatus()
      .then(setStatus)
      .catch((err) => setError(toMessage(err)))
  }, [])

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

  if (!status) return <p className="text-sm text-fg-muted">{error || 'Loading…'}</p>

  return (
    <div className="space-y-4">
      <p className="text-sm text-fg-muted">
        Optional. Connect an assistant and it can do everything this window does, through{' '}
        {status.toolCount} tools. Connecting writes one entry named{' '}
        <span className="font-mono">aimusicmaker</span> into that assistant's own config and
        leaves the rest of the file alone. Restart the assistant afterwards to pick it up.
      </p>
      {error && <p className="text-sm text-danger">{error}</p>}
      <ul className="grid gap-3 sm:grid-cols-2">
        {status.clients.map((client) => {
          const ready = client.present && !client.problem
          const done = client.connected && client.current
          return (
            <li
              key={client.id}
              className={
                'flex flex-col gap-3 rounded-xl border bg-surface p-4 ' +
                (done ? 'border-accent' : 'border-edge')
              }
            >
              <div className="flex items-start gap-3">
                <span className="flex h-11 w-11 shrink-0 items-center justify-center rounded-lg bg-tile">
                  <ProviderLogo id={CLIENT_LOGO[client.id] ?? ''} size={24} />
                </span>
                <span className="min-w-0 flex-1">
                  <span className="flex flex-wrap items-center gap-2">
                    <span className="font-semibold">{client.name}</span>
                    <MCPClientPill client={client} />
                  </span>
                  <span className="mt-0.5 block truncate text-xs text-fg-muted">
                    {client.problem ||
                      (client.present ? client.path : 'Run it once and it will show up here')}
                  </span>
                </span>
              </div>
              {ready && (
                <div className="mt-auto flex flex-wrap gap-2">
                  {done ? (
                    <button
                      className="sm-button sm-button-quiet"
                      type="button"
                      disabled={busy === client.id}
                      onClick={() => void act(client.id, DisconnectMCPClient(client.id))}
                    >
                      Remove
                    </button>
                  ) : (
                    <button
                      className="sm-button sm-button-primary"
                      type="button"
                      disabled={busy === client.id}
                      onClick={() => void act(client.id, ConnectMCPClient(client.id))}
                    >
                      {busy === client.id
                        ? 'Connecting…'
                        : client.connected
                          ? 'Point it here'
                          : 'Connect ' + client.name}
                    </button>
                  )}
                </div>
              )}
            </li>
          )
        })}
      </ul>
    </div>
  )
}
