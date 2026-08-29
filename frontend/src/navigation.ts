import {
  Disc3,
  Home,
  ListMusic,
  Music4,
  Settings as SettingsIcon,
  Sparkles,
  type LucideIcon,
} from 'lucide-react'

/**
 * Every place in the app.
 *
 * One list rather than a router's route table, because a desktop window has no
 * address bar and nothing to deep-link from — see App.tsx. What this file is for
 * instead is being the single place that knows a view exists: its title, which
 * sidebar section lights up for it, and (for the settings tabs) what it is
 * called on the strip. Adding a page means adding it here, and every consumer
 * picks it up.
 */
export type ViewId =
  | 'dashboard'
  | 'artists'
  | 'artist-new'
  | 'artist-details'
  | 'album-details'
  | 'track-details'
  | 'skills'
  | 'skill-details'
  | 'settings-ai'
  | 'settings-ai-service'
  | 'settings-assistants'
  | 'settings-skills'
  | 'settings-extension'
  | 'settings-bugs'
  | 'settings-about'

/** What the top bar calls each view. Also what a bug report records as `route`. */
export const ROUTE_TITLES: Record<ViewId, string> = {
  dashboard: 'Dashboard',
  artists: 'Artists',
  'artist-new': 'Create an artist',
  'artist-details': 'Artist',
  'album-details': 'Album',
  'track-details': 'Track',
  skills: 'Skills',
  'skill-details': 'Skill',
  'settings-ai': 'Settings — AI',
  'settings-ai-service': 'Settings — AI service',
  'settings-assistants': 'Settings — AI assistants',
  'settings-skills': 'Settings — Skills',
  'settings-extension': 'Settings — Chrome extension',
  'settings-bugs': 'Settings — Bug reports',
  'settings-about': 'Settings — About',
}

/**
 * Which sidebar item stays lit for a view.
 *
 * A details page is not its own sidebar entry — there is no "Artist" item to
 * click — but leaving the rail unlit while one is open reads as having navigated
 * out of the app. So every view maps to the section it belongs under.
 */
export const SECTION_OF: Record<ViewId, ViewId> = {
  dashboard: 'dashboard',
  artists: 'artists',
  'artist-new': 'artists',
  'artist-details': 'artists',
  'album-details': 'artists',
  'track-details': 'artists',
  skills: 'skills',
  'skill-details': 'skills',
  'settings-ai': 'settings-ai',
  'settings-ai-service': 'settings-ai',
  'settings-assistants': 'settings-ai',
  'settings-skills': 'settings-ai',
  'settings-extension': 'settings-ai',
  'settings-bugs': 'settings-ai',
  'settings-about': 'settings-ai',
}

export interface NavEntry {
  id: ViewId
  label: string
  icon: LucideIcon
}

/**
 * The work, at the top of the rail: the overview, and the library the whole app
 * is about.
 *
 * Deliberately short. Everything a producer opens this app to do is under
 * Artists — an album and a track are reached through the artist that owns them —
 * so a rail with six entries would be inventing places to go.
 */
export const PRIMARY_NAV: NavEntry[] = [
  {id: 'dashboard', label: 'Dashboard', icon: Home},
  {id: 'artists', label: 'Artists', icon: Music4},
]

/**
 * Pinned to the bottom of the rail, below a rule (#10).
 *
 * Neither of these is a place the work happens. Skills is read while doing one
 * of the things above rather than on the way in, and Settings is configuration.
 * Grouping them at the foot says that: the top of the rail is what you came for,
 * the bottom is what supports it — which is the shape Site Maker Pro's rail has,
 * and the reason this was reported.
 */
export const BOTTOM_NAV: NavEntry[] = [
  {id: 'skills', label: 'Skills', icon: Sparkles},
  {id: 'settings-ai', label: 'Settings', icon: SettingsIcon},
]

/** The settings tabs, in the order the strip shows them. */
export const SETTINGS_TABS: {id: ViewId; label: string}[] = [
  {id: 'settings-ai', label: 'AI'},
  {id: 'settings-assistants', label: 'AI assistants'},
  {id: 'settings-skills', label: 'Skills'},
  {id: 'settings-extension', label: 'Chrome extension'},
  {id: 'settings-bugs', label: 'Bug reports'},
  {id: 'settings-about', label: 'About'},
]

export const isSettingsView = (view: ViewId) => view.startsWith('settings-')

/**
 * A route as a trail of readable steps, for the report form's Where panel
 * (#20).
 *
 * The titles above already carry the whole trail — "Settings — Chrome
 * extension" is the section and the tab in one string — so this only has to
 * split them apart. Derived rather than stored, because a fact stored twice is
 * a fact that can disagree with itself.
 *
 * A route this file has never heard of comes back as itself: a report filed by
 * an older build after a rename is still a report, and its raw route is more
 * use than a blank.
 */
export function routeTrail(route: string): string[] {
  const title = ROUTE_TITLES[route as ViewId]
  if (!title) return route ? [route] : []
  return title.split(' — ')
}

/**
 * Icons for the record kinds, used wherever one is named without being the page
 * — a breadcrumb, an empty state, the dashboard's counts. Here rather than at
 * each site so an album is the same disc everywhere.
 */
export const KIND_ICONS = {
  artist: Music4,
  album: Disc3,
  track: ListMusic,
} as const
