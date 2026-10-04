import {useCallback, useEffect, useState} from 'react'
import {FolderOpen} from 'lucide-react'
import {MusicRoot, SetMusicRoot} from '../../wailsjs/go/app/App'
import {TextField} from '../components/Field'
import {SETTING_KEYS, loadSetting, saveSetting} from '../lib/settings'
import {duration, parseDuration} from '../lib/format'
import {toMessage} from '../lib/errors'

/**
 * Settings → Library (#34): the preferences that shape the records
 * themselves — where the audio lives, what a new track starts at, and the
 * gap between songs in an album's music video (#118).
 *
 * They used to sit under the Skills tab, which was where a settings page had
 * room rather than where anybody would look. The redesign gives them their own
 * tab named for what they configure.
 */
export function LibraryPanel() {
  const [musicRoot, setRoot] = useState('')
  const [rootDraft, setRootDraft] = useState('')
  const [length, setLength] = useState('')
  const [gap, setGap] = useState('')
  const [gapSaved, setGapSaved] = useState(false)
  const [error, setError] = useState('')

  const reload = useCallback(() => {
    MusicRoot()
      .then(setRoot)
      .catch(() => setRoot(''))
    loadSetting(SETTING_KEYS.defaultTrackLength).then((value) =>
      setLength(duration(Number(value) || 180)),
    )
    // Unset is one second, the number #117 asked for; the backend falls back
    // to the same when the setting is not a number.
    loadSetting(SETTING_KEYS.musicVideoGap).then((value) => setGap(value?.trim() || '1'))
  }, [])

  useEffect(reload, [reload])

  return (
    <div className="space-y-5">
      <section className="sm-panel space-y-3 p-4">
        <h2 className="flex items-center gap-2 text-sm font-semibold">
          <FolderOpen size={15} />
          Where the music goes
        </h2>
        <p className="text-sm text-fg-muted">
          Imported files are copied here, under Artist/Album. Outside this app's own data
          folder on purpose: the audio should outlive an uninstall and be findable by a
          music player.
        </p>
        <TextField
          label="Music folder"
          hint="Leave it blank for your operating system's music folder."
          value={rootDraft || musicRoot}
          onChange={setRootDraft}
        />
        <button
          className="sm-button sm-button-quiet"
          type="button"
          disabled={!rootDraft || rootDraft === musicRoot}
          onClick={async () => {
            try {
              const applied = await SetMusicRoot(rootDraft)
              setRoot(applied)
              setRootDraft('')
              setError('')
            } catch (err) {
              setError(toMessage(err))
            }
          }}
        >
          Use this folder
        </button>
      </section>

      <section className="sm-panel space-y-3 p-4">
        <h2 className="text-sm font-semibold">New tracks</h2>
        <TextField
          label="Default target length"
          hint="m:ss or seconds. What a new track starts at before you change it."
          value={length}
          onChange={setLength}
        />
        <button
          className="sm-button sm-button-quiet"
          type="button"
          onClick={() =>
            void saveSetting(
              SETTING_KEYS.defaultTrackLength,
              String(parseDuration(length) || 180),
            )
          }
        >
          Save
        </button>
      </section>

      <section className="sm-panel space-y-3 p-4">
        <h2 className="text-sm font-semibold">Music videos</h2>
        <TextField
          label="Gap between songs"
          hint="Seconds of silence between one song and the next, 0 to 30. Decimals are fine."
          type="number"
          value={gap}
          onChange={(next) => {
            setGap(next)
            setGapSaved(false)
          }}
        />
        <button
          className="sm-button sm-button-quiet"
          type="button"
          onClick={async () => {
            const seconds = Number(gap)
            if (!Number.isFinite(seconds) || seconds < 0 || seconds > 30) {
              setError('The gap between songs is a number of seconds from 0 to 30.')
              return
            }
            setError('')
            await saveSetting(SETTING_KEYS.musicVideoGap, String(seconds))
            setGapSaved(true)
          }}
        >
          {gapSaved ? 'Saved' : 'Save'}
        </button>
      </section>

      {error && <p className="text-sm text-danger">{error}</p>}
    </div>
  )
}
