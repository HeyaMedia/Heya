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
      </div>
      <AppTooltip label="Cast">
        <button class="btn-icon"><Icon name="cast" :size="18" /></button>
      </AppTooltip>

      <!-- Activity indicator -->
      <AppMenu
        v-model="activityOpen"
        :width="320"
        trigger-class="btn-icon activity-btn"
        trigger-title="Activity"
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

            <div v-if="runningTasks.length" class="activity-section">
              <div class="activity-section-title">Tasks</div>
              <div v-for="tp in runningTasks" :key="tp.task_id" class="task-card">
                <div class="task-card-header">
                  <div class="task-card-icon">
                    <Icon :name="TASK_LABELS[tp.task_id]?.icon ?? 'timer'" :size="13" />
                  </div>
                  <span class="task-card-title">{{ taskTitle(tp) }}</span>
                  <span class="task-card-counts mono">
                    <template v-if="(tp.running ?? 0) > 0">{{ tp.running }} running</template>
                    <template v-if="(tp.running ?? 0) > 0 && (tp.pending ?? 0) > 0"> · </template>
                    <template v-if="(tp.pending ?? 0) > 0">{{ tp.pending }} pending</template>
                  </span>
                </div>
                <div v-if="tp.current_item" class="task-card-line task-card-item">
                  {{ tp.current_item }}
                </div>
                <div v-if="tp.current_stage" class="task-card-line task-card-stage">
                  <Icon name="chevright" :size="9" /> {{ tp.current_stage }}
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
  </header>
</template>

<script setup lang="ts">
import type { TaskProgressPayload } from '~/composables/useEventBus'

const route = useRoute()
const { user } = useAuth()
const { connected: wsConnected, activeScans, activeJobs, queueStatus, scanProgress, taskProgress } = useEventBus()

// Dev-only Query Cache toggle (left of search). Shares open-state with
// components/dev/QueryCachePanel.vue via this useState key.
const isDev = import.meta.dev
const devQueryOpen = useState('dev_query_panel', () => false)

const progressLibs = computed(() => Object.values(scanProgress.value))

const KIND_LABELS: Record<string, { label: string, icon: string }> = {
  // Scanner pipeline
  kickoff_library_scan:    { label: 'Library scan kickoff',  icon: 'folder' },
  process_file:            { label: 'Processing file',       icon: 'list' },
  ffprobe:                 { label: 'Probing media',         icon: 'eq' },
  detect_local_assets:     { label: 'Detecting sidecars',    icon: 'list' },
  metadata_match:          { label: 'Matching metadata',     icon: 'database' },
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

// The search dropdown is teleported to <body> because `.topbar` has its own
// `backdrop-filter` — a child element's backdrop-filter rendered inside that
// stacking context composites weirdly (the child looks 30% opaque even when
// its background is 92% solid). Living outside .topbar fixes the optics and
// also gives the dropdown the same paint stream as the reka-portaled
// activity/user menus. Position tracks the search-wrap via VueUse's
// useElementBounding + useWindowSize so resizes stay anchored.
const { bottom: swBottom, right: swRight } = useElementBounding(searchWrapRef)
const { width: vw } = useWindowSize()
const searchDropdownTop = computed(() => swBottom.value + 8)
const searchDropdownRight = computed(() => vw.value - swRight.value)

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
      path = `/music/artist/${item.slug || slugify(item.title)}`
      break
    case 'books':
      path = `/books/${item.slug || slugify(item.title)}`
      break
    case 'people':
      path = `/person/${item.slug || slugify(item.name)}`
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

const runningTasks = computed(() => Object.values(taskProgress.value))

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
  analyze_music_facets: { label: 'Sonic Analysis',   icon: 'eq' },
  // Synthetic buckets.
  transcoding:          { label: 'Transcoding',      icon: 'film' },
  artwork:              { label: 'Artwork',          icon: 'image' },
  nfo_writes:           { label: 'NFO Writes',       icon: 'clipboard' },
  external_lookups:     { label: 'External Lookups', icon: 'cloud-download' },
  refresh_actions:      { label: 'Library Refresh',  icon: 'refresh' },
  cleanup:              { label: 'Cleanup',          icon: 'trash' },
}

function taskTitle(tp: TaskProgressPayload): string {
  return TASK_LABELS[tp.task_id]?.label ?? tp.task_id
}

const hasActivity = computed(() =>
  activeScans.value.length > 0 || activeJobs.value.length > 0 || queueStatus.value.pending > 0 || runningTasks.value.length > 0 || nowPlayingSessions.value.length > 0
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
  scan_libraries:       ['kickoff_library_scan', 'process_file', 'ffprobe', 'detect_local_assets', 'metadata_match'],
  refresh_stale_items:  ['kickoff_refresh_stale', 'enrich_media_item'],
  scan_music_loudness:  ['kickoff_music_loudness', 'scan_track_loudness', 'scan_album_loudness'],
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
  const covered = new Set<string>()
  for (const tp of runningTasks.value) {
    for (const k of TASK_KINDS_BY_TASK[tp.task_id] ?? []) covered.add(k)
  }
  return covered
})

const jobsByKind = computed(() => {
  const counts: Record<string, number> = {}
  for (const j of activeJobs.value) {
    if (coveredKinds.value.has(j.kind)) continue
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
.topbar-brand { display: flex; align-items: center; gap: 10px; cursor: pointer; text-decoration: none; justify-self: start; }
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
</style>
