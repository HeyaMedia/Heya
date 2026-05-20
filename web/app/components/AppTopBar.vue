<template>
  <header class="topbar">
    <NuxtLink to="/" class="topbar-brand">
      <div class="brand-mark">
        <svg width="22" height="22" viewBox="0 0 22 22">
          <circle cx="11" cy="11" r="10" fill="none" stroke="var(--gold)" stroke-width="1.5" />
          <circle cx="11" cy="11" r="4" fill="var(--gold)" />
          <circle cx="11" cy="11" r="1.5" fill="#0a0a0a" />
        </svg>
      </div>
      <span class="brand-name">heya<span class="brand-dot">.</span>media</span>
    </NuxtLink>

    <nav class="topbar-tabs">
      <NuxtLink
        v-for="t in tabs"
        :key="t.to"
        :to="t.to"
        class="tab"
        :class="{ active: isActive(t) }"
      >
        <Icon :name="t.icon" :size="16" />
        <span>{{ t.label }}</span>
      </NuxtLink>

    </nav>

    <div class="topbar-right">
      <div class="search-wrap open" ref="searchWrapRef">
        <Icon name="search" :size="16" />
        <input
          ref="searchInput"
          v-model="search.query.value"
          placeholder="Search titles, artists, people…"
          @keydown.enter.prevent="onEnter"
          @keydown.escape.prevent="closeDropdown"
          @keydown.down.prevent="moveSelection(1)"
          @keydown.up.prevent="moveSelection(-1)"
          @focus="searchFocused = true"
        />
        <button v-if="search.query.value" class="search-close" @click="search.reset(); searchFocused = false">
          <Icon name="close" :size="14" />
        </button>

        <Transition name="dropdown">
          <div v-if="showDropdown" class="search-dropdown" @mousedown.prevent>
            <div v-if="search.loading.value && !search.data.value" class="search-loading">
              <span class="search-spinner" /> Searching…
            </div>

            <div v-else-if="search.data.value && sections.length === 0" class="search-empty">
              No results for <strong>{{ search.data.value.query }}</strong>
            </div>

            <div v-else>
              <div v-for="(section, sIdx) in sections" :key="section.key" class="search-section">
                <div class="search-section-header">
                  <span class="search-section-title">{{ section.label }}</span>
                  <span class="search-section-count">{{ section.bucket.total.toLocaleString() }}</span>
                </div>
                <button
                  v-for="(item, iIdx) in section.bucket.items"
                  :key="section.key + ':' + item.id"
                  class="search-result"
                  :class="{ active: flatIndex(sIdx, iIdx) === selectedIdx }"
                  @click="goToResult(section.key, item)"
                  @mouseenter="selectedIdx = flatIndex(sIdx, iIdx)"
                >
                  <div class="search-result-thumb" :class="section.thumbShape">
                    <img v-if="thumbUrl(section.key, item)" :src="thumbUrl(section.key, item)!" loading="lazy" />
                    <Icon v-else :name="section.icon" :size="14" />
                  </div>
                  <div class="search-result-body">
                    <div class="search-result-title">{{ resultTitle(section.key, item) }}</div>
                    <div v-if="resultSub(section.key, item)" class="search-result-sub">
                      {{ resultSub(section.key, item) }}
                    </div>
                  </div>
                  <span v-if="section.badge" class="search-result-badge">{{ section.badge }}</span>
                </button>
                <NuxtLink
                  v-if="section.bucket.total > section.bucket.items.length"
                  :to="`/search?q=${encodeURIComponent(search.query.value)}&type=${section.key}`"
                  class="search-section-more"
                  @click="closeDropdown"
                >
                  View all {{ section.bucket.total }} {{ section.label.toLowerCase() }}
                  <Icon name="arrow-right" :size="11" />
                </NuxtLink>
              </div>

              <NuxtLink
                :to="`/search?q=${encodeURIComponent(search.query.value)}`"
                class="search-footer"
                @click="closeDropdown"
              >
                See all results for "{{ search.query.value }}"
                <Icon name="arrow-right" :size="12" />
              </NuxtLink>
            </div>
          </div>
        </Transition>
      </div>
      <button class="btn-icon" title="Cast"><Icon name="cast" :size="18" /></button>

      <!-- Activity indicator -->
      <div class="activity-wrap" ref="activityRef">
        <button class="activity-btn" title="Activity" @click="activityOpen = !activityOpen">
          <svg v-if="hasActivity" class="activity-ring" viewBox="0 0 36 36">
            <circle cx="18" cy="18" r="16" fill="none" stroke="rgba(255,255,255,0.06)" stroke-width="2" />
            <circle
              class="ring-arc"
              cx="18" cy="18" r="16"
              fill="none"
              stroke="var(--gold)"
              stroke-width="2.5"
              stroke-linecap="round"
              stroke-dasharray="100.53"
              stroke-dashoffset="70"
            />
          </svg>
          <Icon name="pulse" :size="15" class="activity-icon" :class="{ active: hasActivity }" />
        </button>
        <Transition name="dropdown">
          <div v-if="activityOpen" class="activity-dropdown">
            <div class="activity-header">
              <span class="activity-title">Activity</span>
              <span class="activity-status" :class="{ live: wsConnected }">
                <span class="status-pulse" />
                {{ wsConnected ? 'Live' : 'Offline' }}
              </span>
            </div>

            <div v-if="progressLibs.length" class="activity-section">
              <div class="activity-section-title">Libraries</div>
              <div v-for="lp in progressLibs" :key="lp.library_id" class="activity-item">
                <div class="activity-item-icon scan">
                  <svg class="mini-ring" viewBox="0 0 26 26">
                    <circle class="mini-track" cx="13" cy="13" r="10" />
                    <circle class="mini-fill" cx="13" cy="13" r="10"
                      :stroke-dasharray="62.83"
                      :stroke-dashoffset="62.83 - 62.83 * (lp.total > 0 ? lp.processed / lp.total : 0)"
                    />
                  </svg>
                  <Icon name="folder" :size="10" class="mini-icon" />
                </div>
                <div class="activity-item-text">
                  <span class="activity-item-name">{{ lp.name }}</span>
                  <span class="activity-item-detail">{{ lp.processed }}/{{ lp.total }} files · {{ lp.matched }} matched</span>
                </div>
                <span class="activity-pct">{{ lp.total > 0 ? Math.round(lp.processed / lp.total * 100) : 0 }}%</span>
              </div>
            </div>

            <div v-if="jobsByKind.length" class="activity-section">
              <div class="activity-section-title">Running</div>
              <div v-for="grp in jobsByKind" :key="grp.kind" class="activity-item">
                <div class="activity-item-icon job">
                  <Icon :name="jobIcon(grp.kind)" :size="12" />
                </div>
                <div class="activity-item-text">
                  <span class="activity-item-name">{{ jobLabel(grp.kind) }}</span>
                </div>
                <span class="activity-count-badge">{{ grp.count }}</span>
              </div>
            </div>

            <div v-if="queueStatus.pending > 0" class="activity-section">
              <div class="activity-section-title">Queued</div>
              <div class="activity-item">
                <div class="activity-item-icon queue">
                  <Icon name="clock" :size="12" />
                </div>
                <div class="activity-item-text">
                  <span class="activity-item-name">{{ queueStatus.pending.toLocaleString() }} pending</span>
                </div>
              </div>
            </div>

            <div v-if="!hasActivity" class="activity-empty">
              <Icon name="check" :size="14" />
              All clear
            </div>

            <div class="activity-footer">
              <NuxtLink to="/settings/jobs" class="activity-link" @click="activityOpen = false">
                View all jobs
                <Icon name="arrow-right" :size="11" />
              </NuxtLink>
              <button v-if="hasActivity" class="activity-cancel" @click="cancelAllJobs">
                Cancel all
              </button>
            </div>
          </div>
        </Transition>
      </div>

      <NuxtLink to="/settings" class="btn-icon" title="Settings"><Icon name="settings" :size="18" /></NuxtLink>
      <div v-if="user" class="avatar" :title="user.username">
        <span>{{ user.username.slice(0, 2).toUpperCase() }}</span>
      </div>
    </div>
  </header>
