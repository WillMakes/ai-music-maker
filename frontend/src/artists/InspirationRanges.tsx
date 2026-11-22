import {useState} from 'react'
import {AudioLines, ArrowUpToLine, ListMusic, Loader2, Plus, Trash2} from 'lucide-react'
import {FindInspirationTracks, ParseRangeTimes} from '../../wailsjs/go/app/App'
import {app, audioanalysis} from '../../wailsjs/go/models'
import {CopyButton} from '../components/CopyButton'
import {Markdown} from '../components/Markdown'
import {toMessage} from '../lib/errors'

/**
 * The time ranges on one inspiration, and each range's breakdown.
 *
 * A reference is rarely a whole song — it is "1:12–1:48, the pre-chorus
 * build" — so each range carries its own label, its own citation key and its
 * own breakdown measured from that section's audio (see
 * internal/app/inspirationbreakdown.go). The measured numbers and the model's
 * interpretation are drawn as two separate blocks, because they are two
 * different kinds of claim and the producer citing them should see which is
 * which.
 */

/** Where one range's breakdown stands, keyed by range id. */
export type RangeStatus = {state: 'running'} | {state: 'failed'; error: string}

/** m:ss or h:mm:ss, with tenths when the time has them — as the Go side writes it. */
export function formatTime(seconds: number): string {
  const s = Math.max(0, seconds)
  const whole = Math.floor(s)
  const tenths = Math.round((s - whole) * 10)
  const h = Math.floor(whole / 3600)
  const m = Math.floor(whole / 60) % 60
  const sec = whole % 60
  const pad = (n: number) => String(n).padStart(2, '0')
  const out = h > 0 ? `${h}:${pad(m)}:${pad(sec)}` : `${m}:${pad(sec)}`
  return tenths > 0 && tenths < 10 ? `${out}.${tenths}` : out
}

