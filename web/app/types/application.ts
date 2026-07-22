export interface ApplicationCapabilities {
  protocolVersion: 1
  available: boolean
  platform: 'macos' | 'windows' | 'linux' | 'android' | 'ios' | string
  appVersion: string
  updaterSupported: boolean
}

export interface ApplicationProfile {
  id: string
  name: string
  origin: string
  lastConnectedAt: string | null
  serverVersion: string | null
}

export interface ApplicationSettings {
  reconnectOnLaunch: boolean
  nativePlaybackEnabled: boolean
  nativeAudioEnabled: boolean
  audioOutputDeviceId: string | null
  trackChangeNotifications: boolean
}

export interface MpvInstallationOffer {
  supported: boolean
  provider: string | null
  release: string | null
  downloadBytes: number | null
}

export interface NativePlaybackStatus {
  backend: string
  available: boolean
  buildIncludesNativeMpv: boolean
  videoSurface: string
  unavailableReason: string | null
  installation: MpvInstallationOffer
}

export interface NativeAudioStatus {
  backend: string
  available: boolean
  gapless: boolean
  crossfade: boolean
}

export interface ApplicationUpdateStatus {
  currentVersion: string
  available: boolean
  version: string | null
  notes: string | null
  publishedAt: string | null
}

export interface ApplicationSnapshot {
  capabilities: ApplicationCapabilities
  profile: ApplicationProfile | null
  settings: ApplicationSettings
  nativePlayback: NativePlaybackStatus
  nativeAudio: NativeAudioStatus
  update: ApplicationUpdateStatus | null
}

export interface HeyaApplicationBridge {
  readonly protocolVersion: 1
  getApplicationCapabilities(): Promise<ApplicationCapabilities>
  getApplicationSnapshot(): Promise<ApplicationSnapshot>
  saveApplicationSettings(settings: ApplicationSettings): Promise<ApplicationSettings>
  checkForApplicationUpdate(): Promise<ApplicationUpdateStatus>
  installApplicationUpdate(): Promise<void>
  installNativePlaybackRuntime(): Promise<NativePlaybackStatus>
  openServerPicker(): Promise<void>
  resetServerSession(): Promise<void>
  forgetServer(): Promise<void>
}

declare global {
  interface Window {
    readonly __HEYA_APPLICATION__?: Readonly<HeyaApplicationBridge>
  }

  interface WindowEventMap {
    'heya:application:ready-v1': CustomEvent<{
      protocolVersion: 1
      capabilities: ApplicationCapabilities
    }>
    'heya:application:open-settings-v1': CustomEvent<void>
  }
}
