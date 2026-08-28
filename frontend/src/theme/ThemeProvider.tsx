import {createContext, useCallback, useContext, useEffect, useState, type ReactNode} from 'react'
import {SETTING_KEYS, loadSetting, saveSetting} from '../lib/settings'

/**
 * The three states the theme can be in. "system" is not a colour — it is the
 * absence of a choice, and it follows the OS for as long as it is set.
 */
export type ThemePreference = 'system' | 'light' | 'dark'

/**
 * The localStorage mirror index.html reads before first paint.
 *
 * The real store is SQLite and reading it is async, which cannot happen before
 * the page paints — so a dark-theme user would see a white flash on every
 * launch. This key is written alongside every save purely so the inline script
 * in index.html has something synchronous to read. SQLite stays the source of
 * truth; this is a cache with one reader.
 */
const MIRROR_KEY = 'sm:theme'

interface ThemeValue {
  preference: ThemePreference
  /** What the preference resolves to right now, which is what is on screen. */
  dark: boolean
  setPreference: (next: ThemePreference) => void
}

const ThemeContext = createContext<ThemeValue>({
  preference: 'system',
  dark: false,
  setPreference: () => {},
})

const readMirror = (): ThemePreference => {
  try {
    const stored = localStorage.getItem(MIRROR_KEY)
    return stored === 'light' || stored === 'dark' ? stored : 'system'
  } catch {
    // A webview with storage disabled. "system" is the right answer and the
    // absence of a mirror costs one frame of the wrong theme, not a failure.
    return 'system'
  }
}

const systemPrefersDark = () =>
  typeof window !== 'undefined' &&
  window.matchMedia('(prefers-color-scheme: dark)').matches

export function ThemeProvider({children}: {children: ReactNode}) {
  // Opens on the mirror rather than on a default, so the first render matches
  // what index.html already painted.
  const [preference, setStored] = useState<ThemePreference>(readMirror)
  const [systemDark, setSystemDark] = useState(systemPrefersDark)

  // The stored preference, read once. It overrides the mirror if they disagree,
  // which happens when the app is opened on a second machine sharing the
  // database — the mirror is per-browser-profile, the setting is the record.
  useEffect(() => {
    loadSetting(SETTING_KEYS.theme).then((value) => {
      if (value === 'light' || value === 'dark' || value === 'system') {
        setStored(value)
      }
    })
  }, [])

  // The OS theme, watched for as long as the preference is "system". Watched
  // unconditionally rather than only in system mode: the listener is free, and
  // conditional subscription would mean re-subscribing on every change of
  // preference for no gain.
  useEffect(() => {
    const query = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = () => setSystemDark(query.matches)
    query.addEventListener('change', onChange)
    return () => query.removeEventListener('change', onChange)
  }, [])

  const dark = preference === 'dark' || (preference === 'system' && systemDark)

  // colorScheme as well as the class: it is what tells the webview to draw its
  // own scrollbars and form controls dark. Without it the page is dark and the
  // scrollbar beside it is white.
  useEffect(() => {
    const root = document.documentElement
    root.classList.toggle('dark', dark)
    root.style.colorScheme = dark ? 'dark' : 'light'
  }, [dark])

  const setPreference = useCallback((next: ThemePreference) => {
    setStored(next)
    try {
      localStorage.setItem(MIRROR_KEY, next)
    } catch {
      // See readMirror.
    }
    void saveSetting(SETTING_KEYS.theme, next)
  }, [])

  return (
    <ThemeContext.Provider value={{preference, dark, setPreference}}>
      {children}
    </ThemeContext.Provider>
  )
}

export const useTheme = () => useContext(ThemeContext)
