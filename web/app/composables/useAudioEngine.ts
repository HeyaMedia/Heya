import type { Ref } from 'vue'
import type { CrossfadeMode } from '~~/shared/types/audio'
import { AnalyserBridge } from '~/engine/analysis/analyserBridge'
import { getAudioContext, resumeContext } from '~/engine/context'
import { alog, shortUrl } from '~/engine/debug'
import type { TransitionPlan } from '~/engine/crossfade/strategy'
import { DeckManager } from '~/engine/deckManager'
import { createDirectEngine } from '~/engine/directEngine'
import { createCrossfeed } from '~/engine/dsp/crossfeed'
import { createEqualizer } from '~/engine/dsp/equalizer'
import { createLimiter } from '~/engine/dsp/limiter'
import { computeNormalizationGain, createNormalization } from '~/engine/dsp/normalization'
import { createPostgain } from '~/engine/dsp/postgain'
import { createPreamp } from '~/engine/dsp/preamp'
import { Scheduler } from '~/engine/scheduler'
import { SignalChain } from '~/engine/signalChain'

let instance: ReturnType<typeof createEngine> | ReturnType<typeof createDirectEngine> | null = null

function createEngine() {
  const ctx = getAudioContext()
  const deckManager = new DeckManager(ctx)
  const signalChain = new SignalChain()
  const analyserBridge = new AnalyserBridge(ctx)

  const normalization = createNormalization(ctx)
  const preamp = createPreamp(ctx)
  const equalizer = createEqualizer(ctx)
  const postgain = createPostgain(ctx)
  const crossfeed = createCrossfeed(ctx)
  const limiter = createLimiter(ctx)

  // Per-deck normGainNode handles normalization, so the shared block in the
  // chain stays disabled — keeping it present lets us flip on a global mode
  // later without a graph rebuild.
  normalization.enabled = false
  // EQ / preamp / postgain / crossfeed off by default. Limiter stays on as a
  // safety net.
  preamp.enabled = false
  equalizer.enabled = false
  postgain.enabled = false
  crossfeed.enabled = false

  // Crossfeed sits after the EQ/gain stages and before the limiter, so the
  // limiter still catches any peaks the channel summing introduces.
  signalChain.setBlocks([normalization, preamp, equalizer, postgain, crossfeed, limiter])
  signalChain.setSource(deckManager.getActiveOutput())
  signalChain.setDestination(analyserBridge.analyserNode)
  analyserBridge.analyserNode.connect(ctx.destination)

  deckManager.setOnSwap(() => {
    signalChain.setSource(deckManager.getActiveOutput())
  })

  const isPlaying = ref(false)
  const currentTime = ref(0)
  const duration = ref(0)
  const volume = ref(1)

  let transitionCallback: (() => void) | null = null
  let endedCallback: (() => void) | null = null
  let errorCallback: ((err: Error) => void) | null = null

  const scheduler = new Scheduler({
    onTransitionPoint: () => transitionCallback?.(),
  })

  function setOnTransitionPoint(cb: () => void) { transitionCallback = cb }
  function setOnEnded(cb: () => void) { endedCallback = cb }
  function setOnError(cb: (err: Error) => void) { errorCallback = cb }

  deckManager.on('onTimeUpdate', (time, dur) => {
    currentTime.value = time
    duration.value = dur
    scheduler.onTimeUpdate(time, dur)
  })

  deckManager.on('onTrackEnded', () => {
    isPlaying.value = false
    endedCallback?.()
  })

  deckManager.on('onError', (err) => {
    isPlaying.value = false
    errorCallback?.(err)
  })

  // Fast fade applied when hot-swapping the active deck's source (a manual
  // track change), so the hard cut doesn't click. ~60ms is inaudible as a fade
  // but long enough to ramp cleanly through the discontinuity.
  const SWITCH_FADE_SECONDS = 0.06

  async function play(url: string, startPositionSeconds = 0) {
    alog('engine', 'play (cold load on active deck)', shortUrl(url))
    await resumeContext()
    // Jellyfin-style: fade the currently-playing track to silence before the
    // source swap so the cut is click-free, then fade the new track in.
    if (isPlaying.value && !deckManager.active.paused) {
      await deckManager.active.fadeOut(SWITCH_FADE_SECONDS)
    }
    await deckManager.loadAndPlay(url, startPositionSeconds)
    deckManager.active.fadeIn(volume.value, SWITCH_FADE_SECONDS)
    isPlaying.value = true
    scheduler.reset()
  }

  function pause() {
    deckManager.pause()
    isPlaying.value = false
  }

  function stop() {
    deckManager.stopAll()
    isPlaying.value = false
  }

  async function resume() {
    await resumeContext()
    await deckManager.play()
    isPlaying.value = true
  }

  function seek(time: number) { deckManager.seek(time) }

  function setVolume(v: number) {
    const clamped = Math.max(0, Math.min(1, v))
    volume.value = clamped
    deckManager.active.setVolume(clamped)
  }

  async function loadNext(url: string) {
    alog('engine', 'loadNext (buffering pending deck)', shortUrl(url))
    await deckManager.loadNext(url)
  }

  async function transition(mode: CrossfadeMode | 'gapless', plan?: TransitionPlan) {
    if (mode !== 'gapless') {
      // Route the pending deck through the chain so EQ/limiter apply during
      // the overlap, not just to the outgoing track.
      signalChain.connectAdditionalSource(deckManager.pending.getOutputNode())
    }
    await deckManager.transition(mode, plan)
    deckManager.active.setVolume(volume.value)
    scheduler.reset()
    isPlaying.value = true
  }

  function setActiveNormalization(integrated: number, truePeak: number, targetLufs?: number) {
    const gain = computeNormalizationGain(integrated, truePeak, targetLufs)
    alog('norm', `active gain ×${gain.toFixed(3)} (${(20 * Math.log10(gain || 1)).toFixed(1)} dB) — ${integrated.toFixed(1)} LUFS, peak ${truePeak.toFixed(1)} dB`)
    deckManager.setActiveNormalization(gain)
  }
  function setPendingNormalization(integrated: number, truePeak: number, targetLufs?: number) {
    const gain = computeNormalizationGain(integrated, truePeak, targetLufs)
    alog('norm', `pending gain ×${gain.toFixed(3)} (${(20 * Math.log10(gain || 1)).toFixed(1)} dB)`)
    deckManager.setPendingNormalization(gain)
  }
  function resetActiveNormalization() {
    alog('norm', 'active gain reset (×1.0, no normalization)')
    deckManager.setActiveNormalization(1)
  }
  function resetPendingNormalization() { deckManager.setPendingNormalization(1) }

  function dispose() {
    deckManager.dispose()
    signalChain.dispose()
    analyserBridge.dispose()
    instance = null
  }

  return {
    isPlaying, currentTime, duration, volume,
    play, pause, stop, resume, seek, setVolume,
    loadNext, transition, setOnTransitionPoint, setOnEnded, setOnError,
    dispose,
    normalization, preamp, equalizer, postgain, crossfeed, limiter,
    signalChain, analyserBridge, scheduler,
    setActiveNormalization, setPendingNormalization,
    resetActiveNormalization, resetPendingNormalization,
    // Full Web Audio graph — the default everywhere except iOS. See
    // engine/directEngine.ts for the no-graph counterpart and `directMode`'s
    // purpose (gating EQ/visualizer UI that has nothing to attach to there).
    directMode: false,
  }
}

