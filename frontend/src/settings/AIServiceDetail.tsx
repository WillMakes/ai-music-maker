import {useCallback, useEffect, useState} from 'react'
import {ExternalLink, Unplug} from 'lucide-react'
import {
  ConnectAIAccount,
  ConnectAIKey,
  DisconnectAI,
  GetAIService,
  ListAIModels,
  OpenURL,
  SetAIServiceModel,
} from '../../wailsjs/go/app/App'
import {app} from '../../wailsjs/go/models'
import {PageHeader} from '../components/PageHeader'
import {SelectField, TextField} from '../components/Field'
import {StatusPill} from '../components/StatusPill'
import {toMessage} from '../lib/errors'

/**
 * One provider's page: how it is connected, and which model it runs.
 *
 * The key is never read back — the backend returns a masked form and nothing
 * else — so the field here is always empty and always a *new* key. That is the
 * point: a page that could show a key is a page that leaks one to anything that
 * can read the window.
 */
export function AIServiceDetail({serviceId}: {serviceId: string}) {
  const [service, setService] = useState<app.AIServiceStatus | null>(null)
  const [models, setModels] = useState<string[]>([])
  const [key, setKey] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const reload = useCallback(() => {
    GetAIService(serviceId)
      .then((found) => {
        setService(found)
        setError('')
      })
      .catch((err) => setError(toMessage(err)))
  }, [serviceId])

  useEffect(reload, [reload])

  // The model list needs a key to ask with, so account-mode connections answer
  // empty and get a free-text field instead of a picker. Asked rather than
  // hard-coded: a list written into this build is wrong by the next release, and
  // being wrong here means a model the producer cannot select.
  useEffect(() => {
    if (!service?.connected) return
    ListAIModels(serviceId)
      .then((rows) => setModels(rows ?? []))
      .catch(() => setModels([]))
  }, [serviceId, service?.connected, service?.mode])

  if (!service) {
    return <p className="text-sm text-fg-muted">{error || 'Loading…'}</p>
  }

  const act = async (work: Promise<app.AIServiceStatus>) => {
    setBusy(true)
    setError('')
    try {
      setService(await work)
      setKey('')
    } catch (err) {
      setError(toMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title={service.name}
        breadcrumb="Settings — AI"
        subtitle={
          service.connected ? (
            <span className="flex flex-wrap items-center gap-2">
              <StatusPill tone="ok">
                {service.mode === 'account' ? 'Signed-in CLI' : 'API key'}
              </StatusPill>
              <span>{service.account}</span>
              {service.maskedKey && (
                <span className="font-mono text-xs">{service.maskedKey}</span>
              )}
            </span>
          ) : (
            'Not connected.'
          )
        }
        actions={
          service.connected && (
            <button
              className="sm-button sm-button-danger"
              type="button"
              disabled={busy}
              onClick={() => void act(DisconnectAI(serviceId))}
            >
              <Unplug size={15} />
              Disconnect
            </button>
          )
        }
      />

      {error && <p className="text-sm text-danger">{error}</p>}

      {service.cliName && (
        <section className="sm-panel space-y-3 p-4">
          <h2 className="text-sm font-semibold">Use the signed-in CLI</h2>
          <p className="text-sm text-fg-muted">
            Runs <span className="font-mono">{service.cliName}</span> headlessly, so usage
            draws on your subscription. Nothing is stored here — the CLI holds and
            refreshes its own credential.
          </p>
          {service.cliFound ? (
            <p className="font-mono text-xs text-fg-muted">{service.cliPath}</p>
          ) : (
            <p className="text-sm text-warn">
              <span className="font-mono">{service.cliName}</span> was not found on this
              machine.
            </p>
          )}
          <div className="flex flex-wrap gap-2">
            <button
              className="sm-button sm-button-primary"
              type="button"
              disabled={busy || !service.cliFound || service.mode === 'account'}
              onClick={() => void act(ConnectAIAccount(serviceId))}
            >
              {service.mode === 'account' ? 'Connected this way' : 'Use the CLI'}
            </button>
            {!service.cliFound && service.installUrl && (
              <button
                className="sm-button sm-button-quiet"
                type="button"
                onClick={() => void OpenURL(service.installUrl).catch(() => {})}
              >
                <ExternalLink size={15} />
                How to install it
              </button>
            )}
          </div>
        </section>
      )}

      {/* Only where the provider has keys to make (#27): Antigravity has no
          API, and a form whose every submission is refused is worse than no
          form. */}
      {service.keysUrl && (
      <section className="sm-panel space-y-3 p-4">
        <h2 className="text-sm font-semibold">Use an API key</h2>
        <p className="text-sm text-fg-muted">
          Checked against {service.name} before it is stored, so a key that will not work
          is refused here rather than halfway through an interview.
        </p>
        <TextField
          label="API key"
          value={key}
          placeholder={service.maskedKey || 'Paste a key'}
          hint="Stored in this app's own database and never shown again."
          onChange={setKey}
          disabled={busy}
        />
        <div className="flex flex-wrap gap-2">
          <button
            className="sm-button sm-button-primary"
            type="button"
            disabled={busy || !key.trim()}
            onClick={() => void act(ConnectAIKey(serviceId, key))}
          >
            {busy ? 'Checking…' : 'Connect'}
          </button>
          {service.keysUrl && (
            <button
              className="sm-button sm-button-quiet"
              type="button"
              onClick={() => void OpenURL(service.keysUrl).catch(() => {})}
            >
              <ExternalLink size={15} />
              Get a key
            </button>
          )}
        </div>
      </section>
      )}

      {service.connected && (
        <section className="sm-panel space-y-3 p-4">
          <h2 className="text-sm font-semibold">Model</h2>
          {models.length > 0 ? (
            <SelectField
              label="Default model"
              hint="Leave it on the provider's own choice unless you want a particular one."
              value={service.model}
              options={[
                {value: '', label: "The provider's own choice"},
                ...models.map((model) => ({value: model, label: model})),
              ]}
              onChange={(model) => void act(SetAIServiceModel(serviceId, model))}
            />
          ) : (
            <TextField
              label="Default model"
              hint={
                service.mode === 'account'
                  ? 'A signed-in CLI has no key to list models with, so type one or leave it blank.'
                  : 'Leave it blank for the provider’s own choice.'
              }
              value={service.model}
              onChange={(model) =>
                setService(app.AIServiceStatus.createFrom({...service, model}))
              }
            />
          )}
          {models.length === 0 && (
            <button
              className="sm-button sm-button-quiet"
              type="button"
              disabled={busy}
              onClick={() => void act(SetAIServiceModel(serviceId, service.model))}
            >
              Save the model
            </button>
          )}
        </section>
      )}
    </div>
  )
}
