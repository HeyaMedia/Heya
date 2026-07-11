import { defineQueryOptions } from '@pinia/colada'
import type { CollectionBrowse, EnrichedMediaItem, Library, UserList } from '~~/shared/types'
import type { UserStateMovies, UserStateSeries } from '~/composables/useUserState'

export type CatalogKind = 'movie' | 'tv'

export const enrichedCatalogQuery = defineQueryOptions((kind: CatalogKind) => ({
  key: ['media', 'catalog', kind],
  query: async () => {
    const { $heya } = useNuxtApp()
    const response = await $heya('/api/media/enriched', {
      query: { type: kind, limit: 5000 },
    }) as { movies?: EnrichedMediaItem[] | null; tv?: EnrichedMediaItem[] | null }
    return (kind === 'movie' ? response.movies : response.tv) ?? []
  },
  staleTime: 1000 * 60 * 5,
  meta: {
    prefetch: 'visible',
    persistence: 'offline-essential',
    sensitivity: 'normal',
  },
}))

export const librariesQuery = defineQueryOptions(() => ({
  key: ['libraries'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/libraries') as Library[]
  },
  staleTime: 1000 * 60 * 5,
  meta: {
    prefetch: 'none',
    persistence: 'offline-essential',
    sensitivity: 'private',
  },
}))

export const userListsQuery = defineQueryOptions(() => ({
  key: ['me', 'lists'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/me/lists') as UserList[]
  },
  staleTime: 1000 * 60,
  meta: {
    prefetch: 'none',
    persistence: 'offline-essential',
    sensitivity: 'private',
  },
}))

export const movieUserStateQuery = defineQueryOptions(() => ({
  key: ['me', 'state', 'movies'],
  query: async () => await fetchUserState('movies') as UserStateMovies,
  staleTime: 1000 * 30,
  meta: {
    prefetch: 'none',
    persistence: 'offline-essential',
    sensitivity: 'private',
  },
}))

export const seriesUserStateQuery = defineQueryOptions(() => ({
  key: ['me', 'state', 'series'],
  query: async () => await fetchUserState('series') as UserStateSeries,
  staleTime: 1000 * 30,
  meta: {
    prefetch: 'none',
    persistence: 'offline-essential',
    sensitivity: 'private',
  },
}))

export const collectionsBrowseQuery = defineQueryOptions(() => ({
  key: ['collections', 'browse'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return (await $heya('/api/collections/browse') as CollectionBrowse[] | null) ?? []
  },
  staleTime: 1000 * 60 * 10,
  meta: {
    prefetch: 'none',
    persistence: 'offline-essential',
    sensitivity: 'normal',
  },
}))
