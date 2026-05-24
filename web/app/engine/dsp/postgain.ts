import type { DSPBlock } from '~~/shared/types/audio'

const MAX_GAIN_DB = 12

// Post-EQ make-up gain. Lets the user push the chain back up after a preamp
// dip without re-engaging clipping on the limiter.
export function createPostgain(ctx: AudioContext): DSPBlock & { setGain(db: number): void } {
  const gainNode = ctx.createGain()
  const dbToLinear = (db: number) => 10 ** (db / 20)

  return {
    name: 'postgain',
    enabled: true,
    connect(input: AudioNode): AudioNode {
      if (this.enabled) { input.connect(gainNode); return gainNode }
      return input
    },
    setGain(db: number) {
      const clamped = Math.max(-MAX_GAIN_DB, Math.min(MAX_GAIN_DB, db))
      gainNode.gain.setValueAtTime(dbToLinear(clamped), ctx.currentTime)
    },
    dispose() { gainNode.disconnect() },
    getParams() { return { gain: gainNode.gain } },
  }
}
