import { defineQueryOptions } from '@pinia/colada'
export type {
  ManagerDownloadClientInput,
  ManagerDownloadClientView,
  ManagerIndexerInput,
  ManagerIndexerView,
  ManagerPathMapping,
  ManagerQualityItem,
  ManagerQualityProfileInput,
  ManagerQualityProfileView,
  ManagerTestResult,
} from '~~/shared/api/types.gen'

import type {
  ManagerDownloadClientView,
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

export const managerQualityProfilesQuery = defineQueryOptions(() => ({
  key: ['manager', 'quality-profiles'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/manager/quality-profiles') as ManagerQualityProfileView[]
  },
  staleTime: 1000 * 30,
  meta: { prefetch: 'none', sensitivity: 'private' },
}))
