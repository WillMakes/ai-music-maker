import {useCallback, useEffect, useState} from 'react'
import {ExternalLink, Loader2, Youtube} from 'lucide-react'
import {
  ConnectYouTube,
  ConnectYouTubeMusic,
  DisconnectYouTube,
  DisconnectYouTubeMusic,
  GetYouTube,
  GetYouTubeMusic,
  OpenURL,
  SaveYouTubeClient,
} from '../../wailsjs/go/app/App'
import {app} from '../../wailsjs/go/models'
import {TextAreaField, TextField} from '../components/Field'
import {StatusPill} from '../components/StatusPill'
import {useDataChanged} from '../lib/dataChanged'
import {toMessage} from '../lib/errors'

/**
 * Settings → YouTube (#121): the producer's own Google OAuth client, the
 * channel it signs in to, and the YouTube Music library session (#124) — how Jax's Brand Producer connects, so the same
 * client can be pasted here.
 *
 * The client is the producer's because an app shipped as a binary cannot keep
 * a secret, and Google holds the client's owner to its quota and its consent
 * screen. The secret is written, never read back: the field starts empty and
 * says when one is saved.
 */
export function YouTubePanel() {
  const [status, setStatus] = useState<app.YouTubeStatus | null>(null)
  const [clientId, setClientId] = useState('')
  const [secret, setSecret] = useState('')
  const [busy, setBusy] = useState<'' | 'saving' | 'connecting' | 'disconnecting'>('')
  const [error, setError] = useState('')

  const reload = useCallback(() => {
    GetYouTube()
      .then((found) => {
        setStatus(found)
        setClientId((current) => current || found.clientId)
      })
      .catch((err) => setError(toMessage(err)))
  }, [])
  useEffect(reload, [reload])
  useDataChanged(['youtube'], reload)

  const act = async (kind: typeof busy, call: () => Promise<app.YouTubeStatus>) => {
    setBusy(kind)
    setError('')
    try {
      setStatus(await call())
      if (kind === 'saving') setSecret('')
    } catch (err) {
      setError(toMessage(err))
    } finally {
      setBusy('')
    }
  }

  return (
    <div className="space-y-5">
      <section className="sm-panel space-y-3 p-4">
        <h2 className="flex items-center gap-2 text-sm font-semibold">
          <Youtube size={15} />
          <span className="flex-1">Channel</span>
          {status?.connected ? (
            <StatusPill tone="ok">Connected</StatusPill>
          ) : (
            <StatusPill tone="muted">Not connected</StatusPill>
          )}
        </h2>
        {status?.connected ? (
          <>
            <p className="text-sm">
              Signed in to <strong>{status.channelTitle}</strong>
              <span className="text-fg-muted"> · {status.channelId}</span>
            </p>
            <p className="text-xs text-fg-muted">
              Albums can upload their music video to this channel, and read how it is doing.
            </p>
            <div className="flex flex-wrap gap-2">
              <button
                className="sm-button sm-button-quiet"
                type="button"
                disabled={busy !== ''}
                onClick={() => void act('connecting', ConnectYouTube)}
              >
                {busy === 'connecting' && <Loader2 size={15} className="animate-spin" />}
                Sign in again
              </button>
              <button
                className="sm-button sm-button-quiet"
                type="button"
                disabled={busy !== ''}
                onClick={() => void act('disconnecting', DisconnectYouTube)}
              >
                Disconnect
              </button>
            </div>
          </>
        ) : (
          <>
            <p className="text-xs text-fg-muted">
              {status?.hasClient
                ? 'Sign in with the Google account that owns the channel. The sign-in opens in your browser; come back here once it says you can close the tab.'
                : 'Save your OAuth client below first, then sign in here.'}
            </p>
            <button
              className="sm-button sm-button-primary"
              type="button"
              disabled={busy !== '' || !status?.hasClient}
              onClick={() => void act('connecting', ConnectYouTube)}
            >
              {busy === 'connecting' ? <Loader2 size={15} className="animate-spin" /> : <Youtube size={15} />}
              {busy === 'connecting' ? 'Waiting for the browser…' : 'Connect YouTube'}
            </button>
          </>
        )}
      </section>

      <section className="sm-panel space-y-3 p-4">
        <h2 className="text-sm font-semibold">OAuth client</h2>
        <p className="text-xs text-fg-muted">
          In Google Cloud: create a project (or use Jax&apos;s), enable the YouTube Data API v3 and
          the YouTube Analytics API, set up the OAuth consent screen with yourself as a test user,
          and create an OAuth client ID of type <strong>Desktop app</strong>. Paste its id and
          secret here.
        </p>
        <button
          className="flex items-center gap-1 text-xs text-accent hover:underline"
          type="button"
          onClick={() => void OpenURL('https://console.cloud.google.com/apis/credentials')}
        >
          <ExternalLink size={12} />
          Open Google Cloud credentials
        </button>
        <TextField
          label="Client ID"
          value={clientId}
          placeholder="…apps.googleusercontent.com"
          onChange={setClientId}
        />
        <TextField
          label="Client secret"
          hint={
            status?.hasClient
              ? 'A secret is saved. Enter it again only to change the client.'
              : undefined
          }
          value={secret}
          placeholder={status?.hasClient ? '••••••••' : ''}
          onChange={setSecret}
        />
        <button
          className="sm-button sm-button-quiet"
          type="button"
          disabled={busy !== '' || !clientId.trim() || !secret.trim()}
          onClick={() => void act('saving', () => SaveYouTubeClient(clientId, secret))}
        >
          {busy === 'saving' && <Loader2 size={15} className="animate-spin" />}
          Save the client
        </button>
      </section>

      <YouTubeMusicSection />

      {error && <p className="text-sm text-danger">{error}</p>}
    </div>
  )
}

