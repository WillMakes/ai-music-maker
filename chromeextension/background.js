// The service worker holds the host permission for 127.0.0.1, so the fetches to
// the app's bridge go through here rather than from the content script — a
// content script on suno.com cannot reach another origin, but the worker can,
// and it is the one place the token lives outside the page.
importScripts('config.js')

const base = () => `http://127.0.0.1:${self.__SUNOMAKER.port}`
const authed = () => ({Authorization: 'Bearer ' + self.__SUNOMAKER.token})

chrome.runtime.onMessage.addListener((msg, _sender, reply) => {
  ;(async () => {
    try {
      if (msg.type === 'health') {
        // Unauthenticated on the app's side, and deliberately: this is the call
        // that decides whether to draw anything at all, and it has to be
        // answerable before the extension has proved anything.
        const r = await fetch(base() + '/health')
        reply({ok: r.ok})
      } else if (msg.type === 'library') {
        const r = await fetch(base() + '/library', {headers: authed()})
        reply({ok: r.ok, data: r.ok ? await r.json() : null, status: r.status})
      } else if (msg.type === 'track') {
        const r = await fetch(base() + '/track/' + encodeURIComponent(msg.id), {
          headers: authed(),
        })
        reply({ok: r.ok, data: r.ok ? await r.json() : null, status: r.status})
      } else if (msg.type === 'capture') {
        // A song heard to the end (#91). The app downloads and files it; this
        // only says which clip it was.
        const r = await fetch(base() + '/capture', {
          method: 'POST',
          headers: {...authed(), 'Content-Type': 'application/json'},
          body: JSON.stringify(msg.play),
        })
        const data = await r.json().catch(() => null)
        reply({ok: r.ok, data, status: r.status})
      } else if (msg.type === 'open') {
        const r = await fetch(base() + '/open', {method: 'POST', headers: authed()})
        reply({ok: r.ok})
      } else {
        reply({ok: false, error: 'unknown request'})
      }
    } catch (e) {
      // The app not running is the common case, not an error worth a stack.
      reply({ok: false, error: String(e && e.message ? e.message : e)})
    }
  })()
  // Keep the message channel open for the async reply above.
  return true
})
