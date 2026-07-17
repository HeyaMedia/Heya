// Infinite (offset-paged) query definitions for the horizontal media rails on
// Home and the Movies/TV browse landings. Each rail is one cache entry whose
// data is { pages, pageParams } — ContentRow virtualizes the flattened list
// and asks for the next page as the user nears the right edge.
//
// Key discipline: every key here is the pre-existing plain-query key plus a
// trailing 'inf' segment, so the useLiveRefresh invalidation prefixes the
// pages already register (['media','recent','movie'], ['for-you', section],
// ['me','watch','recent'], …) keep matching. On invalidation Pinia Colada
// refetches every loaded page of an active infinite entry in order.
import { defineInfiniteQueryOptions, defineQueryOptions } from '@pinia/colada'
import type { MediaItem } from '~~/shared/types'

// Home rails are the first thing a cold app-open renders, so every one of
// them persists to the device cache (colada-persistence hydrates them at
// boot → last-known shelf paints instantly, then revalidates in place).
// Play-history-derived rails are marked private; library-derived stay normal.
const railMeta = { prefetch: 'none', persistence: 'device', sensitivity: 'normal' } as const
const railMetaPrivate = { ...railMeta, sensitivity: 'private' } as const

/** Page size for the flat recently-added/watched rails. */
export const RAIL_PAGE = 40
/** Discovery-rail pages match the bundle's 24-item head. */
export const DISCOVERY_PAGE = 24
/** For You pages — the engine re-ranks at most its top 200 (fyMMRPool). */
export const FORYOU_PAGE = 40
const FORYOU_DEPTH_CAP = 200

/** Grouped recently-added TV event (mirrors service.RecentlyAddedTVEntry). */
export interface RecentTVEntry {
  media_item_id: number
  media_item_public_id?: string
  title: string
  slug: string
  kind: 'series' | 'season' | 'episodes' | 'episode'
  season_number: number
  episode_number: number
  episode_title?: string
  season_count: number
  episode_count: number
  added_at: string
  // Kind-resolved: show desc for series/episodes, season overview for
  // season, episode overview for episode (server falls back to show desc).
  description?: string
}

/** One tile of a server-ranked discovery rail (mirrors service.RecRailItem). */
export interface RailItem {
  id: number
  title: string
  slug: string
  year?: string
  sub?: string
  media_type: string
  rating?: number
  available: boolean
}

/** A titled discovery rail from /api/me/recommended/{section}. */
export interface Rail {
  key: DiscoveryRailKey
  title: string
  subtitle?: string
  baseline?: string
  baseline_id?: number
  items: RailItem[]
}

/** Row of /api/music/home/recently-added (sqlc ListRecentlyAddedAlbumsRow). */
export interface RecentAlbumRow {
  id: number
  title: string
  slug: string
  year: string
  artist_name: string
  artist_slug: string
  album_type?: string
  cover_path?: string
  /** MIN(file created_at) for the album — pgtype.Timestamptz object. */
  added_at?: { Time?: string; Valid?: boolean } | string
  available?: boolean
}

/** Grouped new/updated artist event from /api/music/home (finite rail). */
export interface RecentArtistEntry {
  id: number
  media_item_id: number
  media_item_public_id?: string
  name: string
  slug: string
  album_count: number
  track_count: number
  kind: 'new' | 'updated'
  new_album_count: number
  latest_album_title?: string
  latest_album_slug?: string
  added_at: string
}

/** Row of /api/me/watch/recent. */
export interface RecentWatchedRow {
  media_item_id: number
  title: string
  poster_path: string
  slug: string
  media_type: string
}

/** Row of /api/me/watch/recent-episodes. */
export interface RecentEpisodeRow {
  episode_id: number
  media_item_id: number
  series_title: string
  series_slug: string
  season_number: number
  episode_number: number
  episode_title: string
}

