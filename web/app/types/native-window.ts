export type NativeWindowPlatform = 'macos' | 'windows' | 'linux'

export interface NativeWindowCapabilities {
  protocolVersion: 1
  platform: NativeWindowPlatform
  customTitlebar: boolean
  nativeControls: boolean
  draggable: boolean
  minimizable: boolean
  maximizable: boolean
  closable: boolean
}

export interface NativeWindowState {
  maximized: boolean
  fullscreen: boolean
  focused: boolean
}

export interface HeyaNativeWindowBridge {
  readonly protocolVersion: 1
  getWindowCapabilities(): Promise<NativeWindowCapabilities>
  getWindowState(): Promise<NativeWindowState>
  minimize(): Promise<void>
  toggleMaximize(): Promise<NativeWindowState>
  startDragging(): Promise<void>
  setNativeControlsVisible(visible: boolean): Promise<void>
  close(): Promise<void>
}

declare global {
  interface Window {
    readonly __HEYA_NATIVE_WINDOW__?: Readonly<HeyaNativeWindowBridge>
  }

  interface WindowEventMap {
    'heya:native-window:ready-v1': CustomEvent<{
      protocolVersion: 1
      capabilities: NativeWindowCapabilities
    }>
  }
}

export {}