</template>

<script setup lang="ts">
const route = useRoute()
const { user } = useAuth()
const { connected: wsConnected, activeScans, activeJobs, queueStatus, scanProgress } = useEventBus()

const progressLibs = computed(() => Object.values(scanProgress.value))

const KIND_LABELS: Record<string, { label: string, icon: string }> = {
  scan_library:     { label: 'Scanning library',       icon: 'folder' },
  process_file:     { label: 'Processing file',        icon: 'list' },
  ffprobe:          { label: 'Analyzing media',        icon: 'eq' },
  metadata_match:   { label: 'Matching metadata',      icon: 'database' },
  metadata_fetch:   { label: 'Fetching metadata',      icon: 'cloud-download' },
  download_image:   { label: 'Downloading artwork',    icon: 'cloud-download' },
  person_fetch:     { label: 'Fetching cast & crew',   icon: 'users' },
  enrichment:       { label: 'Enriching artwork',      icon: 'star' },
  ratings_fetch:    { label: 'Fetching ratings',       icon: 'star' },
  save_nfo:         { label: 'Writing NFO file',       icon: 'clipboard' },
  save_images:      { label: 'Saving images',          icon: 'clipboard' },
  metadata_refresh: { label: 'Refreshing metadata',    icon: 'refresh' },
  transcode:        { label: 'Transcoding',            icon: 'film' },
  soft_delete:      { label: 'Cleaning up',            icon: 'trash' },
}

