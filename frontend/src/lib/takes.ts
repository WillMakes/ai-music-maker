import {app} from '../../wailsjs/go/models'

/**
 * Whether a track has a take that can actually play (#105). A take is filed by
 * absolute path, and one whose file was moved or deleted outside the app is
 * still listed — marked missing — so the producer can see what went. Offering
 * play on a track whose takes are all missing is a button that does nothing.
 */
export const hasPlayableTake = (track: app.Track) => (track.files ?? []).some((file) => !file.missing)

/** "3 takes", "3 takes · 1 missing", or "not made", for a track list row. */
export function takeSummary(track: app.Track): string {
  const files = track.files ?? []
  if (files.length === 0) return 'not made'
  const count = files.length + (files.length === 1 ? ' take' : ' takes')
  const missing = files.filter((file) => file.missing).length
  return missing ? `${count} · ${missing} missing` : count
}
