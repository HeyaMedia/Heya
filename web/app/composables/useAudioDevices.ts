import type { AudioOutputDevice } from '~~/shared/types/audio'
import { audioSinkSupported, setAudioSinkId } from '~/engine/context'
import type { AudioProfile } from '~/stores/audio-settings'

// Per-output-device EQ profiles. Native playback uses CPAL's stable device
// identifiers; browser playback uses MediaDeviceInfo.deviceId. Profiles are
// deliberately kept in Heya because they describe the user's EQ preferences,
// while HeyaClient owns the selected physical output and native stream.

const STORAGE_KEY = 'heya_device_profiles_v1'
const DEFAULT_KEY = '__default__'

type StoredProfile = AudioProfile & { label: string }
type ProfileMap = Record<string, StoredProfile>

function loadProfiles(): ProfileMap {
  if (import.meta.server) return {}
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    return raw ? (JSON.parse(raw) as ProfileMap) : {}
  } catch {
    return {}
  }
}

const availableDevices = ref<AudioOutputDevice[]>([])
const activeDeviceId = ref<string>(DEFAULT_KEY)
const labelsAvailable = ref(false)
const followsSystemDefault = ref(true)
const profiles = ref<ProfileMap>(loadProfiles())
const supported = ref(import.meta.client ? audioSinkSupported() : false)

let deviceChangeTimer: ReturnType<typeof setTimeout> | null = null
let listenerBound = false
let devicesInitialized = false
let profileDeviceKey: string | null = null

function persist() {
  if (import.meta.server) return
  try { localStorage.setItem(STORAGE_KEY, JSON.stringify(profiles.value)) } catch {}
}

function resolveKey(device: AudioOutputDevice): string {
  if (device.deviceId === 'default' || device.deviceId === '') {
    return device.label || DEFAULT_KEY
  }
  return device.deviceId
}

function activeDevice(): AudioOutputDevice | undefined {
  return availableDevices.value.find(device => device.deviceId === activeDeviceId.value)
}

function activeKey(): string {
  const active = activeDevice()
  return active ? resolveKey(active) : (activeDeviceId.value || DEFAULT_KEY)
}

async function enumerateBrowserDevices(): Promise<AudioOutputDevice[]> {
  if (import.meta.server || !navigator.mediaDevices?.enumerateDevices) return []
  const all = await navigator.mediaDevices.enumerateDevices()
  const outputs = all.filter(device => device.kind === 'audiooutput')
  labelsAvailable.value = outputs.some(device => device.label.length > 0)
  return outputs.map(device => ({
    deviceId: device.deviceId,
    label: device.label || `Output ${device.deviceId.slice(0, 6) || 'default'}`,
    isDefault: device.deviceId === 'default' || device.deviceId === '',
  }))
}

