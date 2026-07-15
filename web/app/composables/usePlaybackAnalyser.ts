import type { AnalyserBridge } from '~/engine/analysis/analyserBridge'

/**
 * Backend-neutral analyser data for Heya's lightweight canvas visualizers.
 *
 * Browser playback reads the existing WebAudio AnalyserNode. Native processed
 * playback reads HeyaClient's bounded PCM/FFT snapshots. Milkdrop is excluded:
 * butterchurn requires a real AnalyserNode connection, not copied sample data.
 */
export function usePlaybackAnalyser() {
  const player = usePlayerBindings()
  const engine = useAudioEngine() as ReturnType<typeof useAudioEngine> & {
    analyserBridge?: AnalyserBridge
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
    : engine.analyserBridge != null)

  function getFrequencyData(): Float32Array {
    return nativeFrame() ? nativeFrequencyData : (engine.analyserBridge?.getFrequencyData() ?? new Float32Array(0))
  }

  function getTimeDomainData(): Float32Array {
    return nativeFrame() ? nativeTimeData : (engine.analyserBridge?.getTimeDomainData() ?? new Float32Array(0))
  }

  function fftSize(): number {
    if (nativeFrame()) return Math.max(2, (nativeFrequencyData.length - 1) * 2)
    return engine.analyserBridge?.analyserNode.fftSize ?? 2048
  }

  function sampleRate(): number {
    if (nativeFrame()) return player.nativeAudioState.value?.outputSampleRateHz ?? 48_000
    return engine.analyserBridge?.analyserNode.context.sampleRate ?? 48_000
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
