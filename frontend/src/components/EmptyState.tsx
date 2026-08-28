import type {LucideIcon} from 'lucide-react'
import type {ReactNode} from 'react'

/**
 * What a list says when it has nothing in it.
 *
 * Always with the action that fills it. An empty state that only says "no
 * artists yet" leaves the producer to find the button themselves, which on a
 * first launch is the whole of their impression of the app.
 */
export function EmptyState({
  icon: Icon,
  title,
  body,
  action,
}: {
  icon: LucideIcon
  title: string
  body?: string
  action?: ReactNode
}) {
  return (
    <div className="sm-panel flex flex-col items-center gap-3 px-6 py-12 text-center">
      <Icon className="text-fg-muted" size={28} />
      <div>
        <p className="font-medium">{title}</p>
        {body && <p className="mx-auto mt-1 max-w-md text-sm text-fg-muted">{body}</p>}
      </div>
      {action}
    </div>
  )
}
