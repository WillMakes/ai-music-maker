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
      ],
      match: /lyric/i,
    },
    {
      key: 'style',
      label: 'Style',
      value: (track) => track.prompt,
      selectors: [
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
    panel.style.top = `${window.scrollY + box.bottom + 6}px`
    panel.style.left = `${Math.max(8, window.scrollX + box.right - 360)}px`

    const head = el('div', 'sm-picker-head')
    const brand = el('img', 'sm-picker-logo')
    brand.src = ICON
    brand.alt = ''
    head.appendChild(brand)
    head.appendChild(el('span', 'sm-picker-title', `Insert ${field.label}`))
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
      tree.forEach((artist) => {
        ;(artist.albums || []).forEach((album) => {
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
            const value = field.value(track) || ''
            const row = el('button', 'sm-picker-row')
            row.type = 'button'
            row.appendChild(el('span', 'sm-picker-row-name', `${track.position}. ${track.name}`))
            // What the field would receive, so the choice is not blind — an
            // empty one is the case worth seeing before it overwrites something.
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
              closePicker()
            })
            list.appendChild(row)
          })
        })
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

  const attach = () => {
    if (!appIsUp) return
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
