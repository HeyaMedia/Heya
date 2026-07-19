import { ref } from 'vue'
import type { CrossfadeMode } from '~~/shared/types/audio'
import { alog, shortUrl } from '~/engine/debug'
import type { TransitionPlan } from '~/engine/crossfade/strategy'
import { computeNormalizationGain } from '~/engine/dsp/normalization'

function clamp01(v: number): number {
  return Math.max(0, Math.min(1, v))
}

// Mirrors Deck.load's canplaythrough contract exactly (same event, same
// error tolerance) so callers (usePlayer's loadNext/prepareTransition) see
// identical timing/error behavior regardless of which engine is live.
function waitCanPlayThrough(audio: HTMLAudioElement): Promise<void> {
  return new Promise((resolve, reject) => {
    const onCanPlay = () => { cleanup(); resolve() }
    const onError = () => { cleanup(); reject(new Error(audio.error?.message ?? 'Failed to load audio')) }
    const cleanup = () => {
      audio.removeEventListener('canplaythrough', onCanPlay)
      audio.removeEventListener('error', onError)
    }
    audio.addEventListener('canplaythrough', onCanPlay, { once: true })
    audio.addEventListener('error', onError, { once: true })
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
  slot.audio.pause()
  slot.audio.removeAttribute('src')
  slot.audio.load()
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

  async function play(url: string, startPositionSeconds = 0) {
    alog('engine', 'play (direct, cold load on active element)', shortUrl(url))
    const audio = active.audio
    if (!audio.paused) audio.pause()
    audio.removeAttribute('src')
    audio.src = url
    audio.load()
    applyActiveVolume()
    await waitCanPlayThrough(audio)
    if (startPositionSeconds > 0) {
      audio.currentTime = Math.max(0, Math.min(startPositionSeconds, audio.duration || 0))
    }
    await audio.play()
    isPlaying.value = true
  }

  function pause() {
    active.audio.pause()
    isPlaying.value = false
  }

  function stop() {
    resetSlot(active)
    resetSlot(pending)
    isPlaying.value = false
  }

  async function resume() {
    await active.audio.play()
    isPlaying.value = true
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
    if (!audio.paused) audio.pause()
    audio.removeAttribute('src')
    audio.src = url
    audio.load()
    await waitCanPlayThrough(audio)
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

  function dispose() {
    stop()
    active.events = {}
    pending.events = {}
  }

  return {
    isPlaying, currentTime, duration, volume,
    play, pause, stop, resume, seek, setVolume,
    loadNext, transition, setOnTransitionPoint, setOnEnded, setOnError,
    dispose,
    setActiveNormalization, setPendingNormalization,
    resetActiveNormalization, resetPendingNormalization,
    // Diagnostic hint for gating UI (EQPanel notice, visualizer guards, the
    // NowPlayingSheet artwork-tap cycle) — see useAudioEngine.ts's
    // `data-engine` attribute for the equivalent device-inspectable signal.
    directMode: true as const,
  }
}
