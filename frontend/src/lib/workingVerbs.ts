/**
 * What the interview says while it is thinking.
 *
 * A rotating verb rather than a spinner, because a turn can take thirty seconds
 * and a spinner that has been going that long reads as stuck. Mirrored into the
 * browser extension's content.js if that ever grows a waiting state — a content
 * script cannot import from this bundle.
 */
export const WORKING_VERBS = [
  'Thinking…',
  'Tuning the room…',
  'Auditioning takes…',
  'Reading the reference tracks…',
  'Finding the pocket…',
  'Rewriting the second verse…',
  'Adjusting the reverb…',
  'Arguing about the bridge…',
  'Listening back…',
  'Chasing the hook…',
  'Comping the vocal…',
  'Printing to tape…',
  'Riding the fader…',
  'Muting the hi-hats…',
  'Counting it in…',
  'Looking for the one…',
  'Rolling off the low end…',
  'Doubling the chorus…',
]

/** A verb that is not the one already up, so a change is always visible. */
export function otherVerb(current: string): string {
  const pool = WORKING_VERBS.filter((verb) => verb !== current)
  return pool[Math.floor(Math.random() * pool.length)] ?? WORKING_VERBS[0]
}

/**
 * How long each verb stays up. Sixty seconds: a verb is a line, not a ticker,
 * and a faster rotation reads as flicker.
 */
export const WORKING_VERB_MS = 60000
