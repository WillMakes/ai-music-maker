import {useState} from 'react'
import {
  ChevronLeft,
  ChevronRight,
  Disc3,
  Music4,
  Plus,
  Settings as SettingsIcon,
  Sparkles,
} from 'lucide-react'
import logoMark from '../assets/logo-mark.png'
import type {Route} from '../App'
import {useLibrary} from '../library/LibraryProvider'
import {SECTION_OF} from './../navigation'

/**
 * The navigation rail as the library itself (#34).
 *
 * The redesign's central move: instead of a Dashboard/Artists pair that opens
 * lists, the rail *is* the list — every artist with their albums nested under
 * them, searchable, always one click from anywhere. The two pages that are not
 * records, Skills and Settings, keep their place at the foot below a rule.
 *
 * Collapsible as before, because the content is wide; collapsed, the tree
 * gives way to icons for the fixed destinations — a tree squeezed to 56px
 * would be a column of truncated mysteries.
 */
export function Sidebar({
  current,
  collapsed,
  onGo,
  onToggle,
}: {
  /** The whole route, so the row for the open record is the lit one. */
  current: Route
  collapsed: boolean
  onGo: (route: Route) => void
  onToggle: () => void
}) {
  const {tree} = useLibrary()
  const [query, setQuery] = useState('')

  // The filter keeps an artist whose own name matches (with all their albums),
  // and otherwise keeps only the matching albums under their artist — typing an
  // album's name should surface it, not hide it behind a non-matching artist.
  const needle = query.trim().toLowerCase()
  const filtered = needle
    ? tree
        .map((artist) => {
          if (artist.name.toLowerCase().includes(needle)) return artist
          const albums = artist.albums.filter((album) =>
            album.name.toLowerCase().includes(needle),
          )
          return albums.length > 0 ? {...artist, albums} : null
        })
        .filter((artist): artist is (typeof tree)[number] => artist !== null)
    : tree

  const section = SECTION_OF[current.view]

  return (
    <nav
      className={
        'flex shrink-0 flex-col border-r border-edge bg-surface transition-[width] duration-150 ' +
        (collapsed ? 'w-14' : 'w-60')
      }
      aria-label="Library"
    >
      <button
        className={
          'flex items-center gap-2 px-3 py-3 transition-opacity hover:opacity-80 ' +
          (collapsed ? 'justify-center' : '')
        }
        type="button"
        title="Home"
        aria-label="Go home"
        onClick={() => onGo({view: 'dashboard'})}
      >
        <img src={logoMark} alt="" className="size-8 shrink-0" aria-hidden="true" />
        {!collapsed && <span className="truncate font-semibold">SunoMaker</span>}
      </button>

      {!collapsed && (
        <>
          <div className="px-3 pb-2">
            <input
              className="sm-input w-full px-2.5 py-1.5 text-xs"
              value={query}
              placeholder="Find an artist or album…"
              aria-label="Filter the library"
              onChange={(event) => setQuery(event.target.value)}
            />
          </div>

          <div className="min-h-0 flex-1 overflow-y-auto px-2">
            <div className="flex items-center gap-2 px-2 pb-1.5 pt-1">
              <span className="flex-1 text-[11px] font-semibold uppercase tracking-wider text-fg-muted">
                Library
              </span>
              <button
                className="inline-flex items-center gap-1 rounded px-1 py-0.5 text-[11px] font-semibold text-accent hover:opacity-80"
                type="button"
                title="Create an artist"
                onClick={() => onGo({view: 'artist-new'})}
              >
                <Plus size={11} aria-hidden />
                Artist
              </button>
            </div>

            {filtered.length === 0 && (
              <p className="px-2 py-1 text-xs text-fg-muted">
                {needle ? 'Nothing matches.' : 'No artists yet.'}
              </p>
            )}

            {filtered.map((artist) => {
              const artistOpen =
                current.artistId === artist.id &&
                (current.view === 'artist-details' || current.view === 'album-details')
              return (
                <div key={artist.id} className="mb-0.5 flex flex-col gap-px">
                  <TreeRow
                    icon={<Music4 size={14} className="shrink-0" aria-hidden />}
                    label={artist.name}
                    badge={String(artist.albums.length)}
                    active={current.view === 'artist-details' && current.artistId === artist.id}
                    onClick={() => onGo({view: 'artist-details', artistId: artist.id})}
                  />
                  {/* Albums are open when their artist is anywhere on screen,
                      and always under a search hit — a tree that hides what
                      was just searched for has misunderstood the search. */}
                  {(artistOpen || needle !== '') &&
                    artist.albums.map((album) => (
                      <TreeRow
                        key={album.id}
                        indent
                        icon={<Disc3 size={13} className="shrink-0" aria-hidden />}
                        label={album.name}
                        badge={album.trackCount > 0 ? String(album.trackCount) : ''}
                        active={
                          current.view === 'album-details' && current.albumId === album.id
                        }
                        onClick={() => onGo({view: 'album-details', albumId: album.id})}
                      />
                    ))}
                </div>
              )
            })}
          </div>
        </>
      )}
      {collapsed && <div className="flex-1" />}

      <div className="flex flex-col gap-0.5 border-t border-edge p-2">
        <FootRow
          icon={<Sparkles size={14} aria-hidden />}
          label="Skills"
          collapsed={collapsed}
          active={section === 'skills'}
          onClick={() => onGo({view: 'skills'})}
        />
        <FootRow
          icon={<SettingsIcon size={14} aria-hidden />}
          label="Settings"
          collapsed={collapsed}
          active={section === 'settings-ai'}
          onClick={() => onGo({view: 'settings-ai'})}
        />
        <button
          className="mt-1 flex items-center gap-2 rounded px-2 py-1.5 text-xs text-fg-muted hover:bg-surface-hover hover:text-fg"
          type="button"
          onClick={onToggle}
          aria-label={collapsed ? 'Expand the sidebar' : 'Collapse the sidebar'}
        >
          {collapsed ? <ChevronRight size={14} /> : <ChevronLeft size={14} />}
          {!collapsed && 'Collapse'}
        </button>
      </div>
    </nav>
  )
}

