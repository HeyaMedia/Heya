import { ref } from 'vue'

let ctx: AudioContext | null = null

const state = ref<AudioContextState>('suspended')
const sampleRate = ref(0)

export function getAudioContext(): AudioContext {
  if (!ctx) {
    ctx = new AudioContext()
    sampleRate.value = ctx.sampleRate
    ctx.addEventListener('statechange', () => {
      state.value = ctx!.state
    })
    state.value = ctx.state
  }
  return ctx
}

// Idle auto-suspend (same idea as howler's autoSuspend): a running-but-silent
// AudioContext keeps the browser's realtime audio thread pulling the whole
// DSP graph every render quantum, forever. The engine arms this on pause/stop;
// any play path cancels it via resumeContext().
let idleSuspendTimer: ReturnType<typeof setTimeout> | null = null
let idleSuspendWanted = false
let contextWakeHolds = 0

export function scheduleIdleSuspend(delayMs = 15_000): void {
  idleSuspendWanted = true
  if (idleSuspendTimer) clearTimeout(idleSuspendTimer)
  idleSuspendTimer = setTimeout(() => {
    idleSuspendTimer = null
    if (!idleSuspendWanted || contextWakeHolds > 0) return
    if (ctx && ctx.state === 'running') void ctx.suspend().catch(() => {})
  }, delayMs)
}

export function cancelIdleSuspend(): void {
  idleSuspendWanted = false
  if (idleSuspendTimer) {
    clearTimeout(idleSuspendTimer)
    idleSuspendTimer = null
  }
}

// Components that need live analyser time even while playback is paused
// (Milkdrop's AnalyserNode connection) hold a wake for their lifetime.
// Releasing the last hold re-arms any suspend that was deferred by it.
export function acquireContextWake(): void {
  contextWakeHolds++
}

export function releaseContextWake(): void {
  contextWakeHolds = Math.max(0, contextWakeHolds - 1)
  if (contextWakeHolds === 0 && idleSuspendWanted) scheduleIdleSuspend()
}

export async function resumeContext(): Promise<void> {
  cancelIdleSuspend()
  const ac = getAudioContext()
  if (ac.state === 'suspended') {
    await ac.resume()
  }
}

export async function closeContext(): Promise<void> {
  if (ctx) {
    await ctx.close()
    ctx = null
    state.value = 'closed'
    sampleRate.value = 0
  }
}

// Whether this browser can route the AudioContext to a chosen output device.
// The capability that matters is AudioContext.setSinkId — Chromium has it,
// Safari/Firefox don't (they DO expose enumerateDevices, so testing that
// misdetects them as supported). Probe the prototype so we don't have to
// construct a context just to answer the question.
export function audioSinkSupported(): boolean {
  return typeof AudioContext !== 'undefined' && 'setSinkId' in AudioContext.prototype
}

// setSinkId routes the AudioContext to a specific output device (when the
// browser supports it — Chromium yes, Safari/Firefox lag here).
export async function setAudioSinkId(deviceId: string): Promise<boolean> {
  const ac = getAudioContext()
  if ('setSinkId' in ac && typeof (ac as unknown as { setSinkId: (id: string) => Promise<void> }).setSinkId === 'function') {
    try {
      await (ac as unknown as { setSinkId: (id: string) => Promise<void> }).setSinkId(deviceId)
      return true
    } catch {
      return false
    }
  }
  return false
}

export function useAudioContextState() {
  return { state, sampleRate }
}
