import type { DSPBlock } from '~~/shared/types/audio'

const FREQUENCIES = [32, 64, 125, 250, 500, 1000, 2000, 4000, 8000, 16000]
const DEFAULT_Q = 1.41
const MAX_GAIN_DB = 12

// 10-band parametric EQ (low-shelf, 8 peaking, high-shelf). Bands are chained
// internally; connect/disconnect routes them in/out depending on `enabled`.
export function createEqualizer(ctx: AudioContext): DSPBlock & {
  setBand(index: number, gain: number): void
  setAllBands(gains: number[]): void
  reset(): void
} {
  const filters: BiquadFilterNode[] = FREQUENCIES.map((freq, i) => {
    const filter = ctx.createBiquadFilter()
    filter.frequency.setValueAtTime(freq, ctx.currentTime)
    filter.gain.setValueAtTime(0, ctx.currentTime)
    if (i === 0) filter.type = 'lowshelf'
    else if (i === FREQUENCIES.length - 1) filter.type = 'highshelf'
    else { filter.type = 'peaking'; filter.Q.setValueAtTime(DEFAULT_Q, ctx.currentTime) }
    return filter
  })

  for (let i = 0; i < filters.length - 1; i++) filters[i]!.connect(filters[i + 1]!)

  function clampGain(db: number): number {
    return Math.max(-MAX_GAIN_DB, Math.min(MAX_GAIN_DB, db))
  }

  return {
    name: 'equalizer',
    enabled: true,

    connect(input: AudioNode): AudioNode {
      if (this.enabled) {
        for (let i = 0; i < filters.length - 1; i++) filters[i]!.connect(filters[i + 1]!)
        input.connect(filters[0]!)
        return filters[filters.length - 1]!
      }
      return input
    },

    setBand(index: number, gain: number) {
      if (index < 0 || index >= filters.length) return
      filters[index]!.gain.setValueAtTime(clampGain(gain), ctx.currentTime)
    },

    setAllBands(gains: number[]) {
      for (let i = 0; i < filters.length; i++) {
        const gain = gains[i] ?? 0
        filters[i]!.gain.setValueAtTime(clampGain(gain), ctx.currentTime)
      }
    },

    reset() {
      for (const filter of filters) filter.gain.setValueAtTime(0, ctx.currentTime)
    },

    dispose() {
      for (const filter of filters) filter.disconnect()
    },

    getParams() {
      const params: Record<string, AudioParam> = {}
      for (let i = 0; i < filters.length; i++) {
        params[`band${i}_gain`] = filters[i]!.gain
        params[`band${i}_freq`] = filters[i]!.frequency
      }
      return params
    },
  }
}
