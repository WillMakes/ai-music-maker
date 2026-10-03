import {useState} from 'react'
import {
  Disc3,
  ListMusic,
  Music4,
  Plus,
  Settings as SettingsIcon,
  Sparkles,
} from 'lucide-react'
import logoMark from '../assets/logo-mark.png'
import type {Route} from '../App'
import {useLibrary} from '../library/LibraryProvider'
import {usePlaylists} from '../playlists/PlaylistsProvider'
import {SavePlaylist} from '../../wailsjs/go/app/App'
import {app} from '../../wailsjs/go/models'
import {toMessage} from '../lib/errors'
import {SECTION_OF} from './../navigation'

/**
 * The navigation rail as the library itself (#34).
 *
 * The redesign's central move: instead of a Dashboard/Artists pair that opens
 * lists, the rail *is* the list — every artist with their albums nested under
 * them, searchable, always one click from anywhere. The two pages that are not
 * records, Skills and Settings, keep their place at the foot below a rule.
 *
 * Two lists since #107: the artists, and the playlists under them. Each scrolls
 * on its own, the playlists taking only the room they need up to two fifths of
 * the rail, so a producer with a hundred artists can still reach a playlist
 * and one with a hundred playlists still sees the artists.
 *
 * Not collapsible (#64): the rail earned its width by being the library, and
 * a tree squeezed to 56px was a column of truncated mysteries nobody used —
 * the option cost a persistent setting and a foot-row to answer a question no
 * one was asking.
 */
export function Sidebar({
  current,
  onGo,
}: {
  /** The whole route, so the row for the open record is the lit one. */
  current: Route
  onGo: (route: Route) => void
}) {
  const {tree} = useLibrary()
  const {playlists} = usePlaylists()
  const [query, setQuery] = useState('')
  const [error, setError] = useState('')

  // The filter keeps an artist whose own name matches (with all their albums),
  // and otherwise keeps only the matching albums under their artist — typing an
  // album's name should surface it, not hide it behind a non-matching artist.
  const needle = query.trim().toLowerCase()
  const filtered = needle
    ? tree
        .map((artist) => {
          if (artist.name.toLowerCase().includes(needle)) return artist
          const albums = artist.albums.filter((album) =>
            album.name.toLowerCase().includes(needle),
          )
          return albums.length > 0 ? {...artist, albums} : null
        })
        .filter((artist): artist is (typeof tree)[number] => artist !== null)
    : tree
  const shownPlaylists = needle
    ? playlists.filter((playlist) => playlist.name.toLowerCase().includes(needle))
    : playlists

  // A new playlist opens straight away under a placeholder name, renamed in
  // place on its page — the same pencil every record's title has (#79).
  const newPlaylist = async () => {
    setError('')
    try {
      const made = await SavePlaylist(
        app.SavedPlaylist.createFrom({name: 'New playlist', entries: []}),
      )
      onGo({view: 'playlist-details', playlistId: made.id})
    } catch (err) {
      setError(toMessage(err))
    }
  }

  const section = SECTION_OF[current.view]

  return (
    <nav
      className="flex w-60 shrink-0 flex-col border-r border-edge bg-surface"
      aria-label="Library"
    >
      <button
        className="flex items-center gap-2 px-3 py-3 transition-opacity hover:opacity-80"
        type="button"
        title="Home"
        aria-label="Go home"
        onClick={() => onGo({view: 'dashboard'})}
      >
        <img src={logoMark} alt="" className="size-8 shrink-0" aria-hidden="true" />
        <span className="truncate font-semibold">AIMusicMaker</span>
      </button>

      <div className="px-3 pb-2">
        <input
          className="sm-input w-full px-2.5 py-1.5 text-xs"
          value={query}
          placeholder="Find an artist, album or playlist…"
          aria-label="Filter the library"
          onChange={(event) => setQuery(event.target.value)}
        />
      </div>

      <section className="flex min-h-0 flex-1 flex-col" aria-label="Artists">
        <div className="flex items-center gap-2 px-4 pb-1.5 pt-1">
          <span className="flex-1 text-[11px] font-semibold uppercase tracking-wider text-fg-muted">
            Artists
          </span>
          <button
            className="inline-flex items-center gap-1 rounded px-1 py-0.5 text-[11px] font-semibold text-accent hover:opacity-80"
            type="button"
            title="Create an artist"
            onClick={() => onGo({view: 'artist-new'})}
          >
            <Plus size={11} aria-hidden />
            Artist
          </button>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto px-2">
          {filtered.length === 0 && (
            <p className="px-2 py-1 text-xs text-fg-muted">
              {needle ? 'No artist matches.' : 'No artists yet.'}
            </p>
          )}

          {filtered.map((artist) => {
            // Matched by the album ids as well as the route's artistId: an
            // album route restored from an old build or opened off a bug
            // report may not carry the artist, and the branch collapsing on
            // the very album it shows is the bug (#41).
            const artistOpen =
              (current.artistId === artist.id &&
                (current.view === 'artist-details' || current.view === 'album-details')) ||
              (current.view === 'album-details' &&
                artist.albums.some((album) => album.id === current.albumId))
            return (
              <div key={artist.id} className="mb-0.5 flex flex-col gap-px">
                <TreeRow
                  icon={<Music4 size={14} className="shrink-0" aria-hidden />}
                  label={artist.name}
                  badge={String(artist.albums.length)}
                  active={current.view === 'artist-details' && current.artistId === artist.id}
                  onClick={() => onGo({view: 'artist-details', artistId: artist.id})}
                />
                {/* Albums are open when their artist is anywhere on screen,
                    and always under a search hit — a tree that hides what
                    was just searched for has misunderstood the search. */}
                {(artistOpen || needle !== '') &&
                  artist.albums.map((album) => (
                    <TreeRow
                      key={album.id}
                      indent
                      // The cover as the row's icon (#81): the rail is the
                      // library, and a library is browsed by its spines. The
                      // disc glyph stays for a record with no artwork yet.
                      icon={
                        album.artworkUrl ? (
                          <img
                            src={album.artworkUrl}
                            alt=""
                            className="size-5 shrink-0 rounded-sm object-cover"
                            aria-hidden
                          />
                        ) : (
                          <Disc3 size={13} className="shrink-0" aria-hidden />
                        )
                      }
                      label={album.name}
                      badge={album.trackCount > 0 ? String(album.trackCount) : ''}
                      active={
                        current.view === 'album-details' && current.albumId === album.id
                      }
                      onClick={() =>
                        onGo({view: 'album-details', albumId: album.id, artistId: artist.id})
                      }
                    />
                  ))}
              </div>
            )
          })}
        </div>
      </section>

      <section
        className="flex max-h-[40%] min-h-0 shrink-0 flex-col border-t border-edge pt-1"
        aria-label="Playlists"
      >
        <div className="flex items-center gap-2 px-4 pb-1.5 pt-1">
          <span className="flex-1 text-[11px] font-semibold uppercase tracking-wider text-fg-muted">
            Playlists
          </span>
          <button
            className="inline-flex items-center gap-1 rounded px-1 py-0.5 text-[11px] font-semibold text-accent hover:opacity-80"
            type="button"
            title="Create a playlist"
            onClick={() => void newPlaylist()}
          >
            <Plus size={11} aria-hidden />
            Playlist
          </button>
        </div>
        <div className="min-h-0 overflow-y-auto px-2 pb-2">
          {error && <p className="px-2 py-1 text-xs text-danger">{error}</p>}
          {shownPlaylists.length === 0 && (
            <p className="px-2 py-1 text-xs text-fg-muted">
              {needle ? 'No playlist matches.' : 'No playlists yet.'}
            </p>
          )}
          {shownPlaylists.map((playlist) => (
            <TreeRow
              key={playlist.id}
              icon={<ListMusic size={14} className="shrink-0" aria-hidden />}
              label={playlist.name}
              badge={playlist.trackCount > 0 ? String(playlist.trackCount) : ''}
              active={
                current.view === 'playlist-details' && current.playlistId === playlist.id
              }
              onClick={() => onGo({view: 'playlist-details', playlistId: playlist.id})}
            />
          ))}
        </div>
      </section>
      <div className="flex flex-col gap-0.5 border-t border-edge p-2">
        <FootRow
          icon={<Sparkles size={14} aria-hidden />}
          label="Skills"
          active={section === 'skills'}
          onClick={() => onGo({view: 'skills'})}
        />
        <FootRow
          icon={<SettingsIcon size={14} aria-hidden />}
          label="Settings"
          active={section === 'settings-ai'}
          onClick={() => onGo({view: 'settings-ai'})}
        />
      </div>
    </nav>
  )
}

