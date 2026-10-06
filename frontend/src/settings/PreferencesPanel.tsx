import {useCallback, useEffect, useState} from 'react'
import {Monitor, Moon, Sun} from 'lucide-react'
import {SetSetting} from '../../wailsjs/go/app/App'
import {useTheme, type ThemePreference} from '../theme/ThemeProvider'
import {SETTING_KEYS, loadSetting} from '../lib/settings'
import {useDataChanged} from '../lib/dataChanged'
import {toMessage} from '../lib/errors'

/**
 * Settings → Preferences (#47): how the application looks and behaves for
 * this producer, starting with the theme.
 *
 * The theme control lived in the top header, which spent permanent space on a
 * choice made roughly once. Configuration belongs in Settings; the header
 * keeps to where you are and what acts on the page.
 */
export function PreferencesPanel() {
  const {preference, setPreference} = useTheme()
  const [onboarding, setOnboarding] = useState(false)
  const [error, setError] = useState('')

  // Anything but "off" reads as on: unset is a fresh install, which the shell
  // is about to show the guide to anyway.
  const reload = useCallback(() => {
    void loadSetting(SETTING_KEYS.onboardingHelper).then((value) =>
      setOnboarding(value !== 'off'),
    )
  }, [])
  useEffect(reload, [reload])
  // The guide writes "off" when it is dismissed, which can happen with this
  // tab already open behind it.
  useDataChanged([SETTING_KEYS.onboardingHelper], reload)
  // Three states rather than two, because "follow the system" is the third
  // and the default — a producer whose machine switches at sunset wants that,
  // and a light/dark toggle would silently take it away.
  const options: {value: ThemePreference; label: string; body: string; icon: typeof Sun}[] = [
    {value: 'light', label: 'Light', body: 'Always the light theme.', icon: Sun},
    {value: 'dark', label: 'Dark', body: 'Always the dark theme.', icon: Moon},
    {
      value: 'system',
      label: 'System',
      body: 'Follow the operating system, switching when it does.',
      icon: Monitor,
    },
  ]

  return (
    <div className="space-y-5">
      <section className="sm-panel space-y-3 p-4">
        <h2 className="text-sm font-semibold">Theme</h2>
        <div className="grid gap-2 sm:grid-cols-3" role="radiogroup" aria-label="Theme">
          {options.map((option) => {
            const Icon = option.icon
            const on = preference === option.value
            return (
              <button
                key={option.value}
                className={
                  'flex flex-col items-start gap-1.5 rounded-lg border p-3 text-left ' +
                  (on
                    ? 'border-accent bg-surface-hover'
                    : 'border-edge hover:bg-surface-hover')
                }
                type="button"
                role="radio"
                aria-checked={on}
                onClick={() => setPreference(option.value)}
              >
                <span className="flex items-center gap-2 text-sm font-medium">
                  <Icon size={15} aria-hidden />
                  {option.label}
                </span>
                <span className="text-xs text-fg-muted">{option.body}</span>
              </button>
            )
          })}
        </div>
      </section>

      <section className="sm-panel space-y-2 p-4">
        <label className="flex items-start gap-2 text-sm">
          <input
            type="checkbox"
            className="mt-0.5"
            checked={onboarding}
            onChange={async (event) => {
              const next = event.target.checked
              setOnboarding(next)
              setError('')
              try {
                await SetSetting(SETTING_KEYS.onboardingHelper, next ? 'on' : 'off')
              } catch (err) {
                setOnboarding(!next)
                setError(toMessage(err))
              }
            }}
          />
          <span>
            Onboarding Helper
            <span className="block text-xs text-fg-muted">
              Show the setup guide the next time AIMusicMaker opens. It turns itself off
              again once it is completed or skipped.
            </span>
          </span>
        </label>
        {error && <p className="text-xs text-danger">{error}</p>}
      </section>
    </div>
  )
}
