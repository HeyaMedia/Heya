<template>
  <div class="scroll page-pad search-page" style="height: 100%">
    <!-- Search input -->
    <div class="search-input-wrap">
      <Icon name="search" :size="18" class="search-input-icon" />
      <input
        ref="inputEl"
        v-model="localQuery"
        type="text"
        class="search-input"
        placeholder="Search movies, TV, music, people…"
        @keydown.enter="commitQuery"
      />
      <button v-if="localQuery" class="search-clear" @click="clearSearch"><Icon name="close" :size="14" /></button>
    </div>

    <!-- Has query: show results -->
    <template v-if="query">
      <header class="search-head">
        <h1 class="search-title">
          Results for <span class="search-q">"{{ query }}"</span>
        </h1>
        <div class="search-meta">
          <template v-if="loading">Searching…</template>
          <template v-else-if="totalAcrossBuckets > 0">
            {{ totalAcrossBuckets.toLocaleString() }} match<span v-if="totalAcrossBuckets !== 1">es</span> across {{ sectionsForTabs.length }} categor<span v-if="sectionsForTabs.length === 1">y</span><span v-else>ies</span>
          </template>
          <template v-else>No results</template>
        </div>
      </header>

      <nav v-if="sectionsForTabs.length > 0" class="search-tabs">
        <button class="search-tab" :class="{ active: activeType === '' }" @click="setType('')">
          All <span class="search-tab-count">{{ totalAcrossBuckets.toLocaleString() }}</span>
        </button>
        <button
          v-for="s in sectionsForTabs" :key="s.key"
          class="search-tab" :class="{ active: activeType === s.key }" @click="setType(s.key)"
        >
          {{ s.label }} <span class="search-tab-count">{{ s.bucket.total.toLocaleString() }}</span>
        </button>
      </nav>

      <!-- "All" view -->
      <div v-if="activeType === '' && !loading">
        <section v-for="s in sectionsForTabs" :key="s.key" class="search-section-block">
          <header class="search-block-head">
            <h2>{{ s.label }}</h2>
            <button v-if="s.bucket.total > s.bucket.items.length" class="search-block-more" @click="setType(s.key)">
              View all {{ s.bucket.total.toLocaleString() }} <Icon name="arrow-right" :size="12" />
            </button>
          </header>
          <ResultGrid :section-key="s.key" :items="s.bucket.items" />
        </section>
      </div>

      <!-- Single-type view -->
      <div v-if="activeType !== '' && !loadingFiltered">
        <div v-if="filteredItems.length === 0" class="search-zero">No {{ filteredLabel.toLowerCase() }} found.</div>
        <ResultGrid v-else :section-key="activeType" :items="filteredItems" :large="true" />
        <div v-if="filteredTotal > filteredItems.length" class="search-pager">
          <button class="search-pager-btn" @click="loadMore" :disabled="loadingFiltered">
            {{ loadingFiltered ? 'Loading…' : `Show more (${filteredTotal - filteredItems.length} remaining)` }}
          </button>
        </div>
      </div>

      <div v-if="loading || loadingFiltered" class="search-loading-page">
        <span class="search-spinner" /> Searching…
      </div>
    </template>

    <!-- No query: browse by genre -->
    <template v-else>
      <div v-if="genres.length" class="browse-section">
        <h2 class="browse-title">Browse by Genre</h2>
        <div class="genre-cloud">
          <NuxtLink
            v-for="g in genres" :key="g.genre"
            :to="`/genre/${encodeURIComponent(String(g.genre))}`"
            class="genre-pill"
          >
            {{ g.genre }}
            <span class="genre-pill-count">{{ g.count }}</span>
          </NuxtLink>
        </div>
      </div>

      <div v-if="collections.length" class="browse-section">
        <h2 class="browse-title">Collections</h2>
        <div class="collections-grid">
          <NuxtLink
            v-for="c in collections" :key="c.id"
            :to="`/collection/${c.id}`"
            class="collection-card"
          >
            <div class="collection-poster">
              <Poster :idx="c.id" :src="c.poster_path || undefined" aspect="2/3" :title="c.name" />
            </div>
            <div class="collection-meta">
              <div class="collection-name">{{ c.name }}</div>
              <div class="collection-count">{{ c.movie_count }} movie{{ c.movie_count !== 1 ? 's' : '' }}</div>
            </div>
          </NuxtLink>
        </div>
      </div>

      <div v-if="!genres.length && !genresLoading" class="search-zero" style="margin-top: 80px">
        Start typing to search your library
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import type { QuickSearchResponse, SearchBucket } from '~/composables/useSearch'

const SECTION_LABELS: Record<string, string> = {
  movies: 'Movies', tv: 'TV Shows', music: 'Artists', books: 'Books',
  albums: 'Albums', tracks: 'Tracks', collections: 'Collections', people: 'People',
}
const SECTION_ORDER = ['movies', 'tv', 'music', 'albums', 'tracks', 'books', 'collections', 'people'] as const