function TreeRow({
  icon,
  label,
  badge,
  active,
  indent,
  onClick,
}: {
  icon: React.ReactNode
  label: string
  badge: string
  active: boolean
  indent?: boolean
  onClick: () => void
}) {
  return (
    <button
      className={
        'flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm transition-colors ' +
        (indent ? 'pl-7 ' : '') +
        (active
          ? 'bg-active text-active-fg'
          : 'text-fg hover:bg-surface-hover')
      }
      type="button"
      aria-current={active ? 'page' : undefined}
      onClick={onClick}
    >
      <span className={active ? 'text-nav-icon' : 'text-fg-muted'}>{icon}</span>
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {badge && (
        <span
          className={
            'rounded-full px-1.5 text-[10px] font-semibold ' +
            (active ? 'text-active-fg' : 'text-fg-muted')
          }
        >
          {badge}
        </span>
      )}
    </button>
  )
}

function FootRow({
  icon,
  label,
  active,
  onClick,
}: {
  icon: React.ReactNode
  label: string
  active: boolean
  onClick: () => void
}) {
  return (
    <button
      className={
        'flex items-center gap-2 rounded px-2 py-1.5 text-sm transition-colors ' +
        (active ? 'bg-active text-active-fg' : 'text-fg hover:bg-surface-hover')
      }
      type="button"
      title={label}
      aria-current={active ? 'page' : undefined}
      onClick={onClick}
    >
      <span className={active ? 'text-nav-icon' : 'text-fg-muted'}>{icon}</span>
      {label}
    </button>
  )
}