function jobLabel(kind: string) {
  return KIND_LABELS[kind]?.label ?? kind
}

function jobIcon(kind: string) {
  return KIND_LABELS[kind]?.icon ?? 'timer'
}

const searchInput = ref<HTMLInputElement>()
const searchWrapRef = ref<HTMLElement>()
const searchFocused = ref(false)
const search = useQuickSearch(180)
const selectedIdx = ref(-1)
const activityOpen = ref(false)
const activityRef = ref<HTMLElement>()

interface Section {
  key: 'movies' | 'tv' | 'music' | 'books' | 'albums' | 'tracks' | 'collections' | 'people'
  label: string
  icon: string
  thumbShape: 'poster' | 'square' | 'circle'
  badge?: string
  bucket: { items: any[], total: number }
}

// Order: titles first (so generic-name searches like "Peter" surface the few
// actual movies/shows above the long-tail of people).
const SECTION_DEFS: Array<Omit<Section, 'bucket'>> = [
  { key: 'movies',      label: 'Movies',      icon: 'film',  thumbShape: 'poster' },
  { key: 'tv',          label: 'TV Shows',    icon: 'tv',    thumbShape: 'poster' },
  { key: 'music',       label: 'Artists',     icon: 'music', thumbShape: 'square' },
  { key: 'albums',      label: 'Albums',      icon: 'music', thumbShape: 'square' },
  { key: 'tracks',      label: 'Tracks',      icon: 'music', thumbShape: 'square' },
  { key: 'books',       label: 'Books',       icon: 'book',  thumbShape: 'poster' },
  { key: 'collections', label: 'Collections', icon: 'film',  thumbShape: 'poster' },
  { key: 'people',      label: 'People',      icon: 'users', thumbShape: 'circle' },
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

const totalItems = computed(() =>
  sections.value.reduce((sum, s) => sum + s.bucket.items.length, 0),
)

const showDropdown = computed(() =>
  searchFocused.value && search.query.value.trim().length > 0,
)

function flatIndex(sIdx: number, iIdx: number) {
  let n = 0
  for (let i = 0; i < sIdx; i++) n += sections.value[i].bucket.items.length
  return n + iIdx
}

function moveSelection(delta: number) {
  const max = totalItems.value
  if (max === 0) return
  if (selectedIdx.value === -1) {
    selectedIdx.value = delta > 0 ? 0 : max - 1
    return
  }
  selectedIdx.value = (selectedIdx.value + delta + max) % max
}

function selectedItem(): { sectionKey: Section['key'], item: any } | null {
  if (selectedIdx.value < 0) return null
  let n = 0
  for (const s of sections.value) {
    if (selectedIdx.value < n + s.bucket.items.length) {
      return { sectionKey: s.key, item: s.bucket.items[selectedIdx.value - n] }
    }
    n += s.bucket.items.length
  }
  return null
}

function onEnter() {
  const sel = selectedItem()
  if (sel) {
    goToResult(sel.sectionKey, sel.item)
    return
  }
  const q = search.query.value.trim()
  if (q) {
    navigateTo(`/search?q=${encodeURIComponent(q)}`)
    closeDropdown()
  }
}

function closeDropdown() {
  searchFocused.value = false
  selectedIdx.value = -1
}

// Reset highlight when results change so the cursor doesn't point at stale rows.
watch(() => search.data.value, () => { selectedIdx.value = -1 })

function thumbUrl(kind: Section['key'], item: any): string | null {
  switch (kind) {
    case 'movies':
    case 'tv':
    case 'music':
    case 'books':
      return `/api/media/${item.id}/image/poster`
    case 'people':
      return personImageUrl(item.id)
    case 'albums':
      return albumCoverUrl(item)
    case 'tracks':
      return item.artist_media_item_id
        ? `/api/media/${item.artist_media_item_id}/image/poster`
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
      path = `/music/${item.slug || slugify(item.title)}`
      break
    case 'books':
      path = `/books/${item.slug || slugify(item.title)}`
      break
    case 'people':
      path = `/person/${item.slug || slugify(item.name)}`
      break
    case 'albums':
      // No dedicated album page yet — jump to artist with an anchor.
      path = `/music/${item.artist_slug || slugify(item.artist_name)}#album-${item.id}`
      break
    case 'tracks':
      path = `/music/${item.artist_slug || slugify(item.artist_name)}#track-${item.id}`
      break
    case 'collections':
      path = `/search?q=${encodeURIComponent(item.name)}&type=collections`
      break
  }
  if (path) navigateTo(path)
  closeDropdown()
}

const hasActivity = computed(() =>
  activeScans.value.length > 0 || activeJobs.value.length > 0 || queueStatus.value.pending > 0
)

async function cancelAllJobs() {
  try { await apiFetch('/api/libraries/scan/cancel-all', { method: 'POST' }) } catch {}
}

const jobsByKind = computed(() => {
  const counts: Record<string, number> = {}
  for (const j of activeJobs.value) {
    counts[j.kind] = (counts[j.kind] ?? 0) + 1
  }
  return Object.entries(counts)
    .map(([kind, count]) => ({ kind, count }))
    .sort((a, b) => b.count - a.count)
})

const tabs = [
  { to: '/', label: 'Home', icon: 'home', match: ['/'] },
  { to: '/movies', label: 'Movies', icon: 'film', match: ['/movies'] },
  { to: '/tv', label: 'TV', icon: 'tv', match: ['/tv'] },
  { to: '/music', label: 'Music', icon: 'music', match: ['/music'] },
  { to: '/books', label: 'Books', icon: 'book', match: ['/books'] },
]

function isActive(t: typeof tabs[0]) {
  if (t.to === '/' && route.path === '/') return true
  if (t.to !== '/' && route.path.startsWith(t.to)) return true
  if (t.to === '/movies' && route.path.startsWith('/media/')) return true
  return false
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
})

