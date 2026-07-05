// Per-device playback preferences — deliberately localStorage-only (this is
// "what should THIS phone/tablet/browser do", never a server-side setting).
// Follows the module-level-ref + localStorage shape of useAudioSettings /
// useVisualizer so the audio surface stays uniform. Settings UI lives at
// /settings/device; consumers: the stream URL builder (streamQuality), the
// engine prefetch manager (prefetchCount / wifiOnlyPrefetch), and the engine
// factory (forceDirectEngine).
import { ref } from 'vue'

/**
 * 'original' streams the best playable file untouched (today's behavior).
 * The aac-* tiers ask the server to transcode down to that bitrate — the
 * /api/music/tracks/{id}/stream endpoint takes it as `?quality=aac-256`
 * style; 'original' means the param is omitted entirely.
 */
export type StreamQuality = 'original' | 'aac-320' | 'aac-256' | 'aac-192' | 'aac-128'

export interface DeviceSettings {
  streamQuality: StreamQuality
  /** Upcoming queue tracks to keep fully cached ahead of playback. 0 = off. */
  prefetchCount: number
  /**
   * Only prefetch on unmetered connections. Best-effort: the Network
   * Information API only exists in Chromium (Android/desktop Chrome) — where
   * it's absent (all of iOS, Firefox) this is treated as "always allowed".
   */
  wifiOnlyPrefetch: boolean
  /**
   * Direct-element playback mode (no Web Audio graph — required for iOS
   * background audio, costs EQ/visualizers/crossfade).
   * null = auto (on for iOS, off elsewhere); true/false = explicit override.
   */
  forceDirectEngine: boolean | null
}

const KEY = 'heya_device_settings_v1'

const DEFAULTS: DeviceSettings = {
  streamQuality: 'original',
  prefetchCount: 2,
  wifiOnlyPrefetch: false,
  forceDirectEngine: null,
}

function loadInitial(): DeviceSettings {
  if (import.meta.server) return { ...DEFAULTS }
  try {
    const raw = localStorage.getItem(KEY)
    if (!raw) return { ...DEFAULTS }
    // Merge over defaults so fields added in later versions don't crash
    // older stored blobs.
    return { ...DEFAULTS, ...(JSON.parse(raw) as Partial<DeviceSettings>) }
  } catch {
    return { ...DEFAULTS }
  }
}

const settings = ref<DeviceSettings>(loadInitial())

function persist() {
  if (import.meta.server) return
  try {
    localStorage.setItem(KEY, JSON.stringify(settings.value))
  } catch {
    // Private mode / quota — prefs just won't stick.
  }
}

export function useDeviceSettings() {
  function update(patch: Partial<DeviceSettings>) {
    settings.value = { ...settings.value, ...patch }
    persist()
  }
  return { settings, update }
}
