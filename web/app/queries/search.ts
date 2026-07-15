import { defineInfiniteQueryOptions, defineQueryOptions } from '@pinia/colada'
import type { QuickSearchResponse, SearchBucket, SearchType } from '~/composables/useSearch'

export interface GenreRow { genre: string; count: number }
export interface CollectionRow { id: number; name: string; poster_path: string; movie_count: number }

/** A single card in the spotlight empty-state "For You" strip. Mirrors the
 *  ForYouItem the /api/me/recommendations endpoint returns — only the fields the
 *  strip renders + navigates with. */
export interface SpotlightRecItem {
  id: number
  public_id?: string
  title: string
  slug: string
  year?: string
  media_type: string
  available: boolean
}

/** Compact, availability-filtered recommendation strip for the spotlight empty
 *  state. Reuses the same /api/me/recommendations engine the home "For You"
 *  rail pages, just capped tiny — only locally-available items so every card
 *  has a real in-app route. */
export const spotlightForYouQuery = defineQueryOptions(() => ({
  key: ['spotlight', 'for-you'],
  query: async (): Promise<SpotlightRecItem[]> => {
    const { $heya } = useNuxtApp()
    const res = await $heya('/api/me/recommendations', {
      query: { limit: 12 },
    }) as { items?: SpotlightRecItem[] | null }
    return (res.items ?? []).filter(it => it.available).slice(0, 6)
  },
  staleTime: 1000 * 60 * 5,
  meta: { prefetch: 'none', persistence: 'device', sensitivity: 'private' },
}))

export const searchBrowseQuery = defineQueryOptions(() => ({
  key: ['search', 'browse'],
  query: async () => {
    const { $heya } = useNuxtApp()
    const [genres, collections] = await Promise.all([
      $heya('/api/genres') as Promise<GenreRow[]>,
      $heya('/api/collections', { query: { limit: 20 } }) as Promise<{ items: CollectionRow[] }>,
    ])
    return {
      genres: genres ?? [],
      collections: (collections.items ?? []).filter(item => item.movie_count > 0),
    }
  },
  staleTime: 1000 * 60 * 10,
  meta: { prefetch: 'intent', persistence: 'device', sensitivity: 'normal' },
}))

export const quickSearchQuery = defineQueryOptions((query: string) => ({
  key: ['search', 'quick', query],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/search/quick', { query: { q: query } }) as QuickSearchResponse
  },
  staleTime: 1000 * 60 * 5,
  meta: { prefetch: 'none', persistence: 'device', sensitivity: 'private' },
}))

export const filteredSearchQuery = defineInfiniteQueryOptions((params: { query: string, type: SearchType, limit?: number }) => ({
  key: ['search', 'filtered', params.query, params.type],
  initialPageParam: 0,
  query: async ({ pageParam }) => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/search', {
      query: { q: params.query, type: params.type as any, limit: params.limit ?? 60, offset: pageParam },
    }) as SearchBucket<any>
  },
  getNextPageParam: (lastPage, _pages, lastOffset) => {
    const next = lastOffset + (lastPage.items?.length ?? 0)
    return next < lastPage.total ? next : null
  },
  staleTime: 1000 * 60 * 5,
  meta: { prefetch: 'none', persistence: 'device', sensitivity: 'private' },
}))
