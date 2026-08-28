import {useEffect} from 'react'
import {EventsOn} from '../../wailsjs/runtime/runtime'

/**
 * Re-run `reload` when the backend reports a write to one of `scopes`.
 *
 * The backend emits "data:changed" with a scope after every write (see
 * Store.changed), which is how a page picks up something written behind its
 * back — by the MCP server, by the extension bridge, by a background sweep.
 * Without it, an agent adding a track through MCP would leave the open album
 * page showing yesterday's list until somebody navigated away and back.
 *
 * `scopes` is not in the dependency list on purpose: a caller passing an array
 * literal creates a new one every render, and depending on it would tear the
 * subscription down and build it up on each. The scopes a page cares about do
 * not change, so the first render's list is the right one.
 */
export function useDataChanged(scopes: string[], reload: () => void) {
  useEffect(() => {
    const wanted = new Set(scopes)
    let off: (() => void) | undefined
    try {
      off = EventsOn('data:changed', (scope: string) => {
        if (wanted.has(scope)) reload()
      })
    } catch {
      // No Wails runtime — a plain browser. Nothing ever fires, which is
      // exactly right: nothing is writing behind the page's back either.
    }
    return () => off?.()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reload])
}
