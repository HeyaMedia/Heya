/**
 * Recent search history backed by localStorage. Per-namespace so the music
 * search can have a different history from a future global search. Newest
 * first, deduplicated, capped at 12 entries.
 */
const STORAGE_PREFIX = 'heya_recent_searches_'
const MAX_ENTRIES = 12

export function useRecentSearches(namespace: string) {
  const key = STORAGE_PREFIX + namespace
  const list = useState<string[]>(`recent-searches-${namespace}`, () => [])

  function load() {
    if (!import.meta.client) return
    try {
      const raw = localStorage.getItem(key)
      if (!raw) return
      const parsed = JSON.parse(raw)
      if (Array.isArray(parsed)) {
        list.value = parsed.filter((s): s is string => typeof s === 'string').slice(0, MAX_ENTRIES)
      }
    } catch {
      // Corrupted entry — best to just start fresh than crash the page.
      list.value = []
    }
  }

  function persist() {
    if (!import.meta.client) return
    try {
      localStorage.setItem(key, JSON.stringify(list.value))
    } catch {
      // Quota exceeded etc. — silent; recent searches are nice-to-have.
    }
  }

  function record(query: string) {
    const q = query.trim()
    if (!q) return
    const next = [q, ...list.value.filter((s) => s.toLowerCase() !== q.toLowerCase())]
    list.value = next.slice(0, MAX_ENTRIES)
    persist()
  }

  function remove(query: string) {
    list.value = list.value.filter((s) => s !== query)
    persist()
  }

  function clear() {
    list.value = []
    persist()
  }

  if (import.meta.client && !list.value.length) load()

  return { entries: list, record, remove, clear, load }
}
