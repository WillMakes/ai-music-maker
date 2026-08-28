import {ArrowLeft, ArrowRight, Bug, Monitor, Moon, Sun} from 'lucide-react'
import {useTheme, type ThemePreference} from '../theme/ThemeProvider'

/**
 * The bar above the content: where you are, where you have been, the theme, and
 * the bug button.
 *
 * Back and forward are here rather than in the rail because they are about the
 * history stack, which is what a title bar owns in every application that has
 * one. The rail is about sections.
 */
export function TopBar({
  title,
  canBack,
  canForward,
  openBugs,
  onBack,
  onForward,
  onReportBug,
}: {
  title: string
  canBack: boolean
  canForward: boolean
  /** How many reports are open, for the badge. */
  openBugs: number
  onBack: () => void
  onForward: () => void
  onReportBug: () => void
}) {
  return (
    <header className="flex shrink-0 items-center gap-2 border-b border-edge px-4 py-2.5">
      <div className="flex items-center gap-1">
        <IconButton label="Back" disabled={!canBack} onClick={onBack}>
          <ArrowLeft size={16} />
        </IconButton>
        <IconButton label="Forward" disabled={!canForward} onClick={onForward}>
          <ArrowRight size={16} />
        </IconButton>
      </div>

      <h1 className="min-w-0 flex-1 truncate text-sm font-medium">{title}</h1>

      <ThemeToggle />

      <button
        className="sm-button sm-button-quiet relative"
        type="button"
        onClick={onReportBug}
        title="Report a bug"
      >
        <Bug size={15} />
        <span>Report a bug</span>
        {openBugs > 0 && (
          <span
            className="ml-0.5 rounded-full bg-danger px-1.5 py-0.5 text-[11px] font-semibold leading-none text-bg"
            // The count is announced as what it is: a bare number beside a bug
            // icon is read as a version, an id, or nothing at all.
            aria-label={openBugs + ' open reports'}
          >
            {openBugs}
          </span>
        )}
      </button>
    </header>
  )
}

function IconButton({
  label,
  disabled,
  onClick,
  children,
}: {
  label: string
  disabled?: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      className="sm-button sm-button-quiet border-0 px-1.5 py-1.5 text-fg-muted disabled:opacity-35"
      type="button"
      title={label}
      aria-label={label}
      disabled={disabled}
      onClick={onClick}
    >
      {children}
    </button>
  )
}

/**
 * The three theme states as a segmented control rather than a toggle.
 *
 * A toggle can only express two, and "follow the system" is the third and the
 * default — a producer whose machine switches at sunset wants that, and a toggle
 * would silently take it away the first time they pressed it.
 */
function ThemeToggle() {
  const {preference, setPreference} = useTheme()
  const options: {value: ThemePreference; label: string; icon: typeof Sun}[] = [
    {value: 'light', label: 'Light', icon: Sun},
    {value: 'dark', label: 'Dark', icon: Moon},
    {value: 'system', label: 'System', icon: Monitor},
  ]
  return (
    <div
      className="flex items-center gap-0.5 rounded-md border border-edge p-0.5"
      role="group"
      aria-label="Theme"
    >
      {options.map((option) => {
        const Icon = option.icon
        const on = preference === option.value
        return (
          <button
            key={option.value}
            className={
              'rounded px-1.5 py-1 ' +
              (on ? 'bg-active text-active-fg' : 'text-fg-muted hover:bg-surface-hover')
            }
            type="button"
            title={option.label}
            aria-label={option.label}
            aria-pressed={on}
            onClick={() => setPreference(option.value)}
          >
            <Icon size={15} />
          </button>
        )
      })}
    </div>
  )
}
