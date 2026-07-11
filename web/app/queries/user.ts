import { defineQueryOptions } from '@pinia/colada'

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

export interface UserSettingsBlob extends Record<string, unknown> {
  playback?: PlaybackSettings
  ui?: { pinned_hero_mode?: string }
  home?: { sections?: unknown[] }
  appearance?: Record<string, unknown>
}

export const meSettingsQuery = defineQueryOptions(() => ({
  key: ['me', 'settings'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/me/settings') as UserSettingsBlob
  },
  staleTime: 1000 * 60 * 5,
  meta: {
    prefetch: 'none',
    persistence: 'device',
    sensitivity: 'private',
  },
}))
