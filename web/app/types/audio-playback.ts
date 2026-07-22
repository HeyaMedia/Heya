export type LocalAudioPlaybackBackendKind = 'browser' | 'native'

// A clock sample is the common state boundary between Heya's browser audio
// elements and HeyaClient's Rust renderer. Events may prompt a new sample,
// but consumers must be able to read the backend directly as well.
export interface AudioPlaybackClockSample {
  positionSeconds: number
  durationSeconds: number
  playing: boolean
  paused: boolean
  loading: boolean
  buffering: boolean
  ended: boolean
  sampledAtMilliseconds: number
}

export interface AudioPlaybackClockSource {
  readonly kind: LocalAudioPlaybackBackendKind
  readClock(): AudioPlaybackClockSample
  reconcileClock(): void | Promise<void>
}
