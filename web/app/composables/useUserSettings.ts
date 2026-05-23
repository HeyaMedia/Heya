export interface LibraryPlaybackOverride {
  default_audio_language?: string
  default_subtitle_language?: string
  subtitle_mode?: string
  subtitle_priority?: string[]
}

export interface PlaybackSettings {
  default_audio_language: string
  default_subtitle_language: string
  subtitle_mode: 'auto' | 'always' | 'forced_only' | 'off'
  subtitle_priority: string[]
  default_quality: string
  library_overrides: Record<string, LibraryPlaybackOverride>
}

export interface UserSettingsData {
  playback: PlaybackSettings
}

const DEFAULT_SETTINGS: UserSettingsData = {
  playback: {
    default_audio_language: '',
    default_subtitle_language: '',
    subtitle_mode: 'auto',
    subtitle_priority: ['ass', 'srt', 'subrip', 'webvtt', 'pgs'],
    default_quality: 'auto',
    library_overrides: {},
  },
}

const _settings = ref<UserSettingsData | null>(null)
const _loaded = ref(false)

export function useUserSettings() {
  const settings = computed<UserSettingsData>(() => _settings.value ?? DEFAULT_SETTINGS)

  async function load() {
    if (_loaded.value) return
    try {
      _settings.value = await apiFetch<UserSettingsData>('/api/user/settings')
      _loaded.value = true
    } catch {
      _settings.value = { ...DEFAULT_SETTINGS }
      _loaded.value = true
    }
  }

  async function save(updated: UserSettingsData) {
    _settings.value = updated
    try {
      await apiFetch<UserSettingsData>('/api/user/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(updated),
      })
    } catch {}
  }

  function playbackForLibrary(libraryId?: number | string): PlaybackSettings {
    const base = settings.value.playback
    const result: PlaybackSettings = { ...base }
    if (!libraryId) return result
    const ov = base.library_overrides[String(libraryId)]
    if (!ov) return result
    if (ov.default_audio_language) result.default_audio_language = ov.default_audio_language
    if (ov.default_subtitle_language) result.default_subtitle_language = ov.default_subtitle_language
    if (ov.subtitle_mode) result.subtitle_mode = ov.subtitle_mode as PlaybackSettings['subtitle_mode']
    if (ov.subtitle_priority?.length) result.subtitle_priority = ov.subtitle_priority
    return result
  }

  return { settings, load, save, playbackForLibrary }
}
