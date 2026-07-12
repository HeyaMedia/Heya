import { defineQueryOptions } from '@pinia/colada'
import type { MusicAlbumDetail, MusicAlbumRow, MusicArtistRow, MusicListPage } from '~~/shared/types'

export interface PlaylistTrackRow {
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
  format: string | null
  bitrate_kbps: number | null
  sample_rate_hz: number | null
  bit_depth: number | null
}

export interface PlaylistDetailResponse {
  playlist: {
    id: number
    user_id: number
    name: string
    slug: string
    description: string
    cover_path: string
    has_cover: boolean
    created_at: string
    updated_at: string
  }
  tracks: PlaylistTrackRow[]
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
  auto_cover: string
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

const intentMeta = {
  prefetch: 'intent',
  persistence: 'device',
  sensitivity: 'normal',
} as const

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
  key: ['music', 'home', 'mixes-for-you'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return ((await $heya('/api/music/home/mixes-for-you')) as { items: MusicMix[] }).items ?? []
  },
  staleTime: 1000 * 60 * 60,
  meta: {
    ...intentMeta,
    sensitivity: 'private',
  },
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

export const musicBrowseTracksQuery = defineQueryOptions((target: { kind: MusicBrowseKind, key: string }) => ({
  key: ['music', 'browse', target.kind, target.key, 'tracks'],
  query: async () => {
    const { $heya } = useNuxtApp()
    let response: { items: MusicBrowseTrack[] }
    if (target.kind === 'mood') {
      response = await $heya('/api/music/browse/moods/{mood}/tracks', {
        path: { mood: target.key }, query: { limit: 500 },
      }) as { items: MusicBrowseTrack[] }
    } else if (target.kind === 'genre') {
      response = await $heya('/api/music/browse/genres/{name}/tracks', {
        path: { name: target.key }, query: { limit: 500 },
      }) as { items: MusicBrowseTrack[] }
    } else {
      response = await $heya('/api/music/browse/tempo/{band}/tracks', {
        path: { band: target.key }, query: { limit: 500 },
      }) as { items: MusicBrowseTrack[] }
    }
    return response.items ?? []
  },
  staleTime: 1000 * 60 * 10,
  meta: { prefetch: 'intent', persistence: 'device', sensitivity: 'normal' },
}))
