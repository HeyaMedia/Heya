// Random-access page cache for full-length virtualized lists/grids.
//
// The point (vs append-only infinite scroll): the scroll track is sized to
// the server-reported TOTAL immediately, so the scrollbar spans the whole
// dataset — grab it and jump to 70% and the pages covering that window are
// fetched on demand while skeletons hold the space. ensureRange(a, b) is
// wired to the virtual scroller's rendered-range event; anything already
// loaded or in flight is skipped.
//
// Stores are module-scoped, keyed by the caller's cache key (which should
// encode sort/filter), so back-navigation repaints instantly; a store idle
// for STALE_MS is dropped and refetched fresh.
import type { MaybeRefOrGetter } from 'vue'

export interface VirtualCatalogPage<T> {
  items: T[]
  total: number
}

export interface VirtualCatalogSource<T> {
  /** Cache identity — encode sort/filter so each combination pages its own store. */
  key: string
  pageSize: number
  fetch: (offset: number, limit: number) => Promise<VirtualCatalogPage<T>>
}

interface CatalogStore {
  total: number | null
  pages: Map<number, unknown[]>
  inflight: Set<number>
  fetchedAt: number
}

const STALE_MS = 60_000
const catalogStores = new Map<string, CatalogStore>()

export function useVirtualCatalog<T>(source: MaybeRefOrGetter<VirtualCatalogSource<T>>) {
  // Bumped after every page landing — itemAt/total read it so computeds and
  // templates that call them re-run without deep-watching the Maps.
  const version = ref(0)

  function store(): CatalogStore {
    const { key } = toValue(source)
    let s = catalogStores.get(key)
    if (s && s.inflight.size === 0 && Date.now() - s.fetchedAt > STALE_MS) {
      catalogStores.delete(key)
      s = undefined
    }
    if (!s) {
      s = { total: null, pages: new Map(), inflight: new Set(), fetchedAt: Date.now() }
      catalogStores.set(key, s)
    }
    return s
  }

  const total = computed(() => {
    void version.value
    void toValue(source).key
    return store().total
  })
  /** True until the first page (and therefore the total) has landed. */
  const pending = computed(() => total.value === null)

  function itemAt(i: number): T | undefined {
    void version.value
    const { pageSize } = toValue(source)
    const page = store().pages.get(Math.floor(i / pageSize))
    return page?.[i % pageSize] as T | undefined
  }

  async function fetchPage(pageIdx: number) {
    const src = toValue(source)
    const s = store()
    if (pageIdx < 0 || s.pages.has(pageIdx) || s.inflight.has(pageIdx)) return
    s.inflight.add(pageIdx)
    try {
      const res = await src.fetch(pageIdx * src.pageSize, src.pageSize)
      // The source key may have moved on (sort/filter change) mid-flight —
      // a late page must not pollute the new store.
      if (toValue(source).key !== src.key) return
      s.pages.set(pageIdx, res.items)
      s.total = res.total
      s.fetchedAt = Date.now()
      version.value++
    } catch {
      // Dropped from inflight below — the next scroll into this range retries.
    } finally {
      s.inflight.delete(pageIdx)
    }
  }

  /** Fetch every page overlapping the [start, end] item-index range. */
  function ensureRange(start: number, end: number) {
    const src = toValue(source)
    const s = store()
    const upper = s.total !== null ? Math.max(0, s.total - 1) : Math.max(start, end)
    const from = Math.max(0, Math.min(start, upper))
    const to = Math.max(from, Math.min(end, upper))
    const firstPage = Math.floor(from / src.pageSize)
    const lastPage = Math.floor(to / src.pageSize)
    for (let p = firstPage; p <= lastPage; p++) void fetchPage(p)
  }

  /** Every loaded item with its absolute index, in index order — for building
   *  play queues from whatever is currently known. */
  function loadedItems(): { item: T; index: number }[] {
    void version.value
    const { pageSize } = toValue(source)
    const s = store()
    const pageIdxs = [...s.pages.keys()].sort((a, b) => a - b)
    const out: { item: T; index: number }[] = []
    for (const p of pageIdxs) {
      const items = s.pages.get(p)! as T[]
      for (let i = 0; i < items.length; i++) out.push({ item: items[i]!, index: p * pageSize + i })
    }
    return out
  }

  // Prime page 0 (learns the total) on mount and whenever the key changes.
  watch(() => toValue(source).key, () => {
    version.value++
    ensureRange(0, toValue(source).pageSize - 1)
  })
  onMounted(() => ensureRange(0, toValue(source).pageSize - 1))

  return { total, pending, itemAt, ensureRange, loadedItems }
}
