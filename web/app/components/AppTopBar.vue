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
      <div class="search-wrap open">
        <Icon name="search" :size="16" />
        <input
          v-model="searchVal"
          placeholder="Search titles, artists, books…"
          @keydown.enter="doSearch"
          @keydown.escape="searchVal = ''"
        />
        <button v-if="searchVal" class="search-close" @click="searchVal = ''">
          <Icon name="close" :size="14" />
        </button>
      </div>
      <button class="btn-icon" title="Cast"><Icon name="cast" :size="18" /></button>

      <!-- Activity indicator -->
      <div class="activity-wrap" ref="activityRef">
        <button class="btn-icon" title="Activity" @click="activityOpen = !activityOpen">
          <Icon name="bell" :size="18" />
          <span v-if="hasActivity" class="activity-dot" />
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

            <div v-if="activeScans.length" class="activity-section">
              <div class="activity-section-title">Scanning</div>
              <div v-for="scan in activeScans" :key="scan.library_id" class="activity-item">
                <div class="activity-item-icon scan">
                  <Icon name="folder" :size="12" />
                </div>
                <div class="activity-item-text">
                  <span class="activity-item-name">{{ scan.library_name || `Library ${scan.library_id}` }}</span>
                  <span v-if="scan.discovered" class="activity-item-detail">{{ scan.discovered }} files found</span>
                </div>
              </div>
            </div>

            <div v-if="jobsByKind.length" class="activity-section">
              <div class="activity-section-title">Running jobs</div>
              <div v-for="grp in jobsByKind" :key="grp.kind" class="activity-item">
                <div class="activity-item-icon job">
                  <Icon :name="jobIcon(grp.kind)" :size="12" />
                </div>
                <div class="activity-item-text">
                  <span class="activity-item-name">{{ jobLabel(grp.kind) }}</span>
                </div>
                <span class="activity-count-badge">×{{ grp.count }}</span>
              </div>
            </div>

            <div v-if="queueStatus.pending > 0" class="activity-section">
              <div class="activity-section-title">Pending</div>
              <div class="activity-item">
                <div class="activity-item-icon queue">
                  <Icon name="clock" :size="12" />
                </div>
                <div class="activity-item-text">
                  <span class="activity-item-name">{{ queueStatus.pending }} job{{ queueStatus.pending === 1 ? '' : 's' }} waiting</span>
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
const { connected: wsConnected, activeScans, activeJobs, queueStatus } = useEventBus()

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

const searchOpen = ref(false)
const searchVal = ref('')
const searchInput = ref<HTMLInputElement>()
const activityOpen = ref(false)
const activityRef = ref<HTMLElement>()

const hasActivity = computed(() =>
  activeScans.value.length > 0 || activeJobs.value.length > 0 || queueStatus.value.pending > 0
)

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

function openSearch() {
  searchOpen.value = true
  nextTick(() => searchInput.value?.focus())
}

function closeSearch() {
  searchOpen.value = false
  searchVal.value = ''
}

function doSearch() {
  if (searchVal.value.trim()) {
    navigateTo(`/search?q=${encodeURIComponent(searchVal.value)}`)
    closeSearch()
  }
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
}
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
.search-wrap input { background: transparent; border: 0; outline: 0; color: var(--fg-0); font-size: 13px; flex: 1; padding: 0; }
.search-wrap input::placeholder { color: var(--fg-3); }
.search-close { color: var(--fg-3); }
.search-close:hover { color: var(--fg-0); }
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

/* Activity indicator */
.activity-wrap { position: relative; }

.activity-dot {
  position: absolute;
  top: 6px; right: 6px;
  width: 7px; height: 7px;
  border-radius: 50%;
  background: var(--gold);
  border: 2px solid rgba(7, 7, 10, 0.85);
  animation: pulse-activity 2s ease-in-out infinite;
}

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

.activity-item-icon.scan { background: var(--gold-soft); color: var(--gold); }
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
  padding: 10px 16px;
  border-top: 1px solid var(--border);
}

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
