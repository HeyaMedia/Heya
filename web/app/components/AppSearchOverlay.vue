<!--
  AppSearchOverlay — phone-only fullscreen search.

  AppTopBar's inline `.search-wrap` dropdown (desktop/tablet, >720px) is
  cramped at phone widths — this replaces it there with a fullscreen
  Teleport'd surface instead of trying to squeeze the same dropdown into a
  390px viewport. Desktop is untouched: AppTopBar only ever flips
  `open` to true from the phone trigger button.

  Wired to the same `useQuickSearch(180)` composable AppTopBar's dropdown
  uses (debounce, latest-wins seq guard, `/api/search/quick`), but keeps its
  own instance — this component owns its own query/results lifecycle,
  independent of the desktop dropdown's `searchFocused`/`selectedIdx`
  keyboard-nav state, which doesn't apply on a touch overlay.

  Close paths:
  - Back-arrow button
  - Selecting a result / "See all" (navigates, then closes)
  - Route change while open (safety net for any other navigation)
  - Android hardware back — see the pushState/popstate wiring below

  Hardware-back handling: opening pushes a dummy history entry so the
  phone's back gesture/button closes the overlay instead of leaving the
  page. The back-arrow button calls `history.back()` to consume that entry
  when it's still on top; other close paths (selecting a result, route
  change) just flip `open` to false and leave the dummy entry to be
  consumed by a later back tap — same tradeoff most "modal via history"
  patterns make. Popstate NEVER reopens the overlay, only closes it.
-->
<template>
  <Teleport to="body">
    <Transition name="so-fade">
      <div v-if="open" class="search-overlay" role="dialog" aria-modal="true" aria-label="Search">
        <header class="so-header">
          <button type="button" class="so-back" aria-label="Close search" @click="handleBack">
            <Icon name="back" :size="20" />
          </button>
          <div class="so-input-wrap">
            <Icon name="search" :size="16" />
            <input
              ref="inputRef"
              v-model="search.query.value"
              type="search"
              autofocus
              autocapitalize="off"
              autocorrect="off"
              enterkeyhint="search"
              placeholder="Search titles, artists, people…"
              @keydown.enter.prevent="onEnter"
            />
            <button v-if="search.query.value" type="button" class="so-clear" aria-label="Clear search" @click="search.reset()">
              <Icon name="close" :size="14" />
            </button>
          </div>
        </header>

        <div class="so-body">
          <div v-if="!search.query.value.trim()" class="so-state">
            <Icon name="search" :size="26" />
            <p>Search your library</p>
          </div>

          <div v-else-if="search.loading.value && !search.data.value" class="so-state">
            <span class="so-spinner" />
            <p>Searching…</p>
          </div>

          <div v-else-if="search.data.value && sections.length === 0" class="so-state">
            <p>No results for <strong>{{ search.data.value.query }}</strong></p>
          </div>

          <template v-else>
            <div v-for="section in sections" :key="section.key" class="so-section">
              <div class="so-section-header">
                <span class="so-section-title">{{ section.label }}</span>
                <span class="so-section-count">{{ section.bucket.total.toLocaleString() }}</span>
              </div>
              <button
                v-for="item in section.bucket.items"
                :key="section.key + ':' + item.id"
                type="button"
                class="so-result"
                @click="goToResult(section.key, item)"
              >
                <div class="so-result-thumb" :class="section.thumbShape">
                  <NuxtImg v-if="thumbUrl(section.key, item)" :src="thumbUrl(section.key, item)!" :width="80" :quality="80" loading="lazy" />
                  <Icon v-else :name="section.icon" :size="16" />
                </div>
                <div class="so-result-body">
                  <div class="so-result-title">{{ resultTitle(section.key, item) }}</div>
                  <div v-if="resultSub(section.key, item)" class="so-result-sub">
                    {{ resultSub(section.key, item) }}
                  </div>
                </div>
              </button>
            </div>

            <button type="button" class="so-see-all" @click="seeAll">
              See all results for "{{ search.query.value }}"
              <Icon name="arrow-right" :size="12" />
            </button>
          </template>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
const open = defineModel<boolean>('open', { default: false })

const route = useRoute()
const search = useQuickSearch(180)
const inputRef = ref<HTMLInputElement>()

interface Section {
  key: 'movies' | 'tv' | 'music' | 'books' | 'albums' | 'tracks' | 'collections' | 'people'
  label: string
  icon: string
  thumbShape: 'poster' | 'square' | 'circle'
  bucket: { items: any[], total: number }
}

