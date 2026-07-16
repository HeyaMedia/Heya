import type {
  HeyaNativeWindowBridge,
  NativeWindowCapabilities,
} from '~/types/native-window'

export const NATIVE_WINDOW_READY_EVENT = 'heya:native-window:ready-v1' as const
export const NATIVE_WINDOW_PROTOCOL_VERSION = 1 as const

export interface NativeWindowHandshake {
  bridge: Readonly<HeyaNativeWindowBridge>
  capabilities: NativeWindowCapabilities
}

function currentBridge(): Readonly<HeyaNativeWindowBridge> | null {
  if (!import.meta.client) return null
  const bridge = window.__HEYA_NATIVE_WINDOW__
  return bridge?.protocolVersion === NATIVE_WINDOW_PROTOCOL_VERSION ? bridge : null
}

/** A successful origin-validated handshake is the only native-window gate. */
export async function waitForNativeWindowBridge(timeoutMilliseconds = 1500): Promise<NativeWindowHandshake | null> {
  const immediate = currentBridge()
  if (immediate) {
    const capabilities = await immediate.getWindowCapabilities().catch(() => null)
    return capabilities?.protocolVersion === 1 ? { bridge: immediate, capabilities } : null
  }
  if (!import.meta.client) return null

  return await new Promise((resolve) => {
    let settled = false
    const finish = (result: NativeWindowHandshake | null) => {
      if (settled) return
      settled = true
      window.removeEventListener(NATIVE_WINDOW_READY_EVENT, onReady)
      clearTimeout(timer)
      resolve(result)
    }
    const onReady = (event: WindowEventMap[typeof NATIVE_WINDOW_READY_EVENT]) => {
      const bridge = currentBridge()
      if (!bridge || event.detail?.protocolVersion !== 1) return
      void bridge.getWindowCapabilities()
        .then(capabilities => finish(capabilities.protocolVersion === 1 ? { bridge, capabilities } : null))
        .catch(() => finish(null))
    }
    const timer = setTimeout(() => finish(null), timeoutMilliseconds)
    window.addEventListener(NATIVE_WINDOW_READY_EVENT, onReady)

    const bridge = currentBridge()
    if (bridge) {
      void bridge.getWindowCapabilities()
        .then(capabilities => finish({ bridge, capabilities }))
        .catch(() => finish(null))
    }
  })
}
