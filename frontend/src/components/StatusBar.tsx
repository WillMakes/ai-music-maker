import {useCallback, useEffect, useRef, useState, type ReactNode} from 'react'
import {
  AlertTriangle,
  Clapperboard,
  Bug,
  Chrome,
  Image as ImageIcon,
  Loader2,
  Music4,
  Plug,
  Sparkles,
} from 'lucide-react'
import {
  GetAIServices,
  GetBuildInfo,
  GetExtensionStatus,
  GetMCPStatus,
} from '../../wailsjs/go/app/App'
import {app} from '../../wailsjs/go/models'
import {useArtists} from '../artists/ArtistsProvider'
import {useArtworks} from '../artworks/ArtworksProvider'
import {useVideos} from '../videos/VideosProvider'
import {useEvolutions} from '../evolutions/EvolutionsProvider'
import {summarise, useBugs} from '../bugs/BugsProvider'
import {useDataChanged} from '../lib/dataChanged'
import {formatElapsed} from '../lib/format'
import type {ViewId} from '../navigation'

/**
 * The bar along the bottom of the shell (#19).
 *
 * Everything on it is something the app already knew and had nowhere to say:
 * whether an AI provider is connected, whether the assistants can reach this
 * app's tools, whether the browser extension has been exported, whether the
 * database opened at all. The last of those matters most — a store that failed
 * to open makes every write fail silently, and it was admitted on one Settings
 * tab and nowhere else.
 *
 * Each segment is a way in rather than an ornament: clicking one goes to the page
 * that owns it. The always-on readouts are icon-only, because four labels would
 * be most of the bar saying the same thing from launch to quit; the transient
 * ones are labelled, because there the words are the point.
 */
