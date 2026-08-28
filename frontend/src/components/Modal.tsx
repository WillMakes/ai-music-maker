import {useEffect, type ReactNode} from 'react'
import {X} from 'lucide-react'

/**
 * A dialog over the page.
 *
 * Escape closes it and the scrim is clickable, because both are what a person
 * expects and neither is what a "×" alone gives them. The listener is on
 * document in the capture phase so it fires before a field inside the modal can
 * swallow the key.
 *
 * Deliberately not a <dialog>: the native element's backdrop cannot be painted
 * from the token set (::backdrop does not inherit custom properties in every
 * engine this ships to), and the whole point of the palette is that nothing
 * paints itself.
 */
export function Modal({
  title,
  onClose,
  children,
  footer,
  wide,
}: {
  title: string
  onClose: () => void
  children: ReactNode
  footer?: ReactNode
  /** For the interview, which is a conversation rather than a form. */
  wide?: boolean
}) {
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.stopPropagation()
        onClose()
      }
    }
    document.addEventListener('keydown', onKey, true)
    return () => document.removeEventListener('keydown', onKey, true)
  }, [onClose])

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-scrim p-6"
      onClick={onClose}
      role="presentation"
    >
      <div
        className={
          'sm-panel flex max-h-full w-full flex-col overflow-hidden bg-bg shadow-2xl ' +
          (wide ? 'max-w-3xl' : 'max-w-lg')
        }
        role="dialog"
        aria-modal="true"
        aria-label={title}
        // The scrim closes on click; without this the same click passes through
        // the panel and closes the modal the producer is typing in.
        onClick={(event) => event.stopPropagation()}
      >
        <header className="flex items-center gap-3 border-b border-edge px-5 py-3">
          <h2 className="flex-1 text-base font-semibold">{title}</h2>
          <button
            className="sm-button sm-button-quiet border-0 px-1.5 py-1"
            type="button"
            onClick={onClose}
            aria-label="Close"
          >
            <X size={16} />
          </button>
        </header>
        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">{children}</div>
        {footer && (
          <footer className="flex items-center justify-end gap-2 border-t border-edge px-5 py-3">
            {footer}
          </footer>
        )}
      </div>
    </div>
  )
}
