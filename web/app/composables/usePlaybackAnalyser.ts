import type { AnalyserBridge } from '~/engine/analysis/analyserBridge'
import { useAudioSettingsStore } from '~/stores/audio-settings'

// Live count of mounted visualizer consumers. The native engine only streams
// FFT/PCM frames while someone is actually drawing them — this is what flips
// `visualizerEnabled` in nativeProcessingSettings().
const nativeAnalyserDemand = ref(0)

/** True while any mounted visualizer is consuming analyser frames. */
export function nativeAnalyserDemandActive(): boolean {
  return nativeAnalyserDemand.value > 0
}

/**
 * Backend-neutral analyser data for Heya's lightweight canvas visualizers.
 *
 * Browser playback reads the existing WebAudio AnalyserNode. Native playback
 * reads HeyaClient's bounded PCM/FFT snapshots; Milkdrop adapts those copied
 * samples into butterchurn's explicit audio-level input.
 */
export function usePlaybackAnalyser(options: {
  registerNativeDemand?: boolean
  connectBrowserEngine?: boolean
} = {}) {
  const player = usePlayerBindings()
  const engine = options.connectBrowserEngine === false
    ? null
    : useAudioEngine() as ReturnType<typeof useAudioEngine> & { analyserBridge?: AnalyserBridge }

  // Each component instance counts as demand for its scope's lifetime, and
  // every 0↔1 flip re-pushes processing settings so the native engine starts
  // or stops its visualizer tap.
  if (options.registerNativeDemand !== false && getCurrentScope()) {
    const settings = useAudioSettingsStore()
    nativeAnalyserDemand.value++
    if (nativeAnalyserDemand.value === 1) settings.applyToEngine()
    onScopeDispose(() => {
      nativeAnalyserDemand.value = Math.max(0, nativeAnalyserDemand.value - 1)
      if (nativeAnalyserDemand.value === 0) settings.applyToEngine()
    })
  }

  let nativeRevision = -1
  let nativeTimeData = new Float32Array(0)
  let nativeFrequencyData = new Float32Array(0)

  function nativeFrame() {
    if (player.playbackBackend.value !== 'native') return null
    const frame = player.nativeAudioVisualizer.value
    if (!frame) return null
    if (frame.visualizerRevision !== nativeRevision) {
      nativeRevision = frame.visualizerRevision
      nativeTimeData = Float32Array.from(frame.samples)
      nativeFrequencyData = Float32Array.from(frame.frequencyBins)
    }
    return frame
  }

  const isNative = computed(() => player.playbackBackend.value === 'native')
  const available = computed(() => isNative.value
    ? player.nativeAudioVisualizer.value != null
    : engine?.analyserBridge != null)

  function getFrequencyData(): Float32Array {
    return nativeFrame() ? nativeFrequencyData : (engine?.analyserBridge?.getFrequencyData() ?? new Float32Array(0))
  }

  function getTimeDomainData(): Float32Array {
    return nativeFrame() ? nativeTimeData : (engine?.analyserBridge?.getTimeDomainData() ?? new Float32Array(0))
  }

  function fftSize(): number {
    if (nativeFrame()) return Math.max(2, (nativeFrequencyData.length - 1) * 2)
    return engine?.analyserBridge?.analyserNode.fftSize ?? 2048
  }

  function sampleRate(): number {
    if (nativeFrame()) return player.nativeAudioState.value?.outputSampleRateHz ?? 48_000
    return engine?.analyserBridge?.analyserNode.context.sampleRate ?? 48_000
  }

  return {
    available,
    isNative,
    getFrequencyData,
    getTimeDomainData,
    fftSize,
    sampleRate,
  }
}