export function InspirationRanges({
  row,
  saved,
  linkStart,
  status,
  onRanges,
  onDuration,
  onBreakDown,
  onBreakDownAll,
}: {
  row: app.Inspiration
  /** The reference is in the stored record, so its ranges can be broken down. */
  saved: boolean
  /** The link's own t=, offered as the start of a new range. */
  linkStart: number | null
  status: Record<string, RangeStatus>
  /** Replaces the ranges on the draft; the auto-save writes them down. */
  onRanges: (ranges: app.InspirationRange[]) => void
  /** Records the video's length when a lookup learns it. */
  onDuration: (seconds: number) => void
  onBreakDown: (range: app.InspirationRange) => void
  /** Breaks down, one after another, every saved range that has none yet. */
  onBreakDownAll: () => void
}) {
  const ranges = row.ranges ?? []
  // The tracks found in a long video, offered before they become ranges: a
  // split heard from the audio is a guess, and the producer picks which to
  // keep rather than finding thirty ranges they did not ask for.
  const [finding, setFinding] = useState(false)
  const [found, setFound] = useState<app.TrackSplit | null>(null)
  const [picked, setPicked] = useState<boolean[]>([])
  const [findError, setFindError] = useState('')
  const find = async () => {
    setFinding(true)
    setFindError('')
    try {
      const split = await FindInspirationTracks(row.url)
      if (split.durationSeconds && !row.durationSeconds) onDuration(split.durationSeconds)
      setFound(split)
      setPicked((split.tracks ?? []).map(() => true))
    } catch (err) {
      setFindError(toMessage(err))
    } finally {
      setFinding(false)
    }
  }
  const addFound = () => {
    if (!found) return
    const chosen = (found.tracks ?? []).filter((_, i) => picked[i])
    onRanges([
      ...ranges,
      ...chosen.map((track, i) =>
        app.InspirationRange.createFrom({
          id: '',
          startSeconds: track.startSeconds,
          endSeconds: track.endSeconds,
          label: track.title,
          citeKey: '',
          citation: '',
          breakdown: {source: ''},
          position: ranges.length + i,
        }),
      ),
    ])
    setFound(null)
  }
  // A boundary heard in the audio can fall inside a song (a breakdown, a
  // drop); joining a track to the one before it takes the boundary away.
  const joinWithPrevious = (index: number) => {
    if (!found || index === 0) return
    const tracks = [...(found.tracks ?? [])]
    tracks[index - 1] = app.DetectedTrack.createFrom({
      ...tracks[index - 1],
      endSeconds: tracks[index].endSeconds,
    })
    tracks.splice(index, 1)
    setFound(app.TrackSplit.createFrom({...found, tracks}))
    setPicked((current) => current.filter((_, i) => i !== index))
  }
  const pendingBreakdowns = ranges.filter((r) => r.id && !r.breakdown?.source).length
  const [start, setStart] = useState('')
  const [end, setEnd] = useState('')
  const [label, setLabel] = useState('')
  const [error, setError] = useState('')

  const add = async () => {
    try {
      const times = await ParseRangeTimes(
        start || (linkStart !== null ? formatTime(linkStart) : ''),
        end,
        row.durationSeconds ?? 0,
      )
      onRanges([
        ...ranges,
        app.InspirationRange.createFrom({
          id: '',
          startSeconds: times.startSeconds,
          endSeconds: times.endSeconds,
          label: label.trim(),
          citeKey: '',
          citation: '',
          breakdown: {source: ''},
          position: ranges.length,
        }),
      ])
      setStart('')
      setEnd('')
      setLabel('')
      setError('')
    } catch (err) {
      setError(toMessage(err))
    }
  }

  const change = (index: number, patch: Partial<app.InspirationRange>) =>
    onRanges(
      ranges.map((r, i) => (i === index ? app.InspirationRange.createFrom({...r, ...patch}) : r)),
    )

  return (
    <div className="space-y-3">
      <div className="flex items-baseline gap-2">
        <h3 className="flex-1 text-sm font-medium">Time ranges</h3>
        <span className="text-xs text-fg-muted">
          {row.durationSeconds ? 'Video length ' + formatTime(row.durationSeconds) : ''}
        </span>
      </div>
      <p className="text-xs text-fg-muted">
        The sections of this song you actually mean. Each one is broken down from that
        section&apos;s audio and gets its own citation key for the album interviews. No ranges
        means the whole song. A long video — a full album or a mix — can be split into its
        tracks.
      </p>
      <div className="flex flex-wrap gap-2">
        <button
          className="sm-button sm-button-quiet"
          type="button"
          disabled={finding}
          onClick={() => void find()}
        >
          {finding ? <Loader2 size={15} className="animate-spin" /> : <ListMusic size={15} />}
          {finding ? 'Finding tracks…' : 'Find tracks in this video'}
        </button>
        {pendingBreakdowns > 1 && (
          <button className="sm-button sm-button-quiet" type="button" onClick={onBreakDownAll}>
            <AudioLines size={15} />
            Break down all {pendingBreakdowns}
          </button>
        )}
      </div>
      {finding && (
        <p className="text-xs text-fg-muted">
          Chapters first, then the description&apos;s tracklist; without either, the audio is
          fetched and listened through for gaps and changes of sound — a minute or more on a
          long video.
        </p>
      )}
      {findError && <p className="text-xs text-danger">{findError}</p>}
      {found && (
        <div className="space-y-2 rounded-md border border-accent p-3">
          <p className="text-xs">
            <span className="font-semibold">
              {(found.tracks ?? []).length} track{(found.tracks ?? []).length === 1 ? '' : 's'}
            </span>{' '}
            — {found.note}
          </p>
          <ul className="max-h-64 space-y-1 overflow-auto">
            {(found.tracks ?? []).map((track, i) => (
              <li key={i}>
                <label className="flex items-center gap-2 text-xs">
                  <input
                    type="checkbox"
                    checked={picked[i] ?? false}
                    onChange={(event) =>
                      setPicked((current) =>
                        current.map((v, k) => (k === i ? event.target.checked : v)),
                      )
                    }
                  />
                  <span className="w-28 shrink-0 font-mono">
                    {formatTime(track.startSeconds)}–{formatTime(track.endSeconds)}
                  </span>
                  <span className="flex-1 truncate">{track.title}</span>
                  {i > 0 && (
                    <button
                      className="sm-button sm-button-quiet border-0 px-1.5 py-0.5"
                      type="button"
                      title="Join with the track before — the boundary is inside a song"
                      onClick={(event) => {
                        event.preventDefault()
                        joinWithPrevious(i)
                      }}
                    >
                      <ArrowUpToLine size={13} />
                    </button>
                  )}
                </label>
              </li>
            ))}
          </ul>
          <div className="flex gap-2">
            <button
              className="sm-button sm-button-primary"
              type="button"
              disabled={!picked.some(Boolean)}
              onClick={addFound}
            >
              <Plus size={15} />
              Add {picked.filter(Boolean).length} as ranges
            </button>
            <button className="sm-button sm-button-quiet" type="button" onClick={() => setFound(null)}>
              Dismiss
            </button>
          </div>
        </div>
      )}

      {ranges.map((r, index) => (
        <RangeCard
          key={r.id || 'new-' + index}
          range={r}
          duration={row.durationSeconds ?? 0}
          saved={saved && !!r.id}
          status={r.id ? status[r.id] : undefined}
          onChange={(patch) => change(index, patch)}
          onRemove={() => onRanges(ranges.filter((_, i) => i !== index))}
          onBreakDown={() => onBreakDown(r)}
        />
      ))}

      <div className="grid grid-cols-[6rem_6rem_1fr_auto] items-end gap-2 rounded-md border border-dashed border-edge p-2">
        <SmallInput
          label="Start"
          value={start}
          placeholder={linkStart !== null ? formatTime(linkStart) : '1:12'}
          onChange={setStart}
        />
        <SmallInput label="End" value={end} placeholder="1:48" onChange={setEnd} />
        <SmallInput
          label="Label (optional)"
          value={label}
          placeholder="the pre-chorus build"
          onChange={setLabel}
          onEnter={() => void add()}
        />
        <button className="sm-button sm-button-quiet" type="button" onClick={() => void add()}>
          <Plus size={15} />
          Add range
        </button>
        <p className="col-span-4 text-xs text-fg-muted">
          m:ss or h:mm:ss. Start also takes a whole range (1:12–1:48) or a link with t=.
        </p>
        {error && <p className="col-span-4 text-xs text-danger">{error}</p>}
      </div>
    </div>
  )
}

