import { prefetchManager } from '~/engine/prefetch'
import {
  clearPersistedQueryCache,
  loadPersistedQueryCache,
  queryCacheNamespace,
} from '~/utils/queryPersistence.client'

export type DeviceStorageArea = 'offline-data' | 'audio' | 'images'

export interface StorageAreaUsage {
  entries: number
  bytes: number
  exact: boolean
  available: boolean
}

export interface DeviceStorageSnapshot {
  totalBytes: number | null
  quotaBytes: number | null
  persisted: boolean | null
  offlineData: StorageAreaUsage
  audio: StorageAreaUsage
  images: StorageAreaUsage
  appShell: StorageAreaUsage
}

export interface DeviceStorageAdapter {
  snapshot(userId: number | string): Promise<DeviceStorageSnapshot>
  clear(area: DeviceStorageArea, userId: number | string): Promise<void>
}

const emptyUsage = (available = true): StorageAreaUsage => ({
  entries: 0,
  bytes: 0,
  exact: true,
  available,
})

async function cacheUsage(names: string[], materializeMissingSizes = false): Promise<StorageAreaUsage> {
  if (!('caches' in window)) return emptyUsage(false)
  let entries = 0
  let bytes = 0
  let exact = true
  for (const name of names) {
    const cache = await caches.open(name)
    const requests = await cache.keys()
    entries += requests.length
    for (const request of requests) {
      try {
        const response = await cache.match(request)
        if (!response) continue
        const declared = Number(response.headers.get('content-length'))
        if (Number.isFinite(declared) && declared > 0) bytes += declared
        else if (materializeMissingSizes) bytes += (await response.clone().blob()).size
        else exact = false
      } catch {
        exact = false
      }
    }
  }
  return { entries, bytes, exact, available: true }
}

async function browserSnapshot(userId: number | string): Promise<DeviceStorageSnapshot> {
  let totalBytes: number | null = null
  let quotaBytes: number | null = null
  let persisted: boolean | null = null
  try {
    const estimate = await navigator.storage?.estimate?.()
    totalBytes = estimate?.usage ?? null
    quotaBytes = estimate?.quota ?? null
    persisted = navigator.storage?.persisted ? await navigator.storage.persisted() : null
  } catch { /* storage estimates are optional */ }

  const queryRecord = await loadPersistedQueryCache(queryCacheNamespace(userId))
  const cacheNames = 'caches' in window ? await caches.keys().catch(() => []) : []
  const imageNames = cacheNames.filter(name => name === 'heya-images')
  const appNames = cacheNames.filter(name => name.startsWith('workbox-precache-'))
  const [audio, images, appShell] = await Promise.all([
    prefetchManager.usage().then(value => ({ ...value, exact: true, available: 'caches' in window })),
    cacheUsage(imageNames),
    cacheUsage(appNames, true),
  ])

  return {
    totalBytes,
    quotaBytes,
    persisted,
    offlineData: queryRecord
      ? { entries: Object.keys(queryRecord.entries).length, bytes: queryRecord.bytes, exact: true, available: true }
      : emptyUsage(),
    audio,
    images,
    appShell,
  }
}

export const deviceStorage: DeviceStorageAdapter = {
  snapshot: browserSnapshot,
  async clear(area, userId) {
    if (area === 'offline-data') await clearPersistedQueryCache(userId)
    if (area === 'audio') await prefetchManager.clearAll()
    if (area === 'images' && 'caches' in window) await caches.delete('heya-images')
  },
}
