import type {
  HeyaNativePlaybackBridge,
  NativePlaybackCapabilities,
} from '~/types/native-playback'

export const NATIVE_PLAYBACK_READY_EVENT = 'heya:native-playback:ready-v1' as const
export const NATIVE_PLAYBACK_PROTOCOL_VERSION = 1 as const

export interface NativePlaybackHandshake {
  bridge: Readonly<HeyaNativePlaybackBridge>
  capabilities: NativePlaybackCapabilities
}

function currentBridge(): Readonly<HeyaNativePlaybackBridge> | null {
  if (!import.meta.client) return null
  const bridge = window.__HEYA_NATIVE_PLAYBACK__
  return bridge?.protocolVersion === NATIVE_PLAYBACK_PROTOCOL_VERSION ? bridge : null
}

/**
 * Wait briefly for HeyaClient's origin-validated bridge. The Tauri surface
 * marker is deliberately not consulted here: it is spoofable metadata, while
 * successful bridge installation/capability negotiation is the only gate.
 */
export async function waitForNativePlaybackBridge(timeoutMilliseconds = 1200): Promise<NativePlaybackHandshake | null> {
  const immediate = currentBridge()
  if (immediate) {
    const capabilities = await immediate.getPlaybackCapabilities().catch(() => null)
    return capabilities?.protocolVersion === 1 ? { bridge: immediate, capabilities } : null
  }
  if (!import.meta.client) return null

  return await new Promise((resolve) => {
    let settled = false
    const finish = (result: NativePlaybackHandshake | null) => {
      if (settled) return
      settled = true
      window.removeEventListener(NATIVE_PLAYBACK_READY_EVENT, onReady)
      clearTimeout(timer)
      resolve(result)
    }
    const onReady = (event: WindowEventMap[typeof NATIVE_PLAYBACK_READY_EVENT]) => {
      const bridge = currentBridge()
      if (!bridge || event.detail?.protocolVersion !== 1) return
      // DOM events are page-spoofable. Always obtain the authoritative
      // capability snapshot through the origin-validated bridge transport.
      void bridge.getPlaybackCapabilities()
        .then(capabilities => finish(capabilities.protocolVersion === 1 ? { bridge, capabilities } : null))
        .catch(() => finish(null))
    }
    const timer = setTimeout(() => finish(null), timeoutMilliseconds)
    window.addEventListener(NATIVE_PLAYBACK_READY_EVENT, onReady)

    // Close the tiny race between the first lookup and listener attachment.
    const bridge = currentBridge()
    if (bridge) {
      void bridge.getPlaybackCapabilities()
        .then(capabilities => finish({ bridge, capabilities }))
        .catch(() => finish(null))
    }
  })
}
