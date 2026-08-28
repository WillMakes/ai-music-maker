import {useId, type ReactNode} from 'react'

/**
 * The app's form controls.
 *
 * A view that needs an input uses one of these rather than styling its own —
 * which is what keeps every field in the app the same field. The classes live in
 * style.css (`.sm-input`); nothing here names a colour.
 *
 * Each generates its own id and wires the label to it, so a label click focuses
 * the control without every caller inventing an id.
 */

interface FieldShell {
  label: string
  /** Shown under the control, in muted text. What the field is for. */
  hint?: ReactNode
  /** Shown under the control in danger text, replacing the hint. */
  error?: string
  required?: boolean
}

function Shell({
  label,
  hint,
  error,
  required,
  id,
  children,
}: FieldShell & {id: string; children: ReactNode}) {
  return (
    <div className="flex flex-col gap-1">
      <label className="text-sm font-medium" htmlFor={id}>
        {label}
        {required && <span className="text-fg-muted"> *</span>}
      </label>
      {children}
      {error ? (
        <p className="text-xs text-danger">{error}</p>
      ) : hint ? (
        <p className="text-xs text-fg-muted">{hint}</p>
      ) : null}
    </div>
  )
}

export function TextField({
  value,
  onChange,
  placeholder,
  disabled,
  type = 'text',
  ...shell
}: FieldShell & {
  value: string
  onChange: (next: string) => void
  placeholder?: string
  disabled?: boolean
  type?: 'text' | 'url' | 'number'
}) {
  const id = useId()
  return (
    <Shell {...shell} id={id}>
      <input
        className="sm-input"
        id={id}
        type={type}
        value={value}
        placeholder={placeholder}
        disabled={disabled}
        onChange={(event) => onChange(event.target.value)}
      />
    </Shell>
  )
}

export function TextAreaField({
  value,
  onChange,
  placeholder,
  rows = 5,
  disabled,
  mono,
  ...shell
}: FieldShell & {
  value: string
  onChange: (next: string) => void
  placeholder?: string
  rows?: number
  disabled?: boolean
  /**
   * Monospaced, for the fields read as structured text rather than as prose: a
   * technical style breakdown and a lyric sheet both line up in a way a
   * proportional face hides.
   */
  mono?: boolean
}) {
  const id = useId()
  return (
    <Shell {...shell} id={id}>
      <textarea
        className={'sm-input resize-y' + (mono ? ' font-mono text-xs' : '')}
        id={id}
        rows={rows}
        value={value}
        placeholder={placeholder}
        disabled={disabled}
        onChange={(event) => onChange(event.target.value)}
      />
    </Shell>
  )
}

export function SelectField({
  value,
  onChange,
  options,
  disabled,
  ...shell
}: FieldShell & {
  value: string
  onChange: (next: string) => void
  options: {value: string; label: string}[]
  disabled?: boolean
}) {
  const id = useId()
  return (
    <Shell {...shell} id={id}>
      <select
        className="sm-input"
        id={id}
        value={value}
        disabled={disabled}
        onChange={(event) => onChange(event.target.value)}
      >
        {options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
    </Shell>
  )
}
