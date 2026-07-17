import { defineQueryOptions } from '@pinia/colada'
import type { ContinueWatchingItem } from '~/types/home'

export interface ActivityItem {
  type: string
  timestamp: string
  title: string
  subtitle?: string
  media_id?: number
  media_type?: string
  slug?: string
  image_url?: string
}

export const continueWatchingQuery = defineQueryOptions(() => ({
  key: ['me', 'watch', 'continue'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return (await $heya('/api/me/watch/continue') as ContinueWatchingItem[] | null) ?? []
  },
  staleTime: 1000 * 30,
  meta: { prefetch: 'none', persistence: 'offline-essential', sensitivity: 'private' },
}))

/** Row of /api/me/watch/recent (deduped: one row per title). */
export interface RecentWatchedTitle {
  media_item_id: number
  title: string
  poster_path: string
  slug: string
  media_type: string
}

// Feeds the home page's Up Next derivation (useUpNext) — distinct from
// rails.ts recentWatchedInfinite, which pages the same endpoint for rails.
export const recentWatchedQuery = defineQueryOptions(() => ({
  key: ['me', 'watch', 'recent'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return (await $heya('/api/me/watch/recent')) as RecentWatchedTitle[]
  },
  staleTime: 1000 * 30,
  meta: { prefetch: 'none', persistence: 'device', sensitivity: 'private' },
}))

export const activityFeedQuery = defineQueryOptions(() => ({
  key: ['activity'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return (await $heya('/api/activity') as ActivityItem[] | null) ?? []
  },
  staleTime: 1000 * 30,
  meta: { prefetch: 'none', persistence: 'device', sensitivity: 'private' },
}))
