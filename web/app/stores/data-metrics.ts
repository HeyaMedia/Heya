import { defineStore } from 'pinia'

export const useDataMetricsStore = defineStore('data-metrics', () => {
  const navigations = ref(0)
  const warmNavigations = ref(0)
  const coldNavigations = ref(0)
  const totalNavigationMs = ref(0)
  const lastNavigationMs = ref(0)
  const lastPath = ref('')
  const prefetchAttempts = ref(0)
  const prefetchAlreadyCached = ref(0)
  const prefetchUsed = ref(0)
  const prefetchWasted = ref(0)
  const cacheEntries = ref(0)
  const cacheBytes = ref(0)
  const hydratedEntries = ref(0)
  const persistedEntries = ref(0)
  const persistedBytes = ref(0)
  const persistenceWrites = ref(0)
  const totalPersistenceMs = ref(0)
  const lastPersistenceMs = ref(0)
  const maxPersistenceMs = ref(0)

  const averageNavigationMs = computed(() => navigations.value ? Math.round(totalNavigationMs.value / navigations.value) : 0)
  const prefetchUseRate = computed(() => prefetchAttempts.value ? Math.round((prefetchUsed.value / prefetchAttempts.value) * 100) : 0)
  const averagePersistenceMs = computed(() => persistenceWrites.value ? Math.round(totalPersistenceMs.value / persistenceWrites.value) : 0)

  function recordNavigation(path: string, elapsed: number, warm: boolean | null) {
    navigations.value++
    lastPath.value = path
    lastNavigationMs.value = Math.round(elapsed)
    totalNavigationMs.value += elapsed
    if (warm === true) warmNavigations.value++
    if (warm === false) coldNavigations.value++
  }
  function recordPrefetch(alreadyCached: boolean) {
    prefetchAttempts.value++
    if (alreadyCached) prefetchAlreadyCached.value++
  }
  function recordPrefetchUsed() { prefetchUsed.value++ }
  function recordPrefetchWasted() { prefetchWasted.value++ }
  function setCacheStats(entries: number, bytes: number) {
    cacheEntries.value = entries
    cacheBytes.value = bytes
  }
  function setPersistenceStats(hydrated: number, entries: number, bytes: number) {
    hydratedEntries.value = hydrated
    persistedEntries.value = entries
    persistedBytes.value = bytes
  }
  function recordPersistence(elapsed: number) {
    const rounded = Math.round(elapsed)
    persistenceWrites.value++
    totalPersistenceMs.value += elapsed
    lastPersistenceMs.value = rounded
    maxPersistenceMs.value = Math.max(maxPersistenceMs.value, rounded)
  }

  return {
    navigations, warmNavigations, coldNavigations, totalNavigationMs,
    lastNavigationMs, lastPath, averageNavigationMs,
    prefetchAttempts, prefetchAlreadyCached, prefetchUsed, prefetchWasted, prefetchUseRate,
    cacheEntries, cacheBytes, hydratedEntries, persistedEntries, persistedBytes,
    persistenceWrites, lastPersistenceMs, maxPersistenceMs, averagePersistenceMs,
    recordNavigation, recordPrefetch, recordPrefetchUsed, recordPrefetchWasted,
    setCacheStats, setPersistenceStats, recordPersistence,
  }
})