export function StatusBar({
  onNavigate,
  onOpenAlbum,
}: {
  onNavigate: (view: ViewId) => void
  /** For the evolution segments (#82), whose destination is a record. */
  onOpenAlbum: (albumId: string, artistId: string) => void
}) {
  const {artists} = useArtists()
  const {reports} = useBugs()
  const {evolutions, dismiss} = useEvolutions()
  const {artworks, dismiss: dismissArtwork} = useArtworks()
  const {videos, dismiss: dismissVideo} = useVideos()

  const [info, setInfo] = useState<app.BuildInfo | null>(null)
  const [services, setServices] = useState<app.AIServiceStatus[]>([])
  const [mcp, setMcp] = useState<app.MCPStatus | null>(null)
  const [extension, setExtension] = useState<app.ExtensionStatus | null>(null)

  // Read once: whether the database opened is decided before the window exists
  // and cannot change while it is up.
  useEffect(() => {
    GetBuildInfo()
      .then(setInfo)
      .catch(() => setInfo(null))
  }, [])

  // The three that a Settings page can change under the bar. Re-read on the
  // ai_services scope: connecting a provider is the moment somebody is most
  // likely to have just registered an assistant too, and without this the bar
  // would go on saying "no AI provider" until the app was restarted.
  const loadConnections = useCallback(() => {
    GetAIServices()
      .then((rows) => setServices(rows ?? []))
      .catch(() => setServices([]))
    GetMCPStatus()
      .then(setMcp)
      .catch(() => setMcp(null))
    GetExtensionStatus()
      .then(setExtension)
      .catch(() => setExtension(null))
  }, [])
  useEffect(loadConnections, [loadConnections])
  useDataChanged(['ai_services'], loadConnections)

  // The one being worked, of however many are open. Oldest claim first: the
  // queue is worked in order, so on the rare occasion two are claimed at once
  // the older one has been going longest and is the one worth watching.
  const working = reports
    .filter((report) => report.checkedOut)
    .sort((a, b) => (a.updatedAt ?? '').localeCompare(b.updatedAt ?? ''))[0]

  // The clock on that segment, ticked rather than left where it was drawn. A
  // duration is the one thing here that goes stale while nothing happens, and
  // "2m" that stays 2m for half an hour reads as work that has stopped.
  //
  // Every half minute, and only while there is something to time: this bar is
  // mounted for the life of the app, and a timer under an idle queue is a render
  // a minute for nothing.
  const [, tick] = useState(0)
  useEffect(() => {
    if (!working) return
    const timer = setInterval(() => tick((n) => n + 1), 30000)
    return () => clearInterval(timer)
  }, [working])

  // The transient messages, as one list (#137). Each kind keeps its own words
  // and destination; what changes is that the bar shows only the oldest, with
  // how many wait behind it.
  //
  //  - artwork generations (#83): every click goes to the record the cover is
  //    for, and a ready one retires on the way;
  //  - music-video renders (#118) and uploads (#122, #124), with how far each
  //    has got, since they are minutes long;
  //  - evolutions (#82): running walks back to the source, ready opens the
  //    successor and retires, failed walks back to where the retry is;
  //  - the report being worked: what the queue is *doing*, named and timed,
  //    because which report and how long are the reasons to look.
  const notices: Notice[] = [
    ...artworks.map((job): Notice => ({
      key: 'artwork:' + job.albumId,
      state: job.state,
      icon:
        job.state === 'running' ? (
          <Loader2 size={12} className="animate-spin" />
        ) : job.state === 'done' ? (
          <ImageIcon size={12} />
        ) : (
          <AlertTriangle size={12} />
        ),
      label:
        job.state === 'running'
          ? `Drawing the cover for “${job.albumName}”…`
          : job.state === 'done'
            ? `The cover for “${job.albumName}” is ready — open it`
            : `Drawing the cover for “${job.albumName}” failed`,
      open: () => {
        if (job.state === 'done') dismissArtwork(job.albumId)
        onOpenAlbum(job.albumId, job.artistId)
      },
      retire: () => dismissArtwork(job.albumId),
    })),
    ...videos.map((job): Notice => {
      const what = job.kind === 'render' ? 'Rendering' : 'Uploading'
      const thing =
        job.kind === 'ytmusic'
          ? `“${job.albumName}” to YouTube Music`
          : `the music video for “${job.albumName}”`
      return {
        key: 'video:' + job.kind + ':' + job.albumId,
        state: job.state,
        icon:
          job.state === 'running' ? (
            <Loader2 size={12} className="animate-spin" />
          ) : job.state === 'done' ? (
            <Clapperboard size={12} />
          ) : (
            <AlertTriangle size={12} />
          ),
        label:
          job.state === 'running'
            ? `${what} ${thing} — ${Math.round(job.fraction * 100)}%`
            : job.state === 'done'
              ? job.kind === 'upload'
                ? `The music video for “${job.albumName}” is on YouTube — open it`
                : job.kind === 'ytmusic'
                  ? `“${job.albumName}” is in your YouTube Music library — open it`
                  : `The music video for “${job.albumName}” is ready — open it`
              : `${what} ${thing} failed`,
        open: () => {
          if (job.state === 'done') dismissVideo(job.albumId, job.kind)
          onOpenAlbum(job.albumId, job.artistId)
        },
        retire: () => dismissVideo(job.albumId, job.kind),
      }
    }),
    ...evolutions.map((evolution): Notice => ({
      key: 'evolution:' + evolution.albumId,
      state: evolution.state,
      icon:
        evolution.state === 'running' ? (
          <Loader2 size={12} className="animate-spin" />
        ) : evolution.state === 'done' ? (
          <Sparkles size={12} />
        ) : (
          <AlertTriangle size={12} />
        ),
      label:
        evolution.state === 'running'
          ? `Evolving “${evolution.albumName}”…`
          : evolution.state === 'done'
            ? `“${evolution.nextAlbumName}” is ready — open it`
            : `Evolving “${evolution.albumName}” failed`,
      open: () => {
        if (evolution.state === 'done') {
          dismiss(evolution.albumId)
          onOpenAlbum(evolution.nextAlbumId, evolution.artistId)
        } else {
          onOpenAlbum(evolution.albumId, evolution.artistId)
        }
      },
      retire: () => dismiss(evolution.albumId),
    })),
    ...(working
      ? [
          {
            key: 'bug:' + working.id,
            state: 'running' as const,
            // A spinner rather than a gear: at twelve pixels the one thing this
            // has to say is that something is happening.
            icon: <Loader2 size={12} className="animate-spin" />,
            label: `${summarise(working.description, 60)} · ${formatElapsed(working.updatedAt)}`,
            open: () => onNavigate('settings-bugs'),
          },
        ]
      : []),
  ]

  const connected = services.filter((service) => service.connected)
  const starred = connected.find((service) => service.default)
  const albums = artists.reduce((total, artist) => total + artist.albumCount, 0)

  return (
    <footer className="flex h-7 shrink-0 items-center gap-5 border-t border-edge bg-surface px-4 text-xs text-fg-muted">
      {/* First and loudest: with the database unavailable, everything else on
          this bar is describing an app that cannot save anything. */}
      {info && !info.storeOk && (
        <Segment
          icon={<AlertTriangle size={12} />}
          label="No database — nothing will be saved"
          tone="danger"
          showLabel
          onClick={() => onNavigate('settings-about')}
        />
      )}

      <Segment
        icon={<Music4 size={12} />}
        label={
          artists.length === 0
            ? 'No artists yet'
            : `${artists.length} artist${artists.length === 1 ? '' : 's'}` +
              (albums > 0 ? `, ${albums} album${albums === 1 ? '' : 's'}` : '')
        }
        onClick={() => onNavigate('artists')}
      />

      <Segment
        icon={<Sparkles size={12} />}
        label={
          connected.length === 0
            ? 'No AI provider connected — the interviews cannot run'
            : `${starred ? starred.name : connected[0].name} · ` +
              `${connected.length} provider${connected.length === 1 ? '' : 's'} connected`
        }
        // Not merely "no problem": once the label is behind a hover the colour is
        // most of what a glance gets, and a connected provider that looked
        // identical to a missing one would be a readout that has stopped
        // reporting.
        tone={connected.length === 0 ? 'warn' : 'ok'}
        onClick={() => onNavigate('settings-ai')}
      />

      {mcp && <AssistantsSegment mcp={mcp} onOpen={() => onNavigate('settings-ai')} />}

      {extension && (
        <Segment
          icon={<Chrome size={12} />}
          label={
            extension.exported
              ? `Chrome extension exported · fills Suno's fields on port ${extension.port}`
              : 'Chrome extension not exported — Suno cannot reach your tracks'
          }
          tone={extension.exported ? 'ok' : undefined}
          onClick={() => onNavigate('settings-extension')}
        />
      )}

      {/* Pushed right: the queue is the one segment whose best news is saying
          nothing at all. */}
      <span className="ml-auto flex items-center gap-4">
        {/* One message at a time, oldest first (#137). */}
        <Notices notices={notices} />

        <Segment
          icon={<Bug size={12} />}
          label={
            reports.length === 0
              ? 'No open reports'
              : `${reports.length} open report${reports.length === 1 ? '' : 's'}`
          }
          // The one segment whose good news is the empty case, so the colours
          // read the other way round: quiet green for nothing open, warn for a
          // queue with something in it.
          tone={reports.length === 0 ? 'ok' : 'warn'}
          align="right"
          onClick={() => onNavigate('settings-bugs')}
        />
      </span>
    </footer>
  )
}