function TreeRow({
  icon,
  label,
  badge,
  active,
  indent,
  onClick,
}: {
  icon: React.ReactNode
  label: string
  badge: string
  active: boolean
  indent?: boolean
  onClick: () => void
}) {
  return (
    <button
      className={
        'flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm transition-colors ' +
        (indent ? 'pl-7 ' : '') +
        (active
          ? 'bg-active text-active-fg'
          : 'text-fg hover:bg-surface-hover')
      }
      type="button"
      aria-current={active ? 'page' : undefined}
      onClick={onClick}
    >
      <span className={active ? 'text-nav-icon' : 'text-fg-muted'}>{icon}</span>
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {badge && (
        <span
          className={
            'rounded-full px-1.5 text-[10px] font-semibold ' +
            (active ? 'text-active-fg' : 'text-fg-muted')
          }
        >
          {badge}
        </span>
      )}
    </button>
  )
}

function FootRow({
  icon,
  label,
  collapsed,
  active,
  onClick,
}: {
  icon: React.ReactNode
  label: string
  collapsed: boolean
  active: boolean
  onClick: () => void
}) {
  return (
    <button
      className={
        'flex items-center gap-2 rounded px-2 py-1.5 text-sm transition-colors ' +
        (collapsed ? 'justify-center ' : '') +
        (active ? 'bg-active text-active-fg' : 'text-fg hover:bg-surface-hover')
      }
      type="button"
      title={label}
      aria-current={active ? 'page' : undefined}
      onClick={onClick}
    >
      <span className={active ? 'text-nav-icon' : 'text-fg-muted'}>{icon}</span>
      {!collapsed && label}
    </button>
  )
}