/**
 * The YouTube Music session (#124): uploads to the producer's own library go
 * through the web app's private upload, the way ytmusicapi does it, which is
 * signed in by the browser's cookies rather than OAuth — so the producer
 * pastes a signed-in tab's request headers, and the app checks them before
 * keeping them. Never shown back: they are the producer's Google sign-in.
 */
function YouTubeMusicSection() {
  const [status, setStatus] = useState<app.YTMusicStatus | null>(null)
  const [headers, setHeaders] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const reload = useCallback(() => {
    GetYouTubeMusic()
      .then(setStatus)
      .catch((err) => setError(toMessage(err)))
  }, [])
  useEffect(reload, [reload])
  useDataChanged(['youtube'], reload)

  const act = async (call: () => Promise<app.YTMusicStatus>) => {
    setBusy(true)
    setError('')
    try {
      setStatus(await call())
      setHeaders('')
    } catch (err) {
      setError(toMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="sm-panel space-y-3 p-4">
      <h2 className="flex items-center gap-2 text-sm font-semibold">
        <span className="flex-1">YouTube Music library</span>
        {status?.connected ? (
          <StatusPill tone="ok">Connected</StatusPill>
        ) : (
          <StatusPill tone="muted">Not connected</StatusPill>
        )}
      </h2>
      <p className="text-xs text-fg-muted">
        Albums can upload their official takes to your own YouTube Music library, tagged and with
        the cover. These uploads are private to your account — releasing a record on YouTube Music
        goes through a distributor. YouTube Music has no official upload, so this uses the web
        app&apos;s own, signed in as your browser is; it can break if Google changes it.
      </p>
      {status?.connected && (
        <>
          <p className="text-sm">
            Signed in{status.account ? <> as <strong>{status.account}</strong></> : null}.
          </p>
          <button
            className="sm-button sm-button-quiet"
            type="button"
            disabled={busy}
            onClick={() => void act(DisconnectYouTubeMusic)}
          >
            Disconnect
          </button>
        </>
      )}
      <TextAreaField
        label={status?.connected ? 'Paste new headers to sign in again' : 'Request headers'}
        hint="Open music.youtube.com signed in, open the developer tools' Network tab, click any request to music.youtube.com (browse is a good one), and copy its request headers here — all of them, including the cookie."
        value={headers}
        onChange={setHeaders}
        rows={4}
        mono
        disabled={busy}
      />
      <button
        className="sm-button sm-button-primary"
        type="button"
        disabled={busy || !headers.trim()}
        onClick={() => void act(() => ConnectYouTubeMusic(headers))}
      >
        {busy && <Loader2 size={15} className="animate-spin" />}
        Connect YouTube Music
      </button>
      {error && <p className="text-sm text-danger">{error}</p>}
    </section>
  )
}