/**
 * The assistants' registration reduced to one line and a colour, or nothing at
 * all when no assistant app is installed to register with — a warning about a
 * connection nobody could make would be noise rather than news.
 *
 * A stale entry outranks a missing one: an assistant pointed at a moved or
 * replaced binary launches the wrong thing and fails confusingly, which is the
 * one worth chasing first.
 */
function AssistantsSegment({
  mcp,
  onOpen,
}: {
  mcp: app.MCPStatus
  onOpen: () => void
}) {
  const present = mcp.clients.filter((client) => client.present)
  if (present.length === 0) return null

  const stale = present.filter((client) => client.connected && !client.current)
  const live = present.filter((client) => client.connected && client.current)

  let label: string
  let tone: 'ok' | 'warn' | undefined
  if (stale.length > 0) {
    label = `${stale.map((client) => client.name).join(', ')} points at another build — reconnect`
    tone = 'warn'
  } else if (live.length > 0) {
    label = `${live.map((client) => client.name).join(', ')} can reach this app's ${mcp.toolCount} tools`
    tone = 'ok'
  } else {
    label = `${present.length} assistant${present.length === 1 ? '' : 's'} installed, none connected`
    tone = undefined
  }

  return <Segment icon={<Plug size={12} />} label={label} tone={tone} onClick={onOpen} />
}

/** One transient message on the bar. */
interface Notice {
  /** Stable for the job's life, whatever its state. */
  key: string
  state: 'running' | 'done' | 'failed'
  icon: ReactNode
  label: string
  /** What clicking the message does. */
  open: () => void
  /** Takes a finished job off its provider; absent for what cannot be. */
  retire?: () => void
}

/**
 * The oldest message, with a "+N" for the rest (#137). Several jobs at once
 * used to be several labelled segments, which ran off the bar and truncated
 * each other; one at a time is readable, and the count says there is more.
 *
 * "+N" clears the message shown and brings up the next. A finished or failed
 * job is retired, as opening it would; a job still running is only set aside,
 * keyed by its state, so it comes back when it finishes — clearing a progress
 * line is not asking never to hear how it ended.
 *
 * Oldest is by when this bar first saw each job. The providers keep no start
 * time and re-append a job when its state changes, so their own order is
 * "most recently changed" — the wrong one for a queue read front to back.
 */