// Order per the phone overlay spec — leads with the music buckets (Artists/
// Albums/Songs) rather than AppTopBar's movies-first order, which is tuned
// for the desktop dropdown's "generic name" disambiguation case.
const SECTION_DEFS: Array<Omit<Section, 'bucket'>> = [
  { key: 'music',       label: 'Artists',     icon: 'music', thumbShape: 'square' },
  { key: 'albums',      label: 'Albums',      icon: 'music', thumbShape: 'square' },
  { key: 'tracks',      label: 'Songs',       icon: 'music', thumbShape: 'square' },
  { key: 'movies',      label: 'Movies',      icon: 'film',  thumbShape: 'poster' },
  { key: 'tv',          label: 'TV Shows',    icon: 'tv',    thumbShape: 'poster' },
  { key: 'books',       label: 'Books',       icon: 'book',  thumbShape: 'poster' },
  { key: 'people',      label: 'People',      icon: 'users', thumbShape: 'circle' },
  { key: 'collections', label: 'Collections', icon: 'film',  thumbShape: 'poster' },
]

const sections = computed<Section[]>(() => {
  const data = search.data.value
  if (!data) return []
  const out: Section[] = []
  for (const def of SECTION_DEFS) {
    const b = (data.buckets as any)[def.key]
    if (b && b.items && b.items.length > 0) out.push({ ...def, bucket: b })
  }
  return out
})

function thumbUrl(kind: Section['key'], item: any): string | null {
  switch (kind) {
    case 'movies':
    case 'tv':
    case 'music':
    case 'books':
      return usePosterUrl(item)
    case 'people':
      return personImageUrl(item.id)
    case 'albums':
      return albumCoverUrl(item)
    case 'tracks':
      return item.artist_media_item_id
        ? usePosterUrl({ id: item.artist_media_item_id, public_id: item.artist_media_item_public_id })
        : null
    case 'collections':
      return null
  }
}

function resultTitle(kind: Section['key'], item: any): string {
  if (kind === 'people') return item.name
  if (kind === 'collections') return item.name
  return item.title
}

function resultSub(kind: Section['key'], item: any): string {
  switch (kind) {
    case 'movies':
    case 'tv':
    case 'music':
    case 'books':
      return item.year || ''
    case 'albums':
      return item.artist_name + (item.year ? ' · ' + item.year : '')
    case 'tracks':
      return [item.artist_name, item.album_title].filter(Boolean).join(' · ')
    case 'people': {
      const parts: string[] = []
      if (item.cast_count) parts.push(`${item.cast_count} role${item.cast_count === 1 ? '' : 's'}`)
      if (item.crew_count) parts.push(`${item.crew_count} credit${item.crew_count === 1 ? '' : 's'}`)
      return parts.join(' · ')
    }
    case 'collections':
      return ''
  }
}

function goToResult(kind: Section['key'], item: any) {
  let path = ''
  switch (kind) {
    case 'movies':
      path = `/movies/${item.slug || slugify(item.title)}`
      break
    case 'tv':
      path = `/tv/${item.slug || slugify(item.title)}`
      break
    case 'music':
      path = `/music/artist/${item.slug || slugify(item.title)}`
      break
    case 'books':
      path = `/books/${item.slug || slugify(item.title)}`
      break
    case 'people':
      path = `/person/${item.slug || item.id}`
      break
    case 'albums':
      if (item.artist_slug && item.slug) {
        path = `/music/artist/${item.artist_slug}/${item.slug}`
      } else {
        path = `/music/artist/${item.artist_slug || slugify(item.artist_name)}`
      }
      break
    case 'tracks':
      if (item.artist_slug && item.album_slug) {
        path = `/music/artist/${item.artist_slug}/${item.album_slug}`
      } else {
        path = `/music/artist/${item.artist_slug || slugify(item.artist_name)}`
      }
      break
    case 'collections':
      path = `/search?q=${encodeURIComponent(item.name)}&type=collections`
      break
  }
  if (path) navigateTo(path)
  close()
}

function onEnter() {
  const q = search.query.value.trim()
  if (q) seeAll()
}

function seeAll() {
  const q = search.query.value.trim()
  if (!q) return
  navigateTo(`/search?q=${encodeURIComponent(q)}`)
  close()
}

function close() {
  open.value = false
}

// ── Hardware-back / history wiring ──────────────────────────────────────
// `pushedState` tracks whether our dummy history entry is still the top of
// the stack (i.e. no popstate has consumed it yet since we pushed it).
let pushedState = false

function handleBack() {
  if (pushedState) {
    // Consumes the dummy entry — onPopState below flips `open` to false.
    history.back()
  } else {
    close()
  }
}

function onPopState() {
  // Fires for our own dummy entry being popped (hardware back) — but could
  // also fire for unrelated browser navigation while we happen to be open.
  // Either way, only ever close; never reopen from a popstate.
  pushedState = false
  if (open.value) open.value = false
}

