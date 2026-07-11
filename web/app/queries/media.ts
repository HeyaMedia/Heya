import { defineQueryOptions } from '@pinia/colada'
import type { MediaDetail, MediaItem } from '~~/shared/types'

/** Stable identifier shared by a media URL and its detail-query key. */
export function mediaDetailTarget(item: Pick<MediaItem, 'id' | 'slug' | 'title' | 'year'>): string {
  if (item.slug) return item.slug
  return slugify(item.title) + (item.year ? `-${item.year}` : '')
}

// Pages, intent prefetchers and mutations all use this definition. Metadata
// remains serializable so a future IndexedDB/native cache can selectively
// persist safe entries without teaching every caller about storage.
export const mediaDetailQuery = defineQueryOptions((id: string | number) => ({
  key: ['media', 'detail', String(id)],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/media/{id}', {
      path: { id: String(id) as never },
    }) as MediaDetail
  },
  staleTime: 1000 * 60 * 5,
  retry: 0,
  meta: {
    prefetch: 'intent',
    persistence: 'device',
    sensitivity: 'normal',
  },
}))
