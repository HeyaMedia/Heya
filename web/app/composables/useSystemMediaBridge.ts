import type {
  HeyaSystemMediaBridge,
  SystemMediaCapabilities,
} from '~/types/system-media'

export const SYSTEM_MEDIA_READY_EVENT = 'heya:system-media:ready-v1' as const
export const SYSTEM_MEDIA_PROTOCOL_VERSION = 1 as const

export interface SystemMediaHandshake {
  bridge: Readonly<HeyaSystemMediaBridge>
  capabilities: SystemMediaCapabilities
}

function currentBridge(): Readonly<HeyaSystemMediaBridge> | null {
  if (!import.meta.client) return null
  const bridge = window.__HEYA_SYSTEM_MEDIA__
  return bridge?.protocolVersion === SYSTEM_MEDIA_PROTOCOL_VERSION ? bridge : null
}

/** A successful origin-validated handshake, never the spoofable surface marker, authorizes native integration. */
export async function waitForSystemMediaBridge(timeoutMilliseconds = 1200): Promise<SystemMediaHandshake | null> {
  const immediate = currentBridge()
  if (immediate) {
    const capabilities = await immediate.getSystemMediaCapabilities().catch(() => null)
    return capabilities?.protocolVersion === 1 ? { bridge: immediate, capabilities } : null
  }
  if (!import.meta.client) return null

  return await new Promise((resolve) => {
    let settled = false
    const finish = (result: SystemMediaHandshake | null) => {
      if (settled) return
      settled = true
      window.removeEventListener(SYSTEM_MEDIA_READY_EVENT, onReady)
      clearTimeout(timer)
      resolve(result)
    }
    const onReady = (event: WindowEventMap[typeof SYSTEM_MEDIA_READY_EVENT]) => {
      const bridge = currentBridge()
      if (!bridge || event.detail?.protocolVersion !== 1) return
      void bridge.getSystemMediaCapabilities()
        .then(capabilities => finish(capabilities.protocolVersion === 1 ? { bridge, capabilities } : null))
        .catch(() => finish(null))
    }
    const timer = setTimeout(() => finish(null), timeoutMilliseconds)
    window.addEventListener(SYSTEM_MEDIA_READY_EVENT, onReady)

    const bridge = currentBridge()
    if (bridge) {
      void bridge.getSystemMediaCapabilities()
        .then(capabilities => finish({ bridge, capabilities }))
        .catch(() => finish(null))
    }
  })
}
