import {useCallback, useEffect, useRef, useState} from 'react'
import {BarChart3, ChevronDown, ChevronRight, Clapperboard, ExternalLink, FileText, FolderInput, FolderOpen, ImagePlus, ListMusic, ListPlus, Loader2, Maximize2, Pause, Pencil, Play, RefreshCw, Sparkles, Trash2, Upload, Wand2} from 'lucide-react'
import {TakesList, TrackEditor} from './TrackDetails'
import {SunoMCPPanel} from './SunoMCPPanel'
import {
  ClearChat,
  DeleteAlbum,
  DeleteTrack,
  ExtractTracks,
  GenerateAlbumStyle,
  GetAlbum,
  GetAlbumVideoStats,
  GetYouTube,
  GetYouTubeMusic,
  ImportAlbumArtwork,
  PickImageFile,
  ImportAlbumTakeDrop,
  ImportAlbumTakeFile,
  ImportTrackFile,
  ListTracks,
  OpenURL,
  RemoveAlbumArtwork,
  RevealTrackFile,
  PickAudioFiles,
  SaveAlbum,
  SaveTrack,
  WriteAlbumTracks,
} from '../../wailsjs/go/app/App'
import {app, youtube} from '../../wailsjs/go/models'
import {CopyButton} from '../components/CopyButton'
import {EmptyState} from '../components/EmptyState'
import {PageHeader} from '../components/PageHeader'
import {SelectField, TextField} from '../components/Field'
import {MarkdownField} from '../components/MarkdownField'
import {InterviewDialog} from '../chat/InterviewDialog'
import {Modal} from '../components/Modal'
import {TabStrip} from '../components/TabStrip'
import {Cover} from './ArtistDetails'
import {useDataChanged} from '../lib/dataChanged'
import {duration, parseDuration} from '../lib/format'
import {toMessage} from '../lib/errors'
import {attachmentPathsByName, isAudioPath, readAsDataURL} from '../lib/files'
import {usePlayer} from '../player/PlayerProvider'
import {useEvolutions} from '../evolutions/EvolutionsProvider'
import {useArtworks} from '../artworks/ArtworksProvider'
import {useVideos} from '../videos/VideosProvider'
import {AddToPlaylistMenu} from '../playlists/AddToPlaylistMenu'
import {hasPlayableTake, takeSummary} from '../lib/takes'

/**
 * One album: its cover, what is different about this record, its tracks, and the
 * release links that only exist once it is out.
 *
 * Artwork generation is a long call — up to several minutes on the Codex path —
 * so it has its own busy state rather than sharing the page's. A producer
 * waiting on a cover can still edit the fields beside it.
 */
