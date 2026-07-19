import { defineQueryOptions } from '@pinia/colada'
import type { MediaDetail, MusicAlbumDetail, MusicAlbumRow, MusicArtistRow, MusicListPage } from '~~/shared/types'
import type { RichTrackWire } from '~/utils/trackListMeta'

export interface PlaylistTrackRow extends RichTrackWire {
  track_id: number
  track_title: string
  duration: number
  disc_number: number
  track_number: number
  album_id: number
  album_title: string
  album_cover_path: string
  album_year: string
  album_slug: string
  artist_id: number
  artist_name: string
  artist_slug: string
  position: number
  added_at: string
  available?: boolean
  rating?: number | null
}

export interface PlaylistDetailResponse {
  playlist: {
    id: number
    user_id: number
    name: string
    slug: string
    description: string
    cover_path: string
    created_at: string
    updated_at: string
  }
  tracks: PlaylistTrackRow[]
  /** Top-level (not nested in playlist) — mirrors the server's PlaylistDetail. */
  has_cover: boolean
  syncs: Array<{
    service: string
    external_id: string
    external_url?: string
    last_synced_at?: string
    last_error?: string
    sync_mode: 'two_way' | 'pull_only'
  }>
}

export interface UserPlaylistRow {
  id: number
  user_id: number
  name: string
  slug: string
  description: string
  cover_path: string
  has_cover: boolean
  created_at: string
  updated_at: string
  track_count: number
  pinned: boolean
  sidebar_pinned: boolean
  sidebar_position: number
  tags: string[] | null
  sync_services: string[]
  /** First track's addressing pair — feed playlistCoverSrc(), never render
   *  cover_path directly (it's the custom cover's disk path). */
  auto_artist_slug: string
  auto_album_slug: string
}

export interface UserPlaylistsResponse {
  items: UserPlaylistRow[]
}

export interface MusicMixTrack {
  track_id: number
  track_title: string
  duration: number
  album_id: number
  album_title: string
  album_slug: string
  album_cover_path: string
  album_year: string
  artist_id: number
  artist_name: string
  artist_slug: string
  play_count: number
}

export interface MusicMix {
  slug: string
  kind: 'for_you' | 'discovery' | 'rediscovery' | 'deep_cuts' | 'artist' | string
  description: string
  seed_artist_id: number
  seed_artist_name: string
  seed_artist_slug: string
  seed_artist_media_item_id: number
  seed_artist_media_item_public_id?: string
  name: string
  tracks: MusicMixTrack[]
}

export interface LyricsLine { time_ms: number; text: string }
export interface LyricsResponse { synced: boolean; lines: LyricsLine[] }

export type MusicBrowseKind = 'mood' | 'genre' | 'tempo'
export interface MusicBrowseTrack {
  track_id: number
  track_title: string
  duration: number
  disc_number: number
  track_number: number
  album_id: number
  album_title: string
  album_slug: string
  album_cover_path: string
  album_year: string
  artist_id: number
  artist_name: string
  artist_slug: string
}

// Browse landing (moods/genres/tempo) — up to 6 top artists per bucket, for
// the tile's rotating background art. {id, public_id} matches MediaImageRef,
// so usePosterUrl(entry) works directly.
export interface BrowseBucketArtist {
  id: number
  public_id: string
}
export interface MoodBucket {
  key: string
  label: string
  threshold: number
  track_count: number
  artists: BrowseBucketArtist[]
}
export interface GenreBucket {
  name: string
  label: string
  parent: string
  track_count: number
  artists: BrowseBucketArtist[]
}
export interface TempoBucket {
  key: string
  label: string
  min_bpm: number
  max_bpm: number
  track_count: number
  artists: BrowseBucketArtist[]
}

// Three independent, device-persisted queries — a repeat visit to
// /music/browse paints instantly from cache instead of refetching cold.
const browseMeta = { prefetch: 'none', persistence: 'device', sensitivity: 'normal' } as const

