import {createContext, useCallback, useContext, useEffect, useState, type ReactNode} from 'react'
import {LibraryTree} from '../../wailsjs/go/app/App'
import {app} from '../../wailsjs/go/models'
import {useDataChanged} from '../lib/dataChanged'

/**
 * The library tree the rail draws (#34): every artist with their albums, from
 * one call. A provider rather than the rail's own state because the crumbs in
 * the top bar and the Home view's resume cards read the same names — three
 * fetchers of the same tree would be three ways for it to disagree.
 */
const LibraryContext = createContext<{tree: app.LibraryArtist[]}>({tree: []})

export function LibraryProvider({children}: {children: ReactNode}) {
  const [tree, setTree] = useState<app.LibraryArtist[]>([])

  const reload = useCallback(() => {
    LibraryTree()
      // A rail that cannot load has nothing useful to say about why; the pages
      // it links to will say it better.
      .then((rows) => setTree(rows ?? []))
      .catch(() => {})
  }, [])

  useEffect(reload, [reload])
  useDataChanged(['artists', 'albums', 'tracks'], reload)

  return <LibraryContext.Provider value={{tree}}>{children}</LibraryContext.Provider>
}

export const useLibrary = () => useContext(LibraryContext)
