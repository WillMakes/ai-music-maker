import {useCallback, useEffect, useMemo, useState} from 'react'
import {CheckCircle2, CircleAlert, ExternalLink, Loader2, Square, Wand2} from 'lucide-react'
import {
  CancelSunoAlbumRun,
  GetMCPServer,
  GetSunoAccount,
  GetSunoAlbumRun,
  OpenURL,
  StartSunoAlbumRun,
} from '../../wailsjs/go/app/App'
import {app} from '../../wailsjs/go/models'
import {EventsOn} from '../../wailsjs/runtime/runtime'
import {SelectField} from '../components/Field'
import {Modal} from '../components/Modal'
import {useDataChanged} from '../lib/dataChanged'
import {toMessage} from '../lib/errors'

/**
 * Making the album through Suno's MCP server, beside the extension.
 *
 * Shown only while Suno is connected and authorized in Settings → MCP servers:
 * without it the album page is exactly what it was, and the extension is the
 * way. The run itself lives in the app's process (sunomcp.go), so leaving the
 * page does not stop it; this panel reads it back and follows its event.
 */
export function SunoMCPPanel({albumId, tracks}: {albumId: string; tracks: app.Track[]}) {
  const [connected, setConnected] = useState(false)
  const [run, setRun] = useState<app.SunoAlbumRun | null>(null)
  const [choosing, setChoosing] = useState(false)
  const [error, setError] = useState('')

  const reloadServer = useCallback(() => {
    GetMCPServer('suno')
      .then((server) => setConnected(server.state === 'connected'))
      .catch(() => setConnected(false))
  }, [])
  useEffect(reloadServer, [reloadServer])
  useDataChanged(['mcp_servers'], reloadServer)

  useEffect(() => {
    GetSunoAlbumRun(albumId)
      .then((found) => setRun(found.state ? found : null))
      .catch(() => setRun(null))
    return EventsOn('sunomcp:run', (next: app.SunoAlbumRun) => {
      if (next.albumId === albumId) setRun(next)
    })
  }, [albumId])

  const running = run?.state === 'running'
  // A run still going is shown even if the sign-in lapsed under it, so its
  // failure can be read where it happened.
  if (!connected && !run) return null

  return (
    <section className="sm-panel space-y-3 p-4" aria-label="Make it in Suno">
      <h2 className="text-sm font-semibold">Make it in Suno</h2>
      <p className="text-xs text-fg-muted">
        Generate the tracks on your Suno account from here — title, style, lyrics and length, as
        the extension fills them — and file the takes into this album. Or open Suno in Chrome and
        let the extension fill the form, as before.
      </p>
      <div className="flex flex-wrap gap-2">
        <button
          className="sm-button sm-button-primary"
          type="button"
          disabled={!connected || running || tracks.length === 0}
          onClick={() => {
            setError('')
            setChoosing(true)
          }}
        >
          {running ? <Loader2 size={15} className="animate-spin" /> : <Wand2 size={15} />}
          {running ? 'Making it…' : 'Create via Suno MCP'}
        </button>
        <button
          className="sm-button sm-button-quiet"
          type="button"
          onClick={() => void OpenURL('https://suno.com/create')}
        >
          <ExternalLink size={15} />
          Chrome extension
        </button>
      </div>
      {tracks.length === 0 && <p className="text-xs text-fg-muted">Needs tracks first.</p>}

      {run && <RunProgress run={run} albumId={albumId} />}
      {error && <p className="text-sm text-danger">{error}</p>}

      {choosing && (
        <StartDialog
          albumId={albumId}
          tracks={tracks}
          onClose={() => setChoosing(false)}
          onStarted={(started) => {
            setRun(started)
            setChoosing(false)
          }}
        />
      )}
    </section>
  )
}

const TRACK_STATE: Record<string, string> = {
  queued: 'Waiting',
  submitting: 'Sending to Suno…',
  generating: 'Suno is making it…',
  downloading: 'Filing the takes…',
  filed: 'Filed',
  in_library: 'In your Suno library',
  failed: 'Failed',
  skipped: 'Not sent',
}

