import type {
  HeyaNativeAudioBridge,
  NativeAudioCapabilities,
} from '~/types/native-audio'

export const NATIVE_AUDIO_READY_EVENT = 'heya:native-audio:ready-v2' as const
export const NATIVE_AUDIO_PROTOCOL_VERSION = 2 as const

export interface NativeAudioHandshake {
  bridge: Readonly<HeyaNativeAudioBridge>
  capabilities: NativeAudioCapabilities
}

function currentBridge(): Readonly<HeyaNativeAudioBridge> | null {
  if (!import.meta.client) return null
  const bridge = window.__HEYA_NATIVE_AUDIO__
  return bridge?.protocolVersion === NATIVE_AUDIO_PROTOCOL_VERSION ? bridge : null
}

/** The successful origin-validated handshake, never the spoofable surface marker, authorizes native audio. */
export async function waitForNativeAudioBridge(timeoutMilliseconds = 1200): Promise<NativeAudioHandshake | null> {
  const immediate = currentBridge()
  if (immediate) {
    const capabilities = await immediate.getAudioCapabilities().catch(() => null)
    return capabilities?.protocolVersion === NATIVE_AUDIO_PROTOCOL_VERSION ? { bridge: immediate, capabilities } : null
  }
  if (!import.meta.client) return null

  return await new Promise((resolve) => {
    let settled = false
    const finish = (result: NativeAudioHandshake | null) => {
      if (settled) return
      settled = true
      window.removeEventListener(NATIVE_AUDIO_READY_EVENT, onReady)
      clearTimeout(timer)
      resolve(result)
    }
    const onReady = (event: WindowEventMap[typeof NATIVE_AUDIO_READY_EVENT]) => {
      const bridge = currentBridge()
      if (!bridge || event.detail?.protocolVersion !== NATIVE_AUDIO_PROTOCOL_VERSION) return
      void bridge.getAudioCapabilities()
        .then(capabilities => finish(capabilities.protocolVersion === NATIVE_AUDIO_PROTOCOL_VERSION ? { bridge, capabilities } : null))
        .catch(() => finish(null))
    }
    const timer = setTimeout(() => finish(null), timeoutMilliseconds)
    window.addEventListener(NATIVE_AUDIO_READY_EVENT, onReady)

    const bridge = currentBridge()
    if (bridge) {
      void bridge.getAudioCapabilities()
        .then(capabilities => finish({ bridge, capabilities }))
        .catch(() => finish(null))
    }
  })
}
