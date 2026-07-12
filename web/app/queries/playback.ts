import { defineQueryOptions } from '@pinia/colada'
import type { MediaLanguagesResponse, PlaybackPreference } from '~~/shared/types'

export const mediaLanguagesQuery = defineQueryOptions((mediaId: number) => ({
  key: ['media', mediaId, 'languages'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/media/{id}/languages', { path: { id: mediaId } }) as MediaLanguagesResponse
  },
  staleTime: 1000 * 60 * 10,
  meta: { prefetch: 'none', persistence: 'device', sensitivity: 'normal' },
}))

export const playbackPreferenceQuery = defineQueryOptions((mediaId: number) => ({
  key: ['me', 'playback', mediaId],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/me/playback/{media_id}', { path: { media_id: mediaId } }) as PlaybackPreference
  },
  staleTime: 1000 * 60,
  meta: { prefetch: 'none', persistence: 'offline-essential', sensitivity: 'private' },
}))
