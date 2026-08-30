import {Monitor, Moon, Sun} from 'lucide-react'
import {useTheme, type ThemePreference} from '../theme/ThemeProvider'

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
    </div>
  )
}
