export type VideoPlaybackBackendKind = 'browser' | 'mpv' | 'cast'

export interface VideoPlaybackState {
  playing: boolean
  paused: boolean
  ended: boolean
  loading: boolean
  buffering: boolean
  currentTime: number
  duration: number
  buffered: number
  volume: number
  muted: boolean
  fullscreen: boolean
  error: string | null

  // Monotonic counter for user-visible seeks. Consumers can react to seeks
  // without treating every position update as a seek.
  seekRevision: number
}

export interface VideoPlaybackTransportDiagnostics {
  bufferedSeconds?: number
  bufferedBytes?: number
  inputBytesPerSecond?: number
  segmentsLoaded?: number
  activeVariantIndex?: number
  lastSegmentBytes?: number
  lastSegmentMilliseconds?: number
}

export interface VideoPlaybackVideoDiagnostics {
  source?: {
    codec?: string
    profile?: string
    width?: number
    height?: number
    nominalFramesPerSecond?: number
    bitrateBitsPerSecond?: number
  }
  decoded?: {
    pixelFormat?: string
    measuredFramesPerSecond?: number
    hardwareDecoder?: string
    hardwareInterop?: string
  }
  output?: {
    width?: number
    height?: number
    pixelFormat?: string
  }
  color?: {
    primaries?: string
    transfer?: string
    matrix?: string
    dolbyVisionProfile?: number
    maxContentLight?: number
    maxFrameAverageLight?: number
  }
}

export interface VideoPlaybackAudioDiagnostics {
  source?: {
    codec?: string
    profile?: string
    channels?: string
    sampleRate?: number
    sampleFormat?: string
    bitrateBitsPerSecond?: number
  }
  output?: {
    device?: string
    channels?: string
    sampleRate?: number
    sampleFormat?: string
  }
}

export interface VideoPlaybackHealthDiagnostics {
  decodedFrames?: number
  droppedFrames?: number
  decoderDroppedFrames?: number
  mistimedFrames?: number
  avSyncMilliseconds?: number
}

// Diagnostics are deliberately optional and non-authoritative. A backend may
// omit or withdraw any measurement without affecting playback controls/state.
export interface VideoPlaybackDiagnostics {
  backend: VideoPlaybackBackendKind
  sampledAtMilliseconds?: number
  transport?: VideoPlaybackTransportDiagnostics
  video?: VideoPlaybackVideoDiagnostics
  audio?: VideoPlaybackAudioDiagnostics
  health?: VideoPlaybackHealthDiagnostics
}

export interface VideoPlaybackCapabilities {
  backend: VideoPlaybackBackendKind
  videoSurface: 'html-media-element' | 'native-window' | 'native-surface' | 'remote'
  diagnostics: boolean
  audioTrackSelection: boolean
  subtitleTrackSelection: boolean
  qualitySelection: boolean
}

// Backends expose desired-state commands rather than toggles. This keeps
// asynchronous/native renderers deterministic when commands are delayed.
export interface VideoPlaybackControls {
  play: () => void | Promise<void>
  pause: () => void | Promise<void>
  seek: (seconds: number) => void | Promise<void>
  setVolume: (level: number) => void | Promise<void>
  setMuted: (muted: boolean) => void | Promise<void>
  setFullscreen: (fullscreen: boolean) => void | Promise<void>
}

export interface VideoPlaybackBackend<TLoadRequest> {
  readonly kind: VideoPlaybackBackendKind
  readonly capabilities: Readonly<VideoPlaybackCapabilities>
  readonly state: VideoPlaybackState
  readonly diagnostics: VideoPlaybackDiagnostics | null
  readonly controls: VideoPlaybackControls
  load: (request: TLoadRequest) => void | Promise<void>
  dispose: () => void | Promise<void>
}