function handleClickOutside(e: MouseEvent) {
  if (activityRef.value && !activityRef.value.contains(e.target as Node)) {
    activityOpen.value = false
  }
  if (searchWrapRef.value && !searchWrapRef.value.contains(e.target as Node)) {
    closeDropdown()
  }
}

// Close dropdown on route changes (e.g. after clicking a result).
watch(() => route.fullPath, () => { closeDropdown() })
</script>

<style scoped>
.topbar {
  display: grid;
  grid-template-columns: auto 1fr auto;
  align-items: center;
  gap: 24px;
  padding: 0 24px;
  background: rgba(7, 7, 10, 0.85);
  backdrop-filter: blur(20px);
  -webkit-backdrop-filter: blur(20px);
  border-bottom: 1px solid var(--border);
  height: var(--topbar-h);
  z-index: 50;
  position: relative;
}
.topbar-brand { display: flex; align-items: center; gap: 10px; cursor: pointer; text-decoration: none; }
.brand-mark { display: flex; align-items: center; justify-content: center; }
.brand-name { font-size: 16px; font-weight: 600; letter-spacing: -0.01em; color: var(--fg-0); }
.brand-name .brand-dot { color: var(--gold); }
.topbar-tabs { display: flex; gap: 2px; justify-self: center; }
.topbar-tabs .tab {
  display: inline-flex; align-items: center; gap: 8px;
  padding: 0 16px; height: 36px;
  border-radius: var(--r-md);
  color: var(--fg-2);
  font-size: 13px; font-weight: 500;
  transition: color 0.15s ease, background 0.15s ease;
  text-decoration: none;
}
.topbar-tabs .tab:hover { color: var(--fg-0); background: rgba(255,255,255,0.04); }
.topbar-tabs .tab.active { color: var(--gold); }
.topbar-right { display: flex; align-items: center; gap: 4px; }
.search-wrap { display: flex; align-items: center; gap: 8px; }
.search-wrap.open {
  background: var(--bg-3);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 0 10px 0 12px;
  height: 36px;
  width: 280px;
}
.search-wrap { position: relative; }
.search-wrap input { background: transparent; border: 0; outline: 0; color: var(--fg-0); font-size: 13px; flex: 1; padding: 0; }
.search-wrap input::placeholder { color: var(--fg-3); }
.search-close { color: var(--fg-3); }
.search-close:hover { color: var(--fg-0); }