export function useAudioDevices() {
  const settings = useAudioSettingsStore()
  const player = usePlayerBindings()
  const { isTauriClient } = useClientSurface()

  function applyProfileFor(key: string) {
    if (profileDeviceKey === key) return
    const profile = profiles.value[key]
    if (profile) settings.applyAudioProfile(profile)
    else settings.resetAudioProfile()
    profileDeviceKey = key
  }

  function syncNativeSnapshot() {
    availableDevices.value = player.nativeAudioOutputDevices.value.map(device => ({
      deviceId: device.deviceId,
      label: device.label,
      isDefault: device.isDefault,
    }))
    activeDeviceId.value = player.nativeAudioOutputDeviceId.value
      ?? availableDevices.value.find(device => device.isDefault)?.deviceId
      ?? availableDevices.value[0]?.deviceId
      ?? DEFAULT_KEY
    followsSystemDefault.value = player.nativeAudioFollowsSystemDefault.value
    labelsAvailable.value = true
  }

  async function refreshNative(): Promise<boolean> {
    const ready = await player.refreshNativeAudioOutputs()
    if (!ready) return false
    syncNativeSnapshot()
    return true
  }

  async function refresh() {
    const previousKey = activeKey()
    if (isTauriClient.value) {
      supported.value = await refreshNative()
    } else {
      availableDevices.value = await enumerateBrowserDevices()
      supported.value = audioSinkSupported()
      followsSystemDefault.value = activeDeviceId.value === DEFAULT_KEY
      if (!activeDevice()) {
        const fallback = availableDevices.value.find(device => device.isDefault)
          ?? availableDevices.value[0]
        if (fallback) activeDeviceId.value = fallback.deviceId
      }
    }
    const nextKey = activeKey()
    if (nextKey !== previousKey) applyProfileFor(nextKey)
  }

  function onDeviceChange() {
    if (deviceChangeTimer) clearTimeout(deviceChangeTimer)
    deviceChangeTimer = setTimeout(() => { void refresh() }, 500)
  }

  async function init() {
    if (import.meta.server) return
    if (isTauriClient.value) {
      const backend = await player.probeNativeAudio()
      supported.value = !!backend?.capabilities.outputDeviceSelection && await refreshNative()
    } else {
      availableDevices.value = await enumerateBrowserDevices()
      supported.value = audioSinkSupported()
      if (availableDevices.value.length > 0 && !activeDevice()) {
        const fallback = availableDevices.value.find(device => device.isDefault)
          ?? availableDevices.value[0]
        if (fallback) activeDeviceId.value = fallback.deviceId
      }
      if (!listenerBound && navigator.mediaDevices) {
        navigator.mediaDevices.addEventListener('devicechange', onDeviceChange)
        listenerBound = true
      }
    }
    if (availableDevices.value.length > 0) applyProfileFor(activeKey())
    devicesInitialized = true
  }

  async function ensureInitialized() {
    // Following the OS default can resolve to different physical hardware
    // between tracks, so re-resolve that lightweight snapshot before load.
    if (!devicesInitialized || (isTauriClient.value && followsSystemDefault.value)) {
      await init()
    }
  }

  async function revealLabels(): Promise<boolean> {
    if (isTauriClient.value) return true
    if (import.meta.server || !navigator.mediaDevices?.getUserMedia) return false
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true })
      for (const track of stream.getTracks()) track.stop()
      availableDevices.value = await enumerateBrowserDevices()
      return labelsAvailable.value
    } catch {
      return false
    }
  }

  async function selectDevice(deviceId: string): Promise<boolean> {
    const changed = isTauriClient.value
      ? await player.setNativeAudioOutputDevice(deviceId)
      : await setAudioSinkId(deviceId)
    if (!changed) return false
    if (isTauriClient.value) syncNativeSnapshot()
    else {
      activeDeviceId.value = deviceId
      followsSystemDefault.value = deviceId === 'default' || deviceId === ''
    }
    applyProfileFor(activeKey())
    return true
  }

  async function useSystemDefault(): Promise<boolean> {
    if (!isTauriClient.value) {
      const fallback = availableDevices.value.find(device => device.isDefault)
      return fallback ? selectDevice(fallback.deviceId) : false
    }
    const changed = await player.setNativeAudioOutputDevice(null)
    if (!changed) return false
    syncNativeSnapshot()
    applyProfileFor(activeKey())
    return true
  }

  function saveActiveProfile() {
    const key = activeKey()
    const label = activeDevice()?.label ?? key
    profiles.value = {
      ...profiles.value,
      [key]: { ...settings.currentAudioProfile(), label },
    }
    persist()
  }

  function deleteProfile(key: string) {
    if (!(key in profiles.value)) return
    const copy = { ...profiles.value }
    delete copy[key]
    profiles.value = copy
    persist()
    if (key === activeKey()) {
      settings.resetAudioProfile()
      profileDeviceKey = key
    }
  }

  function activeHasProfile(): boolean {
    return activeKey() in profiles.value
  }

  function hasProfile(device: AudioOutputDevice): boolean {
    return resolveKey(device) in profiles.value
  }

  return {
    availableDevices: readonly(availableDevices),
    activeDeviceId: readonly(activeDeviceId),
    labelsAvailable: readonly(labelsAvailable),
    followsSystemDefault: readonly(followsSystemDefault),
    profiles: readonly(profiles),
    supported: readonly(supported),
    init, ensureInitialized, refresh, revealLabels, selectDevice, useSystemDefault,
    saveActiveProfile, deleteProfile,
    activeKey, activeHasProfile, hasProfile,
  }
}
