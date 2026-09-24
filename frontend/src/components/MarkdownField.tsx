import {
  Bold,
  Check,
  Code,
  Copy,
  Heading2,
  Italic,
  Link2,
  List,
  ListOrdered,
  Pencil,
  Quote,
  Strikethrough,
  type LucideIcon,
} from 'lucide-react'
import {useEffect, useLayoutEffect, useRef, useState} from 'react'
import {Markdown} from './Markdown'

/**
 * A markdown field with two modes (#32): the rendered prose with a quiet Edit
 * button, or a toolbar over a textarea. Ported whole from Site Maker Pro,
 * which is what the report asked for; the issue numbers in the comments below
 * are that project's. An empty value opens in edit mode — there is
 * nothing to read yet — and a filled one opens as what it says.
 *
 * The toolbar writes markdown around the selection rather than hiding it: the
 * value is stored as markdown and read back by `Markdown`, so a true rich-text
 * surface would have to round-trip through a syntax it cannot fully represent.
 * Buttons for the marks people forget the syntax for, the text itself for
 * everything else.
 *
 * The Edit button is faded rather than absent so it holds its place in the tab
 * order — a control that only exists under a pointer is one a keyboard can
 * never reach. `Field.tsx` hides its reveal the same way.
 */
export function MarkdownField({
  label,
  value,
  onChange,
  placeholder,
  hint,
  onDone,
  copyable,
  autoEdit,
  autoFocus,
  collapsedHeight,
  fill,
}: {
  label: string
  value: string
  onChange: (value: string) => void
  placeholder?: string
  /** Guidance shown under the control while it is being edited. */
  hint?: string
  /** Editing finished — where a caller saves what was typed. */
  onDone?: () => void
  /**
   * Open in edit mode whatever the value (#501). The default opens a filled
   * value as prose, which is right where a document is read more than written;
   * a form whose whole purpose is to write — the bug report — passes this so it
   * lands on the editor rather than an empty-looking preview the reporter then
   * has to click into. Pair it with a key that changes each time the surface is
   * shown, so a field left in preview last time reopens in edit.
   */
  autoEdit?: boolean
  /**
   * Offer the markdown itself on the clipboard, beside Edit.
   *
   * The source rather than the rendering, because what anybody wants a
   * document out of this app for is to paste it somewhere that renders
   * markdown — a pull request, an issue, another editor — and a copy of the
   * rendering arrives there as one long paragraph.
   */
  copyable?: boolean
  /**
   * Put the caret in the editor as soon as it appears, so somebody who opened
   * this to write can start typing without clicking first (#519). Meant for a
   * field remounted each time its dialog opens, so the focus lands on each open.
   */
  autoFocus?: boolean
  /**
   * Cap the rendered prose at this many pixels, with Show all to open it
   * (#98). For a field that shares a row with something of fixed height: a
   * long value otherwise pushes the row open and leaves a gap beside it as
   * tall as the overflow. Only the reading view is capped — the editor keeps
   * its own size, because hiding text somebody is writing is worse than a gap.
   */
  collapsedHeight?: number
  /**
   * Grow the reading view to the height its flex parent leaves (#98), so a
   * short value in a tall panel reads as a box of text rather than a box with
   * an empty strip of panel under it.
   */
  fill?: boolean
}) {
  const [editing, setEditing] = useState(autoEdit || !value.trim())
  const [copied, setCopied] = useState(false)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const proseRef = useRef<HTMLDivElement>(null)
  const [expanded, setExpanded] = useState(false)
  const [overflows, setOverflows] = useState(false)

  // Whether the capped prose actually hides anything, so a short value gets
  // no toggle and no fade. Measured rather than guessed from the length: the
  // answer depends on the width the field is given, which changes with the
  // window. Only measured while collapsed — expanded, the cap is off and the
  // comparison would always say it fits.
  useLayoutEffect(() => {
    const el = proseRef.current
    if (!el || !collapsedHeight || expanded) return
    const measure = () => setOverflows(el.scrollHeight > el.clientHeight + 1)
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(el)
    return () => observer.disconnect()
  }, [collapsedHeight, expanded, value, editing])

  // Focus the editor on open when asked, with the caret at the end so editing an
  // existing value continues where it left off rather than before its first
  // character. Only while editing — there is no textarea to focus in preview.
  useEffect(() => {
    if (!autoFocus || !editing) return
    const el = textareaRef.current
    if (!el) return
    el.focus()
    el.setSelectionRange(el.value.length, el.value.length)
  }, [autoFocus, editing])

  // Long enough to read, short enough that the button is a button again before
  // anybody wants to press it twice.
  useEffect(() => {
    if (!copied) return
    const timer = setTimeout(() => setCopied(false), 1600)
    return () => clearTimeout(timer)
  }, [copied])

  if (!editing) {
    return (
      <div className={'flex flex-col gap-1' + (fill ? ' flex-1' : '')}>
        <span className="text-sm font-medium">{label}</span>
        <div
          className={
            'group relative rounded-lg border border-edge bg-bg px-4 py-3' + (fill ? ' flex-1' : '')
          }
        >
        <div
          ref={proseRef}
          className="relative overflow-hidden"
          style={collapsedHeight && !expanded ? {maxHeight: collapsedHeight} : undefined}
        >
          <Markdown>{value}</Markdown>
          {/* A fade into the field's own background says the text goes on, so
              the cut line never reads as the end of it. */}
          {overflows && !expanded && (
            <div className="pointer-events-none absolute inset-x-0 bottom-0 h-10 bg-gradient-to-t from-bg to-transparent" />
          )}
        </div>
        {overflows && (
          <button
            type="button"
            onClick={() => setExpanded((open) => !open)}
            aria-expanded={expanded}
            className="mt-1 text-xs font-semibold text-accent hover:underline"
          >
            {expanded ? 'Show less' : 'Show all'}
          </button>
        )}
        {/* Both controls in one corner, revealed together. They are the two
            things anybody does to a document they are looking at, and a button
            that is always there over prose is a button in the way of reading
            it. Kept in the DOM rather than mounted on hover, so a keyboard can
            reach them (#107). */}
        <div className="absolute right-2 top-2 flex items-center gap-1">
          {copyable && (
            <button
              type="button"
              onClick={() => {
                // Clipboard access can be refused; saying nothing would look
                // like the button did nothing, so the label only changes on
                // success.
                navigator.clipboard
                  .writeText(value)
                  .then(() => setCopied(true))
                  .catch(() => setCopied(false))
              }}
              aria-label={`Copy the ${label.toLowerCase()}`}
              className="inline-flex items-center gap-1 rounded-md border border-edge bg-surface px-2 py-1 text-xs font-medium text-fg-muted opacity-0 shadow-sm transition-opacity focus-visible:opacity-100 group-hover:opacity-100 hover:bg-surface-hover hover:text-fg"
            >
              {copied ? <Check size={12} aria-hidden /> : <Copy size={12} aria-hidden />}
              {copied ? 'Copied' : 'Copy'}
            </button>
          )}
          <button
            type="button"
            onClick={() => setEditing(true)}
            aria-label={`Edit ${label.toLowerCase()}`}
            className="inline-flex items-center gap-1 rounded-md border border-edge bg-surface px-2 py-1 text-xs font-medium text-fg-muted opacity-0 shadow-sm transition-opacity focus-visible:opacity-100 group-hover:opacity-100 hover:bg-surface-hover hover:text-fg"
          >
            <Pencil size={12} aria-hidden />
            Edit
          </button>
        </div>
        </div>
      </div>
    )
  }

  /** Replace the selection, then restore focus and select the new range. */
  const replaceSelection = (
    build: (selected: string) => {
      text: string
      selectFrom: number
      selectTo: number
    },
  ) => {
    const el = textareaRef.current
    if (!el) return
    const start = el.selectionStart
    const end = el.selectionEnd
    const {text, selectFrom, selectTo} = build(value.slice(start, end))
    onChange(value.slice(0, start) + text + value.slice(end))
    // After React has re-rendered with the new value: setting a range against
    // the old text would land the caret in the wrong place.
    requestAnimationFrame(() => {
      el.focus()
      el.setSelectionRange(start + selectFrom, start + selectTo)
    })
  }

  /** Wrap the selection — or a placeholder word — in before/after markers. */
  const wrap = (before: string, after: string, fallback: string) =>
    replaceSelection((selected) => {
      const inner = selected || fallback
      return {
        text: before + inner + after,
        selectFrom: before.length,
        selectTo: before.length + inner.length,
      }
    })

  /** Prefix every selected line, for the marks that are per-line: lists,
   *  headings, quotes. The range is widened to whole lines first, so a prefix
   *  cannot land in the middle of one. */
  const prefixLines = (prefix: string | ((i: number) => string)) => {
    const el = textareaRef.current
    if (!el) return
    const lineStart = value.lastIndexOf('\n', el.selectionStart - 1) + 1
    const nextBreak = value.indexOf('\n', el.selectionEnd)
    const lineEnd = nextBreak === -1 ? value.length : nextBreak
    const prefixed = value
      .slice(lineStart, lineEnd)
      .split('\n')
      .map((line, i) => (typeof prefix === 'string' ? prefix : prefix(i)) + line)
      .join('\n')
    onChange(value.slice(0, lineStart) + prefixed + value.slice(lineEnd))
    requestAnimationFrame(() => {
      el.focus()
      el.setSelectionRange(lineStart, lineStart + prefixed.length)
    })
  }

  const tools: {icon: LucideIcon; label: string; apply: () => void}[] = [
    {icon: Bold, label: 'Bold', apply: () => wrap('**', '**', 'bold')},
    {icon: Italic, label: 'Italic', apply: () => wrap('*', '*', 'italic')},
    {
      icon: Strikethrough,
      label: 'Strikethrough',
      apply: () => wrap('~~', '~~', 'struck'),
    },
    {icon: Heading2, label: 'Heading', apply: () => prefixLines('## ')},
    {icon: List, label: 'Bullet list', apply: () => prefixLines('- ')},
    {
      icon: ListOrdered,
      label: 'Numbered list',
      apply: () => prefixLines((i) => `${i + 1}. `),
    },
    {icon: Quote, label: 'Quote', apply: () => prefixLines('> ')},
    {icon: Code, label: 'Code', apply: () => wrap('`', '`', 'code')},
    {
      icon: Link2,
      label: 'Link',
      apply: () =>
        replaceSelection((selected) => {
          const text = selected || 'link text'
          // The url placeholder is selected, not the label, so the one part
          // that cannot be guessed is what gets typed over.
          return {
            text: `[${text}](url)`,
            selectFrom: text.length + 3,
            selectTo: text.length + 6,
          }
        }),
    },
  ]

  return (
    <div className="flex flex-col gap-1">
      <span className="text-sm font-medium">{label}</span>
      <div className="overflow-hidden rounded-lg border border-edge bg-bg focus-within:border-accent">
        <div className="flex items-center gap-0.5 border-b border-edge bg-surface px-1.5 py-1">
          {tools.map((tool) => (
            <button
              key={tool.label}
              type="button"
              title={tool.label}
              aria-label={tool.label}
              onClick={tool.apply}
              className="flex h-6 w-6 items-center justify-center rounded text-fg-muted transition-colors hover:bg-surface-hover hover:text-fg"
            >
              <tool.icon size={13} aria-hidden />
            </button>
          ))}
          <span className="mx-1 text-[10px] tracking-wide text-fg-muted/70 uppercase">
            Markdown
          </span>
          {/* Done is always offered, but only leaves edit mode when there is
              something to render. Emptying the field is how a description is
              deleted, and hiding Done on an empty value made that the one edit
              that could not be saved — while showing the blank result would be
              a bordered box with nothing in it. So: always saves, and stays
              here when what it saved was nothing. */}
          <button
            type="button"
            onClick={() => {
              if (value.trim()) setEditing(false)
              onDone?.()
            }}
            className="ml-auto inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-xs font-semibold text-accent transition-colors hover:bg-surface-hover"
          >
            <Check size={12} aria-hidden />
            Done
          </button>
        </div>
        <textarea
          ref={textareaRef}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          rows={8}
          placeholder={placeholder}
          spellCheck
          className="w-full resize-y bg-bg px-3 py-2 font-mono text-[13px] leading-relaxed text-fg outline-none placeholder:text-fg-muted/70"
        />
      </div>
      {hint && <p className="mt-1.5 text-xs text-fg-muted">{hint}</p>}
    </div>
  )
}