export interface ForYouPage {
  items: {
    id: number
    public_id?: string
    title: string
    slug: string
    year?: string
    media_type: string
    reason?: string
    available: boolean
  }[]
  has_signal: boolean
}

/** Full-page offset continuation: another page exists iff this one was full. */
function nextOffset(pageLen: number, pageSize: number, lastParam: number): number | null {
  return pageLen === pageSize ? lastParam + pageLen : null
}

/** Recently-added movies/books — /api/media sort=added, offset-paged. */
export const recentMediaInfinite = defineInfiniteQueryOptions((type: 'movie' | 'book') => ({
  key: ['media', 'recent', type, 'inf'],
  initialPageParam: 0,
  query: async ({ pageParam }): Promise<MediaItem[]> => {
    const { $heya } = useNuxtApp()
    return (await $heya('/api/media', {
      query: { type, sort: 'added', limit: RAIL_PAGE, offset: pageParam },
    })) as MediaItem[]
  },
  getNextPageParam: (last: MediaItem[], _all: MediaItem[][], lastParam: number) =>
    nextOffset(last.length, RAIL_PAGE, lastParam),
  staleTime: 1000 * 60,
  meta: railMeta,
}))

/** Recently-added TV — grouped events, offset in entry space. */
export const recentTVInfinite = defineInfiniteQueryOptions(() => ({
  key: ['media', 'recent', 'tv', 'inf'],
  initialPageParam: 0,
  query: async ({ pageParam }): Promise<RecentTVEntry[]> => {
    const { $heya } = useNuxtApp()
    return (await $heya('/api/media/tv/recently-added', {
      query: { limit: RAIL_PAGE, offset: pageParam },
    })) as RecentTVEntry[]
  },
  getNextPageParam: (last: RecentTVEntry[], _all: RecentTVEntry[][], lastParam: number) =>
    nextOffset(last.length, RAIL_PAGE, lastParam),
  staleTime: 1000 * 60,
  meta: railMeta,
}))

/** Recently-added albums — insert-order pages from the music home shelf. */
export const recentAlbumsInfinite = defineInfiniteQueryOptions(() => ({
  key: ['home', 'recent-albums', 'inf'],
  initialPageParam: 0,
  query: async ({ pageParam }): Promise<RecentAlbumRow[]> => {
    const { $heya } = useNuxtApp()
    const res = (await $heya('/api/music/home/recently-added', {
      query: { limit: RAIL_PAGE, offset: pageParam },
    })) as { items: RecentAlbumRow[] }
    return res.items ?? []
  },
  getNextPageParam: (last: RecentAlbumRow[], _all: RecentAlbumRow[][], lastParam: number) =>
    nextOffset(last.length, RAIL_PAGE, lastParam),
  staleTime: 1000 * 60,
  meta: railMeta,
}))

/** Recently watched, deduped to one row per title (movies + shows). */
export const recentWatchedInfinite = defineInfiniteQueryOptions(() => ({
  key: ['me', 'watch', 'recent', 'inf'],
  initialPageParam: 0,
  query: async ({ pageParam }): Promise<RecentWatchedRow[]> => {
    const { $heya } = useNuxtApp()
    return (await $heya('/api/me/watch/recent', {
      query: { limit: RAIL_PAGE, offset: pageParam },
    })) as RecentWatchedRow[]
  },
  getNextPageParam: (last: RecentWatchedRow[], _all: RecentWatchedRow[][], lastParam: number) =>
    nextOffset(last.length, RAIL_PAGE, lastParam),
  staleTime: 1000 * 30,
  meta: railMetaPrivate,
}))

/** Recently watched EPISODES — one row per episode, for the TV landing. */
export const recentEpisodesInfinite = defineInfiniteQueryOptions(() => ({
  key: ['me', 'watch', 'recent-episodes', 'inf'],
  initialPageParam: 0,
  query: async ({ pageParam }): Promise<RecentEpisodeRow[]> => {
    const { $heya } = useNuxtApp()
    return (await $heya('/api/me/watch/recent-episodes', {
      query: { limit: RAIL_PAGE, offset: pageParam },
    })) as RecentEpisodeRow[]
  },
  getNextPageParam: (last: RecentEpisodeRow[], _all: RecentEpisodeRow[][], lastParam: number) =>
    nextOffset(last.length, RAIL_PAGE, lastParam),
  staleTime: 1000 * 30,
  meta: railMetaPrivate,
}))

