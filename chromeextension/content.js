// The button on Suno's create form.
//
// Suno's create page has three fields this app has an answer for — Lyrics, Style
// and Title — and the round trip without this extension is "switch window, find
// the track, copy, switch back, paste", three times per track. So each of those
// fields gets a small button; pressing it opens a picker of everything in
// SunoMaker, and choosing a track fills the field.
//
// Nothing is drawn until the app has confirmed it is running. A producer who is
// on Suno without SunoMaker open gets no buttons at all, which is the right
// outcome — there is nothing here to offer them.
//
// Suno is a single-page app and re-renders its form constantly, so the buttons
// are re-attached by a MutationObserver rather than mounted once. Each field is
// marked when it has been decorated, which is what stops the observer from
// stacking a second button on every re-render.
;(() => {
  if (window.__sunoMakerMounted) return
  window.__sunoMakerMounted = true

  const send = (message) =>
    new Promise((resolve) => chrome.runtime.sendMessage(message, resolve))

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
        'textarea[data-testid="lyrics-input-textarea"]',
        'textarea[placeholder*="lyric" i]',
        'textarea[aria-label*="lyric" i]',
        // Suno's current form (#79) names nothing: the lyrics box is the one
        // textarea with a 3000-character limit, an example song as its
        // placeholder, and no label near it.
        'textarea[maxlength="3000"]',
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

  const fill = (element, value, mode) => {
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
  const fillAll = (track, mode) => {
    rememberTrack(track)
    FIELDS.forEach((field) => {
      const target = findField(field)
      const value = field.value(track) || ''
      if (target && value) fill(target, value, mode)
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
        el('div', 'sm-picker-note', 'SunoMaker is not answering. Is it open?'),
      )
      return
    }
    const anyTracks = tree.some((artist) =>
      (artist.albums || []).some((album) => (album.tracks || []).length),
    )
    if (!anyTracks) {
      body.appendChild(
        el('div', 'sm-picker-note', 'No tracks yet — plan one in SunoMaker first.'),
      )
      const open = el('button', 'sm-picker-open', 'Open SunoMaker')
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

  // ---- Filing what is heard (#91) ----
  //
  // A song played to the end on Suno is reported to the app, which downloads
  // Suno's own file for it and files it onto the track of the same name. The
  // audio itself is never recorded here: Suno's file is the take bit for bit,
  // where a recording of the tab would be a re-encode of it.
  //
  // "To the end" means heard, not skipped to: the player's own record of what
  // it played has to cover nearly all of the song. Seeking to the last second
  // fires the same 'ended' event, and a take nobody listened to is not one
  // somebody chose to keep.

  const CLIP = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i
  const HEARD = 0.9

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

  // Clips already reported from this page, so a song on repeat is one
  // report. The app refuses a second copy anyway; this saves asking.
  const reported = new Set()

  const onEnded = async (event) => {
    const media = event.target
    if (!(media instanceof HTMLMediaElement)) return
    const address = media.currentSrc || media.src || ''
    const found = address.match(CLIP)
    if (!found) return
    const clip = found[0].toLowerCase()
    // A song still being generated streams with no known length; it has no
    // finished file to fetch yet.
    if (!isFinite(media.duration) || media.duration <= 0) return
    if (heardSeconds(media) < media.duration * HEARD) return
    if (reported.has(clip)) return
    reported.add(clip)

    const title = titleOf(clip)
    const trackId = title ? await rememberedTrack(title) : ''
    const r = await send({
      type: 'capture',
      play: {clipId: clip, title, audioUrl: address, trackId},
    })
    if (!r || !r.ok) {
      // Not reported after all, so hearing it again once the app is open
      // gets another chance.
      reported.delete(clip)
      if (r && r.status === 401) {
        toast('SunoMaker did not recognise this extension. Export it again from Settings.', true)
      } else if (r && r.data && r.data.error) {
        toast(r.data.error, true)
      }
      // No answer at all is the app being closed, which is not news.
      return
    }
    // Only a filing is worth interrupting for. Most songs heard on Suno are
    // not the producer's tracks, and saying so after each would be noise.
    if (r.data && r.data.outcome === 'filed') toast(r.data.message)
  }

  // Media events do not bubble, so this listens in the capture phase, which
  // sees them on their way to any audio element in the page. Installed
  // whether or not the app is up: it may be opened mid-song.
  document.addEventListener('ended', onEnded, true)

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
    dock.title = "Fill Suno's form from a SunoMaker track"
    const logo = el('img', 'sm-button-logo')
    logo.src = ICON
    logo.alt = ''
    dock.appendChild(logo)
    dock.appendChild(el('span', null, 'Fill from SunoMaker'))
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
      if (target.dataset.sunomaker === field.key) {
        if (target.__sunoMakerButton && target.__sunoMakerButton.isConnected) return
      }
      target.dataset.sunomaker = field.key

      const button = el('button', 'sm-button')
      button.type = 'button'
      button.title = `Insert ${field.label} from SunoMaker`
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
      target.__sunoMakerButton = button
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
