import {ChevronLeft, ChevronRight} from 'lucide-react'
import {PRIMARY_NAV, SETTINGS_NAV, type ViewId} from '../navigation'

/**
 * The navigation rail.
 *
 * Collapsible, and the collapsed state is remembered, because this app's content
 * is wide — a lyric sheet beside a style prompt — and the rail is the first
 * thing somebody gives up to get room.
 *
 * A selected item is a filled chip with its icon in --color-nav-icon, which is a
 * different hue rather than a lighter accent: see the token's own note in
 * style.css for why lightness alone cannot do that job.
 */
export function Sidebar({
  current,
  collapsed,
  onNavigate,
  onToggle,
}: {
  /** The section that is lit, which for a details page is its parent. */
  current: ViewId
  collapsed: boolean
  onNavigate: (view: ViewId) => void
  onToggle: () => void
}) {
  const entries = [...PRIMARY_NAV, SETTINGS_NAV]
  return (
    <nav
      className={
        'flex shrink-0 flex-col border-r border-edge bg-surface transition-[width] duration-150 ' +
        (collapsed ? 'w-14' : 'w-56')
      }
      aria-label="Sections"
    >
      <div className="flex items-center gap-2 px-3 py-3">
        <div
          className="flex size-8 shrink-0 items-center justify-center rounded-md bg-accent font-semibold text-accent-fg"
          aria-hidden="true"
        >
          S
        </div>
        {!collapsed && <span className="truncate font-semibold">SunoMaker</span>}
      </div>

      <ul className="flex flex-1 flex-col gap-0.5 px-2 py-2">
        {entries.map((entry) => {
          const active = current === entry.id
          const Icon = entry.icon
          return (
            <li key={entry.id}>
              <button
                className={
                  'flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-sm ' +
                  (active
                    ? 'bg-active font-medium text-active-fg'
                    : 'text-fg hover:bg-surface-hover')
                }
                type="button"
                title={collapsed ? entry.label : undefined}
                aria-current={active ? 'page' : undefined}
                onClick={() => onNavigate(entry.id)}
              >
                <Icon
                  className={active ? 'text-nav-icon' : 'text-fg-muted'}
                  size={17}
                  aria-hidden="true"
                />
                {!collapsed && <span className="truncate">{entry.label}</span>}
              </button>
            </li>
          )
        })}
      </ul>

      <button
        className="flex items-center gap-2 border-t border-edge px-3 py-2.5 text-xs text-fg-muted hover:bg-surface-hover"
        type="button"
        onClick={onToggle}
      >
        {collapsed ? <ChevronRight size={15} /> : <ChevronLeft size={15} />}
        {!collapsed && <span>Collapse</span>}
      </button>
    </nav>
  )
}
