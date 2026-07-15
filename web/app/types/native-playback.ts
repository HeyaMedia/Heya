import type { VideoPlaybackDiagnostics } from '~/types/video-playback'

export type NativePlaybackTerminationReason =
  | 'ended'
  | 'stopped'
  | 'window_closed'
  | 'disposed'
  | 'failed'
  | 'native_crashed'
  | 'logged_out'
  | 'server_switched'
  | 'app_quit'

export type NativePlaybackErrorCode =
  | 'invalid_request'
  | 'protocol_mismatch'
  | 'origin_not_allowed'
  | 'playback_grant_required'
  | 'backend_unavailable'
  | 'unknown_session'
  | 'renderer_stopping'
  | 'command_failed'
  | 'internal_error'

export interface NativePlaybackCapabilities {
  protocolVersion: 1
  backend: 'mpv'
  available: boolean
  videoSurface: 'native-surface' | 'native-window'
  diagnostics: boolean
  audioTrackSelection: boolean
  subtitleTrackSelection: boolean
  qualitySelection: boolean
  unavailableReason?: NativePlaybackErrorCode
}

export interface NativePlaybackTrack {
  id: string
  kind: 'audio' | 'subtitle'
  language?: string
  title?: string
  selected: boolean
}

export interface NativePlaybackState {
  playing: boolean
  paused: boolean
  ended: boolean
  loading: boolean
  buffering: boolean
  videoSurfaceReady: boolean
  currentTime: number
  duration: number
  buffered: number
  volume: number
  muted: boolean
  fullscreen: boolean
  seekRevision: number
  audioTracks: NativePlaybackTrack[]
  subtitleTracks: NativePlaybackTrack[]
  selectedAudioTrackId?: string
  selectedSubtitleTrackId?: string
  error?: { code: NativePlaybackErrorCode, message: string }
  terminationReason?: NativePlaybackTerminationReason
}

export interface NativePlaybackStateEvent {
  protocolVersion: 1
  rendererSessionId: string
  stateRevision: number
  payload: NativePlaybackState
}

export interface NativePlaybackDiagnosticsEvent {
  protocolVersion: 1
  rendererSessionId: string
  diagnosticsRevision: number
  payload: (VideoPlaybackDiagnostics & { backend: 'mpv' }) | null
}

export interface NativePlaybackLoadRequest {
  mediaUrl: string
  playbackGrant: string
  startPositionSeconds?: number
}

export type NativePlaybackCommand = {
  rendererSessionId: string
  commandId: string
} & (
  | { type: 'play' }
  | { type: 'pause' }
  | { type: 'seek', positionSeconds: number }
  | { type: 'setVolume', volume: number }
  | { type: 'setMuted', muted: boolean }
  | { type: 'setFullscreen', fullscreen: boolean }
  | { type: 'selectAudioTrack', trackId: string }
  | { type: 'selectSubtitleTrack', trackId: string | null }
  | { type: 'selectVariant', variantId: string }
  | { type: 'stop' }
)

export interface NativePlaybackCommandResult {
  rendererSessionId: string
  commandId: string
  commandSequence: number
  accepted: boolean
  duplicate: boolean
  error?: { code: NativePlaybackErrorCode, message: string }
}

export interface HeyaNativePlaybackBridge {
  readonly protocolVersion: 1
  getPlaybackCapabilities(): Promise<NativePlaybackCapabilities>
  loadPlayback(request: NativePlaybackLoadRequest): Promise<{
    rendererSessionId: string
    videoSurface: 'native-surface' | 'native-window'
  }>
  sendPlaybackCommand(command: NativePlaybackCommand): Promise<NativePlaybackCommandResult>
  subscribePlaybackState(listener: (event: NativePlaybackStateEvent) => void): () => void
  subscribePlaybackDiagnostics(listener: (event: NativePlaybackDiagnosticsEvent) => void): () => void
  disposePlayback(request: { rendererSessionId: string }): Promise<void>
}

declare global {
  interface Window {
    readonly __HEYA_NATIVE_PLAYBACK__?: Readonly<HeyaNativePlaybackBridge>
  }

  interface WindowEventMap {
    'heya:native-playback:ready-v1': CustomEvent<{
      protocolVersion: 1
      capabilities: NativePlaybackCapabilities
    }>
  }
}

export {}
