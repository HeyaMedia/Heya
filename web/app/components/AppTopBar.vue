<template>
  <header class="topbar">
    <!-- topbar-left wraps the burger + brand as a flex row on phone (<=720px)
         and in the compact band (720.02-1200px) — see
         useSectionSidebar()/useViewport() below. The burger is a SIBLING of
         the brand anchor, never nested inside it (the anchor is a link;
         nesting a button in it would fire both on tap). At >1200px desktop the
         burger never renders (persistent sidebars are visible there), so
         `.topbar-left` holds a single child and lays out identically to the
         old bare `.topbar-brand` grid item. Phone shows it too so the section
         nav (movies/tv/books Library, music Browse) is always one tap away in
         the same spot as tablet — the per-page Browse/Library buttons are
         retired in favour of this single standardized trigger. -->
    <div class="topbar-left">
      <button
        v-if="sidebar.kind.value && (isCompact || isPhone)"
        type="button"
        class="btn-icon topbar-burger-btn"
        aria-label="Toggle navigation"
        @click="sidebar.toggle()"
      >
        <Icon name="menu" :size="18" />
      </button>
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
    </div>

    <nav class="topbar-tabs">
      <NuxtLink
        v-for="t in tabs"
        :key="t.to"
        :to="t.to"
        class="tab"
        :class="{ active: isActive(t) }"
        :title="t.label"
        :aria-label="t.label"
      >
        <Icon :name="t.icon" :size="16" />
        <span>{{ t.label }}</span>
      </NuxtLink>

    </nav>

    <div class="topbar-right">
      <button
        v-if="isDev"
        class="btn-icon qcp-nav-btn"
        :class="{ active: devQueryOpen }"
        title="Query cache (⌘⇧Q)"
        @click="devQueryOpen = !devQueryOpen"
      >
        <Icon name="database" :size="16" />
      </button>
      <div class="search-wrap open" ref="searchWrapRef">
        <!-- Phone (<=720px) and the compact band (720.02-1200px, see
             useViewport().isCompact): the dropdown below is cramped at
             these widths, so the field becomes a non-editable trigger that
             opens AppSearchOverlay (fullscreen) instead — it doesn't mount
             the input/dropdown/Teleport at all. Desktop (>1200px) keeps the
             original inline behavior untouched. -->
        <button
          v-if="isPhone || isCompact"
          type="button"
          class="search-trigger"
          @click="searchOverlayOpen = true"
        >
          <Icon name="search" :size="16" />
          <span class="search-trigger-label">Search</span>
        </button>
        <template v-else>
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

          <Teleport to="body">
          <Transition name="dropdown">
            <div
              v-if="showDropdown"
              ref="searchDropdownRef"
              class="search-dropdown surface"
              :style="{ top: searchDropdownTop + 'px', right: searchDropdownRight + 'px' }"
              @mousedown.prevent
            >
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
                      <NuxtImg v-if="thumbUrl(section.key, item)" :src="thumbUrl(section.key, item)!" :width="80" :quality="80" loading="lazy" />
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
          </Teleport>
        </template>
      </div>
      <AppTooltip label="Cast">
        <button class="btn-icon topbar-cast-btn"><Icon name="cast" :size="18" /></button>
      </AppTooltip>

      <!-- Activity indicator -->
      <AppMenu
        v-model="activityOpen"
        :width="320"
        trigger-class="btn-icon activity-btn"
        trigger-title="Activity"
        content-class="activity-panel"
      >
        <template #trigger>
          <svg v-if="hasActivity" class="activity-ring" viewBox="0 0 36 36" preserveAspectRatio="xMidYMid meet">
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
        </template>
            <div class="activity-header">
              <span class="activity-title">Activity</span>
              <span class="activity-status" :class="{ live: wsConnected }">
                <span class="status-pulse" />
                {{ wsConnected ? 'Live' : 'Offline' }}
              </span>
            </div>

            <div v-if="nowPlayingSessions.length" class="activity-section">
              <div class="activity-section-title">Now Playing</div>
              <div v-for="s in nowPlayingSessions" :key="s.session_id" class="now-playing-card">
                <div class="np-header">
                  <Icon :name="s.paused ? 'pause' : 'play'" :size="11" :class="['np-icon', { paused: s.paused }]" />
                  <span class="np-title">{{ s.media_title || 'Unknown' }}</span>
                </div>
                <div v-if="s.media_subtitle" class="np-subtitle">{{ s.media_subtitle }}</div>
                <div class="np-meta">
                  <span class="np-user">{{ s.username }}</span>
                  <span v-if="transcodeLabel(s)" class="np-sep">·</span>
                  <span v-if="transcodeLabel(s)" class="np-mode">{{ transcodeLabel(s) }}</span>
                  <span v-if="s.video_codec" class="np-sep">·</span>
                  <span v-if="s.video_codec" class="np-codec mono">{{ s.video_codec.toUpperCase() }}{{ s.height ? ` ${s.height}p` : '' }}</span>
                </div>
                <div class="np-progress">
                  <div class="np-progress-bar"><div class="np-progress-fill" :style="{ width: sessionProgressPct(s) + '%' }" /></div>
                  <span class="np-progress-label mono">{{ formatSessionTime(s.position_seconds) }} / {{ formatSessionTime(s.total_seconds) }}</span>
                </div>
              </div>
            </div>

            <div v-if="progressLibs.length" class="activity-section">
              <div class="activity-section-title">Libraries</div>
              <div
                v-for="lp in progressLibs"
                :key="lp.library_id"
                class="activity-item"
                v-memo="[lp.processed, lp.total, lp.matched, lp.detail]"
              >
                <div class="activity-item-icon scan">
                  <svg class="mini-ring" viewBox="0 0 26 26">
                    <circle class="mini-track" cx="13" cy="13" r="10" />
                    <circle class="mini-fill" cx="13" cy="13" r="10"
                      :stroke-dasharray="62.83"
                      :stroke-dashoffset="62.83 - 62.83 * (lp.total > 0 ? lp.processed / lp.total : 0.18)"
                    />
                  </svg>
                  <Icon name="folder" :size="10" class="mini-icon" />
                </div>
                <div class="activity-item-text">
                  <span class="activity-item-name">{{ lp.name }}</span>
                  <span v-if="lp.total > 0" class="activity-item-detail">{{ lp.processed }}/{{ lp.total }} files · {{ lp.matched }} matched</span>
                  <span v-if="lp.detail" class="activity-item-detail scan-event-detail" :title="lp.detail">
                    {{ lp.detail }}
                  </span>
                </div>
                <span v-if="lp.total > 0" class="activity-pct">{{ Math.round(lp.processed / lp.total * 100) }}%</span>
              </div>
            </div>

            <div v-if="runningTasks.length" class="activity-section">
              <div class="activity-section-title">Tasks</div>
              <div
                v-for="tp in runningTasks"
                :key="tp.task_id"
                class="task-card"
                v-memo="[tp.running, tp.pending, tp.item, tp.stage]"
              >
                <div class="task-card-header">
                  <div class="task-card-icon">
                    <Icon :name="tp.icon" :size="13" />
                  </div>
                  <span class="task-card-title">{{ tp.title }}</span>
                  <span class="task-card-counts mono">
                    <template v-if="(tp.running ?? 0) > 0">{{ tp.running }} running</template>
                    <template v-if="(tp.running ?? 0) > 0 && (tp.pending ?? 0) > 0"> · </template>
                    <template v-if="(tp.pending ?? 0) > 0">{{ tp.pending }} pending</template>
                  </span>
                </div>
                <div v-if="tp.item" class="task-card-line task-card-item" :title="tp.item">
                  {{ tp.item }}
                </div>
                <div v-if="tp.stage" class="task-card-line task-card-stage" :title="tp.stage">
                  <Icon name="chevright" :size="9" /> {{ tp.stage }}
                </div>
              </div>
            </div>

            <div v-if="jobsByKind.length" class="activity-section">
              <div class="activity-section-title">Other activity</div>
              <div v-for="grp in jobsByKind" :key="grp.kind" class="activity-item">
                <div class="activity-item-icon job">
                  <Icon :name="jobIcon(grp.kind)" :size="12" />
                </div>
                <div class="activity-item-text">
                  <span class="activity-item-name">{{ jobLabel(grp.kind) }}</span>
                  <span v-if="grp.detail" class="activity-item-detail activity-job-detail" :title="grp.detail">{{ grp.detail }}</span>
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
              <NuxtLink to="/settings/tasks" class="activity-link" @click="activityOpen = false">
                View tasks
                <Icon name="arrow-right" :size="11" />
              </NuxtLink>
              <button v-if="hasActivity" class="activity-cancel" @click="cancelAllJobs">
                Cancel all
              </button>
            </div>
      </AppMenu>

      <UserDropdown />
    </div>

    <!-- The overlay owns a search composable and history listeners, so defer
         mounting the whole subtree until a compact/phone user opens it. -->
    <AppSearchOverlay v-if="searchOverlayOpen" v-model:open="searchOverlayOpen" />
  </header>