function RunProgress({run, albumId}: {run: app.SunoAlbumRun; albumId: string}) {
  const running = run.state === 'running'
  const spent =
    run.before.reported && run.after.reported
      ? {
          credits: run.before.creditsRemaining - run.after.creditsRemaining,
          downloads: run.before.downloadCreditsRemaining - run.after.downloadCreditsRemaining,
        }
      : null
  return (
    <div className="space-y-2">
      <ul className="divide-y divide-edge rounded-md border border-edge">
        {run.tracks.map((track) => {
          const busy = ['submitting', 'generating', 'downloading'].includes(track.state)
          const ok = track.state === 'filed' || track.state === 'in_library'
          return (
            <li key={track.trackId} className="space-y-1 px-3 py-2">
              <div className="flex items-center gap-2 text-sm">
                {busy ? (
                  <Loader2 size={14} className="shrink-0 animate-spin text-accent" />
                ) : ok ? (
                  <CheckCircle2 size={14} className="shrink-0 text-ok" />
                ) : track.state === 'failed' ? (
                  <CircleAlert size={14} className="shrink-0 text-danger" />
                ) : (
                  <span className="w-3.5 shrink-0" />
                )}
                <span className="min-w-0 flex-1 truncate">
                  {String(track.position).padStart(2, '0')} {track.name}
                </span>
                <span className="shrink-0 text-xs text-fg-muted">
                  {TRACK_STATE[track.state] ?? track.state}
                </span>
              </div>
              {track.clips.length > 0 && (
                <div className="flex flex-wrap gap-x-3 pl-5 text-xs text-fg-muted">
                  {track.clips.map((clip, i) => (
                    <button
                      key={clip.id}
                      type="button"
                      className="hover:text-accent hover:underline"
                      title={clip.id}
                      onClick={() => void OpenURL(clip.songUrl || 'https://suno.com/song/' + clip.id)}
                    >
                      Song {i + 1}: {clip.fileId ? 'filed' : clip.status}
                      {clip.seconds ? ` · ${Math.round(clip.seconds)}s` : ''}
                    </button>
                  ))}
                </div>
              )}
              {track.error && <p className="pl-5 text-xs text-danger">{track.error}</p>}
            </li>
          )
        })}
      </ul>
      {running ? (
        <div className="flex items-center gap-2">
          <p className="flex-1 text-xs text-fg-muted">
            Keeps going if you leave this page. Songs take a minute or two each.
          </p>
          <button
            className="sm-button sm-button-quiet"
            type="button"
            onClick={() => void CancelSunoAlbumRun(albumId)}
          >
            <Square size={13} />
            Stop
          </button>
        </div>
      ) : (
        <>
          {run.error && (
            <p className={'text-sm ' + (run.state === 'done' ? 'text-warn' : 'text-danger')}>
              {run.error}
            </p>
          )}
          {spent && (
            <p className="text-xs text-fg-muted">
              Spent {spent.credits} credits
              {spent.downloads > 0 ? ` and ${spent.downloads} download credits` : ''} ·{' '}
              {run.after.creditsRemaining} credits left
            </p>
          )}
        </>
      )}
    </div>
  )
}

/**
 * What to make and what it costs, read from Suno before anything is spent.
 * The download credit is asked about here because audio-download says to ask
 * the person first; nothing confirms it on their behalf.
 */
