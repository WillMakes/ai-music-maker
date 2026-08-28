/**
 * A short state, read as a word rather than seen as an indicator — which is why
 * the colours here are held to the same contrast bar as body text (see the
 * status tokens in style.css) and why every pill carries its label.
 *
 * A colour with no word beside it would be this app asking somebody to remember
 * what green means.
 */
export function StatusPill({
  tone,
  children,
}: {
  tone: 'ok' | 'warn' | 'danger' | 'muted'
  children: string
}) {
  const tones = {
    ok: 'text-ok border-ok',
    warn: 'text-warn border-warn',
    danger: 'text-danger border-danger',
    muted: 'text-fg-muted border-edge',
  }
  return (
    <span
      className={
        'inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium ' +
        tones[tone]
      }
    >
      {children}
    </span>
  )
}