// SSR-safe stub. Engine touches AudioContext, which doesn't exist on the
// server — callers can still import the composable from server code, they
// just won't get a working engine until the client hydrates.
type EngineStub = {
  isPlaying: Ref<boolean>
  currentTime: Ref<number>
  duration: Ref<number>
  volume: Ref<number>
  play: (url: string, startPositionSeconds?: number) => Promise<void>
  pause: () => void
  stop: () => void
  resume: () => Promise<void>
  seek: (time: number) => void
  setVolume: (v: number) => void
  loadNext: (url: string) => Promise<void>
  transition: (mode: CrossfadeMode | 'gapless', plan?: TransitionPlan) => Promise<void>
  setOnTransitionPoint: (cb: () => void) => void
  setOnEnded: (cb: () => void) => void
  setOnError: (cb: (err: Error) => void) => void
  dispose: () => void
  setActiveNormalization: (integrated: number, truePeak: number, targetLufs?: number) => void
  setPendingNormalization: (integrated: number, truePeak: number, targetLufs?: number) => void
  resetActiveNormalization: () => void
  resetPendingNormalization: () => void
  // Present on every branch (graph, direct, SSR stub) so UI can read
  // `useAudioEngine().directMode` without a cast. True only for
  // engine/directEngine.ts's no-graph engine.
  directMode: boolean
}

const serverStub: EngineStub = {
  isPlaying: ref(false),
  currentTime: ref(0),
  duration: ref(0),
  volume: ref(1),
  play: async () => {},
  pause: () => {},
  stop: () => {},
  resume: async () => {},
  seek: () => {},
  setVolume: () => {},
  loadNext: async () => {},
  transition: async () => {},
  setOnTransitionPoint: () => {},
  setOnEnded: () => {},
  setOnError: () => {},
  dispose: () => {},
  setActiveNormalization: () => {},
  setPendingNormalization: () => {},
  resetActiveNormalization: () => {},
  resetPendingNormalization: () => {},
  directMode: false,
}

export function useAudioEngine() {
  if (import.meta.server) return serverStub
  if (!instance) {
    // Read once, at first construction. iOS gets the direct-element engine
    // by default (no Web Audio graph — see engine/directEngine.ts for why);
    // forceDirectEngine lets a user override in either direction. This is
    // deliberately NOT reactive — the module singleton is built once and
    // reused for the app's lifetime, so flipping the setting later needs a
    // reload to take effect (the device-settings UI says so).
    const direct = useDeviceSettings().settings.value.forceDirectEngine ?? isIOS()
    instance = direct ? createDirectEngine() : createEngine()
    // Cheap, permanent diagnosability hook — inspectable from devtools or a
    // real device without instrumenting engine internals, and doubles as the
    // structural probe used in this feature's own smoke test.
    if (import.meta.client) document.documentElement.dataset.engine = direct ? 'direct' : 'graph'
  }
  return instance
}
