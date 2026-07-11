import { defineQueryOptions } from '@pinia/colada'
import type { MediaItem, PersonResponse } from '~~/shared/types'

export interface CollectionDetailResponse {
  collection: {
    id: number
    name: string
    overview: string
    poster_path: string
    backdrop_path: string
  }
  parts: Array<{
    title: string
    year?: number
    tmdb_id?: number
    poster_path?: string
    vote_average?: number
    local_media_item_id?: number | null
    local_public_id?: string | null
    local_slug?: string | null
  }>
  movies: MediaItem[]
  genres: string[]
  keywords: string[]
  owned_count: number
}

const intentMeta = {
  prefetch: 'intent',
  persistence: 'device',
  sensitivity: 'normal',
} as const

export const collectionDetailQuery = defineQueryOptions((id: number) => ({
  key: ['collection', 'detail', id],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/collections/{id}', { path: { id } }) as CollectionDetailResponse
  },
  staleTime: 1000 * 60 * 5,
  retry: 0,
  meta: intentMeta,
}))

export const personDetailQuery = defineQueryOptions((id: string) => ({
  key: ['person', 'detail', id],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/person/{id}', { path: { id } }) as PersonResponse
  },
  staleTime: 1000 * 60 * 10,
  retry: 0,
  meta: intentMeta,
}))