</template>

<script setup lang="ts">
import type { ActiveJob } from '~/composables/useEventBus'

const route = useRoute()
const { user } = useAuth()
const {
  connected: wsConnected,
  activeScans,
  activeJobs,
  queueStatus,
  scanProgress,
  scannerEvents,
  taskProgress,
  scanActivityCount,
  taskActivityCount,
} = useEventBus()
// Compact-band (720.02-1200px) burger trigger — see useSectionSidebar.ts.
// `kind` gates whether the current route even has a section sidebar to
// open; the drawer itself is mounted by the section pages, not here.
const sidebar = useSectionSidebar()

// Dev-only Query Cache toggle (left of search). Shares open-state with
// components/dev/QueryCachePanel.vue via this useState key.
const isDev = import.meta.dev
const devQueryOpen = useState('dev_query_panel', () => false)

type ActivityLibraryProgress = {
  library_id: number
  name: string
  total: number
  processed: number
  matched: number
  detail: string
}

const ACTIVITY_ROW_LIMIT = 8

const progressLibs = computed<ActivityLibraryProgress[]>(() => {
  if (!activityOpen.value) return []
  const rows = new Map<number, ActivityLibraryProgress>()
  for (const p of Object.values(scanProgress.value)) {
    const ev = scannerEvents.value[p.library_id]
    rows.set(p.library_id, {
      library_id: p.library_id,
      name: p.name,
      total: p.total,
      processed: p.processed,
      matched: p.matched,
      detail: scanEventDetail(ev),
    })
  }
  for (const ev of Object.values(scannerEvents.value)) {
    if (!rows.has(ev.library_id)) {
      rows.set(ev.library_id, {
        library_id: ev.library_id,
        name: ev.library_name || `Library ${ev.library_id}`,
        total: 0,
        processed: 0,
        matched: 0,
        detail: scanEventDetail(ev),
      })
    }
  }
  return [...rows.values()].slice(0, ACTIVITY_ROW_LIMIT)
})

