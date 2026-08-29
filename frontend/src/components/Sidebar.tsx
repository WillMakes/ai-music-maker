import {ChevronLeft, ChevronRight} from 'lucide-react'
import logoMark from '../assets/logo-mark.png'
import {BOTTOM_NAV, PRIMARY_NAV, type NavEntry, type ViewId} from '../navigation'

/**
 * The navigation rail.
 *
 * Two groups (#10). The work is at the top — the overview and the library —
 * and Skills and Settings are pinned to the foot below a rule, because neither
 * is a place the work happens: one is read while doing something else, the other
 * is configuration. It is the shape Site Maker Pro's rail has, and grouping them
 * that way is what stops the rail reading as five equal destinations.
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
  return (
    <nav
      className={
        'flex shrink-0 flex-col border-r border-edge bg-surface transition-[width] duration-150 ' +
        (collapsed ? 'w-14' : 'w-56')
      }
      aria-label="Sections"
    >
      {/* The lockup is the way back to the dashboard, the way a site's logo is
          home on the web. A button rather than a div, so it carries the
          affordance to the keyboard and the accessibility tree too. */}
      <button
        className={
          'flex items-center gap-2 px-3 py-3 transition-opacity hover:opacity-80 ' +
          (collapsed ? 'justify-center' : '')
        }
        type="button"
        title="Dashboard"
        aria-label="Go to the dashboard"
        onClick={() => onNavigate('dashboard')}
      >
        {/* The mascot rather than a lettered tile (#21): the mark carries its
            own dark glow, so it sits on the rail directly with no background
            of its own — a coloured tile behind it would fight the artwork. */}
        <img
          src={logoMark}
          alt=""
          className="size-8 shrink-0"
          aria-hidden="true"
        />
        {!collapsed && <span className="truncate font-semibold">SunoMaker</span>}
      </button>

      {/* flex-1 is what pushes the bottom group to the foot of the rail rather
          than leaving it under the last primary item. */}
      <ul className="flex flex-1 flex-col gap-0.5 px-2 py-2">
        {PRIMARY_NAV.map((entry) => (
          <Item
            key={entry.id}
            entry={entry}
            active={current === entry.id}
            collapsed={collapsed}
            onSelect={onNavigate}
          />
        ))}
      </ul>

      <ul className="flex flex-col gap-0.5 border-t border-edge px-2 py-2">
        {BOTTOM_NAV.map((entry) => (
          <Item
            key={entry.id}
            entry={entry}
            active={current === entry.id}
            collapsed={collapsed}
            onSelect={onNavigate}
          />
        ))}
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

/**
 * One rail entry.
 *
 * Its own component so the two groups cannot drift apart: a highlight or a
 * padding changed in one list and not the other is exactly the kind of
 * difference nobody notices until the rail looks wrong.
 */
function Item({
  entry,
  active,
  collapsed,
  onSelect,
}: {
  entry: NavEntry
  active: boolean
  collapsed: boolean
  onSelect: (view: ViewId) => void
}) {
  const Icon = entry.icon
  return (
    <li>
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
        onClick={() => onSelect(entry.id)}
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
}
