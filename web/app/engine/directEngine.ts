import { ref } from 'vue'
import type { CrossfadeMode } from '~~/shared/types/audio'
import type { AudioPlaybackClockSample } from '~/types/audio-playback'
import { alog, shortUrl } from '~/engine/debug'
import type { TransitionPlan } from '~/engine/crossfade/strategy'
import { computeNormalizationGain } from '~/engine/dsp/normalization'

function clamp01(v: number): number {
  return Math.max(0, Math.min(1, v))
}

// Mirrors Deck.load's canplaythrough contract exactly (same event, same
// error tolerance) so callers (usePlayer's loadNext/prepareTransition) see
// identical timing/error behavior regardless of which engine is live.
function waitCanPlayThrough(slot: ElementSlot): Promise<void> {
  const audio = slot.audio
  return new Promise((resolve, reject) => {
    let timeout: ReturnType<typeof setTimeout> | null = null
    const onCanPlay = () => { cleanup(); resolve() }
    const onError = () => { cleanup(); reject(new Error(audio.error?.message ?? 'Failed to load audio')) }
    const cancel = () => { cleanup(); reject(new Error('Audio load superseded')) }
    const cleanup = () => {
      if (timeout) clearTimeout(timeout)
      audio.removeEventListener('canplaythrough', onCanPlay)
      audio.removeEventListener('error', onError)
      if (slot.cancelLoad === cancel) slot.cancelLoad = undefined
    }
    slot.cancelLoad = cancel
    audio.addEventListener('canplaythrough', onCanPlay, { once: true })
    audio.addEventListener('error', onError, { once: true })
    timeout = setTimeout(() => {
      cleanup()
      reject(new Error('Audio load timed out'))
    }, 20_000)
  })
}

interface ElementEvents {
  onEnded?: () => void
  onTimeUpdate?: (currentTime: number, duration: number) => void
  onError?: (error: Error) => void
}

// A bare HTMLAudioElement + a mutable event bag, wired once at creation —
// same shape as engine/deck.ts's Deck, minus everything Web-Audio (no
// AudioContext, no MediaElementAudioSourceNode, no gain nodes). Swapping
// "active" is just a variable reassignment; the elements themselves never
// move.
interface ElementSlot {
  audio: HTMLAudioElement
  events: ElementEvents
  cancelLoad?: () => void
  fadeTimer?: ReturnType<typeof setInterval>
}

// There is no gain graph here, so envelopes are stepped on a timer against
// HTMLMediaElement.volume. Coarser than Web Audio automation, same shape, and
// on every platform that honours element volume it removes the same clicks.
const FADE_STEP_MS = 16
// Equal-power progress shaping: the pair sums to constant perceived loudness
// across an overlap, matching engine/crossfade/curves.ts.
const EQUAL_POWER_OUT = (t: number) => 1 - Math.cos(t * Math.PI * 0.5)
const EQUAL_POWER_IN = (t: number) => Math.sin(t * Math.PI * 0.5)

function cancelFade(slot: ElementSlot) {
  if (slot.fadeTimer) {
    clearInterval(slot.fadeTimer)
    slot.fadeTimer = undefined
  }
}

function rampVolume(
  slot: ElementSlot,
  from: number,
  to: number,
  seconds: number,
  shape: (t: number) => number,
): Promise<void> {
  cancelFade(slot)
  const steps = Math.max(1, Math.round((seconds * 1000) / FADE_STEP_MS))
  slot.audio.volume = clamp01(from)
  return new Promise((resolve) => {
    let step = 0
    slot.fadeTimer = setInterval(() => {
      step += 1
      slot.audio.volume = clamp01(from + (to - from) * shape(Math.min(1, step / steps)))
      if (step >= steps) {
        cancelFade(slot)
        resolve()
      }
    }, FADE_STEP_MS)
  })
}

