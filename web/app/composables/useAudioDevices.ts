import type { AudioOutputDevice } from '~~/shared/types/audio'
import { audioSinkSupported, setAudioSinkId } from '~/engine/context'
import type { AudioProfile } from '~/stores/audio-settings'

// Per-output-device EQ profiles.
//
// Different transducers (open-backs, IEMs, laptop speakers, a soundbar) want
// wildly different EQ + crossfeed. This composable enumerates the system's
// audio outputs, routes the AudioContext to a chosen one via setSinkId, and
// remembers a saved AudioProfile per device — re-applying it automatically
// whenever that device becomes active (on selection or a hot-plug event).
//
// Design choices vs. hibiki's port:
//  - Profiles are OPT-IN. Switching to a device with no saved profile leaves
//    the current EQ untouched (hibiki reset to flat, which surprises). A device
//    only steers the EQ once you've explicitly "Saved to this device".
//  - Labels stay hidden until requested. We do NOT call getUserMedia on init
//    (that pops a mic-permission prompt). `revealLabels()` unlocks names on
//    demand when the user opts in from the Output tab.

const STORAGE_KEY = 'heya_device_profiles_v1'

// The default output reports a deviceId of 'default' (or '') that maps to
// different hardware over time, so it's a poor profile key. Fall back to the
// label, then this sentinel, so "default" profiles at least follow the name.
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
const profiles = ref<ProfileMap>(loadProfiles())
// Reflects AudioContext.setSinkId support (the routing capability), not mere
// device enumeration — Safari/Firefox enumerate fine but can't route.
const supported = ref(import.meta.client ? audioSinkSupported() : true)

let deviceChangeTimer: ReturnType<typeof setTimeout> | null = null
let listenerBound = false

function persist() {
  if (import.meta.server) return
  try { localStorage.setItem(STORAGE_KEY, JSON.stringify(profiles.value)) } catch {}
}

// The stable key a profile is stored under. Real devices key on deviceId; the
// system default keys on its human label (or the sentinel) so it survives the
// deviceId churn the "default" slot exhibits.
function resolveKey(device: AudioOutputDevice): string {
  if (device.deviceId === 'default' || device.deviceId === '') {
    return device.label || DEFAULT_KEY
  }
  return device.deviceId
}

function activeDevice(): AudioOutputDevice | undefined {
  return availableDevices.value.find((d) => d.deviceId === activeDeviceId.value)
}

function activeKey(): string {
  const active = activeDevice()
  return active ? resolveKey(active) : (activeDeviceId.value || DEFAULT_KEY)
}

async function enumerate(): Promise<AudioOutputDevice[]> {
  if (import.meta.server || !navigator.mediaDevices?.enumerateDevices) return []
  const all = await navigator.mediaDevices.enumerateDevices()
  const outputs = all.filter((d) => d.kind === 'audiooutput')
  labelsAvailable.value = outputs.some((d) => d.label.length > 0)
  return outputs.map((d) => ({
    deviceId: d.deviceId,
    label: d.label || `Output ${d.deviceId.slice(0, 6) || 'default'}`,
    isDefault: d.deviceId === 'default' || d.deviceId === '',
  }))
}

export function useAudioDevices() {
  const settings = useAudioSettingsStore()

  // Apply the saved profile for a device key, if one exists. No profile → leave
  // the current EQ/crossfeed as-is (opt-in behavior).
  function applyProfileFor(key: string) {
    const p = profiles.value[key]
    if (p) settings.applyAudioProfile(p)
  }

  async function refresh() {
    const prevKey = activeKey()
    availableDevices.value = await enumerate()
    // Keep the active id if it still exists; otherwise fall to the default.
    if (!activeDevice()) {
      const def = availableDevices.value.find((d) => d.isDefault) ?? availableDevices.value[0]
      if (def) activeDeviceId.value = def.deviceId
    }
    const newKey = activeKey()
    if (newKey !== prevKey) applyProfileFor(newKey)
  }

  function onDeviceChange() {
    // Debounce: OS hot-plug events fire in bursts (a USB DAC re-enumerates a
    // handful of times as it settles).
    if (deviceChangeTimer) clearTimeout(deviceChangeTimer)
    deviceChangeTimer = setTimeout(() => { void refresh() }, 500)
  }

  async function init() {
    if (import.meta.server) return
    availableDevices.value = await enumerate()
    if (availableDevices.value.length > 0 && !activeDevice()) {
      const def = availableDevices.value.find((d) => d.isDefault) ?? availableDevices.value[0]
      if (def) activeDeviceId.value = def.deviceId
    }
    applyProfileFor(activeKey())
    if (!listenerBound && navigator.mediaDevices) {
      navigator.mediaDevices.addEventListener('devicechange', onDeviceChange)
      listenerBound = true
    }
  }

  // Unlock device labels by requesting (then immediately releasing) mic access.
  // Chromium hides audiooutput labels until the page has been granted device
  // permission at least once.
  async function revealLabels(): Promise<boolean> {
    if (import.meta.server || !navigator.mediaDevices?.getUserMedia) return false
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true })
      for (const track of stream.getTracks()) track.stop()
      availableDevices.value = await enumerate()
      return labelsAvailable.value
    } catch {
      return false
    }
  }

  // Route audio to a device and load its profile. Returns false when the
  // browser rejects setSinkId (Safari/Firefox lack it) — the active id is only
  // advanced on success so the UI reflects the real routing.
  async function selectDevice(deviceId: string): Promise<boolean> {
    const ok = await setAudioSinkId(deviceId)
    if (ok) {
      activeDeviceId.value = deviceId
      applyProfileFor(activeKey())
    }
    return ok
  }

  // Snapshot the live EQ + crossfeed as this device's profile.
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
    profiles: readonly(profiles),
    supported: readonly(supported),
    init, refresh, revealLabels, selectDevice,
    saveActiveProfile, deleteProfile,
    activeKey, activeHasProfile, hasProfile,
  }
}