onMounted(() => {
  window.addEventListener('popstate', onPopState)
})
onUnmounted(() => {
  window.removeEventListener('popstate', onPopState)
  // Guard against leaking the lock if the component unmounts while open
  // (shouldn't happen — AppTopBar mounts this for the app's lifetime).
  if (open.value) document.documentElement.style.overflow = ''
})

watch(open, (isOpen) => {
  if (isOpen) {
    pushedState = true
    history.pushState({ heyaSearch: true }, '')
    document.documentElement.style.overflow = 'hidden'
    nextTick(() => inputRef.value?.focus())
  } else {
    document.documentElement.style.overflow = ''
    search.reset()
  }
})

// Route changes (e.g. a result navigation resolving) are a safety net on
// top of the explicit close() calls above — covers any navigation path we
// didn't anticipate.
watch(() => route.fullPath, () => { if (open.value) close() })
</script>

<style scoped>
.search-overlay {
  position: fixed;
  inset: 0;
  z-index: 450;
  display: flex;
  flex-direction: column;
  background: var(--bg-1);
}

.so-header {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: calc(env(safe-area-inset-top, 0px) + 10px) 16px 10px;
  border-bottom: 1px solid var(--border);
}

.so-back {
  flex-shrink: 0;
  width: 36px;
  height: 36px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--r-md);
  color: var(--fg-1);
  background: transparent;
  border: 0;
}
.so-back:active { background: rgba(255,255,255,0.08); }

.so-input-wrap {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 8px;
  height: 40px;
  padding: 0 10px 0 12px;
  background: var(--bg-3);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  color: var(--fg-3);
}
.so-input-wrap input {
  flex: 1;
  min-width: 0;
  background: transparent;
  border: 0;
  outline: 0;
  color: var(--fg-0);
  font-size: 15px;
  padding: 0;
  /* Prevent iOS Safari from auto-zooming the page on focus (fires below 16px). */
}
.so-input-wrap input::placeholder { color: var(--fg-3); }
/* We render our own clear button (.so-clear) — hide the native WebKit one
   for type="search" so there's only one X. */
.so-input-wrap input::-webkit-search-cancel-button { -webkit-appearance: none; appearance: none; }
.so-clear { flex-shrink: 0; color: var(--fg-3); background: transparent; border: 0; display: flex; }
.so-clear:active { color: var(--fg-0); }

.so-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
  padding-bottom: calc(env(safe-area-inset-bottom, 0px) + 12px);
}

.so-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 20vh 24px 0;
  color: var(--fg-3);
  font-size: 14px;
  text-align: center;
}
.so-state strong { color: var(--fg-1); font-weight: 600; }

.so-spinner {
  width: 16px; height: 16px;
  border: 2px solid var(--border-strong);
  border-top-color: var(--gold);
  border-radius: 50%;
  animation: so-spin 0.7s linear infinite;
  display: inline-block;
}
@keyframes so-spin { to { transform: rotate(360deg); } }

.so-section { padding: 10px 8px 4px; }
.so-section + .so-section { border-top: 1px solid var(--border); }

.so-section-header {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  padding: 4px 10px 8px;
}
.so-section-title {
  font-size: 10px;
  font-weight: 700;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--fg-3);
}
.so-section-count {
  font-size: 11px;
  font-family: var(--font-mono);
  color: var(--fg-4);
}

.so-result {
  width: 100%;
  min-height: 44px;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 10px;
  border-radius: var(--r-sm);
  background: transparent;
  border: 0;
  text-align: left;
  color: var(--fg-0);
}
.so-result:active { background: rgba(255,255,255,0.06); }

.so-result-thumb {
  width: 38px;
  height: 56px;
  flex-shrink: 0;
  background: var(--bg-3);
  border-radius: var(--r-xs);
  overflow: hidden;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-4);
}
.so-result-thumb.square { width: 40px; height: 40px; }
.so-result-thumb.circle { width: 40px; height: 40px; border-radius: 50%; }
.so-result-thumb img { width: 100%; height: 100%; object-fit: cover; display: block; }

.so-result-body { min-width: 0; flex: 1; }
.so-result-title {
  font-size: 14px;
  font-weight: 500;
  color: var(--fg-0);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.so-result-sub {
  font-size: 12px;
  color: var(--fg-3);
  font-family: var(--font-mono);
  margin-top: 2px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.so-see-all {
  width: 100%;
  min-height: 48px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 12px 18px;
  margin-top: 4px;
  border-top: 1px solid var(--border);
  font-size: 13px;
  font-weight: 500;
  color: var(--fg-1);
  background: rgba(255,255,255,0.02);
}
.so-see-all:active { color: var(--gold); }

.so-fade-enter-active { transition: opacity 0.18s ease; }
.so-fade-leave-active { transition: opacity 0.14s ease; }
.so-fade-enter-from,
.so-fade-leave-to { opacity: 0; }
</style>
