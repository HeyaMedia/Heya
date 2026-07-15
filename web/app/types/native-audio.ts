import type {
  NativePlaybackErrorCode,
  NativePlaybackTerminationReason,
} from '~/types/native-playback'

export type NativeAudioOutputMode = 'processed' | 'bit_perfect'
export type NativeAudioCrossfadeMode = 'gapless' | 'crossfade' | 'smart'

export interface NativeAudioProcessingSettings {
  replayGainEnabled: boolean
  eqEnabled: boolean
  eqBandsDb: number[]
  preampDb: number
  postgainDb: number
  limiterEnabled: boolean
  crossfeedEnabled: boolean
  crossfeedPreset: 'subtle' | 'natural' | 'strong'
  dspOrder: Array<'equalizer' | 'crossfeed'>
  crossfadeMode: NativeAudioCrossfadeMode
  crossfadeSeconds: number
  albumAware: boolean
  visualizerEnabled: boolean
}

export interface NativeAudioMediaRequest {
  mediaUrl: string
  playbackGrant: string
  startPositionSeconds?: number
}

export interface NativeAudioTrackRequest {
  trackId: number
  durationSeconds: number
  albumKey: string
  formatHint?: string
  codec?: string
  sampleRateHz?: number
  bitDepth?: number
  channels?: number
  lossless?: boolean
  gainDb?: number
  media: NativeAudioMediaRequest
}

export interface NativeAudioLoadRequest {
  mode: NativeAudioOutputMode
  processing: NativeAudioProcessingSettings
  track: NativeAudioTrackRequest
}

export interface NativeAudioPreloadRequest {
  rendererSessionId: string
  commandId: string
  track: NativeAudioTrackRequest
}

export interface NativeAudioCapabilities {
  protocolVersion: 1
  backend: 'heya-rust-audio'
  available: boolean
  gapless: boolean
  crossfade: boolean
  replayGain: boolean
  equalizer: boolean
  visualizer: boolean
  outputDeviceSelection: boolean
  preferredOutputMode: NativeAudioOutputMode
  bitPerfect: {
    available: boolean
    requiresExclusiveDevice: boolean
    unavailableReason?: string
  }
  unavailableReason?: NativePlaybackErrorCode
}

export interface NativeAudioState {
  playing: boolean
  paused: boolean
  loading: boolean
  buffering: boolean
  ended: boolean
  positionSeconds: number
  durationSeconds: number
  volume: number
  muted: boolean
  currentTrackId: number | null
  startedTrackId: number | null
  endedTrackId: number | null
  outputMode: NativeAudioOutputMode
  bitPerfectActive: boolean
  sourceSampleRateHz: number | null
  sourceChannels: number | null
  outputSampleRateHz: number | null
  outputChannels: number | null
  resamplerActive: boolean
  dspActive: boolean
  error?: { code: NativePlaybackErrorCode, message: string }
  terminationReason?: NativePlaybackTerminationReason
}

export interface NativeAudioStateEvent {
  protocolVersion: 1
  rendererSessionId: string
  stateRevision: number
  payload: NativeAudioState
}

export interface NativeAudioVisualizerEvent {
  protocolVersion: 1
  rendererSessionId: string
  visualizerRevision: number
  samples: ReadonlyArray<number>
  frequencyBins: ReadonlyArray<number>
}

export type NativeAudioCommand = {
  rendererSessionId: string
  commandId: string
} & (
  | { type: 'play' }
  | { type: 'pause' }
  | { type: 'seek', positionSeconds: number }
  | { type: 'setVolume', volume: number }
  | { type: 'setMuted', muted: boolean }
  | { type: 'updateProcessing', settings: NativeAudioProcessingSettings }
  | { type: 'stop' }
)

export interface NativeAudioCommandResult {
  rendererSessionId: string
  commandId: string
  commandSequence: number
  accepted: boolean
  duplicate: boolean
  error?: { code: NativePlaybackErrorCode, message: string }
}

export interface HeyaNativeAudioBridge {
  readonly protocolVersion: 1
  getAudioCapabilities(): Promise<NativeAudioCapabilities>
  setAudioOutputMode(mode: NativeAudioOutputMode): Promise<NativeAudioCapabilities>
  loadAudio(request: NativeAudioLoadRequest): Promise<{
    rendererSessionId: string
    activeMode: NativeAudioOutputMode
  }>
  preloadNextAudio(request: NativeAudioPreloadRequest): Promise<NativeAudioCommandResult>
  sendAudioCommand(command: NativeAudioCommand): Promise<NativeAudioCommandResult>
  subscribeAudioState(listener: (event: NativeAudioStateEvent) => void): () => void
  subscribeAudioVisualizer(listener: (event: NativeAudioVisualizerEvent) => void): () => void
  disposeAudio(request: { rendererSessionId: string }): Promise<void>
}

declare global {
  interface Window {
    readonly __HEYA_NATIVE_AUDIO__?: Readonly<HeyaNativeAudioBridge>
  }

  interface WindowEventMap {
    'heya:native-audio:ready-v1': CustomEvent<{
      protocolVersion: 1
      capabilities: NativeAudioCapabilities
    }>
  }
}

export {}
