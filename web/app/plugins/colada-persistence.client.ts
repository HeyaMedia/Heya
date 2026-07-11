import { hydrateQueryCache, serializeQueryCache, useQueryCache } from '@pinia/colada'
import type { QueryMeta } from '@pinia/colada'
import type { Pinia } from 'pinia'
import {
  loadPersistedQueryCache,
  queryCacheNamespace,
  savePersistedQueryCache,
  QUERY_CACHE_SCHEMA,
  type PersistedQueryCache,
  type SerializedQueryEntry,
} from '~/utils/queryPersistence.client'

const MAX_ENTRIES = 300
const MAX_BYTES = 32 * 1024 * 1024
const DEVICE_MAX_AGE = 1000 * 60 * 60 * 24 * 3
const OFFLINE_MAX_AGE = 1000 * 60 * 60 * 24 * 14

function maxAge(meta?: QueryMeta) {
  return meta?.persistence === 'offline-essential' ? OFFLINE_MAX_AGE : DEVICE_MAX_AGE
}

function approved(meta?: QueryMeta) {
  return (meta?.persistence === 'device' || meta?.persistence === 'offline-essential')
    && meta.sensitivity !== 'secret'
}

function prune(entries: Record<string, SerializedQueryEntry>, now = Date.now()) {
  const candidates = Object.entries(entries)
    .filter(([, entry]) => {
      const [data, , when = 0, meta] = entry
      return data !== undefined && approved(meta as QueryMeta | undefined) && now - when <= maxAge(meta as QueryMeta | undefined)
    })
    .sort(([, a], [, b]) => (b[2] ?? 0) - (a[2] ?? 0))

  const selected: Record<string, SerializedQueryEntry> = {}
  let bytes = 2
  for (const [key, entry] of candidates) {
    if (Object.keys(selected).length >= MAX_ENTRIES) break
    const entryBytes = new Blob([JSON.stringify([key, entry])]).size
    if (bytes + entryBytes > MAX_BYTES) continue
    // Errors are never persisted. A failed background revalidation keeps the
    // previous data in Colada; storing that data as a successful snapshot is
    // what allows the next offline boot to use it again.
    selected[key] = [entry[0], null, entry[2], entry[3]]
    bytes += entryBytes
  }
  return { entries: selected, bytes }
}

export default defineNuxtPlugin({
  name: 'heya:colada-persistence',
  dependsOn: ['heya:auth'],
  async setup(nuxtApp) {
    const { token, user } = useAuth()
    const queryCache = useQueryCache(nuxtApp.$pinia as Pinia)
    const metrics = useDataMetricsStore(nuxtApp.$pinia as Pinia)
    let activeUserId: number | null = null
    let stopSession: (() => void) | null = null
    let sessionGeneration = 0

    async function startSession(userId: number) {
      if (activeUserId === userId) return
      stopSession?.()
      activeUserId = userId
      const generation = ++sessionGeneration
      const namespace = queryCacheNamespace(userId)
      const persisted = await loadPersistedQueryCache(namespace)
      if (generation !== sessionGeneration || activeUserId !== userId || !token.value) return
      const lastSuccessWhen = new Map<string, number>()
      let diskEntries: Record<string, SerializedQueryEntry> = {}
      if (persisted) {
        const hydrated = prune(persisted.entries)
        diskEntries = hydrated.entries
        hydrateQueryCache(queryCache, hydrated.entries)
        for (const [key, entry] of Object.entries(hydrated.entries)) lastSuccessWhen.set(key, entry[2] ?? 0)
        metrics.setPersistenceStats(Object.keys(hydrated.entries).length, Object.keys(hydrated.entries).length, hydrated.bytes)
      }

      let timer: ReturnType<typeof setTimeout> | null = null
      let writing = false
      let writeAgain = false
      let stopped = false

      async function persist() {
        if (stopped || !token.value || activeUserId !== userId) return
        if (writing) { writeAgain = true; return }
        writing = true
        try {
          const serialized = serializeQueryCache(queryCache) as Record<string, SerializedQueryEntry>
          for (const entry of queryCache.getEntries()) {
            if (entry.state.value.status === 'success') lastSuccessWhen.set(entry.keyHash, entry.when)
            const successfulWhen = lastSuccessWhen.get(entry.keyHash)
            if (successfulWhen && serialized[entry.keyHash]) serialized[entry.keyHash]![2] = successfulWhen
          }
          // GC of inactive memory entries must not erase their longer-lived
          // offline copy, so updates merge into the last disk snapshot.
          const selected = prune({ ...diskEntries, ...serialized })
          diskEntries = selected.entries
          const record: PersistedQueryCache = {
            namespace,
            schema: QUERY_CACHE_SCHEMA,
            appBuild: nuxtApp.$config.app.buildId,
            savedAt: Date.now(),
            entries: selected.entries,
            bytes: selected.bytes,
          }
          await savePersistedQueryCache(record)
          metrics.setPersistenceStats(metrics.hydratedEntries, Object.keys(selected.entries).length, selected.bytes)
        } finally {
          writing = false
          if (writeAgain) { writeAgain = false; void persist() }
        }
      }

      function schedule() {
        if (!token.value) return
        if (timer) clearTimeout(timer)
        timer = setTimeout(() => { timer = null; void persist() }, 750)
      }

      const stopWatch = watch(
        () => queryCache.getEntries().map(entry => [entry.keyHash, entry.when, entry.state.value.status, entry.meta.persistence, entry.meta.sensitivity]),
        schedule,
        { deep: true },
      )
      const flushWhenHidden = () => { if (document.visibilityState === 'hidden') void persist() }
      document.addEventListener('visibilitychange', flushWhenHidden)
      window.addEventListener('pagehide', persist)

      stopSession = () => {
        stopped = true
        stopWatch()
        if (timer) clearTimeout(timer)
        document.removeEventListener('visibilitychange', flushWhenHidden)
        window.removeEventListener('pagehide', persist)
      }
    }

    function currentUserId() {
      const stored = localStorage.getItem('heya_user_id')
      return user.value?.id ?? (stored ? Number(stored) : null)
    }

    const initialUserId = currentUserId()
    if (token.value && initialUserId) await startSession(initialUserId)

    const stopAuthWatch = watch([token, user], ([currentToken]) => {
      const userId = currentUserId()
      if (currentToken && userId) void startSession(userId)
      else {
        sessionGeneration++
        stopSession?.()
        stopSession = null
        activeUserId = null
      }
    })

    nuxtApp.vueApp.onUnmount(() => {
      stopAuthWatch()
      stopSession?.()
    })
  },
})
