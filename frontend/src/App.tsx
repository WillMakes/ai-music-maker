import {useCallback, useEffect, useRef, useState} from 'react'
import {Sidebar} from './components/Sidebar'
import {StatusBar} from './components/StatusBar'
import {TopBar} from './components/TopBar'
import {ReportBugModal} from './bugs/ReportBugModal'
import {useBugs} from './bugs/BugsProvider'
import {SETTING_KEYS, loadSetting, saveSetting} from './lib/settings'
import {
  ROUTE_TITLES,
  SECTION_OF,
  SETTINGS_TABS,
  isSettingsView,
  type ViewId,
} from './navigation'
import {AIServiceDetail} from './settings/AIServiceDetail'
import {AlbumDetails} from './views/AlbumDetails'
import {ArtistDetails} from './views/ArtistDetails'
import {Artists} from './views/Artists'
import {Dashboard} from './views/Dashboard'
import {NewArtist} from './views/NewArtist'
import {Settings} from './views/Settings'
import {SkillDetails} from './views/SkillDetails'
import {Skills} from './views/Skills'
import {TrackDetails} from './views/TrackDetails'

/**
 * A place in the app: the view, plus whatever that view needs to know which
 * record it is showing.
 *
 * Details pages carry an id rather than the record, so a route stays valid while
 * the record behind it is edited — and so the history stack holds no stale
 * copies of data that has since moved.
 */
interface Route {
  view: ViewId
  artistId?: string
  albumId?: string
  trackId?: string
  skillId?: string
  serviceId?: string
}

const HOME: Route = {view: 'dashboard'}

/**
 * The route written down as the last one open, read back — or null when there is
 * none, or what is there names no view this build has.
 *
 * Only the fields a Route carries are kept, and only when they are strings: the
 * setting is this app's own writing, but a build that renamed a field would
 * otherwise restore a route with an id under the wrong name.
 */
function readLastRoute(raw: string | null): Route | null {
  if (!raw) return null
  try {
    const parsed: unknown = JSON.parse(raw)
    if (!parsed || typeof parsed !== 'object') return null
    const candidate = parsed as Record<string, unknown>
    const view = candidate.view
    if (typeof view !== 'string' || !(view in ROUTE_TITLES)) return null
    const pick = (key: keyof Route) =>
      typeof candidate[key] === 'string' ? (candidate[key] as string) : undefined
    return {
      view: view as ViewId,
      artistId: pick('artistId'),
      albumId: pick('albumId'),
      trackId: pick('trackId'),
      skillId: pick('skillId'),
      serviceId: pick('serviceId'),
    }
  } catch {
    return null
  }
}

/**
 * The application shell: navigation rail, top bar, and the routed content
 * region.
 *
 * Routing is a history stack rather than a URL. A desktop window has no address
 * bar to type into and nothing to deep-link from, so the only thing a router
 * would add here is a dependency — but back and forward still have to work,
 * which an array and an index give.
 */