const KIND_LABELS: Record<string, { label: string, icon: string }> = {
  // Scanner pipeline
  kickoff_library_scan:    { label: 'Library scan kickoff',  icon: 'folder' },
  process_scan:    { label: 'Scanning library',      icon: 'folder' },
  fetch_metadata:  { label: 'Fetching metadata',     icon: 'cloud-download' },
  apply_metadata:      { label: 'Applying metadata',     icon: 'folder' },
  ffprobe:                 { label: 'Probing media',         icon: 'eq' },
  scan_keyframes:          { label: 'Scanning keyframes',    icon: 'pulse' },
  detect_local_assets:     { label: 'Detecting sidecars',    icon: 'list' },
  // Enrich pipeline
  kickoff_refresh_stale:   { label: 'Refresh kickoff',       icon: 'refresh' },
  enrich_media_item:       { label: 'Enriching item',        icon: 'cloud-download' },
  person_fetch:            { label: 'Fetching cast & crew',  icon: 'users' },
  ratings_fetch:           { label: 'Fetching ratings',      icon: 'star' },
  force_refresh_metadata:  { label: 'Force refresh',         icon: 'refresh' },
  fetch_artwork:           { label: 'Fetching artwork',      icon: 'star' },
  // Images
  download_image:          { label: 'Downloading artwork',   icon: 'cloud-download' },
  save_images:             { label: 'Saving images',         icon: 'clipboard' },
  force_refresh_images:    { label: 'Force refresh images',  icon: 'refresh' },
  // NFOs
  save_nfo:                { label: 'Writing NFO',           icon: 'clipboard' },
  save_music_nfo:          { label: 'Writing music NFO',     icon: 'clipboard' },
  // Loudness
  kickoff_music_loudness:  { label: 'Loudness kickoff',      icon: 'eq' },
  scan_track_loudness:     { label: 'Measuring loudness',    icon: 'eq' },
  scan_album_loudness:     { label: 'Album loudness',        icon: 'eq' },
  // Trickplay + thumbnails
  kickoff_trickplay:       { label: 'Trickplay kickoff',     icon: 'film' },
  trickplay_file:          { label: 'Generating trickplay',  icon: 'film' },
  kickoff_thumbnails:      { label: 'Thumbnails kickoff',    icon: 'image' },
  thumbnail_extra:         { label: 'Extracting thumbnail',  icon: 'image' },
  // Sonic analysis
  kickoff_sonic_analysis:  { label: 'Sonic kickoff',         icon: 'eq' },
  analyze_track_facets:    { label: 'Analyzing track',       icon: 'eq' },
  refresh_artist_centroids:{ label: 'Refresh artist centroid', icon: 'users' },
  refresh_album_centroids: { label: 'Refresh album centroid',  icon: 'list' },
  // Misc
  transcode:               { label: 'Transcoding',           icon: 'film' },
  soft_delete:             { label: 'Cleaning up',           icon: 'trash' },
}

function jobLabel(kind: string) {
  return KIND_LABELS[kind]?.label ?? kind
}

function jobIcon(kind: string) {
  return KIND_LABELS[kind]?.icon ?? 'timer'
}

const searchInput = ref<HTMLInputElement>()
const searchWrapRef = ref<HTMLElement>()
const searchDropdownRef = ref<HTMLElement>()
const searchFocused = ref(false)
const search = useQuickSearch(180)
const selectedIdx = ref(-1)
const activityOpen = ref(false)
// Phone-only fullscreen search overlay (AppSearchOverlay.vue) — the input
// pill above becomes a non-editable trigger on phone and just flips this.
const searchOverlayOpen = ref(false)

