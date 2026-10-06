import {ArrowLeft, ArrowRight, Bug} from 'lucide-react'
import type {Route} from '../App'
import {UpdateButton} from '../updates/UpdateButton'

/**
 * The bar above the content: where you are, where you have been, a newer
 * release when there is one, and the bug button.
 *
 * Back and forward are here rather than in the rail because they are about the
 * history stack, which is what a title bar owns in every application that has
 * one. The rail is about sections.
 */
export function TopBar({
  crumbs,
  onCrumb,
  canBack,
  canForward,
  openBugs,
  onBack,
  onForward,
  onReportBug,
}: {
  /** The trail down to here (#34); the last step is the page itself. */
  crumbs: {label: string; route?: Route}[]
  onCrumb: (route: Route) => void
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

      {/* The trail rather than a lone title (#34): every step above the page
          is a way back to it, which back/forward alone cannot offer. */}
      <nav className="flex min-w-0 flex-1 items-center gap-1.5 truncate text-sm" aria-label="You are here">
        {crumbs.map((crumb, index) => (
          <span key={index + ':' + crumb.label} className="flex min-w-0 items-center gap-1.5">
            {index > 0 && (
              <span className="text-fg-muted" aria-hidden>
                ›
              </span>
            )}
            {crumb.route ? (
              <button
                className="truncate text-fg-muted hover:text-fg"
                type="button"
                onClick={() => onCrumb(crumb.route as Route)}
              >
                {crumb.label}
              </button>
            ) : (
              <span className="truncate font-medium" aria-current="page">
                {crumb.label}
              </span>
            )}
          </span>
        ))}
      </nav>

      <UpdateButton />

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