/* Search dropdown */
.search-dropdown {
  position: absolute;
  top: calc(100% + 8px);
  right: 0;
  width: 460px;
  max-height: 70vh;
  overflow-y: auto;
  background: var(--bg-2);
  border: 1px solid var(--border-strong);
  border-radius: var(--r-lg);
  box-shadow: var(--shadow-3);
  z-index: 100;
}

.search-loading,
.search-empty {
  padding: 18px 16px;
  color: var(--fg-3);
  font-size: 13px;
  display: flex;
  align-items: center;
  gap: 8px;
}
.search-empty strong { color: var(--fg-1); font-weight: 600; }

.search-spinner {
  width: 12px; height: 12px;
  border: 1.5px solid var(--border-strong);
  border-top-color: var(--gold);
  border-radius: 50%;
  animation: spin 0.7s linear infinite;
  display: inline-block;
}
@keyframes spin { to { transform: rotate(360deg); } }

.search-section { padding: 8px 6px; }
.search-section + .search-section { border-top: 1px solid var(--border); }

.search-section-header {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  padding: 4px 10px 6px;
}
.search-section-title {
  font-size: 9px;
  font-weight: 700;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--fg-3);
}
.search-section-count {
  font-size: 10px;
  font-family: var(--font-mono);
  color: var(--fg-4);
}

.search-result {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 10px;
  border-radius: var(--r-sm);
  background: transparent;
  border: 0;
  text-align: left;
  cursor: pointer;
  color: var(--fg-0);
  transition: background 0.1s ease;
}
.search-result:hover,
.search-result.active {
  background: rgba(255,255,255,0.05);
}
.search-result.active {
  outline: 1px solid var(--gold-soft);
}

.search-result-thumb {
  width: 36px;
  height: 54px;
  flex-shrink: 0;
  background: var(--bg-3);
  border-radius: var(--r-xs);
  overflow: hidden;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-4);
}
.search-result-thumb.square { width: 36px; height: 36px; }
.search-result-thumb.circle { width: 36px; height: 36px; border-radius: 50%; }
.search-result-thumb img { width: 100%; height: 100%; object-fit: cover; display: block; }

.search-result-body { min-width: 0; flex: 1; }
.search-result-title {
  font-size: 13px;
  font-weight: 500;
  color: var(--fg-0);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.search-result-sub {
  font-size: 11px;
  color: var(--fg-3);
  font-family: var(--font-mono);
  margin-top: 2px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.search-result-badge {
  font-size: 9px;
  font-family: var(--font-mono);
  color: var(--fg-3);
  letter-spacing: 0.06em;
  text-transform: uppercase;
  flex-shrink: 0;
}

.search-section-more {
  display: flex;
  align-items: center;
  justify-content: flex-start;
  gap: 4px;
  padding: 6px 10px 4px;
  font-size: 11px;
  font-family: var(--font-mono);
  color: var(--fg-3);
  text-decoration: none;
  transition: color 0.12s ease;
}
.search-section-more:hover { color: var(--gold); }

.search-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  border-top: 1px solid var(--border);
  font-size: 12px;
  font-weight: 500;
  color: var(--fg-1);
  text-decoration: none;
  transition: color 0.12s ease;
  background: rgba(255,255,255,0.02);
}
.search-footer:hover { color: var(--gold); }
.avatar {
  width: 32px; height: 32px;
  border-radius: 50%;
  background: linear-gradient(135deg, var(--gold-deep), var(--gold));
  color: #1a1408;
  font-size: 11px; font-weight: 700;
  display: flex; align-items: center; justify-content: center;
  margin-left: 6px;
  cursor: pointer;
  letter-spacing: 0.04em;
}

/* Activity button */
.activity-wrap { position: relative; }

.activity-btn {
  position: relative;
  width: 36px; height: 36px;
  border-radius: var(--r-md);
  display: flex; align-items: center; justify-content: center;
  color: var(--fg-2);
  transition: color 0.15s, background 0.15s;
}
.activity-btn:hover { color: var(--fg-0); background: rgba(255,255,255,0.04); }

.activity-ring {
  position: absolute;
  inset: 0;
  width: 36px; height: 36px;
  animation: spin-ring 1.4s linear infinite;
}
.ring-arc { transition: stroke-dashoffset 0.3s ease; }

.activity-icon { z-index: 1; }
.activity-icon.active { color: var(--gold); }

@keyframes spin-ring { to { transform: rotate(360deg); } }

@keyframes pulse-activity {
  0%, 100% { box-shadow: 0 0 0 0 rgba(230, 185, 74, 0.4); }
  50% { box-shadow: 0 0 0 4px rgba(230, 185, 74, 0); }
}

.activity-dropdown {
  position: absolute;
  top: calc(100% + 8px);
  right: 0;
  width: 300px;
  background: var(--bg-2);
  border: 1px solid var(--border-strong);
  border-radius: var(--r-lg);
  box-shadow: var(--shadow-3);
  overflow: hidden;
  z-index: 100;
}

.activity-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 16px 10px;
  border-bottom: 1px solid var(--border);
}

