import { defineQueryOptions } from '@pinia/colada'
export type {
  ManagerClientActivityView,
  ManagerClientHistoryItem,
  ManagerClientQueueItem,
  ManagerDownloadClientInput,
  ManagerDownloadClientView,
  ManagerIndexerInput,
  ManagerIndexerStatsView,
  ManagerIndexerView,
  ManagerPathMapping,
  ManagerQualityItem,
  ManagerQualityProfileInput,
  ManagerQualityProfileView,
  ManagerTestResult,
} from '~~/shared/api/types.gen'

import type {
  ManagerClientActivityView,
  ManagerDownloadClientView,
  ManagerIndexerStatsView,
  ManagerIndexerView,
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