const route = useRoute()
const router = useRouter()
const inputEl = ref<HTMLInputElement>()

const query = computed(() => (route.query.q as string) || '')
const activeType = computed(() => (route.query.type as string) || '')

const localQuery = ref(query.value)
let debounceTimer: ReturnType<typeof setTimeout> | null = null

watch(query, (q) => { localQuery.value = q })

watch(localQuery, (q) => {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(() => {
    const trimmed = q.trim()
    if (trimmed !== query.value) {
      router.replace({ path: '/search', query: trimmed ? { q: trimmed } : {} })
    }
  }, 300)
})

function commitQuery() {
  if (debounceTimer) clearTimeout(debounceTimer)
  const trimmed = localQuery.value.trim()
  if (trimmed !== query.value) {
    router.replace({ path: '/search', query: trimmed ? { q: trimmed } : {} })
  }
}

function clearSearch() {
  localQuery.value = ''
  router.replace({ path: '/search' })
  inputEl.value?.focus()
}

// Genre browse data
interface GenreRow { genre: string; count: number }
interface CollectionRow { id: number; name: string; poster_path: string; movie_count: number }

const genres = ref<GenreRow[]>([])
const collections = ref<CollectionRow[]>([])
const genresLoading = ref(true)

onMounted(async () => {
  inputEl.value?.focus()
  try {
    const { $heya } = useNuxtApp()
    const [g, c] = await Promise.all([
      $heya('/api/genres') as Promise<GenreRow[]>,
      $heya('/api/collections', { query: { limit: 20 } }) as Promise<{ items: CollectionRow[] }>,
    ])
    genres.value = g || []
    collections.value = (c?.items || []).filter(x => x.movie_count > 0)
  } catch { /* empty */ }
  genresLoading.value = false
})

// Search results
const data = ref<QuickSearchResponse | null>(null)
const loading = ref(false)

async function fetchAll(q: string) {
  if (!q) { data.value = null; return }
  loading.value = true
  try {
    const { $heya } = useNuxtApp()
    data.value = await $heya('/api/search/quick', { query: { q } }) as QuickSearchResponse
  } catch { data.value = null }
  finally { loading.value = false }
}

watch(query, (q) => { fetchAll(q) }, { immediate: true })

const sectionsForTabs = computed(() => {
  if (!data.value) return []
  const out: { key: string; label: string; bucket: SearchBucket<any> }[] = []
  for (const key of SECTION_ORDER) {
    const b = (data.value.buckets as any)[key]
    if (b && b.items && b.items.length > 0) {
      out.push({ key, label: SECTION_LABELS[key] || key, bucket: b })
    }
  }
  return out
})

const totalAcrossBuckets = computed(() =>
  sectionsForTabs.value.reduce((sum, s) => sum + s.bucket.total, 0),
)

// Paged single-type
const filteredItems = ref<any[]>([])
const filteredTotal = ref(0)
const filteredOffset = ref(0)
const loadingFiltered = ref(false)
const PAGE_SIZE = 60

const filteredLabel = computed(() => SECTION_LABELS[activeType.value] || activeType.value)

async function loadFiltered(reset = true) {
  if (!query.value || !activeType.value) return
  if (reset) { filteredItems.value = []; filteredOffset.value = 0; filteredTotal.value = 0 }
  loadingFiltered.value = true
  try {
    const { $heya } = useNuxtApp()
    const res = await $heya('/api/search', {
      query: { q: query.value, type: activeType.value as any, limit: PAGE_SIZE, offset: filteredOffset.value },
    }) as SearchBucket<any>
    filteredItems.value = filteredItems.value.concat(res.items || [])
    filteredTotal.value = res.total || 0
    filteredOffset.value += (res.items?.length || 0)
  } catch { if (reset) { filteredItems.value = []; filteredTotal.value = 0 } }
  finally { loadingFiltered.value = false }
}

function loadMore() { loadFiltered(false) }

watch([query, activeType], () => { if (activeType.value) loadFiltered(true) })

function setType(t: string) {
  router.replace({ path: '/search', query: { q: query.value, ...(t ? { type: t } : {}) } })
}
</script>

<style scoped>
.search-page { padding-top: 32px; }

