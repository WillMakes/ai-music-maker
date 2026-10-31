import {useEffect, useRef, useState, type ReactNode} from 'react'
import {TextField} from '../components/Field'
import {PageHeader} from '../components/PageHeader'
import {SETTINGS_TABS, type ViewId} from '../navigation'
import {AIAssistants} from '../settings/AIAssistants'
import {AIServices} from '../settings/AIServices'
import {AboutPanel} from '../settings/AboutPanel'
import {BugsPanel} from '../settings/BugsPanel'
import {ExtensionPanel} from '../settings/ExtensionPanel'
import {LibraryPanel} from '../settings/LibraryPanel'
import {MCPServersPanel} from '../settings/MCPServersPanel'
import {PreferencesPanel} from '../settings/PreferencesPanel'
import {YouTubePanel} from '../settings/YouTubePanel'

/**
 * Every setting on one page (#116), with a navigation down the left that
 * searches them — the shape Site Maker Pro's settings have.
 *
 * The groups are still the old tabs' view ids, so a route that names one (the
 * status bar's links, the route an old bug report recorded) lands on its
 * group. Moving between groups here only scrolls: the content region is keyed
 * on the route, so a navigation per click would remount the page and throw
 * away the search and the scroll position it was meant to serve.
 */
export function Settings({
  view,
  onOpenService,
}: {
  view: ViewId
  onOpenService: (serviceId: string) => void
}) {
  const groups: Group[] = SETTINGS_TABS.map((tab) => ({
    ...tab,
    sections: sectionsOf(tab.id, onOpenService),
  }))

  const [query, setQuery] = useState('')
  const [hidden, setHidden] = useState<Set<string>>(new Set())
  const [active, setActive] = useState<string>(view)
  const contentRef = useRef<HTMLDivElement>(null)

  // Searched by what each section actually says rather than by a keyword list
  // kept beside it: a list would drift from the panels, and the producer is
  // searching for words they saw on the page. Re-run as the panels finish
  // loading, since a section that is still fetching has little to match.
  useEffect(() => {
    const root = contentRef.current
    if (!root) return
    const needle = query.trim().toLowerCase()
    const measure = () => {
      if (!needle) {
        setHidden((current) => (current.size ? new Set() : current))
        return
      }
      const next = new Set<string>()
      for (const group of groups) {
        const groupHit = group.label.toLowerCase().includes(needle)
        for (const section of group.sections) {
          const el = root.querySelector<HTMLElement>(`[data-section="${section.id}"]`)
          const text = (section.label + ' ' + (el?.textContent ?? '')).toLowerCase()
          if (!groupHit && !text.includes(needle)) next.add(section.id)
        }
      }
      setHidden((current) => (sameSet(current, next) ? current : next))
    }
    measure()
    if (!needle) return
    const observer = new MutationObserver(measure)
    observer.observe(root, {childList: true, subtree: true, characterData: true})
    return () => observer.disconnect()
    // groups is rebuilt every render, but only ever from SETTINGS_TABS; the
    // query is the one thing that changes the answer.
  }, [query])

  // Arriving on a group's route scrolls to it; the first group is the top.
  useEffect(() => {
    if (view === SETTINGS_TABS[0].id) return
    document.getElementById(anchor(view))?.scrollIntoView({block: 'start'})
  }, [view])

  // The navigation follows the scroll, so it says where the producer is.
  useEffect(() => {
    const root = contentRef.current
    if (!root) return
    const observer = new IntersectionObserver(
      (entries) => {
        const top = entries
          .filter((entry) => entry.isIntersecting)
          .sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top)[0]
        const id = top?.target.getAttribute('data-nav')
        if (id) setActive(id)
      },
      // A band near the top of the window: the section whose heading has
      // reached it is the one being read.
      {rootMargin: '0px 0px -70% 0px'},
    )
    root.querySelectorAll('[data-nav]').forEach((el) => observer.observe(el))
    return () => observer.disconnect()
  }, [])

  const visible = (group: Group) => group.sections.some((s) => !hidden.has(s.id))
  const scrollTo = (id: string) => {
    setActive(id)
    document.getElementById(anchor(id))?.scrollIntoView({block: 'start', behavior: 'smooth'})
  }
  const nothing = query.trim() !== '' && !groups.some(visible)

  return (
    <div className="space-y-5">
      <PageHeader title="Settings" />
      <div className="grid items-start gap-6 md:grid-cols-[220px_minmax(0,1fr)]">
        <nav className="space-y-3 md:sticky md:top-0" aria-label="Settings">
          <TextField
            label="Search settings"
            value={query}
            onChange={setQuery}
            placeholder="Theme, music folder, Claude…"
          />
          <ul className="space-y-0.5">
            {groups.filter(visible).map((group) => {
              const shown = group.sections.filter((s) => !hidden.has(s.id))
              const groupOn = active === group.id || shown.some((s) => s.id === active)
              return (
                <li key={group.id}>
                  <button
                    type="button"
                    className={
                      'w-full rounded-md px-3 py-1.5 text-left text-sm ' +
                      (groupOn
                        ? 'bg-active font-medium text-active-fg'
                        : 'text-fg hover:bg-surface-hover')
                    }
                    onClick={() => scrollTo(group.id)}
                  >
                    {group.label}
                  </button>
                  {/* Only a group of more than one section lists them; a
                      single child would repeat its group's name. */}
                  {shown.length > 1 && (
                    <ul className="my-1 ml-3 space-y-0.5 border-l border-edge pl-2">
                      {shown.map((section) => (
                        <li key={section.id}>
                          <button
                            type="button"
                            className={
                              'w-full rounded px-2 py-1 text-left text-xs ' +
                              (active === section.id
                                ? 'font-medium text-fg'
                                : 'text-fg-muted hover:text-fg')
                            }
                            onClick={() => scrollTo(section.id)}
                          >
                            {section.label}
                          </button>
                        </li>
                      ))}
                    </ul>
                  )}
                </li>
              )
            })}
          </ul>
        </nav>

        <div ref={contentRef} className="min-w-0 space-y-10">
          {nothing && <p className="text-sm text-fg-muted">No setting mentions that.</p>}
          {groups.map((group) => (
            // Hidden rather than unmounted, so a search does not reset a
            // panel's own state — or refetch it — each time it reappears.
            <section
              key={group.id}
              id={anchor(group.id)}
              data-nav={group.id}
              className="scroll-mt-4 space-y-4"
              hidden={!visible(group)}
            >
              <h2 className="text-xs font-semibold uppercase tracking-wider text-fg-muted">
                {group.label}
              </h2>
              {group.sections.map((section) => (
                <div
                  key={section.id}
                  id={anchor(section.id)}
                  data-section={section.id}
                  data-nav={group.sections.length > 1 ? section.id : undefined}
                  className="scroll-mt-4"
                  hidden={hidden.has(section.id)}
                >
                  {section.node}
                </div>
              ))}
            </section>
          ))}
        </div>
      </div>
    </div>
  )
}

