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

/** Row of /api/me/up-next — the server-resolved Up Next rail. */
export interface UpNextRailRow {
  media_item_id: number
  media_item_public_id: string
  title: string
  slug: string
  media_type: string
  episode_id: number
  episode_number: number
  episode_title?: string
  season_id: number
  season_number: number
  runtime?: number
  file_id: number
  file_public_id: string
  last_watched_at: string
}

// The Up Next rail, resolved server-side in one round-trip: per
// recently-watched series, the next unwatched episode that has a playable
// file. Consumed through useUpNext (Home + TV Recommended landing).
export const upNextRailQuery = defineQueryOptions(() => ({
  key: ['me', 'up-next'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return (await $heya('/api/me/up-next') as UpNextRailRow[] | null) ?? []
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
