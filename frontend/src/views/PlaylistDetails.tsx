import {useCallback, useEffect, useState} from 'react'
import {Disc3, ListMusic, Play, Trash2, X} from 'lucide-react'
import {
  DeletePlaylist,
  GetPlaylist,
  RemoveFromPlaylist,
  SavePlaylist,
} from '../../wailsjs/go/app/App'
import {app} from '../../wailsjs/go/models'
import {EmptyState} from '../components/EmptyState'
import {PageHeader} from '../components/PageHeader'
import {useDataChanged} from '../lib/dataChanged'
import {toMessage} from '../lib/errors'
import {duration} from '../lib/format'
import {usePlayer} from '../player/PlayerProvider'

/**
 * One playlist (#107): its songs in the order they were added, each with the
 * record it comes from, played from the top or from any song in it.
 *
 * Songs come in from a track's own "Add to a playlist" menu on the album page —
 * that is where a song is being listened to when the producer decides it
 * belongs somewhere — so this page only orders the listening and lets a song
 * back out.
 */
export function PlaylistDetails({
  playlistId,
  onOpenAlbum,
  onDeleted,
}: {
  playlistId: string
  onOpenAlbum: (albumId: string, trackId: string) => void
  onDeleted: () => void
}) {
  const [playlist, setPlaylist] = useState<app.SavedPlaylist | null>(null)
  const [error, setError] = useState('')
  const [confirmDelete, setConfirmDelete] = useState(false)
  const player = usePlayer()

  const reload = useCallback(() => {
    GetPlaylist(playlistId)
      .then((found) => {
        setPlaylist(found)
        setError('')
      })
      .catch((err) => setError(toMessage(err)))
  }, [playlistId])

  useEffect(reload, [reload])
  // Tracks and albums too: a renamed song or a new cover shows through.
  useDataChanged(['playlists', 'tracks', 'albums', 'artists'], reload)

  if (!playlist) {
    return <p className="text-sm text-fg-muted">{error || 'Loading…'}</p>
  }

  const entries = playlist.entries ?? []
  const playable = entries.some((entry) => entry.playable)

  return (
    <div className="space-y-5">
      <PageHeader
        title={playlist.name}
        icon={
          <span className="flex size-full items-center justify-center text-fg-muted">
            <ListMusic size={28} aria-hidden />
          </span>
        }
        onRename={async (name) => {
          try {
            setPlaylist(await SavePlaylist(app.SavedPlaylist.createFrom({...playlist, name})))
            setError('')
          } catch (err) {
            setError(toMessage(err))
          }
        }}
        subtitle={entries.length === 1 ? '1 track' : entries.length + ' tracks'}
        actions={
          <>
            <button
              className="sm-button sm-button-quiet"
              type="button"
              disabled={!playable}
              onClick={() => void player.play('playlist', playlist.id)}
            >
              <Play size={15} />
              Play
            </button>
            <button
              className="sm-button sm-button-danger"
              type="button"
              onClick={() => setConfirmDelete(true)}
            >
              <Trash2 size={15} />
              Delete
            </button>
          </>
        }
      />

      {error && <p className="text-sm text-danger">{error}</p>}

      {confirmDelete && (
        <div className="sm-panel space-y-3 border-danger p-4">
          <p className="text-sm">
            Delete the playlist <strong>{playlist.name}</strong>? Its songs stay on their
            albums.
          </p>
          <div className="flex gap-2">
            <button
              className="sm-button sm-button-danger"
              type="button"
              onClick={async () => {
                try {
                  await DeletePlaylist(playlist.id)
                  onDeleted()
                } catch (err) {
                  setError(toMessage(err))
                }
              }}
            >
              Delete the playlist
            </button>
            <button
              className="sm-button sm-button-quiet"
              type="button"
              onClick={() => setConfirmDelete(false)}
            >
              Keep it
            </button>
          </div>
        </div>
      )}

      {entries.length === 0 ? (
        <EmptyState
          icon={ListMusic}
          title="No songs yet"
          body="Open an album, pick a track, and use “Add to a playlist” beside its play button."
        />
      ) : (
        <ul className="sm-panel divide-y divide-edge overflow-hidden">
          {entries.map((entry, i) => (
            <li key={entry.id} className="group flex items-center gap-3 px-3 py-2">
              {/* The position, or play from here on hover — the album list's
                  convention (#55), here starting the whole playlist at this
                  song rather than its record. */}
              <button
                className="flex w-6 shrink-0 items-center justify-center text-xs tabular-nums text-fg-muted disabled:cursor-default"
                type="button"
                disabled={!entry.playable}
                title={entry.playable ? 'Play from here' : 'No take of this song is on disk'}
                aria-label={'Play ' + entry.trackName}
                onClick={() => void player.play('playlist', playlist.id, entry.trackId)}
              >
                {entry.playable ? (
                  <>
                    <span className="group-hover:hidden">{i + 1}</span>
                    <Play size={13} className="hidden text-accent group-hover:block" />
                  </>
                ) : (
                  i + 1
                )}
              </button>
              {entry.artworkUrl ? (
                <img
                  src={entry.artworkUrl}
                  alt=""
                  className="size-8 shrink-0 rounded object-cover"
                  aria-hidden
                />
              ) : (
                <span className="flex size-8 shrink-0 items-center justify-center rounded bg-surface-hover text-fg-muted">
                  <Disc3 size={14} aria-hidden />
                </span>
              )}
              <button
                className="min-w-0 flex-1 text-left"
                type="button"
                title="Open this song on its album"
                onClick={() => onOpenAlbum(entry.albumId, entry.trackId)}
              >
                <span className="block truncate text-sm font-medium hover:underline">
                  {entry.trackName}
                </span>
                <span className="block truncate text-xs text-fg-muted">
                  {entry.artistName} — {entry.albumName}
                  {entry.playable ? '' : ' · no take on disk'}
                </span>
              </button>
              <span className="shrink-0 text-xs tabular-nums text-fg-muted">
                {duration(entry.targetSeconds)}
              </span>
              <button
                className="sm-button sm-button-quiet px-2 py-1 opacity-0 transition-opacity group-focus-within:opacity-100 group-hover:opacity-100"
                type="button"
                title="Take this song out of the playlist"
                aria-label={'Remove ' + entry.trackName + ' from the playlist'}
                onClick={async () => {
                  try {
                    setPlaylist(await RemoveFromPlaylist(playlist.id, entry.id))
                  } catch (err) {
                    setError(toMessage(err))
                  }
                }}
              >
                <X size={13} />
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
