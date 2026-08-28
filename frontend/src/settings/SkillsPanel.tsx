import {useCallback, useEffect, useState} from 'react'
import {ArrowRight, FolderOpen} from 'lucide-react'
import {ListSkills, MusicRoot, SetMusicRoot} from '../../wailsjs/go/main/App'
import {main} from '../../wailsjs/go/models'
import {StatusPill} from '../components/StatusPill'
import {TextField} from '../components/Field'
import {SETTING_KEYS, loadSetting, saveSetting} from '../lib/settings'
import {duration, parseDuration} from '../lib/format'
import {toMessage} from '../lib/errors'

/**
 * Settings → Skills: the briefs, plus the two preferences that shape what they
 * produce.
 *
 * The skills themselves are edited on their own pages — this is the way in, and
 * the place that says which have been edited. Putting the whole editor in a
 * settings tab would mean a settings page with a thirty-line textarea in it.
 */
export function SkillsPanel({onOpenSkill}: {onOpenSkill: (skillId: string) => void}) {
  const [skills, setSkills] = useState<main.Skill[]>([])
  const [musicRoot, setRoot] = useState('')
  const [rootDraft, setRootDraft] = useState('')
  const [length, setLength] = useState('')
  const [error, setError] = useState('')

  const reload = useCallback(() => {
    ListSkills()
      .then((rows) => setSkills(rows ?? []))
      .catch((err) => setError(toMessage(err)))
    MusicRoot()
      .then(setRoot)
      .catch(() => setRoot(''))
    loadSetting(SETTING_KEYS.defaultTrackLength).then((value) =>
      setLength(duration(Number(value) || 180)),
    )
  }, [])

  useEffect(reload, [reload])

  return (
    <div className="space-y-5">
      <section className="space-y-3">
        <div>
          <h2 className="text-sm font-semibold">Skills</h2>
          <p className="mt-1 text-sm text-fg-muted">
            What the app tells a model to do. Edit one and the next interview follows the
            edit; reset it and the build's own text comes back.
          </p>
        </div>
        <ul className="sm-panel divide-y divide-edge overflow-hidden">
          {skills
            .filter((skill) => skill.builtIn)
            .map((skill) => (
              <li key={skill.id}>
                <button
                  className="flex w-full items-center gap-3 px-4 py-3 text-left hover:bg-surface-hover"
                  type="button"
                  onClick={() => onOpenSkill(skill.id)}
                >
                  <span className="min-w-0 flex-1">
                    <span className="block font-medium">{skill.title}</span>
                    <span className="block truncate text-xs text-fg-muted">
                      {skill.description}
                    </span>
                  </span>
                  {skill.overridden && <StatusPill tone="warn">Edited</StatusPill>}
                  <ArrowRight className="shrink-0 text-fg-muted" size={15} />
                </button>
              </li>
            ))}
        </ul>
      </section>

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

      {error && <p className="text-sm text-danger">{error}</p>}
    </div>
  )
}
