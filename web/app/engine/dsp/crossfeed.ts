import type { CrossfeedPreset, DSPBlock } from '~~/shared/types/audio'

interface CrossfeedConfig {
  bleed: number   // 0..1 — how much of each channel leaks into the other
  cutoff: number  // Hz — bleed is low-passed so only the spatial low end crosses
}

const PRESETS: Record<CrossfeedPreset, CrossfeedConfig> = {
  subtle: { bleed: 0.15, cutoff: 500 },
  natural: { bleed: 0.30, cutoff: 700 },
  strong: { bleed: 0.45, cutoff: 900 },
}

// ~0.3ms interaural delay on the bleed path — the head-shadow timing cue that
// makes the crossfeed read as space rather than as a mono collapse.
const BLEED_DELAY_SECONDS = 0.0003

// Meier-style headphone crossfeed. Headphones pan hard L/R with zero acoustic
// crosstalk, which fatigues on records mixed for speakers. This bleeds a
// low-passed, slightly-delayed copy of each channel into the opposite ear,
// recreating the natural crosstalk you'd get from speakers in a room.
//
//   split → [L direct]                 ┐
//         → [L → lowpass → delay → g] ─┤→ merge
//           [R direct]                 ┘
//         → [R → lowpass → delay → g]
//
// Bypasses cleanly when disabled (connect returns the input untouched). Mono
// content is essentially unaffected; only stereo width is tamed.
export function createCrossfeed(ctx: AudioContext): DSPBlock & {
  setPreset(preset: CrossfeedPreset): void
} {
  const splitter = ctx.createChannelSplitter(2)
  const merger = ctx.createChannelMerger(2)

  const directL = ctx.createGain()
  const directR = ctx.createGain()

  const bleedLFilter = ctx.createBiquadFilter() // L → R bleed
  const bleedRFilter = ctx.createBiquadFilter() // R → L bleed
  bleedLFilter.type = 'lowpass'
  bleedRFilter.type = 'lowpass'

  const bleedLDelay = ctx.createDelay(1)
  const bleedRDelay = ctx.createDelay(1)
  bleedLDelay.delayTime.value = BLEED_DELAY_SECONDS
  bleedRDelay.delayTime.value = BLEED_DELAY_SECONDS

  const bleedLGain = ctx.createGain()
  const bleedRGain = ctx.createGain()

  // (Re)build the internal splitter→…→merger graph. Called from connect() —
  // NOT just at creation — because dispose() (run on every signalChain rebuild)
  // tears these edges down; without re-wiring them, an enabled crossfeed would
  // route input→splitter→(nothing)→merger and silence the chain. Web Audio
  // dedupes identical connections, so calling this repeatedly is harmless.
  function wireInternal() {
    // Left channel (splitter output 0): straight to L (merger input 0), bleed to R (1).
    splitter.connect(directL, 0)
    directL.connect(merger, 0, 0)
    splitter.connect(bleedLFilter, 0)
    bleedLFilter.connect(bleedLDelay)
    bleedLDelay.connect(bleedLGain)
    bleedLGain.connect(merger, 0, 1)

    // Right channel (splitter output 1): straight to R (merger input 1), bleed to L (0).
    splitter.connect(directR, 1)
    directR.connect(merger, 0, 1)
    splitter.connect(bleedRFilter, 1)
    bleedRFilter.connect(bleedRDelay)
    bleedRDelay.connect(bleedRGain)
    bleedRGain.connect(merger, 0, 0)
  }

  function setPreset(preset: CrossfeedPreset) {
    const cfg = PRESETS[preset] ?? PRESETS.natural
    const t = ctx.currentTime
    // direct + bleed ≈ unity keeps the perceived level steady across presets.
    directL.gain.setValueAtTime(1 - cfg.bleed, t)
    directR.gain.setValueAtTime(1 - cfg.bleed, t)
    bleedLGain.gain.setValueAtTime(cfg.bleed, t)
    bleedRGain.gain.setValueAtTime(cfg.bleed, t)
    bleedLFilter.frequency.setValueAtTime(cfg.cutoff, t)
    bleedRFilter.frequency.setValueAtTime(cfg.cutoff, t)
  }
  setPreset('natural')

  return {
    name: 'crossfeed',
    enabled: false,

    connect(input: AudioNode): AudioNode {
      if (this.enabled) {
        wireInternal()
        input.connect(splitter)
        return merger
      }
      return input
    },

    setPreset,

    dispose() {
      splitter.disconnect()
      directL.disconnect()
      directR.disconnect()
      bleedLFilter.disconnect()
      bleedRFilter.disconnect()
      bleedLDelay.disconnect()
      bleedRDelay.disconnect()
      bleedLGain.disconnect()
      bleedRGain.disconnect()
      merger.disconnect()
    },
  }
}