/* Search input */
.search-input-wrap {
  position: relative;
  max-width: 640px;
  margin-bottom: 32px;
}
.search-input-icon {
  position: absolute;
  left: 16px;
  top: 50%;
  transform: translateY(-50%);
  color: var(--fg-3);
  pointer-events: none;
}
.search-input {
  width: 100%;
  padding: 14px 44px 14px 44px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  color: var(--fg-0);
  font-size: 16px;
  outline: none;
  transition: border-color 0.15s, box-shadow 0.15s;
}
.search-input:focus {
  border-color: var(--gold);
  box-shadow: 0 0 0 3px var(--gold-soft);
}
.search-input::placeholder { color: var(--fg-4); }
.search-clear {
  position: absolute;
  right: 12px;
  top: 50%;
  transform: translateY(-50%);
  width: 28px; height: 28px;
  border-radius: 50%;
  display: flex; align-items: center; justify-content: center;
  color: var(--fg-3);
  transition: background 0.15s, color 0.15s;
}
.search-clear:hover { background: rgba(255,255,255,0.06); color: var(--fg-0); }

/* Results header */
.search-head { margin-bottom: 20px; }
.search-title { font-size: 24px; font-weight: 600; letter-spacing: -0.02em; margin: 0 0 4px; }
.search-q { color: var(--gold); }
.search-meta { font-size: 12px; font-family: var(--font-mono); color: var(--fg-3); }

/* Tabs */
.search-tabs { display: flex; flex-wrap: wrap; gap: 4px; margin-bottom: 28px; padding-bottom: 14px; border-bottom: 1px solid var(--border); }
.search-tab {
  display: inline-flex; align-items: center; gap: 8px; padding: 6px 12px;
  border-radius: var(--r-md); background: transparent; border: 1px solid transparent;
  color: var(--fg-2); font-size: 12px; font-weight: 500; cursor: pointer;
  transition: background 0.12s, color 0.12s, border-color 0.12s;
}
.search-tab:hover { background: rgba(255,255,255,0.04); color: var(--fg-0); }
.search-tab.active { background: var(--gold-soft); color: var(--gold); border-color: var(--gold-soft); }
.search-tab-count { font-size: 10px; font-family: var(--font-mono); color: var(--fg-4); }
.search-tab.active .search-tab-count { color: var(--gold); }

/* Section blocks */
.search-section-block { margin-bottom: 40px; }
.search-block-head { display: flex; align-items: baseline; justify-content: space-between; margin-bottom: 14px; }
.search-block-head h2 { font-size: 16px; font-weight: 600; letter-spacing: -0.01em; margin: 0; }
.search-block-more {
  background: transparent; border: 0; color: var(--fg-3); font-size: 11px; font-family: var(--font-mono);
  cursor: pointer; display: inline-flex; align-items: center; gap: 4px; transition: color 0.12s;
}
.search-block-more:hover { color: var(--gold); }

/* Browse sections */
.browse-section { margin-bottom: 40px; }
.browse-title { font-size: 18px; font-weight: 600; letter-spacing: -0.01em; margin: 0 0 16px; }

.genre-cloud { display: flex; flex-wrap: wrap; gap: 8px; }
.genre-pill {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 6px 14px; border-radius: 100px;
  background: var(--bg-2); border: 1px solid var(--border);
  color: var(--fg-1); font-size: 13px; font-weight: 500;
  transition: all 0.15s; text-decoration: none;
}
.genre-pill:hover { background: var(--gold-soft); color: var(--gold); border-color: transparent; }
.genre-pill-count { font-size: 10px; font-family: var(--font-mono); color: var(--fg-4); }
.genre-pill:hover .genre-pill-count { color: var(--gold); }

.collections-grid {
  display: grid; grid-template-columns: repeat(auto-fill, minmax(160px, 1fr)); gap: 18px;
}
.collection-card { text-decoration: none; color: inherit; }
.collection-card:hover .collection-name { color: var(--gold); }
.collection-poster { border-radius: var(--r-md); overflow: hidden; }
.collection-meta { padding: 6px 2px 0; }
.collection-name { font-size: 13px; font-weight: 500; transition: color 0.15s; }
.collection-count { font-size: 11px; font-family: var(--font-mono); color: var(--fg-3); margin-top: 2px; }

/* Empty / loading */
.search-zero { padding: 60px 0; text-align: center; color: var(--fg-3); font-size: 14px; }
.search-loading-page { display: inline-flex; align-items: center; gap: 8px; color: var(--fg-3); font-size: 13px; padding: 24px 0; }
.search-spinner {
  width: 14px; height: 14px; border: 1.5px solid var(--border-strong); border-top-color: var(--gold);
  border-radius: 50%; animation: spin 0.7s linear infinite; display: inline-block;
}
@keyframes spin { to { transform: rotate(360deg); } }
.search-pager { text-align: center; padding: 24px 0 8px; }
.search-pager-btn {
  background: var(--bg-2); border: 1px solid var(--border); color: var(--fg-1);
  padding: 10px 18px; border-radius: var(--r-md); font-size: 13px; font-weight: 500;
  cursor: pointer; transition: background 0.12s, color 0.12s;
}
.search-pager-btn:hover:not(:disabled) { background: var(--bg-3); color: var(--gold); }
.search-pager-btn:disabled { opacity: 0.5; cursor: default; }
</style>
