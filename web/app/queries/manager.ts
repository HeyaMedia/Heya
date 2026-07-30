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
  ManagerMetadataView,
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

export type { ManagerAlbumDetailView, ManagerTrackView, CalendarEventView, CalendarEventDetailView } from '~~/shared/api/types.gen'
import type { ManagerAlbumDetailView, CalendarEventView } from '~~/shared/api/types.gen'

export type ManagerCalendarParams = {
  from: string // YYYY-MM-DD, inclusive
  to: string // YYYY-MM-DD, inclusive
  libraries?: number[]
  monitored?: boolean
}

export const managerCalendarQuery = defineQueryOptions((p: ManagerCalendarParams) => ({
  key: ['manager', 'calendar', p.from, p.to, (p.libraries ?? []).join(','), p.monitored ?? false],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/manager/calendar', {
      query: {
        from: p.from,
        to: p.to,
        // Comma-joined on purpose: huma parses "3,2" into the []int64 param,
        // while ofetch's repeated-key array form keeps only the first value.
        libraries: p.libraries?.length ? p.libraries.join(',') : undefined,
        monitored: p.monitored || undefined,
      },
    }) as CalendarEventView[]
  },
  // Month navigation swaps the key; carrying the previous window keeps the
  // grid on screen instead of flashing empty while the new range loads.
  placeholderData: (prev: CalendarEventView[] | undefined) => prev,
  staleTime: 1000 * 60 * 5,
  meta: { prefetch: 'none', sensitivity: 'private' },
}))

export const managerAlbumDetailQuery = defineQueryOptions((ref: string) => ({
  key: ['manager', 'album', ref],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya(`/api/manager/album/${ref}`) as ManagerAlbumDetailView
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
  // as placeholder keeps the grid on screen instead of flashing a loader —
  // but only within the SAME library, or switching libraries would render the
  // old library's items under the new one's routes while the fetch runs.
  placeholderData: (prev: ManagerLibraryItemsPage | undefined) =>
    prev && prev.library.id === p.libraryId ? prev : undefined,
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

// ── Acquisition (dry-run) ────────────────────────────────────────────────

export type {
  ManagerDecisionView,
  ManagerHistoryPage,
  ManagerItemDecisionsPage,
  ManagerRejectionView,
  ManagerRunDetailView,
  ManagerSearchCandidateView,
  ManagerSearchRunView,
} from '~~/shared/api/types.gen'
import type {
  ManagerHistoryPage,
  ManagerItemDecisionsPage,
  ManagerRunDetailView,
} from '~~/shared/api/types.gen'

export type ManagerHistoryParams = {
  verdicts?: string[]
  domains?: string[]
  library?: number
}

// Keyset-paged ledger: the page carries next_before/next_id cursors; the
// page component accumulates pages client-side and refetches from the top
// on invalidation.
export const managerHistoryQuery = defineQueryOptions((p: ManagerHistoryParams) => ({
  key: ['manager', 'history', (p.verdicts ?? []).join(','), (p.domains ?? []).join(','), p.library ?? 0],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/manager/history', {
      query: {
        // Comma-joined on purpose — huma keeps only the first repeated param.
        verdicts: p.verdicts?.length ? p.verdicts.join(',') : undefined,
        domains: p.domains?.length ? p.domains.join(',') : undefined,
        library: p.library || undefined,
      },
    }) as ManagerHistoryPage
  },
  placeholderData: (prev: ManagerHistoryPage | undefined) => prev,
  staleTime: 1000 * 15,
  meta: { prefetch: 'none', sensitivity: 'private' },
}))

export const managerRunDetailQuery = defineQueryOptions((id: number) => ({
  key: ['manager', 'run', id],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya(`/api/manager/runs/${id}`) as ManagerRunDetailView
  },
  // Runs are immutable once finished — cache aggressively.
  staleTime: 1000 * 60 * 10,
  meta: { prefetch: 'none', sensitivity: 'private' },
}))

export const managerMetadataQuery = defineQueryOptions((id: number) => ({
  key: ['manager', 'metadata', id],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya(`/api/manager/media/${id}/metadata`) as ManagerMetadataView
  },
  staleTime: 1000 * 15,
  meta: { prefetch: 'none', sensitivity: 'private' },
}))

export const managerItemDecisionsQuery = defineQueryOptions((id: number) => ({
  key: ['manager', 'item-decisions', id],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya(`/api/manager/media/${id}/decisions`) as ManagerItemDecisionsPage
  },
  staleTime: 1000 * 15,
  meta: { prefetch: 'none', sensitivity: 'private' },
}))