export const musicBrowseMoodsQuery = defineQueryOptions(() => ({
  key: ['music', 'browse', 'moods'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return ((await $heya('/api/music/browse/moods')) as { items: MoodBucket[] }).items ?? []
  },
  staleTime: 1000 * 60 * 5,
  meta: browseMeta,
}))

export const musicBrowseGenresQuery = defineQueryOptions(() => ({
  key: ['music', 'browse', 'genres'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return ((await $heya('/api/music/browse/genres')) as { items: GenreBucket[] }).items ?? []
  },
  staleTime: 1000 * 60 * 5,
  meta: browseMeta,
}))

export const musicBrowseTempoQuery = defineQueryOptions(() => ({
  key: ['music', 'browse', 'tempo'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return ((await $heya('/api/music/browse/tempo')) as { items: TempoBucket[] }).items ?? []
  },
  staleTime: 1000 * 60 * 5,
  meta: browseMeta,
}))

const intentMeta = {
  prefetch: 'intent',
  persistence: 'device',
  sensitivity: 'normal',
} as const

// Artist slugs can be entirely numeric (for example, the artist "666").
// /api/media/{id} deliberately interprets an all-digit path value as an
// internal media ID, so resolve the artist through the slug-only music route
// first and use its unambiguous public UUID for the full detail request.
export const musicArtistDetailQuery = defineQueryOptions((slug: string) => ({
  key: ['music', 'artist', 'detail', slug],
  query: async () => {
    const { $heya } = useNuxtApp()
    const artist = await $heya('/api/music/artists/{slug}', {
      path: { slug },
    }) as unknown as MusicArtistRow
    const mediaRef = artist.media_item_public_id || String(artist.media_item_id)
    return await $heya('/api/media/{id}', {
      path: { id: mediaRef as never },
    }) as MediaDetail
  },
  staleTime: 1000 * 60 * 5,
  retry: 0,
  meta: intentMeta,
}))

export const musicAlbumDetailQuery = defineQueryOptions((target: { artistSlug: string; albumSlug: string }) => ({
  key: ['music', 'album', target.artistSlug, target.albumSlug],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/music/artists/{artist_slug}/albums/{album_slug}', {
      path: { artist_slug: target.artistSlug, album_slug: target.albumSlug },
    }) as MusicAlbumDetail
  },
  staleTime: 1000 * 60 * 5,
  meta: intentMeta,
}))

// `ref` is a slug (canonical URLs) or a numeric id (internal callers holding
// only the id; legacy links) — the endpoint resolves both. Key is the
// stringified ref, so callers that mutate by id must invalidate/patch the
// String(id) entry; slug-keyed entries refresh on their 30s staleTime.
export const playlistDetailQuery = defineQueryOptions((ref: string | number) => ({
  key: ['music', 'playlist', String(ref)],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/me/playlists/{id}', { path: { id: String(ref) as never } }) as unknown as PlaylistDetailResponse
  },
  staleTime: 1000 * 30,
  meta: {
    ...intentMeta,
    sensitivity: 'private',
  },
}))

export const userPlaylistsQuery = defineQueryOptions(() => ({
  key: ['me', 'playlists'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/me/playlists') as unknown as UserPlaylistsResponse
  },
  staleTime: 1000 * 30,
  meta: {
    prefetch: 'none',
    persistence: 'device',
    sensitivity: 'private',
  },
}))

export const musicMixesQuery = defineQueryOptions(() => ({
  key: ['music', 'home', 'mixes-for-you', { max: 10 }],
  query: async () => {
    const { $heya } = useNuxtApp()
    return ((await $heya('/api/music/home/mixes-for-you', { query: { max: 10 } })) as { items: MusicMix[] }).items ?? []
  },
  staleTime: 1000 * 60 * 60,
  meta: {
    ...intentMeta,
    sensitivity: 'private',
  },
}))