function makeSlot(): ElementSlot {
  const audio = new Audio()
  audio.preload = 'auto'
  // Only matters for Web Audio graph tainting, which never happens in this
  // engine — harmless to keep, and keeps network/auth behavior identical to
  // the graph engine's Deck.
  audio.crossOrigin = 'use-credentials'
  const slot: ElementSlot = { audio, events: {} }
  audio.addEventListener('ended', () => slot.events.onEnded?.())
  audio.addEventListener('timeupdate', () => {
    slot.events.onTimeUpdate?.(audio.currentTime, audio.duration)
  })
  audio.addEventListener('error', () => {
    const msg = audio.error?.message ?? 'Unknown audio error'
    slot.events.onError?.(new Error(msg))
  })
  return slot
}

function resetSlot(slot: ElementSlot) {
  cancelFade(slot)
  slot.cancelLoad?.()
  slot.audio.pause()
  slot.audio.removeAttribute('src')
  slot.audio.load()
}

// Point an idle element at a new source without letting the old decode buffer
// leak through, and hold it silent so the caller decides how it enters.
function armSlot(slot: ElementSlot, url: string) {
  cancelFade(slot)
  slot.cancelLoad?.()
  if (!slot.audio.paused) slot.audio.pause()
  slot.audio.removeAttribute('src')
  slot.audio.src = url
  slot.audio.load()
  slot.audio.volume = 0
}

