import type { DSPBlock } from '~~/shared/types/audio'

// Sits at the end of the chain as a brick-wall guard. The compressor's hard
// settings (1ms attack, 20:1 ratio, -1dB threshold) prevent clipping if the
// EQ or normalization boosts a signal beyond 0 dBFS.
export function createLimiter(ctx: AudioContext): DSPBlock {
  const compressor = ctx.createDynamicsCompressor()
  compressor.threshold.setValueAtTime(-1, ctx.currentTime)
  compressor.ratio.setValueAtTime(20, ctx.currentTime)
  compressor.attack.setValueAtTime(0.003, ctx.currentTime)
  compressor.release.setValueAtTime(0.25, ctx.currentTime)
  compressor.knee.setValueAtTime(0, ctx.currentTime)

  return {
    name: 'limiter',
    enabled: true,
    connect(input: AudioNode): AudioNode {
      if (this.enabled) { input.connect(compressor); return compressor }
      return input
    },
    dispose() { compressor.disconnect() },
    getParams() {
      return {
        threshold: compressor.threshold,
        ratio: compressor.ratio,
        attack: compressor.attack,
        release: compressor.release,
        knee: compressor.knee,
        reduction: compressor.reduction,
      }
    },
  }
}