// ── Music-home shelves ──────────────────────────────────────────────────────
// One options definition per shelf so MusicHome, the persistence layer, and
// the navigation-prefetch section warmer all share a single source of truth.
// Every shelf persists to device: the home surface is what a cold app-open
// paints first, so it hydrates from the last-known snapshot and revalidates
// in place. Play-history shelves are private; library/rotation shelves normal.
// staleTime mirrors the server Cache-Control; rotating shelves autoRefetch at
// the server's 5-minute seed rotation.

const shelfMeta = { prefetch: 'none', persistence: 'device', sensitivity: 'normal' } as const
const shelfMetaPrivate = { ...shelfMeta, sensitivity: 'private' } as const

/** Row of /api/music/home/recently-played-artists. */
export interface RecentPlayedArtistRow {
  artist_id: number
  artist_name: string
  artist_slug: string
  media_item_id: number
  media_item_public_id?: string
  album_count: number
  track_count: number
  available?: boolean
}

/** Row of /api/music/home/on-this-day. */
export interface OnThisDayRow {
  id: number
  title: string
  slug: string
  artist_name: string
  artist_slug: string
  release_year: number
}

/** Row of /api/music/home/recent-playlists. */
export interface HomePlaylistRow {
  id: number
  slug: string
  name: string
  cover_path: string
  track_count: number
  has_cover?: boolean
  updated_at?: string
  auto_artist_slug?: string
  auto_album_slug?: string
}

export interface ShelfAlbum {
  id: number
  slug: string
  title: string
  year: string
  album_type: string
}
export interface MoreByEntry {
  artist_id: number
  artist_name: string
  artist_slug: string
  albums: ShelfAlbum[]
}

export interface GenreArtist {
  artist_id: number
  artist_name: string
  artist_slug: string
  album_count: number
  track_count: number
}
export interface GenreShelf {
  enabled: boolean
  genre: string
  artists: GenreArtist[]
}

export interface MostPlayedAlbum {
  album_id: number
  album_title: string
  album_slug: string
  artist_name: string
  artist_slug: string
  play_count: number
}
export interface MostPlayedShelf {
  enabled: boolean
  window_label: string
  albums: MostPlayedAlbum[]
}

export interface LapsedAlbum {
  id: number
  slug: string
  title: string
  year: string
}
export interface LapsedArtist {
  artist_id: number
  artist_name: string
  artist_slug: string
  last_played_at: string
  months_lapsed: number
  albums: LapsedAlbum[]
}
export interface LapsedShelf {
  enabled: boolean
  since_label: string
  artists: LapsedArtist[]
}

export interface LabelAlbum {
  album_id: number
  album_title: string
  album_slug: string
  album_year: string
  artist_name: string
  artist_slug: string
}
export interface LabelShelf {
  enabled: boolean
  label: string
  albums: LabelAlbum[]
}

/** Every shelf endpoint returns the common { items: T[] } envelope. */
async function fetchShelfItems<T>(path: string): Promise<T[]> {
  const { $heya } = useNuxtApp()
  const res = await $heya(path as never) as { items: T[] }
  return res.items ?? []
}

export const musicRecentArtistsQuery = defineQueryOptions(() => ({
  key: ['music', 'home', 'recently-played-artists'],
  query: () => fetchShelfItems<RecentPlayedArtistRow>('/api/music/home/recently-played-artists'),
  staleTime: 1000 * 30,
  meta: shelfMetaPrivate,
}))

export const musicOnThisDayQuery = defineQueryOptions(() => ({
  key: ['music', 'home', 'on-this-day'],
  query: () => fetchShelfItems<OnThisDayRow>('/api/music/home/on-this-day'),
  staleTime: 1000 * 60 * 60 * 6,
  meta: shelfMeta,
}))

export const musicRecentPlaylistsQuery = defineQueryOptions(() => ({
  key: ['music', 'home', 'recent-playlists'],
  query: () => fetchShelfItems<HomePlaylistRow>('/api/music/home/recent-playlists'),
  staleTime: 1000 * 30,
  meta: shelfMetaPrivate,
}))

