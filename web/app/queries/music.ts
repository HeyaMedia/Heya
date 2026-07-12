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
    description: string
    cover_path: string
    created_at: string
    updated_at: string
  }
  tracks: PlaylistTrackRow[]
}

export interface UserPlaylistRow {
  id: number
  user_id: number
  name: string
  description: string
  cover_path: string
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

export const playlistDetailQuery = defineQueryOptions((id: number) => ({
  key: ['music', 'playlist', id],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/me/playlists/{id}', { path: { id } }) as unknown as PlaylistDetailResponse
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
