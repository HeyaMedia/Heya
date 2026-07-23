import { afterAll, describe, expect, test } from 'bun:test'
import { DeckManager } from '../app/engine/deckManager.ts'
import { MasterOutput } from '../app/engine/masterOutput.ts'

class FakeAudioParam {
  value = 1
  cancelledAt = null
  setAt = null

  cancelScheduledValues(time) {
    this.cancelledAt = time
  }

  setValueAtTime(value, time) {
    this.value = value
    this.setAt = time
  }

  linearRampToValueAtTime(value) {
    this.value = value
  }

  setValueCurveAtTime(values) {
    this.value = values.at(-1) ?? this.value
  }
}

class FakeGainNode {
  gain = new FakeAudioParam()
  destination = null
  disconnected = false

  connect(destination) {
    this.destination = destination
  }

  disconnect() {
    this.disconnected = true
  }
}

class FakeAudio extends EventTarget {
  paused = true
  currentTime = 0
  duration = 180
  src = ''
  crossOrigin = ''
  preload = ''

  load() {
    queueMicrotask(() => this.dispatchEvent(new Event('canplaythrough')))
  }

  async play() {
    this.paused = false
  }

  pause() {
    this.paused = true
  }

  removeAttribute(name) {
    if (name === 'src') this.src = ''
  }
}

class FakeAudioContext {
  currentTime = 0

  createGain() {
    return new FakeGainNode()
  }

  createMediaElementSource() {
    return new FakeGainNode()
  }
}

const originalAudio = globalThis.Audio
globalThis.Audio = FakeAudio
afterAll(() => { globalThis.Audio = originalAudio })

describe('browser audio master output', () => {
  test('owns the listener volume in one post-mix gain stage', () => {
    const node = new FakeGainNode()
    const destination = {}
    const context = { currentTime: 12.5, createGain: () => node }
    const output = new MasterOutput(context, destination)

    output.setVolume(0.25)

    expect(output.inputNode).toBe(node)
    expect(node.destination).toBe(destination)
    expect(node.gain.value).toBe(0.25)
    expect(node.gain.cancelledAt).toBe(12.5)
    expect(node.gain.setAt).toBe(12.5)
  })

  test('clamps invalid external levels without touching deck gains', () => {
    const node = new FakeGainNode()
    const output = new MasterOutput(
      { currentTime: 0, createGain: () => node },
      {},
    )

    output.setVolume(-1)
    expect(node.gain.value).toBe(0)
    output.setVolume(4)
    expect(node.gain.value).toBe(1)

    output.dispose()
    expect(node.disconnected).toBeTrue()
  })

  test('restores a retired deck before it is reused for gapless playback', async () => {
    const decks = new DeckManager(new FakeAudioContext())
    decks.pending.setTransitionGain(0)

    await decks.loadNext('/api/music/tracks/2/stream')

    expect(decks.pending.transitionGainNode.gain.value).toBe(1)
    decks.dispose()
  })

  test('manual replacement cancels the pending deck without stopping the audible deck', async () => {
    const decks = new DeckManager(new FakeAudioContext())
    await decks.loadAndPlay('/api/music/tracks/1/stream')
    await decks.loadNext('/api/music/tracks/2/stream')
    await decks.pending.play()

    decks.prepareForReplacement()

    expect(decks.active.paused).toBeFalse()
    expect(decks.pending.paused).toBeTrue()
    expect(decks.pending.transitionGainNode.gain.value).toBe(1)
    decks.dispose()
  })
})