export const musicMoreByArtistsQuery = defineQueryOptions(() => ({
  key: ['music', 'home', 'more-by-artists'],
  query: () => fetchShelfItems<MoreByEntry>('/api/music/home/more-by-artists'),
  staleTime: 1000 * 60 * 5,
  autoRefetch: 1000 * 60 * 5,
  meta: shelfMeta,
}))

export const musicGenreShelfQuery = defineQueryOptions(() => ({
  key: ['music', 'home', 'more-in-genre'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return (await $heya('/api/music/home/more-in-genre')) as GenreShelf
  },
  staleTime: 1000 * 60 * 5,
  autoRefetch: 1000 * 60 * 5,
  meta: shelfMeta,
}))

export const musicMostPlayedShelfQuery = defineQueryOptions(() => ({
  key: ['music', 'home', 'most-played-last-month'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return (await $heya('/api/music/home/most-played-last-month')) as MostPlayedShelf
  },
  staleTime: 1000 * 60 * 5,
  meta: shelfMetaPrivate,
}))

export const musicLapsedShelfQuery = defineQueryOptions(() => ({
  key: ['music', 'home', 'lapsed-artists'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return (await $heya('/api/music/home/lapsed-artists')) as LapsedShelf
  },
  staleTime: 1000 * 60 * 5,
  autoRefetch: 1000 * 60 * 5,
  meta: shelfMetaPrivate,
}))

export const musicLabelShelfQuery = defineQueryOptions(() => ({
  key: ['music', 'home', 'more-from-label'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return (await $heya('/api/music/home/more-from-label')) as LabelShelf
  },
  staleTime: 1000 * 60 * 5,
  autoRefetch: 1000 * 60 * 5,
  meta: shelfMeta,
}))

export const musicAlbumsQuery = defineQueryOptions(() => ({
  key: ['music', 'albums', 'list', { limit: 500 }],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/music/albums', { query: { limit: 500 } }) as unknown as MusicListPage<MusicAlbumRow>
  },
  staleTime: 1000 * 60,
}))

export const musicArtistsQuery = defineQueryOptions(() => ({
  key: ['music', 'artists', 'list', { limit: 500 }],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/music/artists', { query: { limit: 500 } }) as unknown as MusicListPage<MusicArtistRow>
  },
  staleTime: 1000 * 60,
}))

// Rows returned by /api/me/ratings/artists|albums — mirrors sqlc's
// ListUserRatedArtistsRow/ListUserRatedAlbumsRow (only the fields the FE
// actually renders). Artist rows carry album/track counts pre-aggregated by
// the query so list pages don't need a follow-up fetch per row.
export interface LovedArtistRow {
  id: number
  name: string
  slug: string
  media_item_id: number
  media_item_public_id?: string
  album_count: number
  track_count: number
}

export interface LovedAlbumRow {
  id: number
  title: string
  slug: string
  year: string
  album_type: string
  cover_path: string
  artist_id: number
  artist_name: string
  artist_slug: string
}

// Shared by the My Music shelf (limit 12) and the My Artists/My Albums list
// pages (limit 500) — same key shape so the shelf's cache entry gets reused
// wholesale when a page happens to request the same limit.
export const lovedArtistsQuery = defineQueryOptions((limit: number) => ({
  key: ['me', 'loved', 'artists', { limit }],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/me/ratings/artists', { query: { min_rating: 1, limit } }) as unknown as MusicListPage<LovedArtistRow>
  },
  staleTime: 1000 * 30,
}))

export const lovedAlbumsQuery = defineQueryOptions((limit: number) => ({
  key: ['me', 'loved', 'albums', { limit }],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/me/ratings/albums', { query: { min_rating: 1, limit } }) as unknown as MusicListPage<LovedAlbumRow>
  },
  staleTime: 1000 * 30,
}))

export const trackLyricsQuery = defineQueryOptions((trackId: number) => ({
  key: ['music', 'track', trackId, 'lyrics'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/music/tracks/{id}/lyrics', { path: { id: trackId } }) as LyricsResponse
  },
  staleTime: 1000 * 60 * 30,
  meta: { prefetch: 'none', persistence: 'device', sensitivity: 'normal' },
}))

