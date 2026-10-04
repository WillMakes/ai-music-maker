import {useEffect, useRef, useState} from 'react'
import {Check, ListMusic, Plus} from 'lucide-react'
import {AddToPlaylist, SavePlaylist} from '../../wailsjs/go/app/App'
import {app} from '../../wailsjs/go/models'
import {TextField} from '../components/Field'
import {toMessage} from '../lib/errors'
import {usePlaylists} from './PlaylistsProvider'

/**
 * The button that puts one track into a playlist (#107), beside the track's
 * play and queue buttons — where a song is being listened to when the
 * producer decides it belongs somewhere.
 *
 * A menu of the playlists, with a name field at the foot that makes a new one
 * with this song already in it: having to leave the track to make a playlist,
 * and then find the track again to fill it, is the friction that stops a
 * playlist being made at all.
 */
export function AddToPlaylistMenu({
  trackId,
  align = 'left',
}: {
  trackId: string
  /** Which edge the menu hangs from: 'right' for a button at a row's end (#115). */
  align?: 'left' | 'right'
}) {
  const {playlists} = usePlaylists()
  const [open, setOpen] = useState(false)
  const [newName, setNewName] = useState('')
  const [added, setAdded] = useState('')
  const [error, setError] = useState('')
  const rootRef = useRef<HTMLDivElement>(null)

  // Closes on a press anywhere else, the way a menu does.
  useEffect(() => {
    if (!open) return
    const onDown = (event: MouseEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [open])

  const addTo = async (playlistId: string, name: string) => {
    setError('')
    try {
      await AddToPlaylist(playlistId, trackId)
      setAdded(name)
      setOpen(false)
      setTimeout(() => setAdded((shown) => (shown === name ? '' : shown)), 1800)
    } catch (err) {
      setError(toMessage(err))
    }
  }

  const create = async () => {
    if (!newName.trim()) return
    setError('')
    try {
      const made = await SavePlaylist(app.SavedPlaylist.createFrom({name: newName, entries: []}))
      setNewName('')
      await addTo(made.id, made.name)
    } catch (err) {
      setError(toMessage(err))
    }
  }

  return (
    <div ref={rootRef} className="relative">
      <button
        className="sm-button sm-button-quiet px-2"
        type="button"
        title={added ? 'Added to ' + added : 'Add to a playlist'}
        aria-label="Add this track to a playlist"
        aria-expanded={open}
        onClick={() => {
          setError('')
          setOpen((shown) => !shown)
        }}
      >
        {added ? <Check size={15} className="text-accent" /> : <ListMusic size={15} />}
      </button>
      {open && (
        <div
          className={
            'sm-panel absolute top-full z-20 mt-1 w-64 space-y-2 p-2 shadow-lg ' +
            (align === 'right' ? 'right-0' : 'left-0')
          }
        >
          <p className="px-1 text-[11px] font-semibold uppercase tracking-wider text-fg-muted">
            Add to a playlist
          </p>
          {playlists.length > 0 && (
            <ul className="max-h-56 overflow-y-auto">
              {playlists.map((playlist) => (
                <li key={playlist.id}>
                  <button
                    className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm hover:bg-surface-hover"
                    type="button"
                    onClick={() => void addTo(playlist.id, playlist.name)}
                  >
                    <ListMusic size={13} className="shrink-0 text-fg-muted" aria-hidden />
                    <span className="min-w-0 flex-1 truncate">{playlist.name}</span>
                    <span className="text-[10px] text-fg-muted">{playlist.trackCount}</span>
                  </button>
                </li>
              ))}
            </ul>
          )}
          <form
            className="flex items-end gap-2 border-t border-edge pt-2"
            onSubmit={(event) => {
              event.preventDefault()
              void create()
            }}
          >
            <div className="min-w-0 flex-1">
              <TextField
                label="New playlist"
                value={newName}
                placeholder="Late drive"
                onChange={setNewName}
              />
            </div>
            <button
              className="sm-button sm-button-primary px-2"
              type="submit"
              disabled={!newName.trim()}
              title="Make the playlist with this song in it"
              aria-label="Make the playlist with this song in it"
            >
              <Plus size={15} />
            </button>
          </form>
          {error && <p className="px-1 text-xs text-danger">{error}</p>}
        </div>
      )}
    </div>
  )
}
