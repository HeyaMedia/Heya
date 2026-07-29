import { defineQueryOptions } from '@pinia/colada'
export type {
  CustomFormatSpec,
  ManagerClientActivityView,
  ManagerClientCategory,
  ManagerClientHistoryItem,
  ManagerClientQueueItem,
  ManagerClientWarning,
  ManagerCustomFormatInput,
  ManagerCustomFormatView,
  ManagerDownloadClientInput,
  ManagerDownloadClientView,
  ManagerFormatImportInput,
  ManagerFormatImportResult,
  ManagerFormatScore,
  ManagerIndexerHistoryDay,
  ManagerIndexerHistoryView,
  ManagerIndexerInput,
  ManagerIndexerStatsView,
  ManagerIndexerView,
  ManagerAlbumView,
  ManagerEpisodeView,
  ManagerFileView,
  ManagerLibraryItemView,
  ManagerLibraryItemsPage,
  ManagerLibraryRef,
  ManagerLibraryStatsView,
  ManagerMediaBulkInput,
  ManagerMediaBulkResult,
  ManagerMediaDetailView,
  ManagerSeasonView,
  ManagerPathMapping,
  ManagerQualityItem,
  ManagerQualityProfileInput,
  ManagerQualityProfileView,
  ManagerReleaseTestView,
  ManagerTestResult,
} from '~~/shared/api/types.gen'

import type {
  ManagerClientActivityView,
  ManagerCustomFormatView,
  ManagerDownloadClientView,
  ManagerIndexerStatsView,
  ManagerIndexerView,
  ManagerLibraryItemsPage,
  ManagerMediaDetailView,
  ManagerQualityItem,
  ManagerQualityProfileView,
} from '~~/shared/api/types.gen'

export const managerIndexersQuery = defineQueryOptions(() => ({
  key: ['manager', 'indexers'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/manager/indexers') as ManagerIndexerView[]
  },
  staleTime: 1000 * 30,
  meta: { prefetch: 'none', sensitivity: 'private' },
}))

export const managerDownloadClientsQuery = defineQueryOptions(() => ({
  key: ['manager', 'download-clients'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/manager/download-clients') as ManagerDownloadClientView[]
  },
  staleTime: 1000 * 30,
  meta: { prefetch: 'none', sensitivity: 'private' },
}))

// Live per-indexer counters for one Prowlarr app row. Pages poll by calling
// refetch on an interval — Prowlarr owns the history, we never persist it.
export const managerIndexerStatsQuery = defineQueryOptions((id: number) => ({
  key: ['manager', 'indexer-stats', id],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya(`/api/manager/indexers/${id}/stats`) as ManagerIndexerStatsView[]
  },
  staleTime: 1000 * 60,
  meta: { prefetch: 'none', sensitivity: 'private' },
}))

export const managerClientActivityQuery = defineQueryOptions((id: number) => ({
  key: ['manager', 'client-activity', id],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya(`/api/manager/download-clients/${id}/activity`) as ManagerClientActivityView
  },
  staleTime: 1000 * 5,
  meta: { prefetch: 'none', sensitivity: 'private' },
}))

export const managerQualityProfilesQuery = defineQueryOptions(() => ({
  key: ['manager', 'quality-profiles'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/manager/quality-profiles') as ManagerQualityProfileView[]
  },
  staleTime: 1000 * 30,
  meta: { prefetch: 'none', sensitivity: 'private' },
}))

export const managerQualityLaddersQuery = defineQueryOptions(() => ({
  key: ['manager', 'quality-ladders'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/manager/quality-ladders') as Record<string, ManagerQualityItem[]>
  },
  staleTime: 1000 * 60 * 10,
  meta: { prefetch: 'none', sensitivity: 'private' },
}))

export const managerCustomFormatsQuery = defineQueryOptions(() => ({
  key: ['manager', 'custom-formats'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/manager/custom-formats') as ManagerCustomFormatView[]
  },
  staleTime: 1000 * 30,
  meta: { prefetch: 'none', sensitivity: 'private' },
}))

export type ManagerLibraryItemsParams = {
  libraryId: number
  search?: string
  monitored?: string
  fileState?: string
  status?: string
  profile?: string
  sort?: string
  dir?: string
  page?: number
  perPage?: number
}

// Callers pass reactive params through a getter (useQuery(() => ...)) so a
// filter change produces a new key and refetches — raw refs inside a plain
// options object silently never would.
export const managerLibraryItemsQuery = defineQueryOptions((p: ManagerLibraryItemsParams) => ({
  key: [
    'manager', 'library-items', p.libraryId,
    p.search ?? '', p.monitored ?? '', p.fileState ?? '', p.status ?? '',
    p.profile ?? '', p.sort ?? 'title', p.dir ?? 'asc', p.page ?? 1, p.perPage ?? 60,
  ],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya(`/api/manager/library/${p.libraryId}/items`, {
      query: {
        search: p.search || undefined,
        monitored: p.monitored || undefined,
        file_state: p.fileState || undefined,
        status: p.status || undefined,
        profile: p.profile || undefined,
        sort: p.sort || undefined,
        dir: p.dir || undefined,
        page: p.page ?? 1,
        per_page: p.perPage ?? 60,
      },
    }) as ManagerLibraryItemsPage
  },
  // The list page fetches a library once (per_page 10000) and filters
  // client-side; a longer stale window keeps back-navigation instant while
  // manager.changed / media.* events still invalidate eagerly.
  staleTime: 1000 * 60,
  // Filter/sort/page changes swap the cache key; carrying the previous page
  // as placeholder keeps the grid on screen instead of flashing a loader.
  placeholderData: (prev: ManagerLibraryItemsPage | undefined) => prev,
  meta: { prefetch: 'none', sensitivity: 'private' },
}))

export const managerMediaDetailQuery = defineQueryOptions((id: number) => ({
  key: ['manager', 'media', id],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya(`/api/manager/media/${id}`) as ManagerMediaDetailView
  },
  staleTime: 1000 * 15,
  meta: { prefetch: 'none', sensitivity: 'private' },
}))