// Artists ride /api/music/home — the grouped new/updated events have no
// offset semantics, so this one stays a plain finite query.
export const homeRecentArtistsQuery = defineQueryOptions(() => ({
  key: ['home', 'recent-artists'],
  query: async () => {
    const { $heya } = useNuxtApp()
    const home = await $heya('/api/music/home', { query: { limit: 20 } }) as {
      recent_artists: RecentArtistEntry[]
    }
    return home.recent_artists ?? []
  },
  staleTime: 1000 * 60,
  meta: railMeta,
}))

export interface ForYouParams {
  section: 'movie' | 'tv' | 'all'
  /** Steer facets (RecsBrowse) — each combination pages its own cache entry. */
  genre?: string
  minRating?: number
}

/** For You — taste-ranked, steerable by section+facets; depth-capped by the engine. */
export const forYouInfinite = defineInfiniteQueryOptions((p: ForYouParams) => ({
  key: ['for-you', p.section, 'inf', p.genre ?? '', String(p.minRating ?? 0)],
  initialPageParam: 0,
  query: async ({ pageParam }): Promise<ForYouPage> => {
    const { $heya } = useNuxtApp()
    return (await $heya('/api/me/recommendations', {
      query: {
        type: p.section === 'all' ? undefined : p.section,
        genre: p.genre || undefined,
        min_rating: p.minRating || undefined,
        limit: FORYOU_PAGE,
        offset: pageParam,
      },
    })) as ForYouPage
  },
  getNextPageParam: (last: ForYouPage, _all: ForYouPage[], lastParam: number) => {
    const next = lastParam + last.items.length
    // Stop at the engine's re-rank pool — deeper offsets return nothing
    // (and the API rejects offsets past the cap).
    return last.items.length === FORYOU_PAGE && next + FORYOU_PAGE <= FORYOU_DEPTH_CAP ? next : null
  },
  staleTime: 1000 * 60 * 5,
  meta: railMetaPrivate,
}))

/** RecRail.key values the pager endpoint accepts (mirrors the huma enum). */
export type DiscoveryRailKey
  = 'recently-released' | 'top-unwatched' | 'by-actor' | 'more-genre'
    | 'recommended' | 'top-rated' | 'rediscover'

export interface DiscoveryRailParams {
  section: 'movie' | 'tv'
  railKey: DiscoveryRailKey
  baseline?: string
  baselineId?: number
  /** Where the bundle's head stops — the pager continues from here. */
  startOffset: number
}

/** Offset continuation of one discovery rail past its bundle head. */
export const discoveryRailInfinite = defineInfiniteQueryOptions((p: DiscoveryRailParams) => ({
  key: ['recommended', p.section, 'rail', p.railKey, p.baseline ?? '', String(p.baselineId ?? 0)],
  initialPageParam: p.startOffset,
  query: async ({ pageParam }): Promise<{ items: RailItem[]; has_more: boolean }> => {
    const { $heya } = useNuxtApp()
    return (await $heya('/api/me/recommended/{section}/rail', {
      path: { section: p.section },
      query: {
        key: p.railKey,
        baseline: p.baseline || undefined,
        baseline_id: p.baselineId || undefined,
        limit: DISCOVERY_PAGE,
        offset: pageParam,
      },
    })) as { items: RailItem[]; has_more: boolean }
  },
  getNextPageParam: (last: { items: RailItem[]; has_more: boolean }, _all: unknown[], lastParam: number) =>
    last.has_more ? lastParam + last.items.length : null,
  staleTime: 1000 * 60 * 5,
}))
