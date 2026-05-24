import type { DSPBlock } from '~~/shared/types/audio'

// EBU R128 target. -18 LUFS gives consistent loudness across a library
// without crushing the dynamic range of well-mastered tracks.
const TARGET_LUFS = -18
const MAX_GAIN_DB = 12

function clampGain(db: number): number {
  return Math.max(-MAX_GAIN_DB, Math.min(MAX_GAIN_DB, db))
}

function dbToLinear(db: number): number { return 10 ** (db / 20) }

// Pull a per-track linear gain from integrated LUFS + true-peak. Backs off
// from the LUFS target when applying the gain would push the peak past -1dB,
// avoiding clipping that would otherwise need to go through the limiter.
export function computeNormalizationGain(integrated: number, truePeak: number): number {
  let gainDb = TARGET_LUFS - integrated
  const peakAfterGain = truePeak + gainDb
  if (peakAfterGain > -1) gainDb -= peakAfterGain + 1
  gainDb = clampGain(gainDb)
  return dbToLinear(gainDb)
}

export function createNormalization(
  ctx: AudioContext,
): DSPBlock & { setLoudness(integrated: number, truePeak: number): void } {
  const gainNode = ctx.createGain()
  return {
    name: 'normalization',
    enabled: true,
    connect(input: AudioNode): AudioNode {
      if (this.enabled) { input.connect(gainNode); return gainNode }
      return input
    },
    setLoudness(integrated: number, truePeak: number) {
      const linear = computeNormalizationGain(integrated, truePeak)
      gainNode.gain.cancelScheduledValues(ctx.currentTime)
      gainNode.gain.setValueAtTime(gainNode.gain.value, ctx.currentTime)
      gainNode.gain.linearRampToValueAtTime(linear, ctx.currentTime + 0.1)
    },
    dispose() { gainNode.disconnect() },
    getParams() { return { gain: gainNode.gain } },
  }
}
