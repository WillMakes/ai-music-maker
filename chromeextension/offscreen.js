// The recorder (#92): the Suno tab's audio, captured while recording is on,
// cut into one take per song and sent to SunoMaker as a WAV.
//
// Capturing a tab silences it, so the stream is also played straight back
// out — the producer goes on hearing the song exactly as before.
//
// The page (content.js) says when a song starts, pauses, resumes, is
// abandoned and ends; this file only keeps the sound between those words.
// Whether a listen counted as whole is content.js's call, and whether the
// take belongs to a track is the app's.

let tabId = 0
let stream = null
let context = null
let tap = null

// The take being recorded now, and every take still gathering sound — a
// finished song keeps gathering its tail while the next one starts.
let current = null
const takes = new Map()
let nextId = 1

const stop = () => {
  if (stream) stream.getTracks().forEach((track) => track.stop())
  if (context) context.close().catch(() => {})
  stream = context = tap = current = null
  takes.clear()
  tabId = 0
}

const start = async (streamId, forTab) => {
  stop()
  stream = await navigator.mediaDevices.getUserMedia({
    audio: {mandatory: {chromeMediaSource: 'tab', chromeMediaSourceId: streamId}},
    video: false,
  })
  context = new AudioContext()
  await context.audioWorklet.addModule('recorder-worklet.js')
  const source = context.createMediaStreamSource(stream)
  source.connect(context.destination)
  tap = new AudioWorkletNode(context, 'sunomaker-recorder')
  // Connected onward so the audio graph pulls it; it writes nothing, so what
  // reaches the speakers is the source above and only that.
  source.connect(tap)
  tap.connect(context.destination)
  tap.port.onmessage = ({data}) => {
    const take = takes.get(data.id)
    if (!take) return
    if (data.pcm) take.chunks.push(data.pcm)
    if (data.done && take.done) take.done()
  }
  tabId = forTab
}

const command = (cmd, id) => tap && tap.port.postMessage({cmd, id})

// 16-bit stereo PCM with the 44-byte header every player reads.
const encodeWAV = (chunks, rate) => {
  const samples = chunks.reduce((n, chunk) => n + chunk.length, 0)
  const buffer = new ArrayBuffer(44 + samples * 2)
  const view = new DataView(buffer)
  const text = (at, s) => [...s].forEach((c, i) => view.setUint8(at + i, c.charCodeAt(0)))
  text(0, 'RIFF')
  view.setUint32(4, 36 + samples * 2, true)
  text(8, 'WAVE')
  text(12, 'fmt ')
  view.setUint32(16, 16, true)
  view.setUint16(20, 1, true)
  view.setUint16(22, 2, true)
  view.setUint32(24, rate, true)
  view.setUint32(28, rate * 4, true)
  view.setUint16(32, 4, true)
  view.setUint16(34, 16, true)
  text(36, 'data')
  view.setUint32(40, samples * 2, true)
  let offset = 44
  chunks.forEach((chunk) => {
    new Int16Array(buffer, offset, chunk.length).set(chunk)
    offset += chunk.length * 2
  })
  return new Blob([buffer], {type: 'audio/wav'})
}

const upload = async (play, wav) => {
  const config = self.__SUNOMAKER
  const query = new URLSearchParams({
    clip: play.clip,
    title: play.title || '',
    track: play.trackId || '',
  })
  const r = await fetch(`http://127.0.0.1:${config.port}/capture?${query}`, {
    method: 'POST',
    headers: {Authorization: 'Bearer ' + config.token, 'Content-Type': 'audio/wav'},
    body: wav,
  })
  const data = await r.json().catch(() => null)
  return {ok: r.ok, status: r.status, data}
}

const finish = async (msg) => {
  if (!current || current.clip !== msg.clip) {
    return {ok: false, error: 'That song was not recorded from its start.'}
  }
  const {id} = current
  const take = takes.get(id)
  current = null
  const done = new Promise((resolve) => {
    take.done = resolve
    // The worklet answers within the tail; a context that stopped rendering
    // would never answer, and this must not wait on it for ever.
    setTimeout(resolve, 5000)
  })
  command('end', id)
  await done
  takes.delete(id)
  if (!take.chunks.length) return {ok: false, error: 'Nothing was heard from the tab.'}
  try {
    return await upload(msg, encodeWAV(take.chunks, context ? context.sampleRate : 48000))
  } catch (e) {
    // SunoMaker closed mid-song: the take is lost, which is the same as the
    // app not being there to hear it.
    return {ok: false}
  }
}

const handle = async (msg, sender) => {
  // Starting and stopping are the worker's, which sends with no tab; a page
  // can only speak about songs.
  if (!sender.tab && msg.type === 'start') {
    await start(msg.streamId, msg.tabId)
    return {ok: true}
  }
  if (!sender.tab && msg.type === 'stop') {
    stop()
    return {ok: true}
  }
  // Everything else comes from a Suno page, and only the tab being recorded
  // has a say — another Suno tab's player is not what this stream holds.
  if (!tap || !sender.tab || sender.tab.id !== tabId) return {ok: false}

  if (msg.type === 'song-start') {
    const id = nextId++
    takes.set(id, {clip: msg.clip, chunks: []})
    current = {id, clip: msg.clip}
    command('start', id)
    return {ok: true}
  }
  if (!current || current.clip !== msg.clip) {
    return msg.type === 'song-end' ? finish(msg) : {ok: false}
  }
  if (msg.type === 'song-pause') command('pause', current.id)
  else if (msg.type === 'song-resume') command('resume', current.id)
  else if (msg.type === 'song-abort') {
    command('drop', current.id)
    takes.delete(current.id)
    current = null
  } else if (msg.type === 'song-end') return finish(msg)
  return {ok: true}
}

chrome.runtime.onMessage.addListener((msg, sender, reply) => {
  if (!msg || msg.target !== 'offscreen') return false
  handle(msg, sender).then(reply, (e) =>
    reply({ok: false, error: String(e && e.message ? e.message : e)}),
  )
  return true
})