// The search dropdown is teleported to <body> because `.topbar` has its own
// `backdrop-filter` — a child element's backdrop-filter rendered inside that
// stacking context composites weirdly (the child looks 30% opaque even when
// its background is 92% solid). Living outside .topbar fixes the optics and
// also gives the dropdown the same paint stream as the reka-portaled
// activity/user menus. Position tracks the search-wrap via VueUse's
// useElementBounding + useWindowSize so resizes stay anchored.
const { bottom: swBottom, right: swRight } = useElementBounding(searchWrapRef)
const { width: vw } = useWindowSize()
const { isPhone, isCompact } = useViewport()
const searchDropdownTop = computed(() => swBottom.value + 8)
// On phone the search-wrap flex-grows to fill the topbar (see the phone
// media query below), so its measured right edge sits close to the
// viewport edge — anchoring the dropdown's `right` off that would push its
// (now near-viewport-width) left edge off-screen. Pin it to a flat 8px
// inset instead, matching the `.search-dropdown` phone width override
// (min(460px, 100vw - 16px)) so it's centered with even margins.
const searchDropdownRight = computed(() => isPhone.value ? 8 : vw.value - swRight.value)

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
  for (let i = 0; i < sIdx; i++) {
    const s = sections.value[i]
    if (s) n += s.bucket.items.length
  }
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
      // Land on the album detail page. `slug` is the album slug; falls back
      // to the artist page if either piece is missing rather than 404'ing.
      if (item.artist_slug && item.slug) {
        path = `/music/artist/${item.artist_slug}/${item.slug}`
      } else {
        path = `/music/artist/${item.artist_slug || slugify(item.artist_name)}`
      }
      break
    case 'tracks':
      // No dedicated track page yet — land on the album so the user can scroll
      // and play. Album-slug shipped on the search row in the slug refactor.
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
  closeDropdown()
}

type ActivityTaskRow = {
  task_id: string
  title: string
  icon: string
  pending?: number
  running?: number
  item: string
  stage: string
  kinds: string[]
}

const runningTasks = computed<ActivityTaskRow[]>(() => {
  if (!activityOpen.value) return []
  return Object.values(taskProgress.value).slice(0, ACTIVITY_ROW_LIMIT).map(tp => ({
    task_id: tp.task_id,
    title: TASK_LABELS[tp.task_id]?.label ?? tp.task_id,
    icon: TASK_LABELS[tp.task_id]?.icon ?? 'timer',
    pending: tp.pending,
    running: tp.running,
    item: tp.current_item || '',
    stage: tp.current_stage || (tp.item_kind ? jobLabel(tp.item_kind) : ''),
    kinds: TASK_KINDS_BY_TASK[tp.task_id] ?? [],
  }))
})

const SCANNER_EVENT_LABELS: Record<string, string> = {
  'scan.start': 'Starting',
  'scan.phase.start': 'Starting phase',
  'scan.phase.complete': 'Finished phase',
  'root.enter': 'Scanning folder',
  'root.complete': 'Finished folder',
  'file.classified': 'Reading file',
  'parse.result': 'Parsed',
  'match.search': 'Searching',
  'match.selected': 'Matched',
  'match.rejected': 'Needs review',
  'match.search_summary': 'Search summary',
  'metadata.fetch': 'Fetching metadata',
  'metadata.preview': 'Fetched metadata',
  'metadata.preview_summary': 'Metadata summary',
  'materialize.preview': 'Planning apply',
  'materialize.preview_summary': 'Apply plan ready',
  'materialize.apply': 'Applying',
  'materialize.apply_summary': 'Apply summary',
  'scan.summary': 'Scan summary',
  'scan.persisted': 'Saved scan result',
}

function scanEventDetail(ev?: ScannerEventPayload): string {
  if (!ev) return ''
  if (ev.detail) return ev.detail
  const data = ev.data ?? {}
  const action = SCANNER_EVENT_LABELS[ev.event] ?? ev.event.replaceAll('.', ' ')
  const target = ev.rel_path || ev.root || ev.path || String(data.title || data.artist || data.album || data.key || '')
  return [ev.phase, action, target].filter(Boolean).join(' · ')
}

// Live playback sessions — populated by the WS push from
// session.update events (see useActiveSessions). Shown in the activity
// panel so you can see who's watching what across all clients.
const { sessions: nowPlayingSessions, formatTime: formatSessionTime, progressPct: sessionProgressPct, transcodeLabel } = useActiveSessions()

// Task titles for the Activity panel cards. Single source of truth —
// shown as the bold header for each running task; the work item goes
// below as a separate line. Covers the 6 scheduled tasks plus the 6
// synthetic buckets (transcoding, artwork, nfo_writes, …) that group
// ad-hoc workers so they show up as labelled cards instead of bare
// counts.
const TASK_LABELS: Record<string, { label: string, icon: string }> = {
  // Scheduled tasks.
  generate_trickplay:   { label: 'Trickplay',        icon: 'film' },
  generate_thumbnails:  { label: 'Thumbnails',       icon: 'image' },
  scan_libraries:       { label: 'Library Scan',     icon: 'folder' },
  refresh_stale_items:  { label: 'Metadata Refresh', icon: 'refresh' },
  scan_music_loudness:  { label: 'Loudness Scan',    icon: 'eq' },
  scan_music_fingerprint: { label: 'Fingerprint Scan', icon: 'eq' },
  analyze_music_facets: { label: 'Sonic Analysis',   icon: 'eq' },
  // Synthetic buckets.
  transcoding:          { label: 'Transcoding',      icon: 'film' },
  artwork:              { label: 'Artwork',          icon: 'image' },
  nfo_writes:           { label: 'NFO Writes',       icon: 'clipboard' },
  external_lookups:     { label: 'External Lookups', icon: 'cloud-download' },
  refresh_actions:      { label: 'Library Refresh',  icon: 'refresh' },
  cleanup:              { label: 'Cleanup',          icon: 'trash' },
}

