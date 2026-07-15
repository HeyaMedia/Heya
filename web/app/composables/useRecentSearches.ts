import { effectScope } from 'vue'

// Recent spotlight searches — a small, localStorage-backed MRU list surfaced
// in the spotlight's empty state. Client-only by nature (the spotlight is
// deferred-mounted on interaction), but useLocalStorage is SSR-safe anyway.
//
// Storage shape: a plain string[] of raw query text, most-recent-first, deduped
// case-insensitively (the first casing seen wins), capped at MAX. Recorded when
// a search actually goes somewhere — Enter/click onto a result, or a "See all"
// escape to the /search page — never on every keystroke.

const STORAGE_KEY = 'heya-recent-searches'
const MAX = 8

// The backing ref lives in a DETACHED effect scope created once, lazily, on
// first use — NOT in the calling component's scope. The spotlight records a
// query and then immediately navigates + unmounts; a component-scoped
// useLocalStorage watcher (flush: 'pre') would be disposed before its write
// flushed, silently dropping the entry. A detached scope's watcher survives the
// unmount, so the write lands. It's a natural singleton too: every spotlight
// mount reads/writes the same list.
let store: Ref<string[]> | undefined

function recentStore(): Ref<string[]> {
  if (!store) {
    effectScope(true).run(() => {
      store = useLocalStorage<string[]>(STORAGE_KEY, [])
    })
  }
  return store!
}

export function useRecentSearches() {
  const items = recentStore()

  function record(raw: string) {
    const q = raw.trim()
    if (!q) return
    const lower = q.toLowerCase()
    // Drop any prior casing of the same query, then prepend the fresh one.
    items.value = [q, ...items.value.filter(x => x.toLowerCase() !== lower)].slice(0, MAX)
  }

  function remove(raw: string) {
    const lower = raw.trim().toLowerCase()
    items.value = items.value.filter(x => x.toLowerCase() !== lower)
  }

  function clear() {
    items.value = []
  }

  return { items, record, remove, clear }
}
