import {createContext, useCallback, useContext, useEffect, useState, type ReactNode} from 'react'
import {ListPlaylists} from '../../wailsjs/go/app/App'
import {app} from '../../wailsjs/go/models'
import {useDataChanged} from '../lib/dataChanged'

/**
 * Every playlist, by name (#107). A provider for the library tree's reason: the
 * rail lists them, the top bar's trail names the open one, and every track's
 * "Add to a playlist" menu offers them — three fetchers would be three ways for
 * one list to disagree.
 */
const PlaylistsContext = createContext<{playlists: app.SavedPlaylist[]}>({playlists: []})

export function PlaylistsProvider({children}: {children: ReactNode}) {
  const [playlists, setPlaylists] = useState<app.SavedPlaylist[]>([])

  const reload = useCallback(() => {
    ListPlaylists()
      // As with the library tree: the pages the rail links to say why far
      // better than an empty rail could.
      .then((rows) => setPlaylists(rows ?? []))
      .catch(() => {})
  }, [])

  useEffect(reload, [reload])
  // Tracks too: deleting a song changes the counts beside every playlist
  // holding it.
  useDataChanged(['playlists', 'tracks', 'albums', 'artists'], reload)

  return <PlaylistsContext.Provider value={{playlists}}>{children}</PlaylistsContext.Provider>
}

export const usePlaylists = () => useContext(PlaylistsContext)
