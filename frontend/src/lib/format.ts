/**
 * Formatting shared by more than one view. Nothing here is clever; it is here so
 * that a duration reads the same on the track page as it does in a list.
 */

/** Seconds as m:ss — how a track length is written everywhere. */
export function duration(seconds: number): string {
  if (!seconds || seconds < 0) return '—'
  const minutes = Math.floor(seconds / 60)
  const rest = Math.round(seconds % 60)
  return `${minutes}:${String(rest).padStart(2, '0')}`
}

/**
 * m:ss or a bare number of seconds, back to seconds. Both accepted because a
 * producer types "3:30" as often as "210", and refusing one of them would be
 * this app deciding how somebody writes a length.
 */
export function parseDuration(value: string): number {
  const trimmed = value.trim()
  if (!trimmed) return 0
  const colon = trimmed.indexOf(':')
  if (colon < 0) return Math.max(0, Math.round(Number(trimmed) || 0))
  const minutes = Number(trimmed.slice(0, colon)) || 0
  const seconds = Number(trimmed.slice(colon + 1)) || 0
  return Math.max(0, Math.round(minutes * 60 + seconds))
}

/** Bytes as a short human size, for a track's file list. */
export function fileSize(bytes: number): string {
  if (!bytes) return '—'
  const units = ['B', 'KB', 'MB', 'GB']
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit += 1
  }
  return `${value < 10 && unit > 0 ? value.toFixed(1) : Math.round(value)} ${units[unit]}`
}

/**
 * An RFC3339 stamp as a date the producer's own locale writes.
 *
 * Undated rather than "Invalid Date" for anything unparseable: the stamps come
 * from this app's own writing, but a row written by an older build is not worth
 * a broken-looking date.
 */
export function when(stamp: string): string {
  if (!stamp) return ''
  const date = new Date(stamp)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  })
}

/**
 * How long ago a stamp was, in the shortest form that is still true: 45s, 12m,
 * 3h, 2d.
 *
 * For the status bar's "this report has been claimed for…" (#19), which is the
 * one readout there that goes stale while nothing happens — "2m" left at 2m for
 * half an hour reads as work that has stopped.
 *
 * Rounded down on purpose. Saying 4m at four minutes fifty is a claim that is
 * still true; saying 5m is one that is not yet.
 */
export function formatElapsed(stamp: string): string {
  if (!stamp) return ''
  const then = new Date(stamp).getTime()
  if (Number.isNaN(then)) return ''
  const seconds = Math.max(0, Math.floor((Date.now() - then) / 1000))
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h`
  return `${Math.floor(hours / 24)}d`
}
