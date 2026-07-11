import { useQuery, useQueryCache } from '@pinia/colada'
import {
  meSettingsQuery,
  type LibraryPlaybackOverride,
  type PlaybackSettings,
  type UserSettingsBlob,
} from '~/queries/user'

export type { LibraryPlaybackOverride, PlaybackSettings } from '~/queries/user'

export interface UserSettingsData {
  playback: PlaybackSettings
}

const DEFAULT_PLAYBACK: PlaybackSettings = {
  default_audio_language: '',
  default_subtitle_language: '',
  subtitle_mode: 'auto',
  subtitle_priority: ['ass', 'srt', 'subrip', 'webvtt', 'pgs'],
  default_quality: 'auto',
  library_overrides: {},
}

export function useUserSettings() {
  const queryCache = useQueryCache()
  const settingsQuery = useQuery(meSettingsQuery())
  const settings = computed<UserSettingsData>(() => ({
    playback: settingsQuery.data.value?.playback ?? DEFAULT_PLAYBACK,
  }))

  async function load() {
    if (settingsQuery.data.value !== undefined) return
    try { await settingsQuery.refetch() } catch { /* defaults remain usable */ }
  }

  async function save(updated: UserSettingsData) {
    const previous = settingsQuery.data.value ?? {}
    const next: UserSettingsBlob = { ...previous, playback: updated.playback }
    queryCache.setQueryData(['me', 'settings'], next)
    try {
      const { $heya } = useNuxtApp()
      await $heya('/api/me/settings', { method: 'PUT', body: next as never })
    } catch {
      queryCache.setQueryData(['me', 'settings'], previous)
    }
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
