import type {ReactNode} from 'react'

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
}: {
  title: string
  subtitle?: ReactNode
  actions?: ReactNode
  /** Where this page sits, for the details views that are three levels down. */
  breadcrumb?: ReactNode
}) {
  return (
    <header className="flex flex-wrap items-start gap-4">
      <div className="min-w-0 flex-1">
        {breadcrumb && <div className="mb-1 text-xs text-fg-muted">{breadcrumb}</div>}
        <h1 className="truncate text-xl font-semibold">{title}</h1>
        {subtitle && <div className="mt-1 text-sm text-fg-muted">{subtitle}</div>}
      </div>
      {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
    </header>
  )
}