const hasActivity = computed(() =>
  activeScans.value.length > 0 || scanActivityCount.value > 0 || activeJobs.value.length > 0 || queueStatus.value.pending > 0 || taskActivityCount.value > 0 || nowPlayingSessions.value.length > 0
)

async function cancelAllJobs() {
  try {
    const { $heya } = useNuxtApp()
    await $heya('/api/libraries/scan/cancel-all', { method: 'POST' })
  } catch {}
}

// Kinds covered by a running task card — they appear in the Tasks
// section with their own labels and counts, so listing them again
// under "Other activity" would be noisy double-display. Mirrors the
// backend's worker.TaskKinds (scheduled + synthetic).
const TASK_KINDS_BY_TASK: Record<string, string[]> = {
  // Scheduled.
  scan_libraries:       ['kickoff_library_scan', 'process_scan', 'fetch_metadata', 'apply_metadata', 'ffprobe', 'scan_keyframes', 'detect_local_assets', 'enrich_media_item', 'scan_track_fingerprint', 'scan_track_loudness', 'scan_album_loudness', 'analyze_track_facets', 'refresh_artist_centroids', 'refresh_album_centroids'],
  refresh_stale_items:  ['kickoff_refresh_stale', 'enrich_media_item'],
  scan_music_loudness:  ['kickoff_music_loudness', 'scan_track_loudness', 'scan_album_loudness'],
  scan_music_fingerprint: ['kickoff_music_fingerprint', 'scan_track_fingerprint'],
  generate_trickplay:   ['kickoff_trickplay', 'trickplay_file'],
  generate_thumbnails:  ['kickoff_thumbnails', 'thumbnail_extra'],
  analyze_music_facets: ['kickoff_sonic_analysis', 'analyze_track_facets', 'refresh_artist_centroids', 'refresh_album_centroids'],
  // Synthetic.
  transcoding:          ['transcode'],
  artwork:              ['download_image', 'fetch_artwork', 'save_images'],
  nfo_writes:           ['save_nfo', 'save_music_nfo'],
  external_lookups:     ['person_fetch', 'ratings_fetch'],
  refresh_actions:      ['force_refresh_metadata', 'force_refresh_images'],
  cleanup:              ['soft_delete'],
}

const coveredKinds = computed(() => {
  if (!activityOpen.value) return new Set<string>()
  const covered = new Set<string>()
  for (const tp of runningTasks.value) {
    for (const k of tp.kinds) covered.add(k)
  }
  return covered
})

const jobsByKind = computed(() => {
  if (!activityOpen.value) return []
  const groups = new Map<string, { kind: string, count: number, sample?: ActiveJob }>()
  for (const j of activeJobs.value) {
    if (coveredKinds.value.has(j.kind)) continue
    const group = groups.get(j.kind)
    if (group) {
      group.count++
      if (!group.sample || (!jobDetail(group.sample) && jobDetail(j))) group.sample = j
    } else {
      groups.set(j.kind, { kind: j.kind, count: 1, sample: j })
    }
  }
  return [...groups.values()]
    .map(grp => ({ ...grp, detail: grp.sample ? jobDetail(grp.sample) : '' }))
    .sort((a, b) => b.count - a.count)
    .slice(0, ACTIVITY_ROW_LIMIT)
})

function jobDetail(job: ActiveJob): string {
  const args = parseJobArgs(job.args)
  if (!args) return ''

  const scopes = asStringArray(args.scope_paths)
  if (scopes.length > 0) {
    const first = pathLeaf(scopes[0] ?? '')
    return scopes.length === 1 ? first : `${first} +${scopes.length - 1}`
  }

  for (const key of ['title', 'name', 'current_item', 'profile']) {
    const value = args[key]
    if (typeof value === 'string' && value.trim()) return value.trim()
  }
  for (const key of ['file_path', 'path', 'source_path', 'target_path']) {
    const value = args[key]
    if (typeof value === 'string' && value.trim()) return pathLeaf(value)
  }
  for (const key of ['media_item_id', 'library_file_id', 'track_file_id', 'track_id', 'album_id', 'artist_id', 'library_id']) {
    const value = args[key]
    if (typeof value === 'number' || typeof value === 'string') {
      const label = key.replaceAll('_', ' ')
      return `${label} ${value}`
    }
  }
  return ''
}

function parseJobArgs(args?: string): Record<string, any> | null {
  if (!args) return null
  try {
    const parsed = JSON.parse(args)
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : null
  } catch {
    return null
  }
}

function asStringArray(value: any): string[] {
  return Array.isArray(value) ? value.filter((v): v is string => typeof v === 'string' && v.trim().length > 0) : []
}

