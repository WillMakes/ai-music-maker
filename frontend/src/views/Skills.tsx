import {useCallback, useEffect, useState} from 'react'
import {Plus, Sparkles} from 'lucide-react'
import {ListSkills, SaveSkill} from '../../wailsjs/go/app/App'
import {app} from '../../wailsjs/go/models'
import {PageHeader} from '../components/PageHeader'
import {StatusPill} from '../components/StatusPill'
import {useDataChanged} from '../lib/dataChanged'
import {toMessage} from '../lib/errors'

/**
 * The app's own briefs.
 *
 * Two groups, and the split is not cosmetic: an Application Skill is a slot the
 * app looks up by id when it runs a feature, so editing one changes how the app
 * behaves and resetting it puts the build's own text back. A written skill is
 * the producer's, belongs to nothing, and is theirs to delete.
 */
export function Skills({onOpen}: {onOpen: (skillId: string) => void}) {
  const [skills, setSkills] = useState<app.Skill[]>([])
  const [error, setError] = useState('')

  const reload = useCallback(() => {
    ListSkills()
      .then((rows) => {
        setSkills(rows ?? [])
        setError('')
      })
      .catch((err) => setError(toMessage(err)))
  }, [])

  useEffect(reload, [reload])
  useDataChanged(['skills'], reload)

  const builtIn = skills.filter((skill) => skill.builtIn)
  const written = skills.filter((skill) => !skill.builtIn)

  return (
    <div className="space-y-5">
      <PageHeader
        title="Skills"
        subtitle="What the app tells a model to do. Edit one and the next interview follows the edit."
        actions={
          <button
            className="sm-button sm-button-primary"
            type="button"
            onClick={async () => {
              try {
                const skill = await SaveSkill(
                  app.Skill.createFrom({
                    title: 'New skill',
                    description: '',
                    body: '',
                    tags: [],
                  }),
                )
                onOpen(skill.id)
              } catch (err) {
                setError(toMessage(err))
              }
            }}
          >
            <Plus size={15} />
            Write a skill
          </button>
        }
      />

      {error && <p className="text-sm text-danger">{error}</p>}

      <Group title="Application skills" skills={builtIn} onOpen={onOpen} />
      {written.length > 0 && (
        <Group title="Your own" skills={written} onOpen={onOpen} />
      )}
    </div>
  )
}

function Group({
  title,
  skills,
  onOpen,
}: {
  title: string
  skills: app.Skill[]
  onOpen: (skillId: string) => void
}) {
  return (
    <section className="space-y-3">
      <h2 className="text-sm font-semibold">{title}</h2>
      <ul className="sm-panel divide-y divide-edge overflow-hidden">
        {skills.map((skill) => (
          <li key={skill.id}>
            <button
              className="flex w-full items-center gap-3 px-4 py-3 text-left hover:bg-surface-hover"
              type="button"
              onClick={() => onOpen(skill.id)}
            >
              <Sparkles className="shrink-0 text-fg-muted" size={16} />
              <span className="min-w-0 flex-1">
                <span className="block truncate font-medium">{skill.title}</span>
                <span className="block truncate text-xs text-fg-muted">
                  {skill.description || skill.id}
                </span>
              </span>
              {skill.overridden && <StatusPill tone="warn">Edited</StatusPill>}
            </button>
          </li>
        ))}
      </ul>
    </section>
  )
}
