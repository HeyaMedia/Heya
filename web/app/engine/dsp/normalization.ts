import type { DSPBlock } from '~~/shared/types/audio'

// EBU R128 target, user-tunable (Settings → EQ → Playback). -14 LUFS matches
// the streaming-service norm (Spotify, Tidal, YouTube); the classic
// ReplayGain -18 reference leaves modern masters ~10 dB below native level,
// which reads as "way too quiet" next to normalization-off playback.
export const DEFAULT_TARGET_LUFS = -14
export const MIN_TARGET_LUFS = -23
export const MAX_TARGET_LUFS = -8
const MAX_GAIN_DB = 12

function clampGain(db: number): number {
  return Math.max(-MAX_GAIN_DB, Math.min(MAX_GAIN_DB, db))
}

function dbToLinear(db: number): number { return 10 ** (db / 20) }

export function clampTargetLufs(value: number): number {
  if (!Number.isFinite(value)) return DEFAULT_TARGET_LUFS
  return Math.max(MIN_TARGET_LUFS, Math.min(MAX_TARGET_LUFS, value))
}

// Pull a per-track linear gain from integrated LUFS + true-peak. Backs off
// from the LUFS target when applying the gain would push the peak past -1dB,
// avoiding clipping that would otherwise need to go through the limiter.
export function computeNormalizationGain(
  integrated: number,
  truePeak: number,
  targetLufs: number = DEFAULT_TARGET_LUFS,
): number {
  let gainDb = clampTargetLufs(targetLufs) - integrated
  const peakAfterGain = truePeak + gainDb
  if (peakAfterGain > -1) gainDb -= peakAfterGain + 1
  gainDb = clampGain(gainDb)
  return dbToLinear(gainDb)
}

export function createNormalization(
  ctx: AudioContext,
): DSPBlock & { setLoudness(integrated: number, truePeak: number, targetLufs?: number): void } {
  const gainNode = ctx.createGain()
  return {
    name: 'normalization',
    enabled: true,
    connect(input: AudioNode): AudioNode {
      if (this.enabled) { input.connect(gainNode); return gainNode }
      return input
    },
    setLoudness(integrated: number, truePeak: number, targetLufs?: number) {
      const linear = computeNormalizationGain(integrated, truePeak, targetLufs)
      gainNode.gain.cancelScheduledValues(ctx.currentTime)
      gainNode.gain.setValueAtTime(gainNode.gain.value, ctx.currentTime)
      gainNode.gain.linearRampToValueAtTime(linear, ctx.currentTime + 0.1)
    },
    dispose() { gainNode.disconnect() },
    getParams() { return { gain: gainNode.gain } },
  }
}
