// The sample tap for a recording (#92), running on the audio thread.
//
// It sees every block of the Suno tab's sound and keeps it only while a song
// is being recorded, as 16-bit stereo — what the WAV will hold, at half the
// memory of the floats it arrives as. Each block posted is tagged with the
// take it belongs to, so a song that starts while the last one's tail is
// still being caught cannot bleed into it.
//
// Commands come from offscreen.js and are applied at a block boundary, which
// at 128 frames is under 3 ms — tighter than the page's own events can say
// when a song started or stopped.

const BLOCK = 4096
// The sound reaches the capture a little after the player says it has ended;
// this much more is kept so a song's last note is not cut off.
const TAIL_SECONDS = 0.2

const toInt16 = (x) => {
  const clamped = Math.max(-1, Math.min(1, x))
  return clamped < 0 ? clamped * 0x8000 : clamped * 0x7fff
}

class SunoMakerRecorder extends AudioWorkletProcessor {
  constructor() {
    super()
    this.id = 0
    // idle | on | paused | tail
    this.state = 'idle'
    this.block = new Int16Array(BLOCK * 2)
    this.at = 0
    this.tailLeft = 0
    this.port.onmessage = ({data}) => this.command(data)
  }

  command({cmd, id}) {
    if (cmd === 'start') {
      // A new song while the last one's tail is still running finishes that
      // one first, so its take is whole and this one starts clean.
      if (this.state === 'tail') this.finish()
      this.id = id
      this.at = 0
      this.state = 'on'
      return
    }
    if (id !== this.id) return
    if (cmd === 'pause' && this.state === 'on') {
      // Flushed now so the sound up to the pause is kept and the silence
      // after it is not.
      this.flush()
      this.state = 'paused'
    } else if (cmd === 'resume' && this.state === 'paused') {
      this.state = 'on'
    } else if (cmd === 'drop') {
      this.at = 0
      this.state = 'idle'
    } else if (cmd === 'end') {
      if (this.state === 'on') {
        this.state = 'tail'
        this.tailLeft = Math.round(sampleRate * TAIL_SECONDS)
      } else {
        this.finish()
      }
    }
  }

  flush() {
    if (this.at) this.port.postMessage({id: this.id, pcm: this.block.slice(0, this.at * 2)})
    this.at = 0
  }

  finish() {
    this.flush()
    this.port.postMessage({id: this.id, done: true})
    this.state = 'idle'
  }

  process(inputs) {
    if (this.state !== 'on' && this.state !== 'tail') return true
    const input = inputs[0] || []
    const left = input[0]
    // A mono source is kept as two equal channels, so every take has the
    // same shape whatever Suno served.
    const right = input[1] || left
    const frames = left ? left.length : 128
    for (let i = 0; i < frames; i++) {
      this.block[this.at * 2] = left ? toInt16(left[i]) : 0
      this.block[this.at * 2 + 1] = right ? toInt16(right[i]) : 0
      if (++this.at === BLOCK) this.flush()
    }
    if (this.state === 'tail') {
      this.tailLeft -= frames
      if (this.tailLeft <= 0) this.finish()
    }
    return true
  }
}

registerProcessor('sunomaker-recorder', SunoMakerRecorder)
