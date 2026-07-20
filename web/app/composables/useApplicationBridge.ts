import type {
  ApplicationCapabilities,
  ApplicationSettings,
  ApplicationSnapshot,
  ApplicationUpdateStatus,
  HeyaApplicationBridge,
  NativePlaybackStatus,
} from '~/types/application'

export const APPLICATION_READY_EVENT = 'heya:application:ready-v1' as const
export const APPLICATION_OPEN_SETTINGS_EVENT = 'heya:application:open-settings-v1' as const
export const APPLICATION_PROTOCOL_VERSION = 1 as const

const bridge = shallowRef<Readonly<HeyaApplicationBridge> | null>(null)
const capabilities = shallowRef<ApplicationCapabilities | null>(null)
const snapshot = shallowRef<ApplicationSnapshot | null>(null)
const applicationAvailable = computed(() => capabilities.value?.available === true && bridge.value !== null)
const applicationUpdateAvailable = computed(() => snapshot.value?.update?.available === true)
const applicationUpdateInstalling = ref(false)
let handshakePromise: Promise<boolean> | null = null
let integrationStarted = false

function currentBridge(): Readonly<HeyaApplicationBridge> | null {
  if (!import.meta.client) return null
  const candidate = window.__HEYA_APPLICATION__
  return candidate?.protocolVersion === APPLICATION_PROTOCOL_VERSION ? candidate : null
}

async function handshake(timeoutMilliseconds = 1200): Promise<boolean> {
  const immediate = currentBridge()
  if (immediate) {
    const value = await immediate.getApplicationCapabilities().catch(() => null)
    if (value?.protocolVersion === APPLICATION_PROTOCOL_VERSION && value.available) {
      bridge.value = immediate
      capabilities.value = value
      return true
    }
  }
  if (!import.meta.client) return false

  return await new Promise((resolve) => {
    let settled = false
    const finish = (available: boolean) => {
      if (settled) return
      settled = true
      window.removeEventListener(APPLICATION_READY_EVENT, onReady)
      clearTimeout(timer)
      resolve(available)
    }
    const accept = async (candidate: Readonly<HeyaApplicationBridge>) => {
      const value = await candidate.getApplicationCapabilities().catch(() => null)
      if (value?.protocolVersion !== APPLICATION_PROTOCOL_VERSION || !value.available) return false
      bridge.value = candidate
      capabilities.value = value
      return true
    }
    const onReady = (event: WindowEventMap[typeof APPLICATION_READY_EVENT]) => {
      const candidate = currentBridge()
      if (!candidate || event.detail?.protocolVersion !== APPLICATION_PROTOCOL_VERSION) return
      void accept(candidate).then(finish)
    }
    const timer = setTimeout(() => finish(false), timeoutMilliseconds)
    window.addEventListener(APPLICATION_READY_EVENT, onReady)

    const candidate = currentBridge()
    if (candidate) void accept(candidate).then(finish)
  })
}

async function ensureBridge(): Promise<Readonly<HeyaApplicationBridge> | null> {
  if (bridge.value) return bridge.value
  handshakePromise ??= handshake().finally(() => { handshakePromise = null })
  return await handshakePromise ? bridge.value : null
}

async function refreshSnapshot(): Promise<ApplicationSnapshot | null> {
  const nativeBridge = await ensureBridge()
  if (!nativeBridge) return null
  snapshot.value = await nativeBridge.getApplicationSnapshot()
  capabilities.value = snapshot.value.capabilities
  return snapshot.value
}

async function saveSettings(settings: ApplicationSettings): Promise<ApplicationSettings> {
  const nativeBridge = await ensureBridge()
  if (!nativeBridge) throw new Error('HeyaClient application settings are unavailable.')
  const saved = await nativeBridge.saveApplicationSettings(settings)
  if (snapshot.value) snapshot.value = { ...snapshot.value, settings: saved }
  return saved
}

async function checkForUpdate(): Promise<ApplicationUpdateStatus> {
  const nativeBridge = await ensureBridge()
  if (!nativeBridge) throw new Error('HeyaClient application updates are unavailable.')
  const update = await nativeBridge.checkForApplicationUpdate()
  if (snapshot.value) snapshot.value = { ...snapshot.value, update }
  return update
}

async function installUpdate(): Promise<void> {
  const nativeBridge = await ensureBridge()
  if (!nativeBridge) throw new Error('HeyaClient application updates are unavailable.')
  if (applicationUpdateInstalling.value) return
  applicationUpdateInstalling.value = true
  try {
    await nativeBridge.installApplicationUpdate()
    if (snapshot.value?.update) {
      snapshot.value = {
        ...snapshot.value,
        update: { ...snapshot.value.update, available: false, version: null },
      }
    }
  } finally {
    applicationUpdateInstalling.value = false
  }
}

async function installNativePlaybackRuntime(): Promise<NativePlaybackStatus> {
  const nativeBridge = await ensureBridge()
  if (!nativeBridge) throw new Error('HeyaClient native playback is unavailable.')
  const status = await nativeBridge.installNativePlaybackRuntime()
  if (snapshot.value) snapshot.value = { ...snapshot.value, nativePlayback: status }
  return status
}

async function invokeApplicationAction(action: 'openServerPicker' | 'resetServerSession' | 'forgetServer'): Promise<void> {
  const nativeBridge = await ensureBridge()
  if (!nativeBridge) throw new Error('HeyaClient application settings are unavailable.')
  await nativeBridge[action]()
}

export function useApplicationBridge() {
  return {
    applicationAvailable: readonly(applicationAvailable),
    applicationUpdateAvailable: readonly(applicationUpdateAvailable),
    applicationUpdateInstalling: readonly(applicationUpdateInstalling),
    applicationCapabilities: readonly(capabilities),
    applicationSnapshot: readonly(snapshot),
    ensureBridge,
    refreshApplicationSnapshot: refreshSnapshot,
    saveApplicationSettings: saveSettings,
    checkForApplicationUpdate: checkForUpdate,
    installApplicationUpdate: installUpdate,
    installNativePlaybackRuntime,
    openApplicationServerPicker: () => invokeApplicationAction('openServerPicker'),
    resetApplicationServerSession: () => invokeApplicationAction('resetServerSession'),
    forgetApplicationServer: () => invokeApplicationAction('forgetServer'),
  }
}

export function useApplicationIntegration() {
  if (import.meta.server || integrationStarted) return
  integrationStarted = true
  window.addEventListener(APPLICATION_OPEN_SETTINGS_EVENT, () => {
    void navigateTo('/settings/application')
  })

  void (async () => {
    const nativeBridge = await ensureBridge()
    if (!nativeBridge) return
    const current = await refreshSnapshot()
    if (!current?.capabilities.updaterSupported) return

    await checkForUpdate().catch((error) => {
      console.warn('[HeyaClient] automatic update check failed', error)
    })
  })()
}
