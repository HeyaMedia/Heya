// Engine-side types shared between the DSP blocks, scheduler, and composables.

export interface DSPBlock {
  readonly name: string
  enabled: boolean
  connect(input: AudioNode): AudioNode
  dispose(): void
  getParams?(): Record<string, AudioParam | number>
}

export interface DSPBlockState {
  name: string
  enabled: boolean
}

export type CrossfadeMode = 'timed' | 'smart'

// Headphone crossfeed strength. See engine/dsp/crossfeed.ts.
export type CrossfeedPreset = 'subtle' | 'natural' | 'strong'

export interface CodecSupport {
  flac: boolean
  alac: boolean
  aac: boolean
  mp3: boolean
  opus: boolean
  vorbis: boolean
  wav: boolean
  pcm: boolean
  wma: boolean
  aiff: boolean
  webm: boolean
  ac3: boolean
  eac3: boolean
  dsd: boolean
  dsf: boolean
  m4a: boolean
}

export interface EQPreset {
  name: string
  builtin: boolean
  preamp: number
  postgain: number
  bands: number[]
}
