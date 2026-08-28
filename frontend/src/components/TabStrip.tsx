/**
 * The strip of tabs a settings page sits under.
 *
 * The tabs are real navigations rather than local state: each one is its own
 * view id (see navigation.ts), so the route the app reopens on and the route a
 * bug report records are the tab that was actually open — not "Settings".
 */
export function TabStrip<T extends string>({
  tabs,
  current,
  onSelect,
}: {
  tabs: {id: T; label: string}[]
  current: T
  onSelect: (id: T) => void
}) {
  return (
    <div className="flex flex-wrap gap-1 border-b border-edge" role="tablist">
      {tabs.map((tab) => {
        const on = tab.id === current
        return (
          <button
            key={tab.id}
            className={
              '-mb-px border-b-2 px-3 py-2 text-sm ' +
              (on
                ? 'border-accent font-medium text-fg'
                : 'border-transparent text-fg-muted hover:text-fg')
            }
            type="button"
            role="tab"
            aria-selected={on}
            onClick={() => onSelect(tab.id)}
          >
            {tab.label}
          </button>
        )
      })}
    </div>
  )
}
