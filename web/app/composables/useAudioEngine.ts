import type { Ref } from 'vue'
import type { CrossfadeMode } from '~~/shared/types/audio'
import { AnalyserBridge } from '~/engine/analysis/analyserBridge'
import { getAudioContext, resumeContext } from '~/engine/context'
import type { TransitionPlan } from '~/engine/crossfade/strategy'
import { DeckManager } from '~/engine/deckManager'
import { createEqualizer } from '~/engine/dsp/equalizer'
import { createLimiter } from '~/engine/dsp/limiter'
import { computeNormalizationGain, createNormalization } from '~/engine/dsp/normalization'
import { createPostgain } from '~/engine/dsp/postgain'
import { createPreamp } from '~/engine/dsp/preamp'
import { Scheduler } from '~/engine/scheduler'
import { SignalChain } from '~/engine/signalChain'

let instance: ReturnType<typeof createEngine> | null = null

function createEngine() {
  const ctx = getAudioContext()
  const deckManager = new DeckManager(ctx)
  const signalChain = new SignalChain()
  const analyserBridge = new AnalyserBridge(ctx)

  const normalization = createNormalization(ctx)
  const preamp = createPreamp(ctx)
  const equalizer = createEqualizer(ctx)
  const postgain = createPostgain(ctx)
  const limiter = createLimiter(ctx)

  // Per-deck normGainNode handles normalization, so the shared block in the
  // chain stays disabled — keeping it present lets us flip on a global mode
  // later without a graph rebuild.
  normalization.enabled = false
  // EQ / preamp / postgain off by default. Limiter stays on as a safety net.
  preamp.enabled = false
  equalizer.enabled = false
  postgain.enabled = false

  signalChain.setBlocks([normalization, preamp, equalizer, postgain, limiter])
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

  async function play(url: string) {
    await resumeContext()
    await deckManager.loadAndPlay(url)
    deckManager.active.setVolume(volume.value)
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

  async function loadNext(url: string) { await deckManager.loadNext(url) }

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

  function setActiveNormalization(integrated: number, truePeak: number) {
    deckManager.setActiveNormalization(computeNormalizationGain(integrated, truePeak))
  }
  function setPendingNormalization(integrated: number, truePeak: number) {
    deckManager.setPendingNormalization(computeNormalizationGain(integrated, truePeak))
  }
  function resetActiveNormalization() { deckManager.setActiveNormalization(1) }
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
    normalization, preamp, equalizer, postgain, limiter,
    signalChain, analyserBridge, scheduler,
    setActiveNormalization, setPendingNormalization,
    resetActiveNormalization, resetPendingNormalization,
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
  play: (url: string) => Promise<void>
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
  setActiveNormalization: (integrated: number, truePeak: number) => void
  setPendingNormalization: (integrated: number, truePeak: number) => void
  resetActiveNormalization: () => void
  resetPendingNormalization: () => void
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
}

export function useAudioEngine() {
  if (import.meta.server) return serverStub
  if (!instance) instance = createEngine()
  return instance
}