// Direct-element playback engine — no AudioContext, no
// createMediaElementSource, ever. Required on iOS: the moment an element is
// routed into the Web Audio graph, Safari suspends it along with the
// AudioContext when the app backgrounds or the screen locks, and the
// connection can't be undone for that element. Two bare <audio> elements
// (active/pending) give the same gapless-swap shape as the graph engine's
// DeckManager, just without any DSP or scheduler-driven early cutoff.
//
// Implements the exact same public shape useAudioEngine's createEngine()
// does (see the EngineStub type in useAudioEngine.ts) so usePlayer.ts,
// applyAudioSettingsToEngine, and the visualizer/EQ components can keep
// talking to "the engine" without knowing which one is live. `directMode`
// is the one addition — a cheap hint so UI can gate EQ/visualizer affordances
// that fundamentally don't exist here.
export function createDirectEngine() {
  let active = makeSlot()
  let pending = makeSlot()

  const isPlaying = ref(false)
  const currentTime = ref(0)
  const duration = ref(0)
  // External volume contract matches the graph engine's setVolume: 0..1,
  // usePlayer passes `volume/100`.
  const volume = ref(1)

  // userVolume × the relevant deck's normGain (linear), clamped, is what
  // actually lands on `.volume`. This folds replay-gain into the plain
  // element volume since there's no gain-node graph to apply it in.
  // NOTE: iOS silently ignores `HTMLMediaElement.volume` when audio is routed
  // to certain hardware outputs (a long-standing WebKit quirk — the OS volume
  // buttons/silent switch are the only real control there), so normalization
  // is best-effort on iOS. It applies correctly on any other platform this
  // engine is forced onto (see useDeviceSettings.forceDirectEngine).
  let userVolume = 1
  let activeNormGain = 1
  let pendingNormGain = 1

  function applyActiveVolume() {
    // A level change is an explicit instruction — it wins over whatever
    // envelope happens to be running.
    cancelFade(active)
    active.audio.volume = clamp01(userVolume * activeNormGain)
  }

  let transitionCallback: (() => void) | null = null
  let endedCallback: (() => void) | null = null
  let errorCallback: ((err: Error) => void) | null = null

  // Stored but never invoked: the scheduler-driven early cutoff (crossfade's
  // "fire N seconds before the end") only exists to run Web Audio gain
  // automation ahead of time. This engine has no scheduler and always swaps
  // on the active element's natural `ended` (usePlayer's gapless path) — see
  // transition() below. usePlayer still calls setOnTransitionPoint
  // unconditionally, so the setter has to exist and accept the callback.
  function setOnTransitionPoint(cb: () => void) { transitionCallback = cb }
  function setOnEnded(cb: () => void) { endedCallback = cb }
  function setOnError(cb: (err: Error) => void) { errorCallback = cb }

  function wireActiveEvents() {
    active.events.onEnded = () => {
      isPlaying.value = false
      endedCallback?.()
    }
    active.events.onTimeUpdate = (t, d) => {
      currentTime.value = t
      duration.value = d || 0
    }
    active.events.onError = (err) => {
      isPlaying.value = false
      errorCallback?.(err)
    }
  }
  wireActiveEvents()

  // Matches the graph engine's timings so both backends sound the same.
  const SWITCH_DUCK_SECONDS = 0.5
  const COLD_START_FADE_SECONDS = 0.06
  const TRANSPORT_FADE_SECONDS = 0.12

  function activeLevel() { return clamp01(userVolume * activeNormGain) }

  function seekSlot(slot: ElementSlot, positionSeconds: number) {
    if (positionSeconds <= 0) return
    slot.audio.currentTime = Math.max(0, Math.min(positionSeconds, slot.audio.duration || 0))
  }

  // Skipping twice in quick succession leaves a first switch mid-fade while a
  // second one arms the very element it is retiring. Whoever started last owns
  // the slots; earlier attempts unwind without touching them.
  let playGeneration = 0

  async function play(url: string, startPositionSeconds = 0) {
    const generation = ++playGeneration

    // Nothing audible to fade from — cold-load on the element that is already
    // wired for playback events and ease it in.
    if (!isPlaying.value || active.audio.paused) {
      alog('engine', 'play (direct, cold load on active element)', shortUrl(url))
      activeNormGain = pendingNormGain
      pendingNormGain = 1
      armSlot(active, url)
      await waitCanPlayThrough(active)
      seekSlot(active, startPositionSeconds)
      await active.audio.play()
      isPlaying.value = true
      void rampVolume(active, 0, activeLevel(), COLD_START_FADE_SECONDS, EQUAL_POWER_IN)
      return
    }

    // A manual change. Buffer on the pending element so the outgoing track
    // keeps playing — cold-loading the active element would silence it for the
    // whole network load — then overlap the two.
    alog('engine', 'play (direct, duck into the preloading pending element)', shortUrl(url))
    armSlot(pending, url)
    try {
      await waitCanPlayThrough(pending)
    } catch (error) {
      // No replacement to fade into. The caller reports this as stopped, so
      // don't leave the outgoing track playing underneath a failed load —
      // unless a newer play already took the slots over.
      if (generation === playGeneration) stop()
      throw error
    }
    seekSlot(pending, startPositionSeconds)
    await duckToPending(SWITCH_DUCK_SECONDS, generation)
  }

  // Overlap the two elements, then retire the outgoing one. Roles swap as soon
  // as the incoming track is audible so the clock and `ended` events follow it
  // immediately rather than waiting out the fade.
  async function duckToPending(seconds: number, generation: number) {
    const outgoing = active
    const outgoingLevel = outgoing.audio.volume
    const incomingLevel = clamp01(userVolume * pendingNormGain)

    pending.audio.volume = 0
    await pending.audio.play()
    const fades = Promise.all([
      rampVolume(outgoing, outgoingLevel, 0, seconds, EQUAL_POWER_OUT),
      rampVolume(pending, 0, incomingLevel, seconds, EQUAL_POWER_IN),
    ])

    outgoing.events = {}
    const retired = active
    active = pending
    pending = retired
    activeNormGain = pendingNormGain
    pendingNormGain = 1
    wireActiveEvents()
    isPlaying.value = true

    await fades
    // A newer switch has already armed this element with its own track.
    if (generation !== playGeneration) return
    resetSlot(retired)
  }

  function pause() {
    if (!isPlaying.value) {
      active.audio.pause()
      return
    }
    isPlaying.value = false
    void rampVolume(active, active.audio.volume, 0, TRANSPORT_FADE_SECONDS, EQUAL_POWER_OUT)
      .then(() => {
        // A resume landed inside the fade and already ramped back up — don't
        // pause out from under it.
        if (isPlaying.value) return
        active.audio.pause()
        applyActiveVolume()
      })
  }

  function stop() {
    resetSlot(active)
    resetSlot(pending)
    isPlaying.value = false
  }

  async function resume() {
    cancelFade(active)
    active.audio.volume = 0
    await active.audio.play()
    isPlaying.value = true
    void rampVolume(active, 0, activeLevel(), TRANSPORT_FADE_SECONDS, EQUAL_POWER_IN)
  }

  function seek(time: number) {
    active.audio.currentTime = Math.max(0, Math.min(time, active.audio.duration || 0))
  }

  function setVolume(v: number) {
    userVolume = clamp01(v)
    volume.value = userVolume
    applyActiveVolume()
  }

  async function loadNext(url: string) {
    alog('engine', 'loadNext (direct, buffering pending element)', shortUrl(url))
    const audio = pending.audio
    pending.cancelLoad?.()
    if (!audio.paused) audio.pause()
    audio.removeAttribute('src')
    audio.src = url
    audio.load()
    await waitCanPlayThrough(pending)
  }

  function cancelPendingTransition() {
    resetSlot(pending)
    pendingNormGain = 1
  }

  // ALWAYS a gapless swap regardless of `mode` — there's no Web Audio graph
  // to run gain-automation crossfade curves on, so 'timed'/'smart' downgrade
  // to gapless here (still click-free: the pending element is fully
  // buffered and normalized ahead of time by loadNext/setPendingNormalization).
  async function transition(_mode: CrossfadeMode | 'gapless', _plan?: TransitionPlan) {
    alog('deck', 'direct: pause active + swap to preloaded pending element (gapless only)')
    active.audio.pause()
    active.events = {}
    const retired = active
    active = pending
    pending = retired
    activeNormGain = pendingNormGain
    pendingNormGain = 1
    wireActiveEvents()
    applyActiveVolume()
    await active.audio.play()
    isPlaying.value = true
    resetSlot(retired)
  }

  function setActiveNormalization(integrated: number, truePeak: number, targetLufs?: number) {
    activeNormGain = computeNormalizationGain(integrated, truePeak, targetLufs)
    alog('norm', `active gain ×${activeNormGain.toFixed(3)} (direct, element volume)`)
    applyActiveVolume()
  }
  function setPendingNormalization(integrated: number, truePeak: number, targetLufs?: number) {
    pendingNormGain = computeNormalizationGain(integrated, truePeak, targetLufs)
    alog('norm', `pending gain ×${pendingNormGain.toFixed(3)} (direct, applied on swap)`)
  }
  function resetActiveNormalization() {
    alog('norm', 'active gain reset (×1.0, no normalization) (direct)')
    activeNormGain = 1
    applyActiveVolume()
  }
  function resetPendingNormalization() {
    pendingNormGain = 1
  }

  function readClock(): AudioPlaybackClockSample {
    return {
      positionSeconds: active.audio.currentTime,
      durationSeconds: active.audio.duration || 0,
      playing: isPlaying.value && !active.audio.paused,
      paused: active.audio.paused,
      loading: false,
      buffering: false,
      ended: active.audio.ended,
      sampledAtMilliseconds: performance.now(),
    }
  }

  function dispose() {
    stop()
    active.events = {}
    pending.events = {}
  }

  return {
    kind: 'browser' as const,
    isPlaying, currentTime, duration, volume,
    play, pause, stop, resume, seek, setVolume,
    loadNext, transition, cancelPendingTransition, setOnTransitionPoint, setOnEnded, setOnError,
    dispose,
    setActiveNormalization, setPendingNormalization,
    resetActiveNormalization, resetPendingNormalization,
    readClock,
    reconcileClock: () => {},
    // Diagnostic hint for gating UI (EQPanel notice, visualizer guards, the
    // NowPlayingSheet artwork-tap cycle) — see useAudioEngine.ts's
    // `data-engine` attribute for the equivalent device-inspectable signal.
    directMode: true as const,
  }
}