export function AlbumDetails({
  albumId,
  trackId,
  onOpenAlbum,
  onOpenArtist,
  onDeleted,
}: {
  albumId: string
  // A track carried on the route — an old track-details link, a bug report's
  // subject — selects that track in the Tracks tab rather than opening a page
  // of its own (#40).
  trackId?: string
  onOpenAlbum: (albumId: string) => void
  onOpenArtist: (artistId: string) => void
  onDeleted: (artistId: string) => void
}) {
  const [album, setAlbum] = useState<app.Album | null>(null)
  const [tracks, setTracks] = useState<app.Track[]>([])
  const [draft, setDraft] = useState<app.Album | null>(null)
  const [saveState, setSaveState] = useState<'idle' | 'saving' | 'saved'>('idle')
  // Only a draft the producer has touched is auto-saved — same guard as the
  // artist page (#52): without it a background write reloading `album` would
  // make the untouched draft look dirty and quietly get reverted.
  const editedRef = useRef(false)
  // Drawing lives in the provider (#83), like evolving (#82): the generation
  // runs for minutes, and the producer must be free to navigate away and find
  // it again — on this page, or in the status bar.
  const {artworks, draw: drawArtwork, dismiss: dismissArtwork} = useArtworks()
  const artworkJob = artworks.find((job) => job.albumId === albumId)
  const drawing = artworkJob?.state === 'running'
  const [feedback, setFeedback] = useState('')
  // Whether the "Make changes with AI" prompt is open on a finished cover
  // (#114). The text survives closing it, so a failed redraw can be retried.
  const [changingArtwork, setChangingArtwork] = useState(false)
  const [error, setError] = useState('')
  const [interviewing, setInterviewing] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  // The tracks section, scrolled to when a route names a track (#97).
  const tracksRef = useRef<HTMLElement>(null)
  // The cover swapped for the rendered video (#143), and the music-video
  // section's folded-away description and upload.
  const [watching, setWatching] = useState(false)
  const [videoMore, setVideoMore] = useState(false)
  const [showDescription, setShowDescription] = useState(false)
  // Where the playing video is, for which song it is on (#145).
  const [videoTime, setVideoTime] = useState(0)
  const videoRef = useRef<HTMLVideoElement>(null)
  // The tabs on the right of the cover (#102, #104, #110), Tracks the
  // default: it is where the producer spends the session, the sound is
  // written once and then read, and the release links are filled in once,
  // after the record is out.
  const [pageTab, setPageTab] = useState<'tracks' | 'sound' | 'releases' | 'takes'>('tracks')
  // The style distillation (#38) has its own busy state, like the artwork's.
  const [generating, setGenerating] = useState(false)
  // The record's two style panes as tabs (#48), this record's own style the
  // default — it is the editable half.
  const [styleTab, setStyleTab] = useState<'style' | 'inherited'>('style')
  // The empty track list's one-press writer (#71).
  const [writingTracks, setWritingTracks] = useState(false)
  // Whether a drag is hovering the track list (#76) — the batch drop zone.
  const [droppingTakes, setDroppingTakes] = useState(false)
  // Which track the detail pane shows (#40). Local state like the tab itself:
  // the album is the place, the selection is a drawer within it.
  const [selectedId, setSelectedId] = useState(trackId ?? '')
  // Evolving lives in the provider, not here (#82): the call runs for as long
  // as a big generation call does, and the producer must be free to navigate
  // away and find it again — on this page, or in the status bar.
  const {evolutions, evolve, dismiss} = useEvolutions()
  const {videos, render: renderVideo, upload: uploadVideo, uploadToYouTubeMusic} = useVideos()
  const ytmusicJob = videos.find((job) => job.albumId === albumId && job.kind === 'ytmusic')
  const toYTMusic = ytmusicJob?.state === 'running'
  const [ytmusicConnected, setYTMusicConnected] = useState(false)
  const videoJob = videos.find((job) => job.albumId === albumId && job.kind === 'render')
  const rendering = videoJob?.state === 'running'
  const uploadJob = videos.find((job) => job.albumId === albumId && job.kind === 'upload')
  const uploading = uploadJob?.state === 'running'
  // Unlisted by default: a first upload is usually checked before it is
  // shown to anybody.
  const [privacy, setPrivacy] = useState('unlisted')
  const [youtubeConnected, setYouTubeConnected] = useState(false)
  const reloadYouTube = useCallback(() => {
    GetYouTube()
      .then((status) => setYouTubeConnected(status.connected))
      .catch(() => setYouTubeConnected(false))
    GetYouTubeMusic()
      .then((status) => setYTMusicConnected(status.connected))
      .catch(() => setYTMusicConnected(false))
  }, [])
  useEffect(reloadYouTube, [reloadYouTube])
  useDataChanged(['youtube'], reloadYouTube)
  const evolution = evolutions.find((e) => e.albumId === albumId)
  const evolving = evolution?.state === 'running'
  // Auto-open the successor only on the transition seen from this page — the
  // old behaviour for a producer who waited here. Returning to this page
  // after the fact must not teleport them; the status bar offers the door.
  const sawRunningRef = useRef(false)
  useEffect(() => {
    if (evolution?.state === 'running') sawRunningRef.current = true
    if (evolution?.state === 'done' && sawRunningRef.current) {
      sawRunningRef.current = false
      dismiss(evolution.albumId)
      onOpenAlbum(evolution.nextAlbumId)
    }
  }, [evolution, dismiss, onOpenAlbum])
  const player = usePlayer()

  // The whole batch at once (#76): each file is routed to its track by name
  // on the backend, and every file that could not be routed or imported is
  // reported together — one stranger must not hide the nine that landed.
  const importTakeErrors = async (imports: (() => Promise<unknown>)[]) => {
    const problems: string[] = []
    for (const run of imports) {
      try {
        await run()
      } catch (err) {
        problems.push(toMessage(err))
      }
    }
    setError(problems.join(' '))
  }

  const importDroppedTakes = (dropped: FileList) =>
    importTakeErrors(
      Array.from(dropped).map((file) => async () => {
        if (!isAudioPath(file.name) && !file.type.startsWith('audio/')) {
          throw new Error(file.name + ' is not an audio file this app knows.')
        }
        await ImportAlbumTakeDrop(albumId, file.name, await readAsDataURL(file))
      }),
    )

  const pickTakes = async () => {
    try {
      const paths = await PickAudioFiles()
      if (!paths?.length) return
      await importTakeErrors(
        paths.map((path) => () => ImportAlbumTakeFile(albumId, path)),
      )
    } catch (err) {
      setError(toMessage(err))
    }
  }

  const reload = useCallback(() => {
    Promise.all([GetAlbum(albumId), ListTracks(albumId)])
      .then(([found, rows]) => {
        setAlbum(found)
        setDraft((current) => current ?? app.Album.createFrom(found))
        setTracks(rows ?? [])
        setError('')
      })
      .catch((err) => setError(toMessage(err)))
  }, [albumId])

  useEffect(reload, [reload])
  useDataChanged(['albums', 'tracks'], reload)

  useEffect(() => {
    if (!trackId) return
    setSelectedId(trackId)
    // Arriving for a track opens the Tracks tab (#102) and scrolls to it once
    // the list has rendered.
    setPageTab('tracks')
    requestAnimationFrame(() => tracksRef.current?.scrollIntoView({block: 'start'}))
  }, [trackId])

  const update = (patch: Partial<app.Album>) => {
    editedRef.current = true
    setDraft((current) => (current ? app.Album.createFrom({...current, ...patch}) : current))
  }

  // The auto-save, ported whole from the artist page (#52) because renaming
  // the record was dying to the same forgotten button (#75): a debounce off
  // the draft, the stored record adopted back only when nothing changed while
  // the save was in flight, and an empty name waiting rather than flashing a
  // refusal per deleted character.
  useEffect(() => {
    if (!draft || !editedRef.current) return
    if (!draft.name.trim()) return
    const sent = JSON.stringify(draft)
    const timer = setTimeout(async () => {
      setSaveState('saving')
      try {
        const saved = await SaveAlbum(draft)
        setAlbum(saved)
        setDraft((current) =>
          current && JSON.stringify(current) === sent
            ? app.Album.createFrom(saved)
            : current,
        )
        // A keystroke during the flight re-armed the debounce before this
        // line runs, so clearing the flag cannot lose an edit.
        editedRef.current = false
        setSaveState('saved')
        setError('')
      } catch (err) {
        setSaveState('idle')
        setError(toMessage(err))
      }
    }, 800)
    return () => clearTimeout(timer)
  }, [draft])

  // Adopt a finished cover (#83): the generation may have ended while this
  // page was somewhere else entirely, so the adoption is an effect on the
  // job's state rather than the tail of the call. Only the artwork fields are
  // taken — the producer may have kept editing the rest — and adopting them
  // into the draft matters beyond display: the auto-save writes the whole
  // draft, and a draft still holding the old cover would quietly put it back.
  useEffect(() => {
    if (artworkJob?.state !== 'done' || !artworkJob.album) return
    const updated = artworkJob.album
    setAlbum(updated)
    setDraft((current) =>
      current
        ? app.Album.createFrom({
            ...current,
            artworkFile: updated.artworkFile,
            artworkUrl: updated.artworkUrl,
            artworkPrompt: updated.artworkPrompt,
          })
        : current,
    )
    setFeedback('')
    dismissArtwork(artworkJob.albumId)
  }, [artworkJob, dismissArtwork])

  const draw = () => {
    // A redraw after a failure is also its acknowledgement — the provider
    // replaces the failed entry with the new run's.
    drawArtwork({id: albumId, name: album?.name ?? '', artistId: album?.artistId ?? ''}, feedback)
  }

  const pickArtwork = async () => {
    // The OS's own picker (#39): the binding owns the window context the
    // dialog needs, so the producer browses instead of pasting a path. An
    // empty path is a cancel, not an error.
    try {
      const path = await PickImageFile()
      if (!path) return
      setAlbum(await ImportAlbumArtwork(albumId, path))
    } catch (err) {
      setError(toMessage(err))
    }
  }

  if (!album || !draft) {
    return <p className="text-sm text-fg-muted">{error || 'Loading…'}</p>
  }

  // The selection falls back to the first track, so the pane is never blank
  // while the record has songs - and heals when the selected one is deleted.
  const selected = tracks.find((t) => t.id === selectedId) ?? tracks[0]

  // The song the playing video is on (#145), read off the description's
  // timestamps — the render wrote them, so they are where each song really
  // starts. Matched to a track by title, and by position when a title does
  // not match, since the video is the record in running order.
  const chapters = videoChapters(album.videoDescription)
  const chapterAt = watching
    ? chapters.reduce<number>((at, chapter, i) => (chapter.start <= videoTime ? i : at), -1)
    : -1
  const videoSong = chapterAt >= 0 ? chapters[chapterAt] : undefined
  const videoTrackId = videoSong
    ? (tracks.find((t) => t.name === videoSong.title) ?? tracks[chapterAt])?.id ?? ''
    : ''

  return (
    /* The whole page is the batch drop zone (#84): each file is routed to its
       track by name, and several files for one track land as several takes.
       The per-track editor keeps its own zone by stopping the bubble. */
    <div
      className={'space-y-5' + (droppingTakes ? ' rounded-lg ring-2 ring-accent' : '')}
      onDragOver={(event) => {
        event.preventDefault()
        setDroppingTakes(true)
      }}
      onDragLeave={() => setDroppingTakes(false)}
      onDrop={(event) => {
        event.preventDefault()
        setDroppingTakes(false)
        if (event.dataTransfer.files.length) {
          void importDroppedTakes(event.dataTransfer.files)
        }
      }}
    >
      <PageHeader
        // The draft's name, so a rename shows in the title the moment it is
        // committed rather than after the debounce lands (#80).
        title={draft.name.trim() ? draft.name : album.name}
        // The one place the record is renamed (#80, #97): it rides the same
        // draft-and-debounce as every other field on the page. The Record
        // tab's separate Name field went with the tab.
        onRename={(name) => update({name})}
        breadcrumb={
          <button
            className="hover:text-fg hover:underline"
            type="button"
            onClick={() => onOpenArtist(album.artistId)}
          >
            {album.artistName}
          </button>
        }
        subtitle={tracks.length === 1 ? '1 track' : tracks.length + ' tracks'}
        // The quick copy beside the name (#74): the album title is what gets
        // typed into Suno's playlist field, and retyping a name that is
        // already on screen is the kind of friction that adds up.
        titleAccessory={<CopyButton text={album.name} label="Copy the album name" />}
        actions={
          <>
            <button
              className="sm-button sm-button-quiet"
              type="button"
              onClick={() => void player.play('album', albumId)}
            >
              <Play size={15} />
              Play
            </button>
            {/* Behind what is already playing rather than over it (#60). */}
            <button
              className="sm-button sm-button-quiet"
              type="button"
              onClick={() => void player.enqueue('album', albumId)}
            >
              <ListPlus size={15} />
              Queue
            </button>
            {/* The finished thing, where it is seen (#143): once the video is
                on YouTube, watching it there is the page's main action. */}
            {album.videoYouTubeUrl && (
              <button
                className="sm-button sm-button-primary"
                type="button"
                onClick={() => void OpenURL(album.videoYouTubeUrl)}
              >
                <ExternalLink size={15} />
                Watch on YouTube
              </button>
            )}
            {/* In the header, where it is in reach from anywhere on the page
                (#84): the picked files are matched to tracks by name, the same
                as a drop anywhere on the page. */}
            <button
              className="sm-button sm-button-quiet"
              type="button"
              onClick={() => void pickTakes()}
            >
              <FolderInput size={15} />
              Import takes
            </button>
            {/* No Save button (#75) — just the word for what the debounce
                is doing, matching the artist page (#52). */}
            {saveState !== 'idle' && (
              <span className="text-xs text-fg-muted" role="status">
                {saveState === 'saving' ? 'Saving…' : 'Saved'}
              </span>
            )}
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
            Delete <strong>{album.name}</strong> and its {tracks.length} track
            {tracks.length === 1 ? '' : 's'}? The audio files stay in your music folder.
          </p>
          <div className="flex gap-2">
            <button
              className="sm-button sm-button-danger"
              type="button"
              onClick={async () => {
                try {
                  await DeleteAlbum(albumId)
                  onDeleted(album.artistId)
                } catch (err) {
                  setError(toMessage(err))
                }
              }}
            >
              Delete the album
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

      {/* The cover with the record that follows it on the left; on the right,
          the record's tracks, sound and release links as tabs (#110). #104 had
          the sound always in view above the tabs, capped with Show all; the
          producer asked for it back in the tab strip, where it has the room to
          be read whole. */}
      <div className="grid items-start gap-4 md:grid-cols-[232px_minmax(0,1fr)]">
        <div className="space-y-4">
          {/* The cover and the record's video in one panel (#145): the video
              plays in the cover's place, and its render button sits with the
              cover's own under the image. Always mounted, with no tab to hide
              it behind: a cover generation or a render runs for minutes and
              its busy state lives here. */}
          <section className="sm-panel space-y-3 p-4" aria-label="Artwork and music video">
            {watching && album.videoUrl && !rendering ? (
              /* The browser's own controls, and a way to full screen: at the
                 sidebar's width this is a preview (#143). Pausing goes back to
                 the cover (#145), and starting it stops the app's player, so
                 two records do not play over each other. */
              <div className="space-y-2">
                <video
                  ref={videoRef}
                  src={album.videoUrl}
                  poster={album.artworkUrl || undefined}
                  controls
                  autoPlay
                  className="w-full rounded bg-tile"
                  onPlay={() => {
                    if (player.playing) player.toggle()
                  }}
                  onPause={() => {
                    // Full screen is left on Escape with the video still
                    // playing; a pause is a pause wherever it happened.
                    if (document.fullscreenElement) void document.exitFullscreen().catch(() => {})
                    setWatching(false)
                  }}
                  onTimeUpdate={(event) => setVideoTime(event.currentTarget.currentTime)}
                />
                {videoSong && (
                  <p className="truncate text-xs text-fg-muted" aria-live="polite">
                    Now: <span className="text-fg">{videoSong.title}</span>
                  </p>
                )}
                <div className="flex gap-2">
                  <button
                    className="sm-button sm-button-quiet flex-1"
                    type="button"
                    onClick={() => void videoRef.current?.requestFullscreen().catch(() => {})}
                  >
                    <Maximize2 size={15} />
                    Full screen
                  </button>
                  {album.videoDescription && (
                    <button
                      className="sm-button sm-button-quiet px-2"
                      type="button"
                      title="The description"
                      aria-label="Show the video's description"
                      onClick={() => setShowDescription(true)}
                    >
                      <FileText size={15} />
                    </button>
                  )}
                </div>
              </div>
            ) : album.artworkFile ? (
              // A finished cover is looked at, not worked on (#114): replacing
              // or removing it, and reading the video's description (#145),
              // wait behind a hover on the image, and focus-within shows the
              // same controls to a keyboard. The play button stays in view.
              <div className="group relative w-fit">
                <Cover url={album.artworkUrl} name={album.name} size={200} />
                {album.videoUrl && !rendering && (
                  <button
                    className="sm-button sm-button-primary absolute left-1/2 top-1/2 z-10 h-12 w-12 -translate-x-1/2 -translate-y-1/2 rounded-full p-0 shadow-lg"
                    type="button"
                    aria-label="Play the music video"
                    title="Play the music video"
                    onClick={() => setWatching(true)}
                  >
                    <Play size={20} />
                  </button>
                )}
                <div className="absolute inset-0 flex items-end justify-center gap-2 rounded bg-scrim p-3 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
                  <button
                    className="sm-button sm-button-quiet"
                    type="button"
                    disabled={drawing}
                    onClick={pickArtwork}
                  >
                    <ImagePlus size={15} />
                    Replace
                  </button>
                  {album.videoDescription && !rendering && (
                    <button
                      className="sm-button sm-button-quiet px-2"
                      type="button"
                      title="The video's description"
                      aria-label="Show the video's description"
                      onClick={() => setShowDescription(true)}
                    >
                      <FileText size={15} />
                    </button>
                  )}
                  <button
                    className="sm-button sm-button-quiet"
                    type="button"
                    disabled={drawing}
                    aria-label="Remove the cover"
                    title="Remove the cover"
                    onClick={async () => {
                      try {
                        setAlbum(await RemoveAlbumArtwork(albumId))
                      } catch (err) {
                        setError(toMessage(err))
                      }
                    }}
                  >
                    <Trash2 size={15} />
                  </button>
                </div>
              </div>
            ) : (
              <Cover url={album.artworkUrl} name={album.name} size={200} />
            )}

            {!album.artworkFile ? (
              <>
                <div className="flex flex-wrap gap-2">
                  <button
                    className="sm-button sm-button-primary"
                    type="button"
                    onClick={draw}
                    disabled={drawing}
                  >
                    {drawing ? <Loader2 size={15} className="animate-spin" /> : <Wand2 size={15} />}
                    {drawing ? 'Drawing…' : 'Generate'}
                  </button>
                  <button
                    className="sm-button sm-button-quiet"
                    type="button"
                    disabled={drawing}
                    aria-label="Use an image file"
                    title="Use an image file"
                    onClick={pickArtwork}
                  >
                    <ImagePlus size={15} />
                  </button>
                </div>
                <TextField
                  label="Describe the cover"
                  hint="Or leave it blank and let the style decide."
                  value={feedback}
                  placeholder="A single lit window"
                  onChange={setFeedback}
                  disabled={drawing}
                />
              </>
            ) : changingArtwork && !drawing ? (
              <>
                <TextField
                  label="What to change"
                  hint="The rest of the cover is kept."
                  value={feedback}
                  placeholder="Colder, less busy"
                  onChange={setFeedback}
                />
                <div className="flex flex-wrap gap-2">
                  <button
                    className="sm-button sm-button-primary"
                    type="button"
                    onClick={() => {
                      draw()
                      setChangingArtwork(false)
                    }}
                  >
                    <RefreshCw size={15} />
                    Redraw
                  </button>
                  <button
                    className="sm-button sm-button-quiet"
                    type="button"
                    onClick={() => setChangingArtwork(false)}
                  >
                    Cancel
                  </button>
                </div>
              </>
            ) : (
              // The prompt waits behind its own button (#114), so a finished
              // record's sidebar is the cover and one way to change it.
              <button
                className="sm-button sm-button-quiet w-full"
                type="button"
                disabled={drawing}
                onClick={() => setChangingArtwork(true)}
              >
                {drawing ? <Loader2 size={15} className="animate-spin" /> : <Wand2 size={15} />}
                {drawing ? 'Drawing…' : 'Make changes with AI'}
              </button>
            )}
            {drawing && (
              <p className="text-xs text-fg-muted">
                This keeps running if you navigate away — watch the status bar.
              </p>
            )}
            {artworkJob?.state === 'failed' && (
              <p className="text-sm text-danger">{artworkJob.error}</p>
            )}

            {/* The record as one video (#118), its button under the cover's
                (#145). Says what is still missing rather than greying the
                button out with no reason, since each gap is something to go
                and fix. */}
            {(() => {
              const unmade = tracks.filter((t) => !t.files.some((f) => !f.missing))
              const missing = [
                !album.artworkFile && 'a cover',
                tracks.length === 0 && 'tracks',
                unmade.length > 0 &&
                  (unmade.length === 1
                    ? `a take of “${unmade[0].name}”`
                    : `takes of ${unmade.length} tracks`),
              ].filter(Boolean)
              return (
                <div className="space-y-1">
                  <div className="flex gap-2">
                    {/* min-w-0 so the button gives way rather than pushing the
                        folder button out of the 200px column (#147): buttons
                        do not wrap their text. */}
                    <button
                      className={
                        'sm-button min-w-0 flex-1 ' +
                        (album.videoFile ? 'sm-button-quiet' : 'sm-button-primary')
                      }
                      type="button"
                      disabled={rendering || missing.length > 0}
                      onClick={() =>
                        renderVideo({id: album.id, name: album.name, artistId: album.artistId})
                      }
                    >
                      {rendering ? <Loader2 size={15} className="animate-spin" /> : <Clapperboard size={15} />}
                      {rendering
                        ? `Rendering… ${Math.round((videoJob?.fraction ?? 0) * 100)}%`
                        : album.videoFile
                          ? 'Render again'
                          : 'Render the video'}
                    </button>
                    {album.videoFile && !rendering && (
                      <button
                        className="sm-button sm-button-quiet px-2"
                        type="button"
                        aria-label="Show the video in its folder"
                        title="Show the video in its folder"
                        onClick={() => void RevealTrackFile(album.videoFile).catch((err) => setError(toMessage(err)))}
                      >
                        <FolderOpen size={15} />
                      </button>
                    )}
                  </div>
                  {missing.length > 0 && !album.videoFile && (
                    <p className="text-xs text-fg-muted">
                      The video needs {missing.join(' and ')} first.
                    </p>
                  )}
                </div>
              )
            })()}
            {rendering && (
              <p className="text-xs text-fg-muted">
                This keeps running if you navigate away — watch the status bar.
              </p>
            )}
            {videoJob?.state === 'failed' && (
              <p className="text-sm text-danger">{videoJob.error}</p>
            )}
            {uploadJob?.state === 'failed' && (
              <p className="text-sm text-danger">{uploadJob.error}</p>
            )}
            {/* To the channel (#122), folded away (#143) and opened while an
                upload runs so its progress is not hidden. */}
            {album.videoFile && !rendering && (
              <>
                <button
                  className="flex w-full items-center gap-1 text-xs text-fg-muted hover:text-fg"
                  type="button"
                  aria-expanded={videoMore || uploading}
                  onClick={() => setVideoMore((was) => !was)}
                >
                  {videoMore || uploading ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
                  YouTube upload and stats
                </button>
                {(videoMore || uploading) && (
                  <div className="space-y-3">
                    {album.videoYouTubeUrl && youtubeConnected && <VideoStatsBlock albumId={album.id} />}
                    {youtubeConnected ? (
                      <>
                        <SelectField
                          label="Privacy"
                          value={privacy}
                          onChange={setPrivacy}
                          disabled={uploading}
                          options={[
                            {value: 'private', label: 'Private'},
                            {value: 'unlisted', label: 'Unlisted'},
                            {value: 'public', label: 'Public'},
                          ]}
                        />
                        <button
                          className="sm-button sm-button-quiet w-full"
                          type="button"
                          disabled={uploading}
                          onClick={() =>
                            uploadVideo({id: album.id, name: album.name, artistId: album.artistId}, privacy)
                          }
                        >
                          {uploading ? <Loader2 size={15} className="animate-spin" /> : <Upload size={15} />}
                          {uploading
                            ? `Uploading… ${Math.round((uploadJob?.fraction ?? 0) * 100)}%`
                            : album.videoYouTubeUrl
                              ? 'Upload again'
                              : 'Upload to YouTube'}
                        </button>
                        {/* Google keeps an unaudited project's uploads private,
                            and the privacy chosen here would otherwise look
                            ignored. */}
                        <p className="text-xs text-fg-muted">
                          Google keeps uploads from an unaudited OAuth project private until the
                          project passes its audit, whatever is chosen here.
                        </p>
                      </>
                    ) : (
                      <p className="text-xs text-fg-muted">
                        Connect YouTube in Settings → YouTube to upload this video.
                      </p>
                    )}
                  </div>
                )}
              </>
            )}
          </section>

          {/* The YouTube description (#120), written with the video because
              its timestamps are the render's — opened from the cover's hover
              or beside the playing video (#145). Read-only: a time typed over
              by hand would stop matching the video it describes. */}
          {showDescription && album.videoDescription && (
            <Modal title="The video's description" onClose={() => setShowDescription(false)}>
              <div className="space-y-2">
                <div className="flex justify-end">
                  <CopyButton text={album.videoDescription} label="Copy the description" />
                </div>
                <pre className="whitespace-pre-wrap rounded-md border border-edge bg-surface px-3 py-2 font-mono text-xs leading-relaxed text-fg-muted">
                  {album.videoDescription}
                </pre>
              </div>
            </Modal>
          )}

          {/* The tracks made through Suno's MCP server, beside the extension.
              Draws nothing unless Suno is authorized in Settings → MCP servers,
              so without it this page is what it was. */}
          <SunoMCPPanel albumId={album.id} tracks={tracks} />

          {/* The takes to the producer's own YouTube Music library (#124),
              tagged for the record. Says when it last went, because a second
              upload is a second copy in the library, not a replacement. Only
              once YouTube Music is connected (#145): before that the panel was
              a sentence pointing at Settings on every album. */}
          {ytmusicConnected && (
          <section className="sm-panel space-y-3 p-4" aria-label="YouTube Music">
            <h2 className="text-sm font-semibold">YouTube Music library</h2>
            {(() => {
              const unmade = tracks.filter((t) => !t.files.some((f) => !f.missing))
              return (
                <>
                  <p className="text-xs text-fg-muted">
                    {tracks.length === 0
                      ? 'Needs tracks first.'
                      : unmade.length > 0
                        ? `Needs a take of ${unmade.length === 1 ? `“${unmade[0].name}”` : `${unmade.length} tracks`} first.`
                        : album.ytmusicUploadedAt
                          ? `Uploaded ${new Date(album.ytmusicUploadedAt).toLocaleString()}. Uploading again adds second copies.`
                          : 'Every official take, tagged with its title, track number and the cover. Private to your account.'}
                  </p>
                  <button
                    className="sm-button sm-button-quiet w-full"
                    type="button"
                    disabled={toYTMusic || tracks.length === 0 || unmade.length > 0}
                    onClick={() =>
                      uploadToYouTubeMusic({id: album.id, name: album.name, artistId: album.artistId})
                    }
                  >
                    {toYTMusic ? <Loader2 size={15} className="animate-spin" /> : <Upload size={15} />}
                    {toYTMusic
                      ? `Uploading… ${Math.round((ytmusicJob?.fraction ?? 0) * 100)}%`
                      : album.ytmusicUploadedAt
                        ? 'Upload again'
                        : 'Upload to YouTube Music'}
                  </button>
                </>
              )
            })()}
            {ytmusicJob?.state === 'failed' && (
              <p className="text-sm text-danger">{ytmusicJob.error}</p>
            )}
          </section>
          )}

          {/* The successor (#42): a new record under the same artist, the
              technical style evolved one step and a fresh track list written to
              flow. Navigation happens only once the album exists — a failure
              leaves the producer here, with the message. */}
          <section className="sm-panel space-y-3 p-4">
            <h2 className="text-sm font-semibold">The next record</h2>
            <p className="text-xs text-fg-muted">
              A new album that evolves this one: the technical style moved a step forward, and
              a whole new track list styled so each song flows into the next. Lyrics stay yours
              — write them per track, or through the interview on the new record.
            </p>
            <button
              className="sm-button sm-button-primary"
              type="button"
              disabled={evolving}
              onClick={() => evolve({id: album.id, name: album.name, artistId: album.artistId})}
            >
              {evolving ? <Loader2 size={15} className="animate-spin" /> : <Sparkles size={15} />}
              {evolving ? 'Evolving…' : 'Create the next record'}
            </button>
            {evolving && (
              <p className="text-xs text-fg-muted">
                This keeps running if you navigate away — watch the status bar.
              </p>
            )}
            {evolution?.state === 'failed' && (
              <p className="text-sm text-danger">{evolution.error}</p>
            )}
          </section>
        </div>

        <div className="min-w-0 space-y-4">
          <TabStrip
            tabs={[
              {id: 'tracks', label: tracks.length ? 'Tracks (' + tracks.length + ')' : 'Tracks'},
              {id: 'sound', label: 'Sound'},
              {id: 'releases', label: 'Releases'},
              {id: 'takes', label: 'Takes'},
            ]}
            current={pageTab}
            onSelect={setPageTab}
          />
          {pageTab === 'sound' && (
            <section className="sm-panel flex flex-col gap-3 p-4" aria-label="The record's sound">
              <div className="flex flex-wrap items-center justify-end gap-2">
                {/* Back by request (#38, reversing #37), with the button that
                    earns its place: it distils the tracks' own style prompts into
                    the record's shared core. Into the draft, not the database —
                    the producer edits and the auto-save keeps it. */}
                {styleTab === 'style' && (
                  <button
                    className="sm-button sm-button-quiet"
                    type="button"
                    disabled={generating}
                    onClick={async () => {
                      setGenerating(true)
                      setError('')
                      try {
                        update({technicalStyle: await GenerateAlbumStyle(albumId)})
                      } catch (err) {
                        setError(toMessage(err))
                      } finally {
                        setGenerating(false)
                      }
                    }}
                  >
                    {generating ? <Loader2 size={15} className="animate-spin" /> : <Wand2 size={15} />}
                    {generating ? 'Distilling…' : 'Generate from the tracks'}
                  </button>
                )}
              </div>
              {/* Tabs rather than a two-column grid (#48): the two halves of
                  combinedStyle are read one at a time, and side by side they
                  halved the room the style is actually written in. */}
              <TabStrip
                tabs={[
                  {id: 'style', label: 'This record'},
                  {id: 'inherited', label: 'Inherited from ' + album.artistName},
                ]}
                current={styleTab}
                onSelect={setStyleTab}
              />
              {styleTab === 'style' && (
                /* Markdown with a preview (#44), matching the artist's base style:
                   the style is read far more often than written, and the rendered
                   form is what it is read in. */
                <MarkdownField
                  label=""
                  hint="Joined with the artist's base style when a track prompt is written."
                  placeholder="Analog synths, colder mix than the debut, 100-110 BPM…"
                  value={draft.technicalStyle}
                  onChange={(technicalStyle) => update({technicalStyle})}
                />
              )}
              {styleTab === 'inherited' && (
                /* The other half of combinedStyle, shown where the joining
                   happens (#40). Read-only: the base style belongs to the artist,
                   and two editable copies of it would drift. */
                <div className="flex flex-1 flex-col gap-1">
                  <pre className="flex-1 overflow-auto whitespace-pre-wrap rounded-md border border-edge bg-surface px-3 py-2 font-mono text-xs leading-relaxed text-fg-muted">
                    {album.artistBaseStyle || 'No base style written yet.'}
                  </pre>
                  <p className="text-xs text-fg-muted">Read-only here — edit it on the artist.</p>
                </div>
              )}
            </section>
          )}

          {pageTab === 'tracks' && (
            <section ref={tracksRef} className="scroll-mt-4 space-y-3" aria-label="Tracks">
              <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                {tracks.length > 0 && (
                  /* The batch import's words (#76), where the tracks the files land
                     on are in view; the button is in the header. */
                  <p className="text-xs text-fg-muted">
                    Drop a batch of takes anywhere on this page — each file lands on the track it
                    is named after.
                  </p>
                )}
              </div>
              {tracks.length === 0 ? (
                <div className="sm-panel space-y-3 p-4">
                  <EmptyState
                    icon={ListMusic}
                    title="No tracks yet"
                    body="Write the whole list from this record's style in one press, or talk it through — the interview knows the combined style and takes requests."
                  />
                  <div className="flex justify-center gap-2">
                    {/* The one-press repair (#71): a record with a style but no
                        tracks gets its list written directly, no second
                        conversation required. */}
                    <button
                      className="sm-button sm-button-primary"
                      type="button"
                      disabled={writingTracks}
                      onClick={async () => {
                        setWritingTracks(true)
                        setError('')
                        try {
                          await WriteAlbumTracks(albumId)
                          await reload()
                        } catch (err) {
                          setError(toMessage(err))
                        } finally {
                          setWritingTracks(false)
                        }
                      }}
                    >
                      {writingTracks ? <Loader2 size={15} className="animate-spin" /> : <Wand2 size={15} />}
                      {writingTracks ? 'Writing…' : 'Write the track list'}
                    </button>
                    <button
                      className="sm-button sm-button-quiet"
                      type="button"
                      onClick={() => setInterviewing(true)}
                    >
                      <Sparkles size={15} />
                      Talk it through
                    </button>
                  </div>
                </div>
              ) : (
                /* Master-detail (#40): the running order on the left, the selected
                   track edited in place on the right. Side by side only from xl now
                   that the cover takes the page's left column (#102) — at md the
                   editor would be left a sliver. */
                <div className="grid items-start gap-4 xl:grid-cols-[260px_minmax(0,1fr)]">
                  {/* Not overflow-hidden: a row's playlist menu hangs out of the
                      panel (#115). The first and last rows round themselves. */}
                  <div className="sm-panel">
                    <ul className="divide-y divide-edge">
                      {tracks.map((track) => (
                        <TrackRow
                          key={track.id}
                          track={track}
                          selected={track.id === selected?.id}
                          inVideo={track.id === videoTrackId}
                          onSelect={() => setSelectedId(track.id)}
                          onChanged={reload}
                          onDeleted={() => {
                            if (track.id === selected?.id) setSelectedId('')
                            reload()
                          }}
                        />
                      ))}
                    </ul>
                    <div className="p-2">
                      <button
                        className="flex w-full items-center justify-center gap-2 rounded-md border border-dashed border-edge py-1.5 text-xs text-fg-muted hover:border-accent hover:text-accent"
                        type="button"
                        onClick={() => setInterviewing(true)}
                      >
                        <Sparkles size={13} />
                        Write more tracks
                      </button>
                    </div>
                  </div>
                  {selected && (
                    <TrackEditor key={selected.id} trackId={selected.id} />
                  )}
                </div>
              )}
            </section>
          )}

          {/* Every track's takes on one tab (#144), in running order: choosing
              the official take is done by listening across the record, which
              one track's editor at a time made a round of clicks. */}
          {pageTab === 'takes' && (
            <section className="space-y-3" aria-label="Takes">
              {tracks.length === 0 ? (
                <div className="sm-panel p-4">
                  <EmptyState
                    icon={ListMusic}
                    title="No tracks yet"
                    body="Takes are filed on tracks. Write the track list first, then import or drop the files Suno gave you."
                  />
                </div>
              ) : (
                <>
                  <p className="text-xs text-fg-muted">
                    Drop a file on a track to add it there, or a batch anywhere on this page to
                    have each one land on the track it is named after.
                  </p>
                  <TakesList tracks={tracks} onChanged={() => void reload()} onError={setError} />
                </>
              )}
            </section>
          )}

          {pageTab === 'releases' && (
            <section className="sm-panel space-y-3 p-4" aria-label="Releases">
              <div>
                <h2 className="text-sm font-semibold">Released at</h2>
                <p className="mt-1 text-xs text-fg-muted">Filled in once the record is out.</p>
              </div>
              <div className="grid gap-3 md:grid-cols-3">
                <TextField
                  label="Apple Music"
                  type="url"
                  value={draft.appleMusicUrl}
                  onChange={(appleMusicUrl) => update({appleMusicUrl})}
                />
                <TextField
                  label="Spotify"
                  type="url"
                  value={draft.spotifyUrl}
                  onChange={(spotifyUrl) => update({spotifyUrl})}
                />
                <TextField
                  label="YouTube Music"
                  type="url"
                  value={draft.youtubeUrl}
                  onChange={(youtubeUrl) => update({youtubeUrl})}
                />
              </div>
            </section>
          )}
        </div>
      </div>

      {interviewing && (
        <InterviewDialog
          title={'Write tracks for ' + album.name}
          subjectKind="track"
          subjectId={album.id}
          opening={
            'Writing for **' +
            album.name +
            '**. I have the combined style and the ' +
            (tracks.length ? tracks.length + ' tracks already on it' : 'empty running order') +
            '.\n\nWhat is the next track — or tell me how many you want and what the ' +
            'record is about, and I will propose a set.'
          }
          finishLabel="Add tracks"
          onFinish={async (messages) => {
            try {
              const drafts = await ExtractTracks(messages)
              if (!drafts?.length) return 'The conversation did not settle on a track yet.'
              // Audio dropped into the chat, resolved from the names the
              // extraction assigned back to the stored paths (#24).
              const attached = attachmentPathsByName(messages)
              // Sequentially rather than in parallel: each save reads the highest
              // position to append after it, and five concurrent saves would all
              // read the same one and land on the same number.
              for (const track of drafts) {
                const saved = await SaveTrack(
                  app.Track.createFrom({
                    albumId: album.id,
                    name: track.name || 'Untitled',
                    prompt: track.prompt,
                    lyrics: track.lyrics,
                    targetSeconds: track.targetSeconds,
                    files: [],
                  }),
                )
                // Each assigned file lands as a variation through the same
                // import every other route uses, so it is copied into the music
                // library before ClearChat sweeps the chat's own copy.
                for (const name of track.files ?? []) {
                  const path = attached[name]
                  if (path) await ImportTrackFile(saved.id, path, '')
                }
              }
              await ClearChat('track', album.id).catch(() => {})
              setInterviewing(false)
              reload()
              return ''
            } catch (err) {
              return toMessage(err)
            }
          }}
          onClose={() => setInterviewing(false)}
        />
      )}
    </div>
  )
}

/**
 * One line of the running order, and everything done to the track as a whole
 * (#115): queue it, put it in a playlist, copy its title, rename it, delete
 * it. Those used to sit in a title card above the editor — a second copy of
 * the row the producer had just clicked — and now wait on the row's own hover,
 * with focus-within showing them to a keyboard.
 */
function TrackRow({
  track,
  selected,
  inVideo = false,
  onSelect,
  onChanged,
  onDeleted,
}: {
  track: app.Track
  selected: boolean
  /** The song the album's playing video is on (#145). */
  inVideo?: boolean
  onSelect: () => void
  onChanged: () => void
  onDeleted: () => void
}) {
  const player = usePlayer()
  const [mode, setMode] = useState<'idle' | 'editing' | 'deleting'>('idle')
  const [name, setName] = useState(track.name)
  const [lengthText, setLengthText] = useState(duration(track.targetSeconds))
  const [error, setError] = useState('')
  const playable = hasPlayableTake(track)
  // Whether the player is on this track, playing or paused.
  const current = player.queue[player.index]?.trackId === track.id

  const startEdit = () => {
    setName(track.name)
    setLengthText(duration(track.targetSeconds))
    setError('')
    setMode('editing')
  }
  const saveEdit = async () => {
    setError('')
    try {
      await SaveTrack(
        app.Track.createFrom({...track, name, targetSeconds: parseDuration(lengthText)}),
      )
      setMode('idle')
      onChanged()
    } catch (err) {
      setError(toMessage(err))
    }
  }
  const remove = async () => {
    setError('')
    try {
      await DeleteTrack(track.id)
      onDeleted()
    } catch (err) {
      setError(toMessage(err))
    }
  }

  // The panel no longer clips its rows (the playlist menu hangs out of it),
  // so the first and last rows carry its corners themselves.
  const corners = 'group-first/row:rounded-t-lg group-last/row:rounded-b-lg'

  if (mode === 'editing') {
    return (
      <li className="group/row">
        <div className={'space-y-2 bg-surface-hover px-3 py-2 ' + corners}>
          <input
            className="sm-input w-full"
            value={name}
            autoFocus
            aria-label="Track title"
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') void saveEdit()
              if (e.key === 'Escape') setMode('idle')
            }}
          />
          <div className="flex items-center gap-2">
            <input
              className="sm-input w-20 tabular-nums"
              title="Target length — m:ss or seconds"
              aria-label="Target length"
              value={lengthText}
              onChange={(e) => setLengthText(e.target.value)}
            />
            <button className="sm-button sm-button-primary" type="button" onClick={saveEdit}>
              Save
            </button>
            <button
              className="sm-button sm-button-quiet"
              type="button"
              onClick={() => setMode('idle')}
            >
              Cancel
            </button>
          </div>
          {error && <p className="text-xs text-danger">{error}</p>}
        </div>
      </li>
    )
  }

  if (mode === 'deleting') {
    return (
      <li className="group/row">
        <div className={'space-y-2 bg-surface-hover px-3 py-2 ' + corners}>
          <p className="text-sm">
            Delete <strong>{track.name}</strong>? The audio files stay in your music folder.
          </p>
          <div className="flex gap-2">
            <button className="sm-button sm-button-danger" type="button" onClick={remove}>
              Delete the track
            </button>
            <button
              className="sm-button sm-button-quiet"
              type="button"
              onClick={() => setMode('idle')}
            >
              Keep it
            </button>
          </div>
          {error && <p className="text-xs text-danger">{error}</p>}
        </div>
      </li>
    )
  }

  return (
    <li className="group/row">
      {/* The row with an open menu is lifted above its neighbours (#126). The
          menu's own z-index cannot do it: the button cluster's translate makes
          it a stacking context, so the menu only outranks its own cluster, and
          every later row — positioned, and painted after — covered it. */}
      <div
        className={
          'group relative flex items-center gap-2 px-3 py-2 has-[[aria-expanded=true]]:z-30 ' +
          corners +
          ' ' +
          (selected ? 'bg-surface-hover' : 'hover:bg-surface-hover')
        }
      >
        {/* Play this track and only this track (#106), shown on every row
            that has a take to play rather than swapped in over the number on
            hover, where it was easy to miss. The album's Play in the header is
            the whole record. A row with nothing to play keeps the slot so the
            numbers stay in one column.

            The track the player is on answers for the player (#142): Pause
            while it plays, and Play resumes it where it stopped rather than
            starting it over, which is what pressing play on a paused song
            means. In the accent, so the row that is playing is findable. */}
        {playable ? (
          <button
            type="button"
            aria-label={`${current && player.playing ? 'Pause' : 'Play'} ${track.name}`}
            title={current && player.playing ? 'Pause' : current ? 'Resume' : 'Play this track'}
            className={
              'flex w-6 shrink-0 items-center justify-center transition-colors hover:text-accent ' +
              (current ? 'text-accent' : 'text-fg-muted')
            }
            onClick={() => (current ? player.toggle() : void player.play('track', track.id))}
          >
            {current && player.playing ? <Pause size={14} /> : <Play size={14} />}
          </button>
        ) : (
          <span className="w-6 shrink-0" aria-hidden="true" />
        )}
        <button
          className="flex min-w-0 flex-1 items-center gap-2 text-left"
          type="button"
          onClick={onSelect}
        >
          {/* The song the album's video is on as it plays (#145), in place
              of its number so the column does not move. */}
          <span
            className={
              'flex w-5 shrink-0 justify-center text-xs tabular-nums ' +
              (inVideo ? 'text-accent' : 'text-fg-muted')
            }
            title={inVideo ? 'Playing in the video' : undefined}
          >
            {inVideo ? <Clapperboard size={13} aria-label="Playing in the video" /> : track.position}
          </span>
          <span className="min-w-0 flex-1">
            <span className="block truncate text-sm font-medium">{track.name}</span>
            <span className="block text-xs text-fg-muted">{takeSummary(track)}</span>
          </span>
          <span className="shrink-0 text-xs tabular-nums text-fg-muted">
            {duration(track.targetSeconds)}
          </span>
        </button>
        {/* Over the row's right end rather than beside it: the column is
            260px, and room held open for buttons that only show on hover
            would truncate every title. The row's own background behind them
            keeps the title and length from showing through. Held shown while
            the playlist menu is open, which lives inside it: WebKit does not
            focus a clicked button, so focus-within alone would fade the menu
            out the moment the pointer left the row on its way into it. */}
        <span className="pointer-events-none absolute right-2 top-1/2 flex -translate-y-1/2 items-center gap-0.5 rounded-md bg-surface-hover opacity-0 transition-opacity group-focus-within:pointer-events-auto group-focus-within:opacity-100 group-hover:pointer-events-auto group-hover:opacity-100 has-[[aria-expanded=true]]:pointer-events-auto has-[[aria-expanded=true]]:opacity-100">
          {playable && (
            <button
              className="sm-button sm-button-quiet border-0 px-1.5 py-1"
              type="button"
              title="Add this track to the queue"
              aria-label="Add this track to the queue"
              onClick={() => void player.enqueue('track', track.id)}
            >
              <ListPlus size={13} />
            </button>
          )}
          {/* Into a playlist (#107) whether or not a take exists yet: a
              playlist can be planned ahead of the songs being made. */}
          <AddToPlaylistMenu trackId={track.id} align="right" />
          {/* The title is the third thing Suno asks for beside the prompt and
              lyrics, and those two already have Copy (#74). */}
          <CopyButton text={track.name} label="Copy the track title" />
          <button
            className="sm-button sm-button-quiet border-0 px-1.5 py-1"
            type="button"
            title="Rename, or change the target length"
            aria-label="Edit the title and length"
            onClick={startEdit}
          >
            <Pencil size={13} />
          </button>
          <button
            className="sm-button sm-button-quiet border-0 px-1.5 py-1"
            type="button"
            title="Delete this track"
            aria-label="Delete this track"
            onClick={() => setMode('deleting')}
          >
            <Trash2 size={13} />
          </button>
        </span>
      </div>
    </li>
  )
}