.activity-title {
  font-size: 13px;
  font-weight: 600;
}

.activity-status {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 10px;
  font-weight: 600;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--fg-3);
}

.activity-status.live { color: var(--good); }

.status-pulse {
  width: 5px; height: 5px;
  border-radius: 50%;
  background: var(--fg-4);
}

.activity-status.live .status-pulse {
  background: var(--good);
  animation: pulse-activity 2s ease-in-out infinite;
}

.activity-section { padding: 10px 16px; }
.activity-section + .activity-section { border-top: 1px solid var(--border); }

.activity-section-title {
  font-size: 9px;
  font-weight: 700;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--fg-3);
  margin-bottom: 8px;
}

.activity-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 4px 0;
}

.activity-item-icon {
  width: 26px; height: 26px;
  border-radius: var(--r-xs);
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}

.activity-item-icon.scan {
  background: var(--gold-soft);
  color: var(--gold);
  position: relative;
}
.mini-ring {
  position: absolute;
  inset: -1px;
  width: calc(100% + 2px);
  height: calc(100% + 2px);
  transform: rotate(-90deg);
}
.mini-track { fill: none; stroke: rgba(255,255,255,0.06); stroke-width: 2.5; }
.mini-fill {
  fill: none;
  stroke: var(--gold);
  stroke-width: 2.5;
  stroke-linecap: round;
  transition: stroke-dashoffset 0.4s ease;
}
.mini-icon { position: relative; z-index: 1; }
.activity-pct {
  font-size: 10px;
  font-weight: 700;
  font-family: var(--font-mono);
  color: var(--gold);
  flex-shrink: 0;
}
.activity-item-icon.queue { background: rgba(140, 160, 255, 0.1); color: rgb(140, 160, 255); }
.activity-item-icon.job { background: rgba(200, 140, 255, 0.1); color: rgb(200, 140, 255); }

.activity-count-badge {
  font-size: 10px;
  font-weight: 700;
  font-family: var(--font-mono);
  color: var(--fg-3);
  background: rgba(255, 255, 255, 0.06);
  padding: 2px 7px;
  border-radius: 100px;
  flex-shrink: 0;
}

.activity-item-text { min-width: 0; }
.activity-item-name { display: block; font-size: 12px; font-weight: 500; color: var(--fg-0); }
.activity-item-detail { display: block; font-size: 10px; color: var(--fg-3); font-family: var(--font-mono); }

.activity-empty {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 16px;
  color: var(--fg-3);
  font-size: 12px;
}

.activity-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 16px;
  border-top: 1px solid var(--border);
}

.activity-cancel {
  font-size: 11px;
  font-weight: 500;
  font-family: var(--font-mono);
  color: var(--bad);
  opacity: 0.7;
  transition: opacity 0.12s;
}
.activity-cancel:hover { opacity: 1; }

.activity-link {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  font-weight: 500;
  color: var(--fg-2);
  text-decoration: none;
  font-family: var(--font-mono);
  transition: color 0.12s ease;
}

.activity-link:hover { color: var(--gold); }

/* Dropdown transition */
.dropdown-enter-active { transition: opacity 0.15s ease, transform 0.15s ease; }
.dropdown-leave-active { transition: opacity 0.1s ease, transform 0.1s ease; }
.dropdown-enter-from { opacity: 0; transform: translateY(-4px) scale(0.98); }
.dropdown-leave-to { opacity: 0; transform: translateY(-2px); }
</style>
