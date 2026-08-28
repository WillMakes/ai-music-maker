import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import {OpenURL} from '../../wailsjs/go/main/App'

/**
 * Markdown, as the interview's replies and the skill pages render it.
 *
 * The styling is a stylesheet rule (`.sm-prose` in style.css) rather than a
 * component map, because what is being styled is arbitrary markdown — every
 * element it can produce needs a rule, and a map would be twenty components that
 * each set one class.
 *
 * Links are intercepted rather than followed. A link followed inside the webview
 * replaces the app with a web page and leaves no way back, there being no
 * address bar and no Back button; OpenURL sends it to the producer's own browser
 * instead.
 */
export function Markdown({children}: {children: string}) {
  return (
    <div className="sm-prose">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          a: ({href, children: label}) => (
            <a
              href={href}
              onClick={(event) => {
                event.preventDefault()
                if (href) void OpenURL(href).catch(() => {})
              }}
            >
              {label}
            </a>
          ),
        }}
      >
        {children}
      </ReactMarkdown>
    </div>
  )
}