interface Group {
  id: ViewId
  label: string
  sections: {id: string; label: string; node: ReactNode}[]
}

/**
 * What each group holds. Providers and assistants share a group (#25): both
 * answer "what AI can reach this app", and they are listed beneath it so each
 * can be gone to directly.
 *
 * A chain of `view === '…'` conditionals rather than a switch, because that is
 * the shape scripts/app-map.mjs reads as a route guard: describe_app then says
 * which panel a settings route lands on, instead of every route reaching every
 * panel on the page.
 */
function sectionsOf(view: ViewId, onOpenService: (serviceId: string) => void): Group['sections'] {
  return view === 'settings-ai'
    ? [
        {id: 'ai-providers', label: 'AI providers', node: <AIServices onOpen={onOpenService} />},
        {id: 'ai-assistants', label: 'AI assistants', node: <AIAssistants />},
      ]
    : view === 'settings-library'
      ? [{id: 'library', label: 'Library', node: <LibraryPanel />}]
      : view === 'settings-extension'
        ? [{id: 'extension', label: 'Chrome extension', node: <ExtensionPanel />}]
        : view === 'settings-mcp'
          ? [{id: 'mcp-servers', label: 'MCP servers', node: <MCPServersPanel />}]
          : view === 'settings-bugs'
          ? [{id: 'bugs', label: 'Bug reports', node: <BugsPanel />}]
          : view === 'settings-youtube'
            ? [{id: 'youtube', label: 'YouTube', node: <YouTubePanel />}]
            : view === 'settings-preferences'
              ? [{id: 'preferences', label: 'Preferences', node: <PreferencesPanel />}]
              : view === 'settings-about'
                ? [{id: 'about', label: 'About', node: <AboutPanel />}]
                : []
}

const anchor = (id: string) => 'settings-section-' + id

function sameSet(a: Set<string>, b: Set<string>) {
  if (a.size !== b.size) return false
  for (const item of a) if (!b.has(item)) return false
  return true
}