/**
 * How the album's uploaded video is doing (#123). Read when asked rather than
 * on every visit to the album: each read spends the producer's YouTube API
 * quota, and the report behind the watch time only moves once a day.
 */
function VideoStatsBlock({albumId}: {albumId: string}) {
  const [stats, setStats] = useState<youtube.Stats | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const load = async () => {
    setLoading(true)
    setError('')
    try {
      setStats(await GetAlbumVideoStats(albumId))
    } catch (err) {
      setError(toMessage(err))
    } finally {
      setLoading(false)
    }
  }

  const count = (n: number) => n.toLocaleString()
  return (
    <div className="space-y-2">
      <button className="sm-button sm-button-quiet w-full" type="button" disabled={loading} onClick={load}>
        {loading ? <Loader2 size={15} className="animate-spin" /> : <BarChart3 size={15} />}
        {stats ? 'Refresh the numbers' : 'How is it doing?'}
      </button>
      {stats && (
        <dl className="grid grid-cols-2 gap-x-3 gap-y-1 text-xs">
          <dt className="text-fg-muted">Views</dt>
          <dd className="text-right tabular-nums">{count(stats.views)}</dd>
          <dt className="text-fg-muted">Likes</dt>
          <dd className="text-right tabular-nums">{count(stats.likes)}</dd>
          <dt className="text-fg-muted">Comments</dt>
          <dd className="text-right tabular-nums">{count(stats.comments)}</dd>
          {stats.analyticsAvailable && (
            <>
              <dt className="text-fg-muted">Watched</dt>
              <dd className="text-right tabular-nums">{count(Math.round(stats.minutesWatched / 60))} h</dd>
              <dt className="text-fg-muted">Average view</dt>
              <dd className="text-right tabular-nums">{duration(stats.averageViewSeconds)}</dd>
              <dt className="text-fg-muted">Subscribers gained</dt>
              <dd className="text-right tabular-nums">{count(stats.subscribersGained)}</dd>
            </>
          )}
        </dl>
      )}
      {stats && !stats.analyticsAvailable && !stats.analyticsError && (
        <p className="text-xs text-fg-muted">
          Watch time arrives two or three days after the upload — the counts above are live.
        </p>
      )}
      {stats?.analyticsError && (
        <p className="text-xs text-danger">Watch time could not be read: {stats.analyticsError}</p>
      )}
      {error && <p className="text-xs text-danger">{error}</p>}
    </div>
  )
}

/**
 * The songs of a music video's description (#120), each with where it starts
 * in seconds: the "M:SS Title" or "H:MM:SS Title" lines the render wrote.
 */
function videoChapters(description: string): {start: number; title: string}[] {
  const out: {start: number; title: string}[] = []
  for (const line of (description || '').split('\n')) {
    const match = line.match(/^(\d+):(\d{2})(?::(\d{2}))?\s+(.+)$/)
    if (!match) continue
    const [, a, b, c, title] = match
    const start = c !== undefined ? +a * 3600 + +b * 60 + +c : +a * 60 + +b
    out.push({start, title: title.trim()})
  }
  return out
}
