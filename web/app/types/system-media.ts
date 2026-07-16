export type SystemMediaPlatform = 'macos' | 'windows' | 'linux' | 'unsupported'

export interface SystemMediaCapabilities {
  protocolVersion: 1
  available: boolean
  platform: SystemMediaPlatform
  nowPlaying: boolean
  mediaCommands: boolean
  trackNotifications: boolean
  trackNotificationsEnabled: boolean
  artwork: boolean
  unavailableReason?: string
}

export interface SystemMediaArtwork {
  cacheKey: string
  mimeType: 'image/jpeg' | 'image/png'
  base64Data: string
}

export interface SystemMediaSnapshot {
  revision: number
  itemKey: string
  title: string
  artist?: string
  album?: string
  durationSeconds: number
  positionSeconds: number
  playbackState: 'playing' | 'paused'
  canGoPrevious: boolean
  canGoNext: boolean
  canSeek: boolean
  artwork?: SystemMediaArtwork
}

export interface SystemMediaTrackNotification {
  revision: number
  itemKey: string
}

export type SystemMediaCommand = {
  commandSequence: number
} & (
  | { type: 'play' }
  | { type: 'pause' }
  | { type: 'togglePlayPause' }
  | { type: 'previous' }
  | { type: 'next' }
  | { type: 'stop' }
  | { type: 'seekTo', positionSeconds: number }
  | { type: 'seekBy', offsetSeconds: number }
)

export interface HeyaSystemMediaBridge {
  readonly protocolVersion: 1
  getSystemMediaCapabilities(): Promise<SystemMediaCapabilities>
  updateSystemMedia(snapshot: SystemMediaSnapshot): Promise<void>
  clearSystemMedia(request: { revision: number }): Promise<void>
  notifyTrackChanged(request: SystemMediaTrackNotification): Promise<{ shown: boolean }>
  subscribeSystemMediaCommands(listener: (command: SystemMediaCommand) => void): () => void
}

declare global {
  interface Window {
    readonly __HEYA_SYSTEM_MEDIA__?: Readonly<HeyaSystemMediaBridge>
  }

  interface WindowEventMap {
    'heya:system-media:ready-v1': CustomEvent<{
      protocolVersion: 1
      capabilities: SystemMediaCapabilities
    }>
  }
}

export {}
