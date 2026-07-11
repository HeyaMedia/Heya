const DB_NAME = 'heya-offline'
const DB_VERSION = 1
const STORE_NAME = 'query-caches'
export const QUERY_CACHE_SCHEMA = 1

export type SerializedQueryEntry = [data: unknown, error: unknown, when?: number, meta?: Record<string, unknown>]
export interface PersistedQueryCache {
  namespace: string
  schema: number
  appBuild: string
  savedAt: number
  entries: Record<string, SerializedQueryEntry>
  bytes: number
}

function openDatabase(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(DB_NAME, DB_VERSION)
    request.onupgradeneeded = () => {
      const db = request.result
      if (!db.objectStoreNames.contains(STORE_NAME)) db.createObjectStore(STORE_NAME, { keyPath: 'namespace' })
    }
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error)
  })
}

async function transact<T>(mode: IDBTransactionMode, run: (store: IDBObjectStore) => IDBRequest<T>): Promise<T> {
  const db = await openDatabase()
  try {
    return await new Promise<T>((resolve, reject) => {
      const tx = db.transaction(STORE_NAME, mode)
      const request = run(tx.objectStore(STORE_NAME))
      request.onsuccess = () => resolve(request.result)
      request.onerror = () => reject(request.error)
      tx.onabort = () => reject(tx.error)
    })
  } finally {
    db.close()
  }
}

export function queryCacheNamespace(userId: number | string) {
  return `${location.origin}|user:${userId}|schema:${QUERY_CACHE_SCHEMA}`
}

export async function loadPersistedQueryCache(namespace: string) {
  try {
    const record = await transact<PersistedQueryCache | undefined>('readonly', store => store.get(namespace))
    if (!record || record.schema !== QUERY_CACHE_SCHEMA) return null
    return record
  } catch {
    return null
  }
}

export async function savePersistedQueryCache(record: PersistedQueryCache) {
  try {
    await transact<IDBValidKey>('readwrite', store => store.put(record))
  } catch {
    // IndexedDB can be unavailable in private mode or over quota. The in-memory
    // Colada cache remains fully functional, so persistence is best-effort.
  }
}

export async function clearPersistedQueryCache(userId: number | string) {
  try {
    await transact<undefined>('readwrite', store => store.delete(queryCacheNamespace(userId)))
  } catch { /* already absent / storage unavailable */ }
}
