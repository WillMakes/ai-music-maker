// The service worker holds the host permission for 127.0.0.1, so the fetches to
// the app's bridge go through here rather than from the content script — a
// content script on suno.com cannot reach another origin, but the worker can,
// and it is the one place the token lives outside the page.
importScripts('config.js')

const base = () => `http://127.0.0.1:${self.__AIMUSICMAKER.port}`
const authed = () => ({Authorization: 'Bearer ' + self.__AIMUSICMAKER.token})

chrome.runtime.onMessage.addListener((msg, _sender, reply) => {
  // The recorder's messages are the offscreen page's to answer. Answering
  // them here, even to refuse, would be the reply the page receives.
  if (!msg || msg.target === 'offscreen') return false
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

// ---- Recording (#92) ----
//
// Chrome lets an extension capture a tab's sound only after the producer has
// pressed the extension's own toolbar button on that tab — no page script and
// no setting can start it. So the button is the switch: press it on the Suno
// tab to start recording, press it again to stop. The badge says REC while
// it is on, and Chrome shows its own capture indicator on the tab.
//
// The stream lives in an offscreen page (offscreen.js), because a service
// worker can neither hold a stream nor run an AudioContext, and it is torn
// down when this worker is not looking — so which tab is being recorded is
// kept in session storage rather than in a variable here.

const OFFSCREEN = 'offscreen.html'

const recordingTab = async () =>
  (await chrome.storage.session.get({recordingTab: 0})).recordingTab

const ensureOffscreen = async () => {
  const open = await chrome.runtime.getContexts({contextTypes: ['OFFSCREEN_DOCUMENT']})
  if (open.length) return
  await chrome.offscreen.createDocument({
    url: OFFSCREEN,
    reasons: ['USER_MEDIA'],
    justification:
      "Records the Suno tab's sound so a song heard to the end is filed onto its track in AIMusicMaker.",
  })
}

const tellTab = (tabId, message) => chrome.tabs.sendMessage(tabId, message).catch(() => {})

const setBadge = (tabId, text, title) => {
  chrome.action.setBadgeText({tabId, text}).catch(() => {})
  chrome.action.setBadgeBackgroundColor({tabId, color: '#b91c1c'}).catch(() => {})
  chrome.action.setTitle({tabId, title}).catch(() => {})
}

const stopRecording = async () => {
  const tabId = await recordingTab()
  await chrome.storage.session.set({recordingTab: 0})
  await chrome.runtime.sendMessage({target: 'offscreen', type: 'stop'}).catch(() => {})
  await chrome.offscreen.closeDocument().catch(() => {})
  if (tabId) {
    setBadge(tabId, '', 'AIMusicMaker')
    tellTab(tabId, {type: 'recording', on: false})
  }
}

const startRecording = async (tab) => {
  const streamId = await chrome.tabCapture.getMediaStreamId({targetTabId: tab.id})
  await ensureOffscreen()
  const r = await chrome.runtime.sendMessage({
    target: 'offscreen',
    type: 'start',
    streamId,
    tabId: tab.id,
  })
  if (!r || !r.ok) throw new Error((r && r.error) || 'the recorder did not start')
  await chrome.storage.session.set({recordingTab: tab.id})
  setBadge(tab.id, 'REC', 'AIMusicMaker is recording this tab — press to stop')
  tellTab(tab.id, {type: 'recording', on: true})
}

chrome.action.onClicked.addListener(async (tab) => {
  const current = await recordingTab()
  if (current) {
    await stopRecording()
    // Pressed on the tab being recorded: that was the stop. Pressed on
    // another tab: the recording moves there.
    if (current === tab.id) return
  }
  // Only a Suno page has the content script that says when songs start and
  // end; recording any other tab would keep sound nobody can file. Asking it
  // is how this worker knows, without a permission to read tab addresses.
  const here = await chrome.tabs.sendMessage(tab.id, {type: 'ping'}).catch(() => null)
  if (!here || !here.ok) return
  try {
    await startRecording(tab)
  } catch (e) {
    setBadge(tab.id, '!', 'AIMusicMaker could not record this tab: ' + (e && e.message ? e.message : e))
    await chrome.offscreen.closeDocument().catch(() => {})
  }
})

chrome.tabs.onRemoved.addListener(async (tabId) => {
  if (tabId === (await recordingTab())) await stopRecording()
})
