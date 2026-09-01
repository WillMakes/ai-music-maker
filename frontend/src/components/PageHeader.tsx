import {useState} from 'react'
import type {ReactNode} from 'react'
import {Pencil} from 'lucide-react'

/**
 * The top of a view: what it is, one line about it, and whatever acts on it.
 *
 * A component rather than a pattern each view repeats, so the gap between a
 * title and its actions is decided once. Every page in the app opens with one.
 */
export function PageHeader({
  title,
  subtitle,
  actions,
  breadcrumb,
  icon,
  backdrop,
  titleAccessory,
  onRename,
}: {
  title: string
  subtitle?: ReactNode
  actions?: ReactNode
  /** Where this page sits, for the details views that are three levels down. */
  breadcrumb?: ReactNode
  /** A leading tile for pages about one thing with a face (#43). */
  icon?: ReactNode
  /**
   * A decorative layer behind the header (#56) — the artist page's artwork
   * collage. The layer must keep itself faint enough to read over (see the
   * caller's opacity note); the header only positions and clips it.
   */
  backdrop?: ReactNode
  /** A small control right beside the title (#74) — the album's quick copy. */
  titleAccessory?: ReactNode
  /**
   * Makes the title itself renameable (#79). The detail pages have long had a
   * Name field in a panel further down, and the producer asked for renaming
   * anyway — the name is *displayed* here, so here is where a rename is looked
   * for. The pencil edits in place; committing hands the new name to the
   * caller, whose draft-and-debounce does the saving it already does.
   */
  onRename?: (name: string) => void
}) {
  return (
    <header
      className={
        'relative' + (backdrop ? ' overflow-hidden rounded-xl border border-edge p-4' : '')
      }
    >
      {backdrop && (
        <div className="absolute inset-0" aria-hidden="true">
          {backdrop}
        </div>
      )}
      <div className="relative flex flex-wrap items-start gap-4">
        {icon && (
          <span
            className="flex h-14 w-14 shrink-0 items-center justify-center rounded-lg bg-tile text-fg-muted"
            aria-hidden="true"
          >
            {icon}
          </span>
        )}
        <div className="min-w-0 flex-1">
          {breadcrumb && <div className="mb-1 text-xs text-fg-muted">{breadcrumb}</div>}
          <div className="flex min-w-0 items-center gap-1">
            {onRename ? (
              <RenameableTitle title={title} onRename={onRename} />
            ) : (
              <h1 className="truncate text-xl font-semibold">{title}</h1>
            )}
            {titleAccessory}
          </div>
          {subtitle && <div className="mt-1 text-sm text-fg-muted">{subtitle}</div>}
        </div>
        {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
      </div>
    </header>
  )
}

/**
 * The title with its pencil, or the input it turns into. Edit state lives
 * here, not in the caller — the caller only learns of a commit, so cancelling
 * (Escape, or blurring an unchanged or emptied field) costs nothing anywhere.
 */
function RenameableTitle({title, onRename}: {title: string; onRename: (name: string) => void}) {
  // null is "not editing" — the empty string must stay distinct from it, or
  // deleting the whole name while typing would slam the editor shut.
  const [value, setValue] = useState<string | null>(null)

  const commit = () => {
    const name = (value ?? '').trim()
    setValue(null)
    // An emptied name is a cancel, not a request: the stores refuse nameless
    // records, so committing it could only surface an error.
    if (name && name !== title) onRename(name)
  }

  if (value === null) {
    return (
      <>
        <h1 className="truncate text-xl font-semibold">{title}</h1>
        <button
          className="shrink-0 p-1 text-fg-muted hover:text-fg"
          type="button"
          title="Rename"
          aria-label="Rename"
          onClick={() => setValue(title)}
        >
          <Pencil size={14} />
        </button>
      </>
    )
  }
  return (
    <input
      className="w-full min-w-0 border-b border-edge bg-transparent text-xl font-semibold outline-none focus:border-focus"
      value={value}
      autoFocus
      onFocus={(event) => event.currentTarget.select()}
      onChange={(event) => setValue(event.target.value)}
      // Enter commits and the commit nulls the state, so the blur that
      // follows re-runs commit against null and no-ops — no double save.
      onBlur={commit}
      onKeyDown={(event) => {
        if (event.key === 'Enter') commit()
        if (event.key === 'Escape') setValue(null)
      }}
    />
  )
}
