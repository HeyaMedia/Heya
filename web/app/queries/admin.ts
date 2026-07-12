import { defineQueryOptions } from '@pinia/colada'
import type { components } from '#open-fetch-schemas/heya'

export type Health = components['schemas']['HealthBody']
export type Ready = components['schemas']['ReadyBody']
export type AdminSystem = components['schemas']['AdminSystemBody']
export type AdminLogLevel = components['schemas']['AdminLogLevelBody']
export type AdminDatabase = components['schemas']['AdminDBBody']
export type DashboardStats = components['schemas']['DashboardStats']
export type MetadataQueueStatus = components['schemas']['MetadataQueueStatus']
export type JobSummaryRow = components['schemas']['JobSummaryRow']
export type TranscodeStatus = components['schemas']['TranscodeStatusBody']
export type TaskResponse = components['schemas']['TaskResponse']

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

export const adminTasksQuery = defineQueryOptions(() => ({
  key: ['admin', 'tasks'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return (await $heya('/api/tasks') as TaskResponse[] | null) ?? []
  },
  staleTime: 1000 * 5,
  meta: privateRuntime,
}))
