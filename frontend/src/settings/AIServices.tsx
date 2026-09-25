import {useCallback, useEffect, useState} from 'react'
import {Check, Star} from 'lucide-react'
import {GetAIServices, SetDefaultAIService} from '../../wailsjs/go/app/App'
import {app} from '../../wailsjs/go/models'
import {StatusPill} from '../components/StatusPill'
import {ProviderLogo} from './ProviderLogo'
import {useDataChanged} from '../lib/dataChanged'
import {toMessage} from '../lib/errors'

/**
 * Settings → AI: which providers this app may act as.
 *
 * Each row says what it is connected as, because the two modes bill differently
 * and that is the fact a producer most needs to be able to check. One is starred
 * as the default — what an interview reaches for — and starring is the only
 * action here; everything else is on the service's own page.
 */
export function AIServices({onOpen}: {onOpen: (serviceId: string) => void}) {
  const [services, setServices] = useState<app.AIServiceStatus[]>([])
  const [error, setError] = useState('')

  const reload = useCallback(() => {
    GetAIServices()
      .then((rows) => {
        setServices(rows ?? [])
        setError('')
      })
      .catch((err) => setError(toMessage(err)))
  }, [])

  useEffect(reload, [reload])
  useDataChanged(['ai_services'], reload)

  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-sm font-semibold">AI providers</h2>
        <p className="mt-1 text-sm text-fg-muted">
          Connect one by its signed-in CLI, which bills your subscription, or by an API
          key, which bills the console account. The starred one is what the interviews
          use.
        </p>
      </div>

      {error && <p className="text-sm text-danger">{error}</p>}

      <ul className="sm-panel divide-y divide-edge overflow-hidden">
        {services.map((service) => (
          <li key={service.id} className="flex items-center gap-3 px-4 py-3">
            {/* The provider's own mark (#25), so the row is recognisable at a
                glance the way the row in any other tool is. */}
            <ProviderLogo id={service.id} />
            <button
              className="min-w-0 flex-1 text-left"
              type="button"
              onClick={() => onOpen(service.id)}
            >
              <span className="flex items-center gap-2">
                <span className="font-medium">{service.name}</span>
                {service.connected ? (
                  <StatusPill tone="ok">
                    {service.mode === 'account' ? 'Signed-in CLI' : 'API key'}
                  </StatusPill>
                ) : (
                  <StatusPill tone="muted">Not connected</StatusPill>
                )}
              </span>
              <span className="mt-0.5 block truncate text-xs text-fg-muted">
                {service.connected
                  ? service.account +
                    (service.model ? ' · ' + service.model : ' · default model')
                  : service.cliName
                    ? 'Sign in to ' +
                      service.cliName +
                      (service.keysUrl ? ', or add an API key' : '')
                    : 'API key only'}
              </span>
            </button>

            <button
              className="sm-button sm-button-quiet px-2"
              type="button"
              title={service.default ? 'The default' : 'Make this the default'}
              aria-label={service.default ? 'The default provider' : 'Make default'}
              disabled={!service.connected || service.default}
              onClick={async () => {
                try {
                  setServices((await SetDefaultAIService(service.id)) ?? [])
                } catch (err) {
                  setError(toMessage(err))
                }
              }}
            >
              <Star
                size={15}
                className={service.default ? 'text-accent' : 'text-fg-muted'}
                fill={service.default ? 'currentColor' : 'none'}
              />
            </button>

            <button
              className="sm-button sm-button-quiet"
              type="button"
              onClick={() => onOpen(service.id)}
            >
              {service.connected ? <Check size={15} /> : null}
              {service.connected ? 'Manage' : 'Connect'}
            </button>
          </li>
        ))}
      </ul>
    </div>
  )
}