function pathLeaf(path: string): string {
  const trimmed = path.trim().replace(/[\\/]+$/, '')
  if (!trimmed) return ''
  return trimmed.split(/[\\/]/).pop() || trimmed
}

// Tab source + active-matching logic live in useNavTabs() — shared with
// BottomNav.vue's phone tab strip so the two never drift apart.
const { tabs, isActive } = useNavTabs()

// Both the search-wrap (trigger) and the teleported dropdown count as
// "inside" — without ignore, clicking a result row would close before the
// row's @click could fire because the row is no longer a DOM descendant of
// the search-wrap.
onClickOutside(searchWrapRef, () => closeDropdown(), { ignore: [searchDropdownRef] })

// Close dropdown on route changes (e.g. after clicking a result).
watch(() => route.fullPath, () => { closeDropdown() })
</script>

<style scoped>
.topbar {
  display: grid;
  /* Equal flexible side columns so the tabs center on the viewport, not on
     the leftover space (the right side — search + buttons — is much wider
     than the brand, which used to skew the tabs left). */
  grid-template-columns: 1fr auto 1fr;
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
/* `.topbar-left` is the actual grid item (column 1) — `.topbar-brand` used
   to hold `justify-self: start` directly, back when it was the grid item
   itself. Moved here so the wrapper shrinks to content instead of
   stretching across the 1fr track; at >1200px and <=720px it holds a single
   child (the brand link) so this is a no-op layout-wise. `display: flex`
   only gets added in the compact media query below, where the burger can
   also be present — see the comment there for why it must stay gated. */
.topbar-left { justify-self: start; min-width: 0; }
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
.topbar-right { display: flex; align-items: center; gap: 10px; justify-self: end; }
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

/* Phone-only trigger (see the `isPhone` branch above) — fills the same
   `.search-wrap.open` pill as the desktop input, just non-editable. */
.search-trigger {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  height: 100%;
  background: transparent;
  border: 0;
  padding: 0;
  color: var(--fg-3);
  font-size: 13px;
  text-align: left;
}
.search-trigger-label { flex: 1; }

/* Search dropdown — teleported to <body> (see useElementBounding wiring in
   script) so we sidestep .topbar's backdrop-filter compositing. Position is
   driven inline via :style.top/.right; the rule below only owns layout +
   the .surface entry animation. */
.search-dropdown {
  position: fixed;
  width: 460px;
  max-height: 70vh;
  overflow-y: auto;
  transform-origin: top right;
  animation: surface-in 0.18s cubic-bezier(0.16, 1, 0.3, 1);
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
/* Activity button — uses the global .btn-icon class for size/hover/active so it
   visually matches the Cast button. The .activity-btn marker only exists to
   pin the spinning ring to the button (see unscoped block below — the button
   element is rendered by AppMenu with a different data-v scope, so
   position:relative + .activity-ring need to live outside `scoped`). */
.ring-arc { transition: stroke-dashoffset 0.3s ease; }
.activity-icon { z-index: 1; }
.activity-icon.active { color: var(--gold); }

@keyframes pulse-activity {
  0%, 100% { box-shadow: 0 0 0 0 rgba(230, 185, 74, 0.4); }
  50% { box-shadow: 0 0 0 4px rgba(230, 185, 74, 0); }
}

/* Activity-dropdown styles moved to the non-scoped block below — see note there. */

/* Dropdown transition */
.dropdown-enter-active { transition: opacity 0.15s ease, transform 0.15s ease; }
.dropdown-leave-active { transition: opacity 0.1s ease, transform 0.1s ease; }
.dropdown-enter-from { opacity: 0; transform: translateY(-4px) scale(0.98); }
.dropdown-leave-to { opacity: 0; transform: translateY(-2px); }

/* Phone (<=720px): BottomNav.vue takes over the tab row, so the topbar
   collapses to brand + search + avatar (Activity is dropped too — see the
   `.activity-btn` rule in the unscoped block below). Desktop rule above is
   untouched — everything mobile-specific is gated behind this query. */
@media (max-width: 720px) {
  .topbar {
    /* Was `1fr auto 1fr` to center .topbar-tabs; with the tabs row hidden
       there's nothing to center, so give the brand a content-sized column
       and hand everything else to topbar-right. */
    grid-template-columns: auto 1fr;
    gap: 12px;
    padding: 0 16px;
  }
  .topbar-tabs { display: none; }
  /* Burger + brand sit in a row (mirrors the compact band below). The burger
     only renders on section routes (`sidebar.kind.value`), so on Home/Settings
     `.topbar-left` still holds just the brand mark and this is a no-op. */
  .topbar-left { display: flex; align-items: center; gap: 6px; }
  /* Drop the wordmark, keep the gold ring mark — reclaims width for the
     search input, which is the thing that actually needs the room. */
  .brand-name { display: none; }
  .topbar-right {
    /* Desktop hugs the right edge (`justify-self: end`) inside an equal-
       width flex column; on phone the column should fill so the flex-grow
       search-wrap below has real space to expand into. */
    justify-self: stretch;
  }
  /* Dev-only query-cache toggle and the (currently inert) cast button
     aren't part of the required phone set (brand/search/avatar) — hide
     them so the search input gets the width instead. */
  .qcp-nav-btn,
  .topbar-cast-btn { display: none; }
  .search-wrap.open {
    flex: 1;
    width: auto;
    min-width: 0;
  }
  /* Teleported to <body> (see script comment above) but still compiled
     with this SFC's scope id since the <Teleport> lives in this template —
     unlike AppMenu/reka-portaled content, this scoped rule does reach it. */
  .search-dropdown {
    width: min(460px, calc(100vw - 16px));
  }
}

/* Compact band (720.02-1200px, see useViewport().isCompact): the persistent
   section sidebars (movies/tv/books index + all of /music) move behind the
   burger in `.topbar-left`, so there's room to keep the tab row visible —
   just icon-only — instead of handing off to BottomNav like phone does.
   Entirely separate from the <=720px query above; nothing here touches
   phone, and nothing outside this query touches >1200px desktop. */
@media (min-width: 720.02px) and (max-width: 1200px) {
  .topbar {
    /* Left-align the whole nav: brand, then the tab row hugging it, with the
       right cluster taking the remainder. `auto auto 1fr` shrinks brand + tabs
       to content and hands the rest to the search cluster — the desktop
       `1fr auto 1fr` centered the tabs, which stranded the left area and boxed
       the search into a narrow strip. This gives the search a big contiguous
       area on the right instead. */
    grid-template-columns: auto auto 1fr;
    gap: 12px;
    padding: 0 16px;
  }
  /* `display: flex` is gated here (not in the always-on `.topbar-left` rule
     above) because it's only needed when the burger sits beside the brand —
     the two need to sit in a row instead of the default block stacking a
     `display:flex` anchor would otherwise fall into. Outside this band the
     burger never renders (see `v-if="sidebar.kind.value && isCompact"`), so
     `.topbar-left` never needs the row treatment there. */
  .topbar-left {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  /* Same reclaim-the-width move as the phone query, just for the narrower
     compact band instead of dropping the tab row entirely. */
  .brand-name { display: none; }
  /* Tabs sit immediately after the brand (left-aligned), not centered. Labels
     stay visible — with the layout left-aligned they no longer compete with a
     centered block for the search's room, so Home/Movies/TV/Music/Books read
     as named buttons just like desktop, only with tighter padding/gap so all
     five labels + brand + search still fit at the narrow end of the band
     (~720px, where a burger also occupies the left). */
  .topbar-tabs { justify-self: start; min-width: 0; }
  .topbar-tabs .tab { padding: 0 10px; gap: 6px; }
  /* Stretch the right cluster across the flexible column so the flex-grow
     search-wrap below actually has room to expand into (mirrors the phone
     query — `justify-self: end` would shrink-wrap it and strand the space). */
  .topbar-right { justify-self: stretch; gap: 8px; min-width: 0; }
  /* Dev-only Query Cache toggle — not part of the compact-band set, and
     crowds the ladder at the narrow end (~744px). Hidden the same way the
     phone query already drops it. */
  .qcp-nav-btn { display: none; }
  /* Fixed 280px on desktop; here the pill flexes to fill the freed space,
     capped generously so it doesn't sprawl on the wide end of the band, and
     floored low so it never forces the row to overflow at the narrow end. */
  .search-wrap.open {
    flex: 1;
    width: auto;
    max-width: 560px;
    min-width: 80px;
  }
}
</style>

<!--
  Activity dropdown is rendered through reka's DropdownMenuPortal (teleported
  to <body>) so its content lives outside this component's scoped CSS context.
  Keep these unscoped so the popover actually picks up a background, padding,
  and the rest of its visual identity.
-->
<style>
/* Surface chrome comes from AppMenu's wrapped AppSurface (the .surface class).
   This file only defines activity-specific inner-layout rules below. */

/* The activity trigger button is rendered inside <AppMenu>, so it gets
   AppMenu's data-v scope id — not this component's. Anything that needs to
   land on the button itself (not its slot contents) has to be unscoped. */
.activity-btn { position: relative; }

/* The panel itself (AppMenu's `content-class="activity-panel"`) is also
   portaled — same reasoning. `:width="320"` on AppMenu sets an inline
   `width: 320px` on the AppSurface element; max-width still clamps the used
   value on top of that regardless of the inline/stylesheet origin mismatch,
   so this keeps the panel from overflowing a 390px phone viewport. */
@media (max-width: 720px) {
  .activity-panel { max-width: calc(100vw - 16px); }
  /* Phone topbar collapses to brand + search + avatar only — Activity's
     panel content (Tasks section) is also reachable via Settings → Tasks,
     so the trigger is dropped rather than squeezed into the narrow bar. */
  .activity-btn { display: none; }
}

.activity-ring {
  position: absolute;
  inset: 0;
  width: 100%; height: 100%;
  animation: spin-ring 1.4s linear infinite;
  pointer-events: none;
}
@keyframes spin-ring { to { transform: rotate(360deg); } }

.activity-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 16px 10px;
  border-bottom: 1px solid var(--border);
}

.activity-title { font-size: 13px; font-weight: 600; }

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

.activity-dropdown .status-pulse {
  width: 5px; height: 5px;
  border-radius: 50%;
  background: var(--fg-4);
}
.activity-status.live .status-pulse {
  background: var(--good);
  animation: pulse-activity 2s ease-in-out infinite;
}
@keyframes pulse-activity {
  0%, 100% { box-shadow: 0 0 0 0 rgba(230, 185, 74, 0.4); }
  50% { box-shadow: 0 0 0 4px rgba(230, 185, 74, 0); }
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

.activity-item { display: flex; align-items: center; gap: 10px; padding: 4px 0; }

/* Task card — used in the Activity dropdown's Tasks section.
   Three lines: header (icon + title + counts), current item, current
   stage. Each task gets its own visual block instead of a one-liner. */
.task-card {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 8px 10px;
  margin: 4px 0;
  background: rgba(255, 255, 255, 0.025);
  border: 1px solid var(--border);
  border-radius: var(--r-xs);
}
.task-card + .task-card { margin-top: 6px; }
.task-card-header {
  display: flex;
  align-items: center;
  gap: 8px;
}
.task-card-icon {
  width: 22px; height: 22px;
  border-radius: var(--r-xs);
  background: var(--gold-soft);
  color: var(--gold);
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}
.task-card-title {
  font-size: 13px;
  font-weight: 600;
  letter-spacing: -0.01em;
  flex: 1;
  min-width: 0;
}
.task-card-counts {
  font-size: 11px;
  color: var(--fg-3);
}
.task-card-line {
  font-size: 12px;
  padding-left: 30px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.task-card-item {
  color: var(--fg-1);
}
.task-card-stage {
  color: var(--fg-3);
  display: flex;
  align-items: center;
  gap: 4px;
  font-family: var(--font-mono);
  font-size: 11px;
}

/* Now Playing — one card per live playback session in the activity panel. */
.now-playing-card {
  padding: 8px 10px;
  background: rgba(255, 196, 50, 0.04);
  border: 1px solid rgba(255, 196, 50, 0.12);
  border-radius: var(--r-sm);
}
.now-playing-card + .now-playing-card { margin-top: 6px; }
.np-header {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 4px;
}
.np-icon { color: var(--gold); }
.np-icon.paused { color: var(--fg-3); }
.np-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--fg-0);
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.np-subtitle {
  font-size: 11px;
  color: var(--fg-2);
  margin-bottom: 4px;
  padding-left: 17px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.np-meta {
  font-size: 10px;
  color: var(--fg-3);
  margin-bottom: 6px;
  padding-left: 17px;
  display: flex;
  align-items: center;
  gap: 4px;
  flex-wrap: wrap;
}
.np-user { color: var(--fg-1); font-weight: 500; }
.np-mode { color: var(--gold); }
.np-codec { color: var(--fg-2); }
.np-sep { color: var(--fg-3); }
.np-progress {
  display: flex;
  align-items: center;
  gap: 6px;
  padding-left: 17px;
}
.np-progress-bar {
  flex: 1;
  height: 3px;
  background: rgba(255, 255, 255, 0.06);
  border-radius: 999px;
  overflow: hidden;
}
.np-progress-fill {
  height: 100%;
  background: var(--gold);
  transition: width 0.3s ease;
}
.np-progress-label {
  font-size: 10px;
  color: var(--fg-3);
  white-space: nowrap;
}

.activity-item-icon {
  width: 26px; height: 26px;
  border-radius: var(--r-xs);
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}
.activity-item-icon.scan { background: var(--gold-soft); color: var(--gold); position: relative; }
.activity-item-icon.queue { background: rgba(140, 160, 255, 0.1); color: rgb(140, 160, 255); }
.activity-item-icon.job { background: rgba(200, 140, 255, 0.1); color: rgb(200, 140, 255); }
.activity-item-icon.task { background: var(--gold-soft); color: var(--gold); position: relative; }

.activity-dropdown .mini-ring {
  position: absolute;
  inset: -1px;
  width: calc(100% + 2px);
  height: calc(100% + 2px);
  transform: rotate(-90deg);
}
.activity-dropdown .mini-track { fill: none; stroke: rgba(255,255,255,0.06); stroke-width: 2.5; }
.activity-dropdown .mini-fill {
  fill: none;
  stroke: var(--gold);
  stroke-width: 2.5;
  stroke-linecap: round;
  transition: stroke-dashoffset 0.4s ease;
}
.activity-dropdown .mini-icon { position: relative; z-index: 1; }

.activity-pct {
  font-size: 10px;
  font-weight: 700;
  font-family: var(--font-mono);
  color: var(--gold);
  flex-shrink: 0;
}

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

.activity-item-text { min-width: 0; flex: 1; }
.activity-item-name { display: block; font-size: 12px; font-weight: 500; color: var(--fg-0); }
.activity-item-detail { display: block; font-size: 10px; color: var(--fg-3); font-family: var(--font-mono); }
.activity-job-detail {
  max-width: 280px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.scan-event-detail {
  max-width: 280px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--gold);
}

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
</style>
