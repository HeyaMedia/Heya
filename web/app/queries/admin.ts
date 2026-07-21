import { defineQueryOptions } from '@pinia/colada'
export type {
  AdminDbBody as AdminDatabase,
  AdminLogLevelBody as AdminLogLevel,
  AdminDiagnosticsBody as AdminDiagnostics,
  AdminSystemBody as AdminSystem,
  AdminWorkersBody as AdminWorkers,
  DashboardStats,
  HealthBody as Health,
  JobSummaryRow,
  MetadataQueueStatus,
  ReadyBody as Ready,
  TaskResponse,
  TranscodeStatusBody as TranscodeStatus,
  TranscodeSessionsBody as TranscodeSessions,
  TranscodeSessionBody as TranscodeSession,
} from '~~/shared/api/types.gen'

import type {
  AdminDbBody as AdminDatabase,
  AdminLogLevelBody as AdminLogLevel,
  AdminDiagnosticsBody as AdminDiagnostics,
  AdminSystemBody as AdminSystem,
  AdminWorkersBody as AdminWorkers,
  DashboardStats,
  HealthBody as Health,
  JobSummaryRow,
  MetadataQueueStatus,
  ReadyBody as Ready,
  TaskResponse,
  TranscodeStatusBody as TranscodeStatus,
  TranscodeSessionsBody as TranscodeSessions,
} from '~~/shared/api/types.gen'

const privateRuntime = {
  prefetch: 'none',
  persistence: 'none',
  sensitivity: 'secret',
} as const

export const serverHealthQuery = defineQueryOptions(() => ({
  key: ['admin', 'health'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/health') as Health
  },
  staleTime: 1000 * 10,
  meta: privateRuntime,
}))

export const serverReadinessQuery = defineQueryOptions(() => ({
  key: ['admin', 'readiness'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/health/ready') as Ready
  },
  staleTime: 1000 * 10,
  meta: privateRuntime,
}))

export const adminSystemQuery = defineQueryOptions(() => ({
  key: ['admin', 'system'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/admin/system') as AdminSystem
  },
  staleTime: 1000 * 2,
  meta: privateRuntime,
}))

export const adminDiagnosticsQuery = defineQueryOptions(() => ({
  key: ['admin', 'diagnostics'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/admin/diagnostics') as AdminDiagnostics
  },
  staleTime: 1000 * 3,
  meta: privateRuntime,
}))

export const adminWorkersQuery = defineQueryOptions(() => ({
  key: ['admin', 'workers'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/admin/workers') as AdminWorkers
  },
  staleTime: 1000 * 3,
  meta: privateRuntime,
}))

export const adminLogLevelQuery = defineQueryOptions(() => ({
  key: ['admin', 'log-level'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/admin/log-level') as AdminLogLevel
  },
  staleTime: 1000 * 10,
  meta: privateRuntime,
}))

export const adminDatabaseQuery = defineQueryOptions(() => ({
  key: ['admin', 'database'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/admin/db') as AdminDatabase
  },
  staleTime: 1000 * 5,
  meta: privateRuntime,
}))

export const dashboardStatsQuery = defineQueryOptions(() => ({
  key: ['admin', 'dashboard', 'stats'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/stats') as DashboardStats
  },
  staleTime: 1000 * 15,
  meta: privateRuntime,
}))

export const metadataQueueQuery = defineQueryOptions(() => ({
  key: ['admin', 'jobs', 'metadata-queue'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/jobs/queue/metadata') as MetadataQueueStatus
  },
  staleTime: 1000 * 5,
  meta: privateRuntime,
}))

export const jobSummaryQuery = defineQueryOptions(() => ({
  key: ['admin', 'jobs', 'summary'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return (await $heya('/api/jobs/summary') as JobSummaryRow[] | null) ?? []
  },
  staleTime: 1000 * 5,
  meta: privateRuntime,
}))

export const transcodeStatusQuery = defineQueryOptions(() => ({
  key: ['admin', 'transcode', 'status'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/transcode/status') as TranscodeStatus
  },
  staleTime: 1000 * 5,
  meta: privateRuntime,
}))

export const transcodeSessionsQuery = defineQueryOptions(() => ({
  key: ['admin', 'transcode', 'sessions'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/transcode/sessions') as TranscodeSessions
  },
  staleTime: 1000,
  meta: privateRuntime,
}))

export const adminTasksQuery = defineQueryOptions(() => ({
  key: ['admin', 'tasks'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return (await $heya('/api/tasks') as TaskResponse[] | null) ?? []
  },
  staleTime: 1000 * 5,
  meta: privateRuntime,
}))
