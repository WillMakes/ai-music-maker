// The button on Suno's create form.
//
// Suno's create page has three fields this app has an answer for — Lyrics, Style
// and Title — and the round trip without this extension is "switch window, find
// the track, copy, switch back, paste", three times per track. So each of those
// fields gets a small button; pressing it opens a picker of everything in
// AIMusicMaker, and choosing a track fills the field.
//
// Nothing is drawn until the app has confirmed it is running. A producer who is
// on Suno without AIMusicMaker open gets no buttons at all, which is the right
// outcome — there is nothing here to offer them.
//
// Suno is a single-page app and re-renders its form constantly, so the buttons
// are re-attached by a MutationObserver rather than mounted once. Each field is
// marked when it has been decorated, which is what stops the observer from
// stacking a second button on every re-render.
;(() => {
  if (window.__aiMusicMakerMounted) return
  window.__aiMusicMakerMounted = true

  // Resolves undefined, rather than throwing, when nothing answers — the
  // recorder not running, or the extension reloaded under this page.
  const send = (message) =>
    new Promise((resolve) => {
      try {
        chrome.runtime.sendMessage(message, (reply) => {
          void chrome.runtime.lastError
          resolve(reply)
        })
      } catch (_) {
        resolve(undefined)
      }
    })

  const ICON = chrome.runtime.getURL('logo.png')

  // The three fields, and how each one is recognised.
  //
  // Suno's markup carries no stable ids, so each field is found by a list of
  // candidate selectors tried in order, then by a label match as the fallback.
  // Written as a list rather than one clever selector because these WILL break
  // when Suno redesigns, and a list is something a person can extend by looking
  // at the page — which is the maintenance story this feature actually has.
  const FIELDS = [
    {
      key: 'lyrics',
      label: 'Lyrics',
      // What to put in it, given a track.
      value: (track) => track.lyrics,
      selectors: [
        // Suno's current form (#138): the lyrics box is a Lexical rich-text
        // editor, a contenteditable div rather than a textarea. fill() knows
        // the difference.
        '[aria-label="Lyrics editor"][contenteditable="true"]',
        '.lyrics-editor-content[data-lexical-editor]',
        'textarea[data-testid="lyrics-input-textarea"]',
        'textarea[placeholder*="lyric" i]',
        'textarea[aria-label*="lyric" i]',
        // The form before that (#79) named nothing: the lyrics box was the one
        // textarea with a 3000-character limit, since raised to 5000.
        'textarea[maxlength="3000"]',
        'textarea[maxlength="5000"]',
      ],
      match: /lyric/i,
    },
    {
      key: 'style',
      label: 'Style',
      value: (track) => track.prompt,
      selectors: [
        // Suno's current form (#79): the styles box lives inside the one
        // wrapper that still carries a testid.
        '[data-testid="create-form-styles-wrapper"] textarea',
        'textarea[data-testid="tag-input-textarea"]',
        'textarea[placeholder*="style" i]',
        'textarea[aria-label*="style" i]',
        'input[placeholder*="style" i]',
      ],
      match: /style|genre/i,
    },
    {
      key: 'title',
      label: 'Title',
      value: (track) => track.name,
      selectors: [
        'input[data-testid="title-input"]',
        'input[placeholder*="title" i]',
        'input[aria-label*="title" i]',
        'textarea[placeholder*="title" i]',
      ],
      match: /title|song name/i,
    },
    {
      key: 'duration',
      label: 'Duration',
      // Plain seconds, not m:ss (#79): Suno's duration control is a number
      // input capped at 300, so a colon in it is a rejected value. The
      // selectors stay best-effort — a field that is not found is simply not
      // decorated (#77).
      value: (track) => (track.targetSeconds ? String(track.targetSeconds) : ''),
      selectors: [
        'input[type="number"][min="1"][max="300"]',
        'input[data-testid="duration-input"]',
        'input[placeholder*="duration" i]',
        'input[aria-label*="duration" i]',
        'input[placeholder*="length" i]',
        'input[aria-label*="length" i]',
      ],
      match: /duration|song length/i,
    },
  ]

  // ---- Talking to the app ----

  let appIsUp = false
  let library = null

  const checkHealth = async () => {
    const r = await send({type: 'health'})
    appIsUp = !!(r && r.ok)
    return appIsUp
  }

  // The library is fetched once and refetched when the picker is opened, rather
  // than held for the life of the page: a producer usually adds a track in the
  // app and then comes here to make it, so a cache from page load would be one
  // version behind exactly when it matters.
  const loadLibrary = async () => {
    const r = await send({type: 'library'})
    if (!r || !r.ok) {
      library = null
      return null
    }
    library = r.data || []
    return library
  }

  // ---- Filling a field ----

  // React holds the value of a controlled input in its own state, so assigning
  // to .value updates what is drawn and nothing else — the next render puts the
  // old value straight back, and submitting sends what React still believes.
  //
  // The fix is to set the value through the native property setter (which
  // bypasses React's own override on the element) and then dispatch the input
  // event React is listening for, so its onChange runs and its state catches up.
  const setNativeValue = (element, value) => {
    const prototype =
      element instanceof HTMLTextAreaElement
        ? HTMLTextAreaElement.prototype
        : HTMLInputElement.prototype
    const setter = Object.getOwnPropertyDescriptor(prototype, 'value')
    if (setter && setter.set) {
      setter.set.call(element, value)
    } else {
      element.value = value
    }
    element.dispatchEvent(new Event('input', {bubbles: true}))
    element.dispatchEvent(new Event('change', {bubbles: true}))
  }

  // The most a rich-text field will take, from the counter Suno draws beside it
  // ("120 / 5000"), or 5000, its lyrics limit, when no counter is found. The
  // nearest counter walking up from the field, a few levels at most: the form
  // as a whole also holds the styles box's counter, which is not this one. A
  // textarea says its own with maxlength and needs none of this.
  const editorLimit = (element) => {
    let scope = element.parentElement
    for (let level = 0; scope && level < 4; level++, scope = scope.parentElement) {
      const counter = (scope.textContent || '').match(/\b\d{1,5}\s*\/\s*(\d{3,5})\b/)
      if (counter) return Number(counter[1])
    }
    return 5000
  }

  // Filling a rich-text editor (#138). Its content is the editor's own state,
  // not the DOM: a .value does not exist, and HTML written in is overwritten on
  // the next render. What it does accept is what a person does — a paste, or
  // typing — so that is what this sends. Replace selects everything first, so
  // the paste lands over it; append puts the caret at the end.
  //
  // A paste first, because the editor turns a pasted text's line breaks into
  // lines. Whether it took the paste is read off the event, not the page: the
  // editor cancels a paste it handles but applies it a moment later, so the
  // text has not changed yet when dispatch returns, and reading the text would
  // send it a second time. A paste nobody took falls back to insertText line
  // by line, with a paragraph break between, which is typing.
  const fillEditor = (element, value, mode) => {
    element.focus()
    const existing = (element.innerText || '').trim()
    const appending = mode === 'append' && existing !== ''
    let text = appending ? '\n\n' + value : value

    const room = editorLimit(element) - (appending ? existing.length : 0)
    if (text.length > room) {
      text = text.slice(0, Math.max(0, room))
      toast(`Suno's lyrics take ${editorLimit(element)} characters, so the end was left off.`, true)
    }

    const selection = window.getSelection()
    const range = document.createRange()
    range.selectNodeContents(element)
    if (appending) range.collapse(false)
    selection.removeAllRanges()
    selection.addRange(range)
    // Told at once rather than left to the browser's own selectionchange,
    // which arrives after this function has returned. During a paste the
    // editor uses the selection it last recorded, not the page's, so without
    // this Replace could paste at an old caret instead of over everything.
    document.dispatchEvent(new Event('selectionchange'))

    if (text === '') return
    const data = new DataTransfer()
    data.setData('text/plain', text)
    const ignored = element.dispatchEvent(
      new ClipboardEvent('paste', {clipboardData: data, bubbles: true, cancelable: true}),
    )
    if (!ignored) return

    text.split('\n').forEach((line, i) => {
      if (i > 0) document.execCommand('insertParagraph')
      if (line) document.execCommand('insertText', false, line)
    })
  }

  const fill = (element, value, mode) => {
    if (element.isContentEditable) {
      fillEditor(element, value, mode)
      return
    }
    const existing = element.value || ''
    // Insert appends at the caret's end rather than replacing, because "add this
    // verse to what I have" is a real thing to want and an overwrite of forty
    // lines of lyrics is not undoable from here.
    const next =
      mode === 'append' && existing.trim() ? existing.replace(/\s*$/, '\n\n') + value : value
    setNativeValue(element, next)
    element.focus()
  }

  // One click, the whole form (#77): every field this page has and the track
  // has an answer for is filled together — three picks per track was this
  // extension's own version of the round trip it exists to end.
  //
  // The rich-text lyrics editor goes last, after the form has settled, and is
  // looked up again when its turn comes (#139, #140). Filling Style, Title and
  // Duration makes Suno re-render the form, and the editor found before that
  // can be a node the re-render has since replaced: the paste went into a
  // copy nobody sees, which is why the Lyrics button alone worked and the
  // one-click fill did not. Last, nothing moves focus away from it either.
  const fillAll = async (track, mode) => {
    rememberTrack(track)
    const wanted = FIELDS.map((field) => ({field, value: field.value(track) || ''})).filter(
      ({value}) => value,
    )
    const isEditor = ({field}) => {
      const target = findField(field)
      return !!(target && target.isContentEditable)
    }
    const plain = wanted.filter((one) => !isEditor(one))
    const editors = wanted.filter(isEditor)
    plain.forEach(({field, value}) => {
      const target = findField(field)
      if (target) fill(target, value, mode)
    })
    if (editors.length === 0) return
    // Two frames: one for the re-render the fills above set off, one for the
    // editor to mount and register its listeners if it was replaced.
    await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)))
    editors.forEach(({field, value}) => {
      const target = findField(field)
      if (target) fill(target, value, mode)
    })
  }

  // ---- The picker ----

  let openPicker = null

  const closePicker = () => {
    if (openPicker) {
      openPicker.remove()
      openPicker = null
      document.removeEventListener('keydown', onEscape, true)
    }
  }

  const onEscape = (event) => {
    if (event.key === 'Escape') {
      event.stopPropagation()
      closePicker()
    }
  }

  const el = (tag, className, text) => {
    const node = document.createElement(tag)
    if (className) node.className = className
    if (text != null) node.textContent = text
    return node
  }

  const showPicker = async (field, target, anchor) => {
    closePicker()

    const panel = el('div', 'sm-picker')
    // Positioned against the button rather than centred, so the field being
    // filled stays visible while a track is chosen — the producer is comparing
    // the two.
    const box = anchor.getBoundingClientRect()
    if (anchor.classList && anchor.classList.contains('sm-dock')) {
      // The dock sits at the bottom of the window, so the picker opens
      // upwards from it instead of off the bottom edge.
      panel.style.top = `${window.scrollY + box.top - 6}px`
      panel.style.transform = 'translateY(-100%)'
    } else {
      panel.style.top = `${window.scrollY + box.bottom + 6}px`
    }
    panel.style.left = `${Math.max(8, window.scrollX + box.right - 360)}px`

    const head = el('div', 'sm-picker-head')
    const brand = el('img', 'sm-picker-logo')
    brand.src = ICON
    brand.alt = ''
    head.appendChild(brand)
    head.appendChild(
      el('span', 'sm-picker-title', field ? `Insert ${field.label}` : 'Fill the form'),
    )
    const close = el('button', 'sm-picker-close', '×')
    close.type = 'button'
    close.setAttribute('aria-label', 'Close')
    close.addEventListener('click', closePicker)
    head.appendChild(close)
    panel.appendChild(head)

    const body = el('div', 'sm-picker-body')
    body.appendChild(el('div', 'sm-picker-note', 'Loading…'))
    panel.appendChild(body)

    document.body.appendChild(panel)
    openPicker = panel
    document.addEventListener('keydown', onEscape, true)

    const tree = await loadLibrary()
    body.textContent = ''

    if (!tree) {
      body.appendChild(
        el('div', 'sm-picker-note', 'AIMusicMaker is not answering. Is it open?'),
      )
      return
    }
    const anyTracks = tree.some((artist) =>
      (artist.albums || []).some((album) => (album.tracks || []).length),
    )
    if (!anyTracks) {
      body.appendChild(
        el('div', 'sm-picker-note', 'No tracks yet — plan one in AIMusicMaker first.'),
      )
      const open = el('button', 'sm-picker-open', 'Open AIMusicMaker')
      open.type = 'button'
      open.addEventListener('click', () => send({type: 'open'}))
      body.appendChild(open)
      return
    }

    const search = el('input', 'sm-picker-search')
    search.type = 'search'
    search.placeholder = 'Filter tracks'
    body.appendChild(search)

    const list = el('div', 'sm-picker-list')
    body.appendChild(list)

    const draw = (filter) => {
      list.textContent = ''
      const needle = filter.trim().toLowerCase()
      // The latest record first (#79): the track being generated right now
      // is almost always on the album made last, and the picker should open
      // on it rather than on the discography's history.
      const groups = []
      tree.forEach((artist) => {
        ;(artist.albums || []).forEach((album) => groups.push({artist, album}))
      })
      groups.sort((a, b) => (b.album.createdAt || '').localeCompare(a.album.createdAt || ''))
      groups.forEach(({artist, album}) => {
        {
          const tracks = (album.tracks || []).filter((track) => {
            if (!needle) return true
            return (
              track.name.toLowerCase().includes(needle) ||
              album.name.toLowerCase().includes(needle) ||
              artist.name.toLowerCase().includes(needle)
            )
          })
          if (!tracks.length) return

          list.appendChild(
            el('div', 'sm-picker-group', `${artist.name} — ${album.name}`),
          )
          tracks.forEach((track) => {
            const row = el('button', 'sm-picker-row')
            row.type = 'button'
            row.appendChild(el('span', 'sm-picker-row-name', `${track.position}. ${track.name}`))
            if (field) {
              const value = field.value(track) || ''
              // What the field would receive, so the choice is not blind — an
              // empty one is the case worth seeing before it overwrites
              // something.
              row.appendChild(
                el(
                  'span',
                  value ? 'sm-picker-row-hint' : 'sm-picker-row-hint sm-picker-row-empty',
                  value ? preview(value) : `no ${field.label.toLowerCase()} written yet`,
                ),
              )
              if (!value) row.disabled = true
              row.addEventListener('click', () => {
                fill(target, value, panel.dataset.mode || 'replace')
                rememberTrack(track)
                closePicker()
              })
            } else {
              // Fill-all (#77): the hint says which fields this track can
              // fill, since that is what the click is about to do.
              const parts = FIELDS.filter((f) => f.value(track)).map((f) => f.label)
              row.appendChild(
                el('span', 'sm-picker-row-hint', parts.join(', ') || 'title only'),
              )
              row.addEventListener('click', () => {
                fillAll(track, panel.dataset.mode || 'replace')
                closePicker()
              })
            }
            list.appendChild(row)
          })
        }
      })
      if (!list.children.length) {
        list.appendChild(el('div', 'sm-picker-note', 'Nothing matches that.'))
      }
    }

    // Replace or append, chosen before the track. A radio pair rather than two
    // buttons per row, because the choice is about this insertion and not about
    // this track, and repeating it on forty rows would be forty chances to pick
    // the wrong one.
    const modes = el('div', 'sm-picker-modes')
    ;[
      ['replace', 'Replace'],
      ['append', 'Add to end'],
    ].forEach(([mode, label], index) => {
      const button = el('button', 'sm-picker-mode', label)
      button.type = 'button'
      if (index === 0) button.classList.add('is-on')
      button.addEventListener('click', () => {
        panel.dataset.mode = mode
        modes.querySelectorAll('.sm-picker-mode').forEach((other) =>
          other.classList.toggle('is-on', other === button),
        )
      })
      modes.appendChild(button)
    })
    panel.dataset.mode = 'replace'
    body.insertBefore(modes, list)

    search.addEventListener('input', () => draw(search.value))
    draw('')
    search.focus()
  }

  // preview is the one-line summary of what would be inserted. Newlines become
  // spaces so a lyric sheet reads as a line rather than breaking the row.
  const preview = (value) => {
    const flat = value.replace(/\s+/g, ' ').trim()
    return flat.length > 70 ? flat.slice(0, 70) + '…' : flat
  }

  // ---- Recording what is heard (#91, #92) ----
  //
  // With recording switched on from the toolbar button, the tab's sound is
  // captured (offscreen.js), and this decides what a take is: one song,
  // heard from its first half-second to its end. Joining a song part-way, or
  // seeking inside it, leaves it unrecorded, because a take somebody skipped
  // through is not the song they listened to (#92). Pausing is allowed — the
  // recording pauses with it, and nothing of the silence is kept.
  //
  // The song itself is found from the player: its audio address carries the
  // clip id, and its title is matched to a track in AIMusicMaker.

  const CLIP = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i
  // How far in a song may be and still count as its start, and how short of
  // its end the player's record of what it played may fall — the slack of the
  // page's own timing, not a part of the song allowed to go unheard.
  const START_SLACK = 0.5
  const END_SLACK = 0.5

  const titleKey = (title) => (title || '').toLowerCase().replace(/[^\p{L}\p{N}]/gu, '')

  // The track the form was last filled from, by the title it gave the song.
  // It is what settles a name two albums share, so it is kept across page
  // loads — the song is often heard long after the form was filled.
  const rememberTrack = (track) => {
    const key = titleKey(track && track.name)
    if (!key) return
    chrome.storage.local.get({filled: {}}, ({filled}) => {
      filled[key] = {id: track.id, at: Date.now()}
      const keys = Object.keys(filled)
      // Bounded, oldest out: this is a hint for recent work, not a history.
      if (keys.length > 200) {
        keys
          .sort((a, b) => filled[a].at - filled[b].at)
          .slice(0, keys.length - 200)
          .forEach((k) => delete filled[k])
      }
      chrome.storage.local.set({filled})
    })
  }

  const rememberedTrack = (title) =>
    new Promise((resolve) =>
      chrome.storage.local.get({filled: {}}, ({filled}) => {
        const hit = filled[titleKey(title)]
        resolve(hit ? hit.id : '')
      }),
    )

  // The song's title. A link to the song's own page is tied to this clip and
  // is tried first; the media session is what Suno tells the operating system
  // is playing, and is the fallback when no such link is on screen.
  const titleOf = (clip) => {
    const link = document.querySelector(`a[href*="/song/${clip}"]`)
    const text = link && (link.textContent || '').trim()
    if (text) return text
    try {
      const meta = navigator.mediaSession && navigator.mediaSession.metadata
      if (meta && meta.title) return meta.title
    } catch (_) {
      // Unreadable metadata is the same as none.
    }
    return ''
  }

  const heardSeconds = (media) => {
    let total = 0
    for (let i = 0; i < media.played.length; i++) {
      total += media.played.end(i) - media.played.start(i)
    }
    return total
  }

  const toast = (text, isError) => {
    const node = el('div', isError ? 'sm-toast sm-toast-error' : 'sm-toast')
    const logo = el('img', 'sm-button-logo')
    logo.src = ICON
    logo.alt = ''
    node.appendChild(logo)
    node.appendChild(el('span', null, text))
    node.setAttribute('role', 'status')
    document.body.appendChild(node)
    setTimeout(() => node.remove(), 6000)
  }

  const clipOf = (media) => {
    const found = (media.currentSrc || media.src || '').match(CLIP)
    return found ? found[0].toLowerCase() : ''
  }

  const toRecorder = (type, fields) => send({target: 'offscreen', type, ...fields})

  // The listen in progress: which song, whether the recorder took it from
  // its start, and whether it is paused.
  let listen = null
  let hinted = false

  const abandon = () => {
    if (listen && listen.recording) toRecorder('song-abort', {clip: listen.clip})
    listen = null
  }

  const begin = async (media) => {
    abandon()
    const clip = clipOf(media)
    if (!clip) return
    const mine = {clip, recording: false, paused: false}
    listen = mine
    const r = await toRecorder('song-start', {clip})
    mine.recording = !!(r && r.ok)
    // Once a page, while AIMusicMaker is open: say how to turn recording on,
    // since nothing else on Suno would tell anyone it exists.
    if (!mine.recording && !hinted && appIsUp) {
      hinted = true
      toast("Press AIMusicMaker's button in Chrome's toolbar to record the songs you listen to.")
    }
  }

  const onPlay = (event) => {
    const media = event.target
    if (!(media instanceof HTMLMediaElement)) return
    const clip = clipOf(media)
    if (listen && listen.clip === clip && listen.paused) {
      listen.paused = false
      if (listen.recording) toRecorder('song-resume', {clip})
      return
    }
    if (media.currentTime <= START_SLACK) begin(media)
    // Joined part-way: not a whole listen, and not the start of one.
    else abandon()
  }

  const onPause = (event) => {
    const media = event.target
    // The player pauses itself on the way to 'ended'; that is the end, not
    // a pause.
    if (!(media instanceof HTMLMediaElement) || media.ended) return
    if (!listen || listen.clip !== clipOf(media)) return
    listen.paused = true
    if (listen.recording) toRecorder('song-pause', {clip: listen.clip})
  }

  const onSeeking = (event) => {
    const media = event.target
    if (!(media instanceof HTMLMediaElement)) return
    // Back to the top while playing is starting over, and the fresh listen
    // is as whole as the first; anywhere else is a skip.
    if (media.currentTime <= START_SLACK && !media.paused) begin(media)
    else abandon()
  }

  const onLoad = (event) => {
    const media = event.target
    if (!(media instanceof HTMLMediaElement)) return
    if (listen && listen.clip !== clipOf(media)) abandon()
  }

  const onEnded = async (event) => {
    const media = event.target
    if (!(media instanceof HTMLMediaElement)) return
    const done = listen
    if (!done || done.clip !== clipOf(media)) return
    listen = null
    if (!done.recording) return
    const whole =
      isFinite(media.duration) &&
      media.duration > 0 &&
      heardSeconds(media) >= media.duration - START_SLACK - END_SLACK
    if (!whole) {
      toRecorder('song-abort', {clip: done.clip})
      return
    }
    const title = titleOf(done.clip)
    const trackId = title ? await rememberedTrack(title) : ''
    const r = await toRecorder('song-end', {clip: done.clip, title, trackId})
    if (!r || !r.ok) {
      if (r && r.status === 401) {
        toast('AIMusicMaker did not recognise this extension. Export it again from Settings.', true)
      } else if (r && r.data && r.data.error) {
        toast(r.data.error, true)
      } else if (r && r.error) {
        toast(r.error, true)
      }
      // No answer at all is the app being closed, which is not news.
      return
    }
    // Only a filing is worth interrupting for. Most songs heard on Suno are
    // not the producer's tracks, and saying so after each would be noise.
    if (r.data && r.data.outcome === 'filed') toast(r.data.message)
  }

  // Media events do not bubble, so these listen in the capture phase, which
  // sees them on their way to any audio element in the page. Installed
  // whether or not the app is up: it may be opened mid-session.
  document.addEventListener('play', onPlay, true)
  document.addEventListener('pause', onPause, true)
  document.addEventListener('seeking', onSeeking, true)
  document.addEventListener('loadstart', onLoad, true)
  document.addEventListener('ended', onEnded, true)

  chrome.runtime.onMessage.addListener((msg, _sender, reply) => {
    if (!msg) return false
    if (msg.type === 'ping') {
      // The worker asking whether this is a Suno page before it records it.
      reply({ok: true})
      return false
    }
    if (msg.type === 'recording') {
      hinted = true
      toast(
        msg.on
          ? 'Recording. Songs you hear from start to finish are filed onto their tracks, from the next one that starts.'
          : 'Stopped recording.',
      )
    }
    return false
  })

  // ---- Attaching the buttons ----

  const findField = (field) => {
    for (const selector of field.selectors) {
      const found = document.querySelector(selector)
      if (found) return found
    }
    // The fallback: a label whose text matches, with a control under it. Suno's
    // own attributes change; the words on the page are what the producer sees
    // and are the last thing to move.
    const labels = Array.from(document.querySelectorAll('label, [role="heading"], h3, h4'))
    for (const label of labels) {
      if (!field.match.test(label.textContent || '')) continue
      const scope = label.closest('div')
      const control = scope && scope.querySelector('textarea, input[type="text"]')
      if (control) return control
    }
    return null
  }

  // The dock is the one element that does not depend on finding Suno's
  // fields (#77): a fixed pill in the window's corner, so the extension
  // visibly shows up whenever the app is running — even on a layout where
  // the per-field buttons found nothing to decorate. One press opens the
  // fill-all picker.
  const attachDock = () => {
    if (document.querySelector('.sm-dock')) return
    const dock = el('button', 'sm-dock')
    dock.type = 'button'
    dock.title = "Fill Suno's form from an AIMusicMaker track"
    const logo = el('img', 'sm-button-logo')
    logo.src = ICON
    logo.alt = ''
    dock.appendChild(logo)
    dock.appendChild(el('span', null, 'Fill from AIMusicMaker'))
    dock.addEventListener('click', (event) => {
      event.preventDefault()
      event.stopPropagation()
      showPicker(null, null, dock)
    })
    document.body.appendChild(dock)
  }

  const attach = () => {
    if (!appIsUp) return
    attachDock()
    FIELDS.forEach((field) => {
      const target = findField(field)
      if (!target) return
      // The marker is on the target rather than on the button, so a button
      // removed by one of Suno's re-renders is re-added on the next sweep.
      if (target.dataset.aimusicmaker === field.key) {
        if (target.__aiMusicMakerButton && target.__aiMusicMakerButton.isConnected) return
      }
      target.dataset.aimusicmaker = field.key

      const button = el('button', 'sm-button')
      button.type = 'button'
      button.title = `Insert ${field.label} from AIMusicMaker`
      const logo = el('img', 'sm-button-logo')
      logo.src = ICON
      logo.alt = ''
      button.appendChild(logo)
      button.appendChild(el('span', null, field.label))
      button.addEventListener('click', (event) => {
        event.preventDefault()
        event.stopPropagation()
        showPicker(field, target, button)
      })

      // Placed in a wrapper positioned over the field's own corner rather than
      // inside Suno's layout: injecting an element into their flex row moves
      // their controls around, and a button that shifts the form it decorates is
      // worse than no button.
      const holder = el('div', 'sm-button-holder')
      holder.appendChild(button)
      const parent = target.parentElement
      if (!parent) return
      if (getComputedStyle(parent).position === 'static') {
        parent.style.position = 'relative'
      }
      parent.appendChild(holder)
      target.__aiMusicMakerButton = button
    })
  }

  // Suno re-renders constantly; the observer is debounced so a burst of mutations
  // costs one sweep rather than one per mutation.
  let pending = 0
  const sweep = () => {
    clearTimeout(pending)
    pending = setTimeout(attach, 200)
  }

  const start = async () => {
    if (!(await checkHealth())) {
      // The app was not up when the page loaded, which is ordinary — it gets
      // opened afterwards. Checked again periodically so the buttons appear
      // without a reload.
      setTimeout(start, 5000)
      return
    }
    attach()
    new MutationObserver(sweep).observe(document.body, {childList: true, subtree: true})
  }

  start()
})()