function StartDialog({
  albumId,
  tracks,
  onClose,
  onStarted,
}: {
  albumId: string
  tracks: app.Track[]
  onClose: () => void
  onStarted: (run: app.SunoAlbumRun) => void
}) {
  const [account, setAccount] = useState<app.SunoAccount | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [selected, setSelected] = useState<Set<string>>(
    () => new Set(tracks.filter((t) => t.files.length === 0).map((t) => t.id)),
  )
  const [model, setModel] = useState('')
  const [download, setDownload] = useState(false)
  const [starting, setStarting] = useState(false)

  useEffect(() => {
    GetSunoAccount()
      .then(setAccount)
      .catch((err) => setError(toMessage(err)))
      .finally(() => setLoading(false))
  }, [])

  const chosen = useMemo(() => tracks.filter((t) => selected.has(t.id)), [tracks, selected])
  const empty = chosen.filter((t) => !t.prompt.trim() && !t.lyrics.trim())
  // audio-create makes two songs a call, with no way to ask for fewer.
  const songs = chosen.length * 2
  const short = account?.reported && download && songs > account.downloadCreditsRemaining

  const toggle = (id: string) =>
    setSelected((current) => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })

  return (
    <Modal
      title="Create via Suno MCP"
      onClose={onClose}
      footer={
        <div className="flex justify-end gap-2">
          <button className="sm-button sm-button-quiet" type="button" onClick={onClose}>
            Cancel
          </button>
          <button
            className="sm-button sm-button-primary"
            type="button"
            disabled={starting || loading || chosen.length === 0 || empty.length > 0 || !!short}
            onClick={async () => {
              setStarting(true)
              setError('')
              try {
                onStarted(
                  await StartSunoAlbumRun(
                    albumId,
                    app.SunoRunOptions.createFrom({
                      trackIds: chosen.map((t) => t.id),
                      model,
                      download,
                    }),
                  ),
                )
              } catch (err) {
                setError(toMessage(err))
                setStarting(false)
              }
            }}
          >
            {starting ? <Loader2 size={15} className="animate-spin" /> : <Wand2 size={15} />}
            {songs === 1 ? 'Make 1 song' : `Make ${songs} songs`}
          </button>
        </div>
      }
    >
      <div className="space-y-4">
        <div className="rounded-md border border-edge px-3 py-2 text-sm">
          {loading ? (
            <span className="flex items-center gap-2 text-fg-muted">
              <Loader2 size={14} className="animate-spin" />
              Reading your Suno account…
            </span>
          ) : account?.reported ? (
            <div className="space-y-0.5">
              <p>
                <strong>{account.creditsRemaining}</strong> credits left
                {account.plan ? <span className="text-fg-muted"> · {account.plan}</span> : null}
              </p>
              <p className="text-xs text-fg-muted">
                {account.downloadCreditsRemaining} download credits left
                {account.monthlyLimit
                  ? ` · ${account.monthlyUsage} of ${account.monthlyLimit} used this month`
                  : ''}
              </p>
            </div>
          ) : (
            <p className="text-fg-muted">Suno did not report this account&apos;s credits.</p>
          )}
        </div>

        <fieldset className="space-y-1">
          <legend className="mb-1 text-xs font-medium text-fg-muted">Tracks</legend>
          <div className="max-h-56 space-y-1 overflow-auto">
            {tracks.map((track) => (
              <label key={track.id} className="flex items-start gap-2 text-sm">
                <input
                  type="checkbox"
                  className="mt-0.5"
                  checked={selected.has(track.id)}
                  onChange={() => toggle(track.id)}
                />
                <span className="min-w-0 flex-1">
                  {String(track.position).padStart(2, '0')} {track.name}
                  {track.files.length > 0 && (
                    <span className="text-xs text-fg-muted">
                      {' '}
                      · has {track.files.length === 1 ? 'a take' : `${track.files.length} takes`}
                    </span>
                  )}
                  {!track.prompt.trim() && !track.lyrics.trim() && (
                    <span className="text-xs text-danger"> · no style or lyrics yet</span>
                  )}
                </span>
              </label>
            ))}
          </div>
        </fieldset>

        <p className="text-xs text-fg-muted">
          Suno makes two songs from every track, as suno.com&apos;s Create does, and each is a
          generation. It no longer takes a request for just one.
        </p>

        <div className="grid gap-3 md:grid-cols-2">
          <SelectField
            label="Model"
            value={model}
            onChange={setModel}
            options={[
              {value: '', label: 'Your plan’s default'},
              ...(account?.allowedModels ?? []).map((m) => ({value: m, label: m})),
            ]}
          />
        </div>

        <label className="flex items-start gap-2 text-sm">
          <input
            type="checkbox"
            className="mt-0.5"
            checked={download}
            onChange={(event) => setDownload(event.target.checked)}
          />
          <span>
            File the takes into this album now
            <span className="block text-xs text-fg-muted">
              Suno charges a download credit for each file it hands over: up to {songs} of your{' '}
              {account?.reported ? account.downloadCreditsRemaining : '…'} download credits. Left
              off, the songs stay in your Suno library under the tracks&apos; titles, and playing
              them through with the extension recording files them for nothing.
            </span>
          </span>
        </label>
        {short && (
          <p className="text-sm text-danger">
            That is more download credits than the account has left.
          </p>
        )}
        {empty.length > 0 && (
          <p className="text-sm text-danger">
            {empty.map((t) => `“${t.name}”`).join(', ')} {empty.length === 1 ? 'has' : 'have'} no
            style prompt or lyrics to send.
          </p>
        )}
        {error && <p className="text-sm text-danger">{error}</p>}
      </div>
    </Modal>
  )
}
