import {Check, Copy} from 'lucide-react'
import {useEffect, useState} from 'react'

/**
 * The quick copy beside a name (#74): one press puts the text on the
 * clipboard, and the icon answers with a check for a moment. Clipboard access
 * can be refused, and saying nothing would look like the button did nothing —
 * so the check only appears on success, the way MarkdownField's Copy behaves.
 */
export function CopyButton({text, label}: {text: string; label: string}) {
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    if (!copied) return
    const timer = setTimeout(() => setCopied(false), 1600)
    return () => clearTimeout(timer)
  }, [copied])

  return (
    <button
      className="sm-button sm-button-quiet shrink-0 border-0 px-1.5 py-1"
      type="button"
      title={copied ? 'Copied' : label}
      aria-label={label}
      onClick={() =>
        navigator.clipboard
          .writeText(text)
          .then(() => setCopied(true))
          .catch(() => setCopied(false))
      }
    >
      {copied ? <Check size={13} className="text-ok" aria-hidden /> : <Copy size={13} aria-hidden />}
    </button>
  )
}