function SmallInput({
  label,
  value,
  placeholder,
  onChange,
  onBlur,
  onEnter,
}: {
  label: string
  value: string
  placeholder?: string
  onChange: (next: string) => void
  onBlur?: () => void
  onEnter?: () => void
}) {
  return (
    <label className="flex min-w-0 flex-col gap-1 text-xs text-fg-muted">
      {label}
      <input
        className="sm-input"
        value={value}
        placeholder={placeholder}
        onChange={(event) => onChange(event.target.value)}
        onBlur={onBlur}
        onKeyDown={(event) => {
          if (event.key === 'Enter') onEnter?.()
        }}
      />
    </label>
  )
}

function RangeCard({
  range,
  duration,
  saved,
  status,
  onChange,
  onRemove,
  onBreakDown,
}: {
  range: app.InspirationRange
  duration: number
  saved: boolean
  status: RangeStatus | undefined
  onChange: (patch: Partial<app.InspirationRange>) => void
  onRemove: () => void
  onBreakDown: () => void
}) {
  // The times are typed as text and committed on blur, through the same
  // parser the add form uses — a half-typed "1:" must not reach the draft,
  // since the auto-save would send it and the store would refuse the save.
  const [start, setStart] = useState(formatTime(range.startSeconds))
  const [end, setEnd] = useState(formatTime(range.endSeconds))
  const [error, setError] = useState('')
  const running = status?.state === 'running'

  const commit = async () => {
    try {
      const times = await ParseRangeTimes(start, end, duration)
      setError('')
      if (times.startSeconds !== range.startSeconds || times.endSeconds !== range.endSeconds) {
        // New seconds, so the old breakdown describes a section this no
        // longer is; the store would drop it too.
        onChange({...times, breakdown: app.RangeBreakdown.createFrom({source: ''})})
      }
    } catch (err) {
      setError(toMessage(err))
    }
  }

  const b = range.breakdown
  return (
    <div className="space-y-3 rounded-md border border-edge p-3">
      <div className="flex flex-wrap items-end gap-2">
        <span className="self-center rounded bg-tile px-1.5 py-0.5 font-mono text-xs">
          {range.citeKey ? '[' + range.citeKey + ']' : 'Saving…'}
        </span>
        <div className="w-24">
          <SmallInput label="Start" value={start} onChange={setStart} onBlur={() => void commit()} />
        </div>
        <div className="w-24">
          <SmallInput label="End" value={end} onChange={setEnd} onBlur={() => void commit()} />
        </div>
        <div className="min-w-40 flex-1">
          <SmallInput
            label="Label"
            value={range.label}
            placeholder="the pre-chorus build"
            onChange={(label) => onChange({label})}
          />
        </div>
        <button
          className="sm-button sm-button-primary"
          type="button"
          disabled={!saved || running}
          title={saved ? '' : 'Waiting for the range to save'}
          onClick={onBreakDown}
        >
          {running ? <Loader2 size={15} className="animate-spin" /> : <AudioLines size={15} />}
          {running ? 'Breaking down…' : b?.source ? 'Break down again' : 'Break down'}
        </button>
        <button className="sm-button sm-button-quiet" type="button" title="Remove the range" onClick={onRemove}>
          <Trash2 size={15} />
        </button>
      </div>
      {error && <p className="text-xs text-danger">{error}</p>}
      {status?.state === 'failed' && <p className="text-xs text-danger">{status.error}</p>}
      {running && (
        <p className="text-xs text-fg-muted">
          Fetching this section&apos;s audio, measuring it and writing the breakdown — a minute or
          two. The page stays usable.
        </p>
      )}
      {range.citation && (
        <div className="flex items-start gap-1 rounded-md bg-surface px-2 py-1.5">
          <p className="flex-1 font-mono text-xs leading-relaxed">{range.citation}</p>
          <CopyButton text={range.citation} label="Copy the citation" />
        </div>
      )}
      {b?.source && <BreakdownView breakdown={b} />}
    </div>
  )
}

