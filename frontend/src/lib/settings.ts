import {GetSetting, SetSetting} from '../../wailsjs/go/app/App'

/**
 * The settings keys the frontend reads and writes.
 *
 * Mirrored from store.go — change both together. Named here rather than typed
 * as string literals at each call site so a renamed key is one edit and a typo
 * is a compile error rather than a setting that silently reads empty.
 */
export const SETTING_KEYS = {
  theme: 'theme',
  lastRoute: 'last_route',
  musicRoot: 'music_root',
  defaultTrackLength: 'default_track_length',
  musicVideoGap: 'music_video_gap',
} as const

/**
 * Reads a setting, answering null when there is no backend.
 *
 * There is no backend in a plain browser — `npm run dev` without `wails dev` —
 * and the whole frontend is written to survive that: a null here means "not
 * set", which every caller already has to handle.
 */
export async function loadSetting(key: string): Promise<string | null> {
  try {
    const value = await GetSetting(key)
    return value ?? null
  } catch {
    return null
  }
}

/** Writes a setting, silently doing nothing without a backend. */
export async function saveSetting(key: string, value: string): Promise<void> {
  try {
    await SetSetting(key, value)
  } catch {
    // See loadSetting.
  }
}
