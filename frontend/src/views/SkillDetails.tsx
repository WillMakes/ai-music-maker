import {useCallback, useEffect, useState} from 'react'
import {Eye, PencilLine, RotateCcw, Trash2} from 'lucide-react'
import {DeleteSkill, GetSkill, ResetSkill, SaveSkill} from '../../wailsjs/go/main/App'
import {main} from '../../wailsjs/go/models'
import {Markdown} from '../components/Markdown'
import {PageHeader} from '../components/PageHeader'
import {StatusPill} from '../components/StatusPill'
import {TextAreaField, TextField} from '../components/Field'
import {toMessage} from '../lib/errors'

/**
 * One skill, read or edited.
 *
 * It opens in reading mode. A skill is a document — several hundred words of
 * instruction — and the common reason to open one is to find out what the app is
 * telling the model, not to rewrite it. Opening in a textarea would put a
 * scrollbar and a monospace face between the producer and their own prose.
 */
export function SkillDetails({skillId, onBack}: {skillId: string; onBack: () => void}) {
  const [skill, setSkill] = useState<main.Skill | null>(null)
  const [draft, setDraft] = useState<main.Skill | null>(null)
  const [editing, setEditing] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  const reload = useCallback(() => {
    GetSkill(skillId)
      .then((found) => {
        setSkill(found)
        setDraft(main.Skill.createFrom(found))
        setError('')
      })
      .catch((err) => setError(toMessage(err)))
  }, [skillId])

  useEffect(reload, [reload])

  if (!skill || !draft) {
    return <p className="text-sm text-fg-muted">{error || 'Loading…'}</p>
  }

  const update = (patch: Partial<main.Skill>) =>
    setDraft((current) => (current ? main.Skill.createFrom({...current, ...patch}) : current))

  const save = async () => {
    setSaving(true)
    setError('')
    try {
      const saved = await SaveSkill(draft)
      setSkill(saved)
      setDraft(main.Skill.createFrom(saved))
      setEditing(false)
    } catch (err) {
      setError(toMessage(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title={skill.title}
        breadcrumb="Skills"
        subtitle={
          <span className="flex flex-wrap items-center gap-2">
            <span className="font-mono text-xs">{skill.id}</span>
            {skill.builtIn && <StatusPill tone="muted">Application skill</StatusPill>}
            {skill.overridden && <StatusPill tone="warn">Edited</StatusPill>}
          </span>
        }
        actions={
          <>
            {skill.builtIn && skill.overridden && (
              <button
                className="sm-button sm-button-quiet"
                type="button"
                onClick={async () => {
                  try {
                    const reset = await ResetSkill(skill.id)
                    setSkill(reset)
                    setDraft(main.Skill.createFrom(reset))
                    setEditing(false)
                  } catch (err) {
                    setError(toMessage(err))
                  }
                }}
              >
                <RotateCcw size={15} />
                Reset to the default
              </button>
            )}
            {!skill.builtIn && (
              <button
                className="sm-button sm-button-danger"
                type="button"
                onClick={async () => {
                  try {
                    await DeleteSkill(skill.id)
                    onBack()
                  } catch (err) {
                    setError(toMessage(err))
                  }
                }}
              >
                <Trash2 size={15} />
                Delete
              </button>
            )}
            <button
              className="sm-button sm-button-quiet"
              type="button"
              onClick={() => {
                // Leaving edit mode throws the draft away rather than keeping it
                // — "stop editing" is how somebody abandons a change, and
                // silently holding it would have it saved by the next Save.
                setDraft(main.Skill.createFrom(skill))
                setEditing((on) => !on)
              }}
            >
              {editing ? <Eye size={15} /> : <PencilLine size={15} />}
              {editing ? 'Read it' : 'Edit'}
            </button>
            {editing && (
              <button
                className="sm-button sm-button-primary"
                type="button"
                onClick={save}
                disabled={saving}
              >
                {saving ? 'Saving…' : 'Save'}
              </button>
            )}
          </>
        }
      />

      {error && <p className="text-sm text-danger">{error}</p>}

      {editing ? (
        <section className="sm-panel space-y-4 p-4">
          {!skill.builtIn && (
            <div className="grid gap-4 md:grid-cols-2">
              <TextField
                label="Title"
                value={draft.title}
                onChange={(title) => update({title})}
              />
              <TextField
                label="Description"
                hint="One line, for the list."
                value={draft.description}
                onChange={(description) => update({description})}
              />
            </div>
          )}
          {skill.builtIn && (
            <p className="text-xs text-fg-muted">
              The title and description stay the build's — they name the slot the app
              looks this skill up by. The body below is yours.
            </p>
          )}
          <TextAreaField
            label="Body"
            mono
            rows={28}
            value={draft.body}
            onChange={(body) => update({body})}
          />
        </section>
      ) : (
        <section className="sm-panel p-5">
          {skill.body ? (
            <Markdown>{skill.body}</Markdown>
          ) : (
            <p className="text-sm text-fg-muted">Nothing written yet.</p>
          )}
        </section>
      )}
    </div>
  )
}