export function BreakdownView({breakdown: b}: {breakdown: app.RangeBreakdown}) {
  const m = b.measurements
  return (
    <div className="space-y-3">
      {b.source === 'audio' && m ? (
        <section className="space-y-2">
          <h4 className="text-xs font-semibold uppercase tracking-wide text-fg-muted">
            Measured from the audio
          </h4>
          <Measured m={m} />
        </section>
      ) : (
        <p className="rounded-md border border-warn px-2 py-1.5 text-xs">
          <span className="font-semibold">Research only</span> — the section&apos;s audio was not
          measured, so everything below is the model&apos;s reading of research about the whole
          song. {b.audioError && <span className="text-fg-muted">({b.audioError})</span>}
        </p>
      )}
      <section className="space-y-2">
        <h4 className="text-xs font-semibold uppercase tracking-wide text-fg-muted">
          Interpretation — written by the model
        </h4>
        {b.proseError ? (
          <p className="text-xs text-danger">The model could not write it: {b.proseError}</p>
        ) : (
          <>
            {b.summary && <p className="text-sm font-medium">{b.summary}</p>}
            {b.interpretation && <Markdown>{b.interpretation}</Markdown>}
          </>
        )}
      </section>
    </div>
  )
}

function Measured({m}: {m: audioanalysis.Measurements}) {
  const db = (v: number) => (v <= -199 ? '−∞' : v.toFixed(1))
  const stats: [string, string][] = [
    [
      'Tempo',
      m.tempoBpm
        ? m.tempoBpm.toFixed(1) +
          ' BPM' +
          (m.tempoAlternate ? ` (or ${m.tempoAlternate.toFixed(1)}, the other octave)` : '') +
          (m.tempoConfidence < 0.3 ? ' (weak pulse)' : '')
        : '—',
    ],
    [
      'Key',
      m.key
        ? m.key + (m.keyConfidence < 0.1 && m.keyRunnerUp ? ` (ambiguous with ${m.keyRunnerUp})` : '')
        : '—',
    ],
    [
      'Loudness',
      m.loudnessMeasured
        ? `${db(m.integratedLufs)} LUFS · LRA ${m.loudnessRangeLu.toFixed(1)} LU · ${db(m.truePeakDbtp)} dBTP`
        : 'not measured',
    ],
    ['Dynamics', `peak ${db(m.peakDbfs)} dBFS · RMS ${db(m.rmsDbfs)} dBFS · crest ${m.crestDb.toFixed(1)} dB`],
    ['Brightness', `centroid ${Math.round(m.spectralCentroidHz)} Hz · 85% below ${Math.round(m.rolloffHz)} Hz`],
    ['Stereo', `width ${m.stereoWidth.toFixed(2)} · correlation ${m.stereoCorrelation.toFixed(2)}`],
    ['Length', formatTime(m.seconds)],
  ]
  return (
    <div className="space-y-2">
      <dl className="grid grid-cols-[6rem_1fr] gap-x-3 gap-y-1 text-xs">
        {stats.map(([k, v]) => (
          <div key={k} className="contents">
            <dt className="text-fg-muted">{k}</dt>
            <dd className="font-mono">{v}</dd>
          </div>
        ))}
        {(m.bands ?? []).length > 0 && (
          <div className="contents">
            <dt className="text-fg-muted">Energy</dt>
            <dd className="font-mono">
              {(m.bands ?? []).map((band) => `${band.name} ${band.percent.toFixed(0)}%`).join(' · ')}
            </dd>
          </div>
        )}
        {(m.energyProfile ?? []).length > 0 && (
          <div className="contents">
            <dt className="text-fg-muted">Over the range</dt>
            <dd className="font-mono">
              {(m.energyProfile ?? []).map((v) => v.toFixed(1)).join(' → ')} dBFS, per{' '}
              {m.energyStepSeconds} s
            </dd>
          </div>
        )}
      </dl>
      {(m.caveats ?? []).length > 0 && (
        <ul className="list-disc pl-5 text-xs text-fg-muted">
          {(m.caveats ?? []).map((c) => (
            <li key={c}>{c}</li>
          ))}
        </ul>
      )}
    </div>
  )
}