function Notices({notices}: {notices: Notice[]}) {
  const firstSeen = useRef(new Map<string, number>())
  const [setAside, setSetAside] = useState<Set<string>>(new Set())

  // A set-aside job that has since changed state or gone is forgotten, so the
  // same job started again later is not hidden from the start.
  const present = notices.map((notice) => notice.key + ':' + notice.state).join('|')
  useEffect(() => {
    const live = new Set(present.split('|'))
    setSetAside((was) => {
      const kept = [...was].filter((key) => live.has(key))
      return kept.length === was.size ? was : new Set(kept)
    })
  }, [present])

  const now = Date.now()
  for (const notice of notices) {
    if (!firstSeen.current.has(notice.key)) firstSeen.current.set(notice.key, now)
  }
  // Forget jobs that have gone, so the map does not grow for the life of the
  // app and a job started again later counts as new.
  for (const key of [...firstSeen.current.keys()]) {
    if (!notices.some((notice) => notice.key === key)) firstSeen.current.delete(key)
  }

  const waiting = notices
    .filter((notice) => !setAside.has(notice.key + ':' + notice.state))
    .sort((a, b) => (firstSeen.current.get(a.key) ?? 0) - (firstSeen.current.get(b.key) ?? 0))
  const shown = waiting[0]
  if (!shown) return null

  return (
    <span className="flex min-w-0 items-center gap-1.5">
      <Segment
        icon={shown.icon}
        label={shown.label}
        tone={shown.state === 'done' ? 'ok' : shown.state === 'failed' ? 'warn' : undefined}
        showLabel
        live={shown.state === 'running'}
        align="right"
        onClick={shown.open}
      />
      {waiting.length > 1 && (
        <button
          type="button"
          className="shrink-0 rounded-full border border-edge px-1.5 leading-4 tabular-nums hover:bg-surface-hover hover:text-fg"
          title="Clear this message and show the next"
          aria-label={`Clear this message; ${waiting.length - 1} more behind it`}
          onClick={() => {
            if (shown.retire && shown.state !== 'running') {
              shown.retire()
            } else {
              setSetAside((was) => new Set(was).add(shown.key + ':' + shown.state))
            }
          }}
        >
          +{waiting.length - 1}
        </button>
      )}
    </span>
  )
}

function Segment({
  icon,
  label,
  tone,
  showLabel = false,
  live = false,
  align = 'left',
  onClick,
}: {
  icon: ReactNode
  label: string
  /**
   * How this segment is doing. `ok` is a state worth colouring in its own right
   * rather than an absence of problems — see the AI segment for why.
   */
  tone?: 'ok' | 'warn' | 'danger'
  /**
   * Draw the text beside the icon. Off for the always-on readouts, which say the
   * same thing from launch to quit and do not need four labels' worth of the bar
   * to do it; on for anything transient, where the words are the point.
   */
  showLabel?: boolean
  /** Something is happening behind this right now, and the label should breathe. */
  live?: boolean
  /**
   * Which edge the tooltip hangs from. Centring it would run the leftmost
   * segment off the left of the window and the right-pushed one off the right,
   * and what runs off is cut rather than scrolled to.
   */
  align?: 'left' | 'right'
  onClick: () => void
}) {
  return (
    <span className="group relative inline-flex">
      <button
        type="button"
        onClick={onClick}
        // The accessible name comes from here rather than from the text, because
        // for most of these there is no text: an icon-only button without one is
        // announced as "button" and nothing else.
        aria-label={label}
        className={
          'inline-flex max-w-[22rem] items-center gap-1.5 rounded transition-colors hover:text-fg ' +
          (tone === 'danger'
            ? 'font-medium text-danger '
            : tone === 'warn'
              ? 'text-warn '
              : tone === 'ok'
                ? 'text-ok '
                : '')
        }
      >
        <span className="shrink-0" aria-hidden="true">
          {icon}
        </span>
        {showLabel && (
          <span className={'truncate ' + (live ? 'animate-pulse' : '')}>{label}</span>
        )}
      </button>

      {/* The label for the icon-only ones, and the whole of a truncated one.
          A hover card rather than a title attribute: the native tooltip takes a
          second to appear and cannot be themed, and this bar is read at a
          glance. */}
      <span
        className={
          'pointer-events-none absolute bottom-full z-20 mb-1.5 hidden whitespace-nowrap rounded ' +
          'border border-edge bg-bg px-2 py-1 text-xs text-fg shadow-lg group-hover:block ' +
          (align === 'right' ? 'right-0' : 'left-0')
        }
        role="tooltip"
      >
        {label}
      </span>
    </span>
  )
}