export default function App() {
  const {counts} = useBugs()

  // The stack and the position in it are one piece of state, not two. Split
  // across separate useStates, a push has to update both from the same starting
  // point — and the second updater would read the first one's stale closure, so
  // a fast double navigation could land on the wrong entry.
  const [nav, setNav] = useState<{stack: Route[]; index: number}>({
    stack: [HOME],
    index: 0,
  })
  const [collapsed, setCollapsed] = useState(false)
  const [reporting, setReporting] = useState(false)

  const current = nav.stack[nav.index]
  const canBack = nav.index > 0
  const canForward = nav.index < nav.stack.length - 1

  const go = useCallback((route: Route) => {
    setNav(({stack, index}) => {
      // Everything after the current position is dropped, which is what makes
      // this a history stack rather than a list: navigating from the middle of
      // one abandons the forward entries, the same way a browser does.
      const trimmed = stack.slice(0, index + 1)
      return {stack: [...trimmed, route], index: trimmed.length}
    })
  }, [])

  const back = useCallback(
    () => setNav((state) => ({...state, index: Math.max(0, state.index - 1)})),
    [],
  )
  const forward = useCallback(
    () =>
      setNav((state) => ({
        ...state,
        index: Math.min(state.stack.length - 1, state.index + 1),
      })),
    [],
  )

  // The collapsed rail, remembered.
  useEffect(() => {
    loadSetting(SETTING_KEYS.navCollapsed).then((value) => setCollapsed(value === 'true'))
  }, [])
  const toggleRail = useCallback(() => {
    setCollapsed((on) => {
      void saveSetting(SETTING_KEYS.navCollapsed, String(!on))
      return !on
    })
  }, [])

  // Where the app was when it was last closed, put back on opening.
  //
  // Two things gate it. The setting is read once, before anything is written, or
  // the dashboard this shell opens on would overwrite the route being restored.
  // And nothing is restored over a navigation already made: a producer who
  // clicked before the setting arrived has said where they want to be.
  const restored = useRef(false)
  useEffect(() => {
    if (restored.current) return
    loadSetting(SETTING_KEYS.lastRoute).then((raw) => {
      if (restored.current) return
      restored.current = true
      const route = readLastRoute(raw)
      if (!route || route.view === HOME.view) return
      // As a second entry above Home rather than in its place, so Back still has
      // somewhere to go.
      setNav((state) => (state.index === 0 ? {stack: [HOME, route], index: 1} : state))
    })
  }, [])

  useEffect(() => {
    // Only once the restore has run, or the dashboard's first render would be
    // written down as the route to come back to.
    if (!restored.current) return
    void saveSetting(SETTING_KEYS.lastRoute, JSON.stringify(current))
  }, [current])

  // What the bug button records: the view, and the record the page was showing.
  const subject = current.trackId
    ? {kind: 'track', id: current.trackId}
    : current.albumId
      ? {kind: 'album', id: current.albumId}
      : current.artistId
        ? {kind: 'artist', id: current.artistId}
        : {kind: '', id: ''}

  return (
    <div className="flex h-full">
      <Sidebar
        current={SECTION_OF[current.view]}
        collapsed={collapsed}
        onNavigate={(view) => go({view})}
        onToggle={toggleRail}
      />

      <div className="flex min-w-0 flex-1 flex-col">
        <TopBar
          title={ROUTE_TITLES[current.view]}
          canBack={canBack}
          canForward={canForward}
          openBugs={counts.open}
          onBack={back}
          onForward={forward}
          onReportBug={() => setReporting(true)}
        />

        <main className="min-h-0 flex-1 overflow-y-auto">
          <div
            // Keyed on the route so the fade replays on every navigation. Without
            // the key React reuses the element and the animation runs once, on
            // the first view the app happens to open.
            key={JSON.stringify(current)}
            className="content-fade-in mx-auto max-w-6xl px-6 py-6"
          >
            {current.view === 'dashboard' && (
              <Dashboard
                onNavigate={(view) => go({view})}
                onOpenArtist={(artistId) => go({view: 'artist-details', artistId})}
              />
            )}

            {current.view === 'artists' && (
              <Artists
                onOpen={(artistId) => go({view: 'artist-details', artistId})}
                onCreate={() => go({view: 'artist-new'})}
              />
            )}

            {current.view === 'artist-new' && (
              <NewArtist
                onCreated={(artistId) => go({view: 'artist-details', artistId})}
                onCancel={back}
              />
            )}

            {current.view === 'artist-details' && current.artistId && (
              <ArtistDetails
                artistId={current.artistId}
                onOpenAlbum={(albumId) =>
                  go({view: 'album-details', albumId, artistId: current.artistId})
                }
                onDeleted={() => go({view: 'artists'})}
              />
            )}

            {current.view === 'album-details' && current.albumId && (
              <AlbumDetails
                albumId={current.albumId}
                onOpenTrack={(trackId) =>
                  go({
                    view: 'track-details',
                    trackId,
                    albumId: current.albumId,
                    artistId: current.artistId,
                  })
                }
                onOpenArtist={(artistId) => go({view: 'artist-details', artistId})}
                onDeleted={(artistId) => go({view: 'artist-details', artistId})}
              />
            )}

            {current.view === 'track-details' && current.trackId && (
              <TrackDetails
                trackId={current.trackId}
                onOpenAlbum={(albumId) =>
                  go({view: 'album-details', albumId, artistId: current.artistId})
                }
                onDeleted={(albumId) => go({view: 'album-details', albumId})}
              />
            )}

            {current.view === 'skills' && (
              <Skills onOpen={(skillId) => go({view: 'skill-details', skillId})} />
            )}

            {current.view === 'skill-details' && current.skillId && (
              <SkillDetails skillId={current.skillId} onBack={() => go({view: 'skills'})} />
            )}

            {isSettingsView(current.view) && current.view !== 'settings-ai-service' && (
              <Settings
                view={current.view}
                onNavigate={(view) => go({view})}
                onOpenService={(serviceId) => go({view: 'settings-ai-service', serviceId})}
                onOpenSkill={(skillId) => go({view: 'skill-details', skillId})}
              />
            )}

            {current.view === 'settings-ai-service' && current.serviceId && (
              <ServiceDetailRoute
                serviceId={current.serviceId}
                onBack={() => go({view: SETTINGS_TABS[0].id})}
              />
            )}
          </div>
        </main>

        {/* Along the bottom of the content, not the window: the rail is its own
            column and already ends in its own collapse control, and a bar
            running under it would have to say something about a rail that has
            nothing to report. */}
        <StatusBar onNavigate={(view) => go({view})} />
      </div>

      {reporting && (
        <ReportBugModal
          route={current.view}
          subjectKind={subject.kind}
          subjectId={subject.id}
          onClose={() => setReporting(false)}
        />
      )}
    </div>
  )
}

/**
 * The AI service page, with its way back.
 *
 * Its own small component rather than inline, because it is the one settings
 * route that is not a tab and so needs a Back that the tab strip does not
 * provide.
 */
function ServiceDetailRoute({
  serviceId,
  onBack,
}: {
  serviceId: string
  onBack: () => void
}) {
  return (
    <div className="space-y-4">
      <button className="sm-button sm-button-quiet" type="button" onClick={onBack}>
        Back to AI
      </button>
      <AIServiceDetail serviceId={serviceId} />
    </div>
  )
}
