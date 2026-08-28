/**
 * What to show the producer when a call failed.
 *
 * Wails rejects a binding with whatever the Go side returned, which arrives as a
 * string far more often than as an Error. Rendering `String(err)` directly gives
 * "[object Object]" for the cases that are objects, which is the message nobody
 * can act on — so every branch is handled here, once, and every view calls this.
 */
export function toMessage(err: unknown): string {
  if (typeof err === 'string') return err
  if (err instanceof Error) return err.message
  if (err && typeof err === 'object' && 'message' in err) {
    const {message} = err as {message?: unknown}
    if (typeof message === 'string') return message
  }
  return 'Something went wrong.'
}
