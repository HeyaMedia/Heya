<script setup lang="ts">
definePageMeta({ layout: 'manager', middleware: 'admin' })

import {
  managerLibraryItemsQuery,
  managerQualityProfilesQuery,
  type ManagerLibraryItemView,
} from '~/queries/manager'

const route = useRoute()
const router = useRouter()
const { $heya } = useNuxtApp()

const libraryId = computed(() => Number(route.params.id))

// ── Data: one fetch per library, filters live client-side ────────────────
// The per-domain completeness CTE aggregates the whole library server-side
// no matter the LIMIT, so fetching every row once costs the same as one
// page — and makes search/filter/sort instant afterwards.

const itemsQuery = useQuery(() => managerLibraryItemsQuery({
  libraryId: libraryId.value,
  perPage: 10000,
}))
const data = computed(() => itemsQuery.data.value)
const allItems = computed(() => data.value?.items ?? [])
const stats = computed(() => data.value?.stats)
const library = computed(() => data.value?.library)
const initialLoading = computed(() => itemsQuery.isLoading.value && !data.value)
const refreshing = computed(() => itemsQuery.isLoading.value && !!data.value)

// Another admin, the CLI, or a scan mutating the catalog shows up live.
useLiveRefresh([
  {
    events: ['manager.changed'],
    keys: [['manager', 'library-items']],
    filter: event => (event.payload as { area?: string } | undefined)?.area === 'library',
  },
  {
    events: ['media.added', 'media.updated'],
    keys: [['manager', 'library-items']],
  },
])

// ── Filters / sort — hydrated from the URL so back-navigation restores ───

function queryString(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

const search = ref(queryString(route.query.q))
const monitored = ref(queryString(route.query.monitored))
const fileState = ref(queryString(route.query.state))
const status = ref(queryString(route.query.status))
const profile = ref(queryString(route.query.profile))
const sort = ref(queryString(route.query.sort) || 'title')
const dir = ref<'asc' | 'desc'>(route.query.dir === 'desc' ? 'desc' : 'asc')

watch(libraryId, () => {
  search.value = ''
  monitored.value = ''
  fileState.value = ''
  status.value = ''
  profile.value = ''
  sort.value = 'title'
  dir.value = 'asc'
  selected.value = new Set()
})

// Mirror filter state into the URL (replace, so typing doesn't spam
// history) — the browser's back button then lands on the filtered view, and
// the detail page's breadcrumb reuses the same stored path.
const urlQuery = computed(() => {
  const query: Record<string, string> = {}
  if (search.value) query.q = search.value
  if (monitored.value) query.monitored = monitored.value
  if (fileState.value) query.state = fileState.value
  if (status.value) query.status = status.value
  if (profile.value) query.profile = profile.value
  if (sort.value !== 'title') query.sort = sort.value
  if (dir.value !== 'asc') query.dir = dir.value
  return query
})
let urlTimer: ReturnType<typeof setTimeout> | undefined
watch(urlQuery, query => {
  clearTimeout(urlTimer)
  urlTimer = setTimeout(() => {
    router.replace({ query })
    sessionStorage.setItem(`heya:mgr-lib-return:${libraryId.value}`,
      router.resolve({ path: route.path, query }).fullPath)
  }, 250)
}, { immediate: true })

const filtered = computed(() => {
  const q = search.value.trim().toLowerCase()
  return allItems.value.filter(item => {
    if (q && !item.title.toLowerCase().includes(q)) return false
    if (monitored.value === 'monitored' && !item.monitored) return false
    if (monitored.value === 'unmonitored' && item.monitored) return false
    if (fileState.value === 'missing' && item.missing_count <= 0) return false
    if (fileState.value === 'complete' && (item.missing_count > 0 || item.have_count <= 0)) return false
    if (status.value && item.status !== status.value) return false
    if (profile.value === 'none' && item.quality_profile_id != null) return false
    if (profile.value && profile.value !== 'none' && item.quality_profile_id !== Number(profile.value)) return false
    return true
  })
})

type SortFn = (a: ManagerLibraryItemView, b: ManagerLibraryItemView) => number
const nullsLast = (a: number | null, b: number | null) => {
  if (a == null && b == null) return 0
  if (a == null) return 1
  if (b == null) return -1
  return a - b
}
const SORT_FNS: Record<string, SortFn> = {
  title: (a, b) => a.title.toLowerCase().localeCompare(b.title.toLowerCase()),
  year: (a, b) => nullsLast(Number(a.year) || null, Number(b.year) || null),
  added: (a, b) => a.added_at.localeCompare(b.added_at),
  size: (a, b) => a.size_on_disk - b.size_on_disk,
  missing: (a, b) => a.missing_count - b.missing_count,
  units: (a, b) => a.total_count - b.total_count,
  progress: (a, b) => nullsLast(
    a.total_count > 0 ? a.have_count / a.total_count : null,
    b.total_count > 0 ? b.have_count / b.total_count : null),
  status: (a, b) => (a.status || '￿').localeCompare(b.status || '￿'),
}
const sorted = computed(() => {
  const fn = SORT_FNS[sort.value] ?? SORT_FNS.title!
  const flip = dir.value === 'desc' ? -1 : 1
  const tiebreak = SORT_FNS.title!
  return [...filtered.value].sort((a, b) => (fn(a, b) * flip) || tiebreak(a, b))
})

// ── Quality profiles (filtered to this library's domain) ─────────────────

const profilesData = useQuery(managerQualityProfilesQuery())
const domainProfiles = computed(() =>
  (profilesData.data.value ?? []).filter(p => p.domain === library.value?.domain))
const profileNames = computed(() => {
  const names: Record<number, string> = {}
  for (const p of profilesData.data.value ?? []) names[p.id] = p.name
  return names
})

// ── Domain vocabulary ────────────────────────────────────────────────────

const UNIT_LABELS: Record<string, { unit: string, group: string }> = {
  movie: { unit: 'movies', group: '' },
  tv: { unit: 'episodes', group: 'Seasons' },
  anime: { unit: 'episodes', group: 'Seasons' },
  music: { unit: 'tracks', group: 'Albums' },
  book: { unit: 'books', group: '' },
}
const vocab = computed(() => UNIT_LABELS[library.value?.media_type ?? 'movie'] ?? UNIT_LABELS.movie!)

const STATUS_LABELS: Record<string, string> = {
  returning_series: 'Continuing',
  ended: 'Ended',
  canceled: 'Canceled',
  released: 'Released',
  in_production: 'In production',
  post_production: 'Post production',
  planned: 'Planned',
  rumored: 'Rumored',
}
function statusLabel(value: string): string {
  if (!value) return ''
  return STATUS_LABELS[value] ?? value.replaceAll('_', ' ')
}

const isMusic = computed(() => library.value?.media_type === 'music')
const posterAspect = computed(() => isMusic.value ? '1/1' : '2/3')
// Artists carry no year or release status — dead columns for music.
const showYearStatus = computed(() => !isMusic.value)

// Items open the manager's own detail page — the arr-style lens — not the
// public library page (that stays one click away from the detail hero).
function detailLink(item: ManagerLibraryItemView): string {
  return `/manager/library/${libraryId.value}/${item.id}`
}

function progressPct(item: ManagerLibraryItemView): number {
  if (item.total_count <= 0) return 0
  return Math.min(100, Math.round((item.have_count / item.total_count) * 100))
}
function progressTone(item: ManagerLibraryItemView): 'good' | 'warn' | 'bad' | 'none' {
  if (item.total_count <= 0) return 'none'
  if (item.have_count <= 0) return 'bad'
  if (item.missing_count > 0) return 'warn'
  return 'good'
}

// ── View mode ────────────────────────────────────────────────────────────

const view = ref<'posters' | 'table'>('posters')
onMounted(() => {
  const saved = localStorage.getItem('heya:manager:library-view')
  if (saved === 'posters' || saved === 'table') view.value = saved
})
watch(view, value => localStorage.setItem('heya:manager:library-view', value))

// Table layout: header and virtualized rows share one grid template so the
// columns stay aligned without a real <table>. No overflow-x wrapper around
// the scroller — RecycleScroller's page-mode adopts the nearest ancestor
// with ANY overflow:auto as its scroll parent, and a horizontal wrapper
// freezes it on the first row window. Narrow screens drop columns instead.
const { isPhone } = useViewport()
const tableTemplate = computed(() => {
  if (isPhone.value) return '34px 34px minmax(140px, 1fr) 130px 50px'
  const cols = ['34px', '34px', 'minmax(220px, 2.2fr)']
  if (showYearStatus.value) cols.push('60px', '120px')
  cols.push('minmax(110px, 1fr)')
  if (vocab.value!.group) cols.push('70px')
  cols.push('160px', '70px', '90px', '80px')
  return cols.join(' ')
})

// ── Scroll restore (the manager layout owns the scroll container) ────────

const scrollKey = computed(() => `heya:mgr-lib-scroll:${libraryId.value}`)
const restoredScroll = ref(false)
watch([sorted, view], async () => {
  if (restoredScroll.value || !sorted.value.length) return
  restoredScroll.value = true
  await nextTick()
  const saved = Number(sessionStorage.getItem(scrollKey.value) || 0)
  const el = document.getElementById('main-content')
  if (saved > 0 && el) el.scrollTop = saved
}, { immediate: true })
watch(libraryId, () => { restoredScroll.value = false })
onBeforeUnmount(() => {
  const el = document.getElementById('main-content')
  if (el) sessionStorage.setItem(scrollKey.value, String(el.scrollTop))
})

// ── Selection + bulk edit ────────────────────────────────────────────────

const selected = ref(new Set<number>())
const bulkProfile = ref('')
const bulkBusy = ref(false)
const flash = ref('')
const flashError = ref(false)
let flashTimer: ReturnType<typeof setTimeout> | undefined
function showFlash(message: string, isError = false) {
  flash.value = message
  flashError.value = isError
  clearTimeout(flashTimer)
  flashTimer = setTimeout(() => { flash.value = '' }, 3500)
}

function toggleSelected(id: number) {
  const next = new Set(selected.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  selected.value = next
}
function selectAllFiltered() {
  const next = new Set(selected.value)
  for (const item of sorted.value) next.add(item.id)
  selected.value = next
}
function clearSelection() {
  selected.value = new Set()
}

const queryCache = useQueryCache()
function invalidateItems() {
  queryCache.invalidateQueries({ key: ['manager', 'library-items'] })
}

async function updateMedia(ids: number[], patch: { monitored?: boolean, quality_profile_id?: number }) {
  await $heya('/api/manager/media', { method: 'PUT', body: { ids, ...patch } })
  invalidateItems()
}

async function toggleMonitor(item: ManagerLibraryItemView) {
  const next = !item.monitored
  item.monitored = next // optimistic; the invalidation refetch corrects on failure
  try {
    await updateMedia([item.id], { monitored: next })
  } catch (e: any) {
    item.monitored = !next
    showFlash(e?.data?.detail ?? 'Update failed.', true)
  }
}

async function bulkMonitor(state: boolean) {
  if (!selected.value.size) return
  bulkBusy.value = true
  try {
    await updateMedia([...selected.value], { monitored: state })
    showFlash(`${state ? 'Monitoring' : 'Unmonitored'} ${selected.value.size} item(s)`)
  } catch (e: any) {
    showFlash(e?.data?.detail ?? 'Update failed.', true)
  } finally {
    bulkBusy.value = false
  }
}

async function bulkAssignProfile() {
  if (!selected.value.size || bulkProfile.value === '') return
  bulkBusy.value = true
  try {
    await updateMedia([...selected.value], { quality_profile_id: Number(bulkProfile.value) })
    showFlash(bulkProfile.value === '0'
      ? `Cleared profile on ${selected.value.size} item(s)`
      : `Profile set on ${selected.value.size} item(s)`)
  } catch (e: any) {
    showFlash(e?.data?.detail ?? 'Update failed.', true)
  } finally {
    bulkBusy.value = false
  }
}

// ── Toolbar options ──────────────────────────────────────────────────────

const SORTS = computed(() => [
  { key: 'title', label: 'Title' },
  { key: 'year', label: 'Year' },
  { key: 'added', label: 'Added' },
  { key: 'size', label: 'Size on disk' },
  { key: 'missing', label: 'Missing' },
  { key: 'units', label: vocab.value!.unit.charAt(0).toUpperCase() + vocab.value!.unit.slice(1) },
  { key: 'progress', label: 'Progress' },
  { key: 'status', label: 'Status' },
])

function headerSort(key: string) {
  if (sort.value === key) {
    dir.value = dir.value === 'asc' ? 'desc' : 'asc'
  } else {
    sort.value = key
    dir.value = key === 'title' || key === 'status' ? 'asc' : 'desc'
  }
}
</script>

<template>
  <div>
    <SettingsContextHero
      :title="library?.name ?? 'Library'"
      :icon="managerLibraryIcon(library?.media_type ?? '')"
      eyebrow="Manager · Library"
      description="The managed lens over this library: what's monitored, what's missing, and what's assigned to which quality profile. Same catalog as the server — a different room, not a second database."
    />

    <div v-if="initialLoading" class="mgr-loading">Loading library…</div>

    <template v-else-if="stats">
      <div class="tiles">
        <MetricTile label="Items" :value="stats.items" icon="folder" tone="neutral" :sub="`${stats.files.toLocaleString()} files on disk`" />
        <MetricTile label="Monitored" :value="stats.monitored" icon="eye" :tone="stats.monitored ? 'good' : 'neutral'" :sub="`of ${stats.items} watched for releases`" />
        <MetricTile
          label="Missing" :value="stats.items_missing" icon="target"
          :tone="stats.items_missing ? 'warn' : 'good'"
          :sub="`${Math.max(0, stats.units_total - stats.units_have).toLocaleString()} ${vocab!.unit} wanted`"
        />
        <MetricTile label="On disk" :value="fmtBytes(stats.size_on_disk)" icon="hard-drives" tone="neutral" :sub="`${stats.units_have.toLocaleString()} of ${stats.units_total.toLocaleString()} ${vocab!.unit}`" />
      </div>

      <div class="lib-toolbar">
        <div class="lib-search">
          <Icon name="search" :size="14" />
          <input v-model="search" class="lib-search-input" type="search" :placeholder="`Search ${library?.name ?? 'library'}…`" aria-label="Search titles">
        </div>

        <div class="lib-chips" role="group" aria-label="Monitoring filter">
          <button type="button" class="lib-chip" :class="{ active: monitored === '' }" @click="monitored = ''">All</button>
          <button type="button" class="lib-chip" :class="{ active: monitored === 'monitored' }" @click="monitored = 'monitored'">Monitored</button>
          <button type="button" class="lib-chip" :class="{ active: monitored === 'unmonitored' }" @click="monitored = 'unmonitored'">Unmonitored</button>
        </div>

        <div class="lib-chips" role="group" aria-label="File state filter">
          <button type="button" class="lib-chip" :class="{ active: fileState === '' }" @click="fileState = ''">Any state</button>
          <button type="button" class="lib-chip" :class="{ active: fileState === 'missing' }" @click="fileState = 'missing'">Missing</button>
          <button type="button" class="lib-chip" :class="{ active: fileState === 'complete' }" @click="fileState = 'complete'">Complete</button>
        </div>

        <select v-if="stats.statuses?.length" v-model="status" class="lib-select" aria-label="Status filter">
          <option value="">Any status</option>
          <option v-for="s in stats.statuses ?? []" :key="s" :value="s">{{ statusLabel(s) }}</option>
        </select>

        <select v-model="profile" class="lib-select" aria-label="Profile filter">
          <option value="">Any profile</option>
          <option value="none">Unassigned</option>
          <option v-for="p in domainProfiles" :key="p.id" :value="String(p.id)">{{ p.name }}</option>
        </select>

        <div class="lib-toolbar-right">
          <select v-model="sort" class="lib-select" aria-label="Sort by">
            <option v-for="s in SORTS" :key="s.key" :value="s.key">{{ s.label }}</option>
          </select>
          <button type="button" class="mgr-btn-icon" :aria-label="dir === 'asc' ? 'Ascending — switch to descending' : 'Descending — switch to ascending'" @click="dir = dir === 'asc' ? 'desc' : 'asc'">
            <Icon name="sort" :size="15" :style="dir === 'desc' ? 'transform: scaleY(-1)' : ''" />
          </button>
          <div class="lib-view-toggle" role="group" aria-label="View mode">
            <button type="button" class="mgr-btn-icon" :class="{ active: view === 'posters' }" aria-label="Poster view" @click="view = 'posters'">
              <Icon name="grid" :size="15" />
            </button>
            <button type="button" class="mgr-btn-icon" :class="{ active: view === 'table' }" aria-label="Table view" @click="view = 'table'">
              <Icon name="rows" :size="15" />
            </button>
          </div>
          <button type="button" class="mgr-btn lib-select-page" @click="selectAllFiltered">Select all</button>
        </div>
      </div>

      <div class="lib-count mono">
        {{ sorted.length.toLocaleString() }} of {{ allItems.length.toLocaleString() }} items
        <template v-if="selected.size"> · {{ selected.size.toLocaleString() }} selected</template>
      </div>

      <div v-if="flash" class="mgr-flash" :class="flashError ? 'err' : 'ok'">{{ flash }}</div>

      <div v-if="!sorted.length" class="lib-empty">
        <Icon name="check" :size="14" /> Nothing matches these filters.
      </div>

      <!-- ── Poster grid (virtualized) ───────────────────────────────── -->
      <div v-else-if="view === 'posters'" :class="{ refreshing }">
        <VirtualPosterGrid
          :total="sorted.length"
          :item-at="i => sorted[i]"
          :aspect="isMusic ? 1 : 1.5"
          :meta-height="46"
          :min-card="150"
        >
          <template #default="{ item, index }">
            <div
              class="lib-card"
              :class="{ selected: selected.has(item.id) }"
            >
              <NuxtLink :to="detailLink(item)" class="lib-card-link" :aria-label="item.title">
                <Poster :idx="index" :src="usePosterUrl({ id: item.id })" :title="item.title" :aspect="posterAspect" :width="220">
                  <div class="lib-card-shade" />
                  <div class="lib-progress" :class="`tone-${progressTone(item)}`">
                    <div class="lib-progress-fill" :style="{ width: `${progressPct(item)}%` }" />
                  </div>
                </Poster>
              </NuxtLink>
              <button
                type="button" class="lib-card-monitor" :class="{ on: item.monitored }"
                :aria-label="item.monitored ? `Unmonitor ${item.title}` : `Monitor ${item.title}`"
                @click.stop.prevent="toggleMonitor(item)"
              >
                <Icon name="bookmark" :size="15" :weight="item.monitored ? 'fill' : 'regular'" />
              </button>
              <button
                type="button" class="lib-card-select" :class="{ on: selected.has(item.id) }"
                :aria-label="selected.has(item.id) ? `Deselect ${item.title}` : `Select ${item.title}`"
                @click.stop.prevent="toggleSelected(item.id)"
              >
                <Icon name="check" :size="12" />
              </button>
              <div class="lib-card-meta">
                <div class="lib-card-title" :title="item.title">{{ item.title }}</div>
                <div class="lib-card-sub">
                  <span v-if="item.year">{{ item.year }}</span>
                  <span class="lib-card-counts">{{ item.have_count }}/{{ item.total_count }}</span>
                </div>
              </div>
            </div>
          </template>
        </VirtualPosterGrid>
      </div>

      <!-- ── Table (virtualized rows) ────────────────────────────────── -->
      <div v-else :class="{ refreshing }">
        <div class="libt">
          <div class="libt-head" :style="{ gridTemplateColumns: tableTemplate }">
            <span />
            <span aria-label="Monitored" />
            <button type="button" class="lib-th" :class="{ active: sort === 'title' }" @click="headerSort('title')">Title <Icon v-if="sort === 'title'" :name="dir === 'asc' ? 'chevup' : 'chevdown'" :size="11" /></button>
            <button v-if="showYearStatus && !isPhone" type="button" class="lib-th" :class="{ active: sort === 'year' }" @click="headerSort('year')">Year <Icon v-if="sort === 'year'" :name="dir === 'asc' ? 'chevup' : 'chevdown'" :size="11" /></button>
            <button v-if="showYearStatus && !isPhone" type="button" class="lib-th" :class="{ active: sort === 'status' }" @click="headerSort('status')">Status <Icon v-if="sort === 'status'" :name="dir === 'asc' ? 'chevup' : 'chevdown'" :size="11" /></button>
            <span v-if="!isPhone" class="lib-th static">Profile</span>
            <span v-if="vocab!.group && !isPhone" class="lib-th static num">{{ vocab!.group }}</span>
            <button type="button" class="lib-th" :class="{ active: sort === 'progress' }" @click="headerSort('progress')">{{ SORTS.find(s => s.key === 'units')?.label }} <Icon v-if="sort === 'progress'" :name="dir === 'asc' ? 'chevup' : 'chevdown'" :size="11" /></button>
            <button type="button" class="lib-th num" :class="{ active: sort === 'missing' }" @click="headerSort('missing')">Missing <Icon v-if="sort === 'missing'" :name="dir === 'asc' ? 'chevup' : 'chevdown'" :size="11" /></button>
            <button v-if="!isPhone" type="button" class="lib-th num" :class="{ active: sort === 'size' }" @click="headerSort('size')">Size <Icon v-if="sort === 'size'" :name="dir === 'asc' ? 'chevup' : 'chevdown'" :size="11" /></button>
            <button v-if="!isPhone" type="button" class="lib-th num" :class="{ active: sort === 'added' }" @click="headerSort('added')">Added <Icon v-if="sort === 'added'" :name="dir === 'asc' ? 'chevup' : 'chevdown'" :size="11" /></button>
          </div>
          <RecycleScroller
            :items="sorted"
            :item-size="44"
            key-field="id"
            page-mode
            :buffer="600"
            v-slot="{ item }"
          >
            <div class="libt-row" :class="{ selected: selected.has(item.id) }" :style="{ gridTemplateColumns: tableTemplate }">
              <span class="libt-cell">
                <button
                  type="button" class="lib-row-select" :class="{ on: selected.has(item.id) }"
                  :aria-label="selected.has(item.id) ? `Deselect ${item.title}` : `Select ${item.title}`"
                  @click="toggleSelected(item.id)"
                >
                  <Icon name="check" :size="11" />
                </button>
              </span>
              <span class="libt-cell">
                <button
                  type="button" class="lib-row-monitor" :class="{ on: item.monitored }"
                  :aria-label="item.monitored ? `Unmonitor ${item.title}` : `Monitor ${item.title}`"
                  @click="toggleMonitor(item)"
                >
                  <Icon name="bookmark" :size="14" :weight="item.monitored ? 'fill' : 'regular'" />
                </button>
              </span>
              <span class="libt-cell title">
                <NuxtLink :to="detailLink(item)" class="lib-row-title">{{ item.title }}</NuxtLink>
              </span>
              <span v-if="showYearStatus && !isPhone" class="libt-cell mono">{{ item.year }}</span>
              <span v-if="showYearStatus && !isPhone" class="libt-cell">
                <span v-if="item.status" class="lib-status-chip">{{ statusLabel(item.status) }}</span>
              </span>
              <span v-if="!isPhone" class="libt-cell profile">{{ item.quality_profile_id ? (profileNames[item.quality_profile_id] ?? `#${item.quality_profile_id}`) : '—' }}</span>
              <span v-if="vocab!.group && !isPhone" class="libt-cell mono num">{{ item.group_count }}</span>
              <span class="libt-cell">
                <span class="lib-progress table" :class="`tone-${progressTone(item)}`">
                  <span class="lib-progress-fill" :style="{ width: `${progressPct(item)}%` }" />
                </span>
                <span class="lib-progress-label mono">{{ item.have_count }}/{{ item.total_count }}</span>
              </span>
              <span class="libt-cell mono num" :class="{ 'has-missing': item.missing_count > 0 }">{{ item.missing_count || '' }}</span>
              <span v-if="!isPhone" class="libt-cell mono num">{{ fmtBytes(item.size_on_disk) }}</span>
              <span v-if="!isPhone" class="libt-cell mono num">{{ timeAgoShort(item.added_at) }}</span>
            </div>
          </RecycleScroller>
        </div>
      </div>
    </template>

    <!-- ── Bulk bar ──────────────────────────────────────────────────── -->
    <Transition name="bulkbar">
      <div v-if="selected.size" class="lib-bulkbar" role="toolbar" aria-label="Bulk edit selection">
        <span class="lib-bulk-count">{{ selected.size }} selected</span>
        <button type="button" class="mgr-btn" :disabled="bulkBusy" @click="bulkMonitor(true)">
          <Icon name="bookmark" :size="13" weight="fill" /> Monitor
        </button>
        <button type="button" class="mgr-btn" :disabled="bulkBusy" @click="bulkMonitor(false)">
          <Icon name="bookmark" :size="13" /> Unmonitor
        </button>
        <div class="lib-bulk-profile">
          <select v-model="bulkProfile" class="lib-select" aria-label="Profile to assign">
            <option value="" disabled>Set profile…</option>
            <option v-for="p in domainProfiles" :key="p.id" :value="String(p.id)">{{ p.name }}</option>
            <option value="0">— Clear profile —</option>
          </select>
          <button type="button" class="mgr-btn-gold" :disabled="bulkBusy || bulkProfile === ''" @click="bulkAssignProfile">Apply</button>
        </div>
        <button type="button" class="mgr-btn lib-bulk-clear" :disabled="bulkBusy" @click="clearSelection">
          <Icon name="close" :size="13" /> Clear
        </button>
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.tiles {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 10px;
  margin-bottom: 20px;
}
@media (max-width: 720px) {
  .tiles { grid-template-columns: repeat(2, 1fr); }
}

/* ── Toolbar ─────────────────────────────────────────────────────────── */
.lib-toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  margin-bottom: 8px;
}
.lib-toolbar-right {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-left: auto;
}
.lib-search {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  height: 32px;
  padding: 0 10px;
  border-radius: var(--r-sm);
  background: rgb(var(--ink) / 0.06);
  border: 1px solid var(--border);
  color: var(--fg-3);
  min-width: 200px;
}
.lib-search:focus-within { border-color: var(--gold); }
.lib-search-input {
  background: none;
  border: none;
  outline: none;
  color: var(--fg-0);
  font-size: 13px;
  width: 100%;
}
.lib-search-input::placeholder { color: var(--fg-3); }

.lib-chips { display: flex; gap: 5px; }
.lib-chip {
  display: inline-flex;
  align-items: center;
  height: 30px;
  padding: 0 11px;
  border-radius: 999px;
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  color: var(--fg-2);
  font-family: var(--font-mono);
  font-size: 10.5px;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  cursor: pointer;
  transition: background 0.12s, color 0.12s, border-color 0.12s;
}
.lib-chip:hover { background: rgb(var(--ink) / 0.09); color: var(--fg-0); }
.lib-chip.active {
  color: var(--gold-bright);
  background: var(--gold-soft);
  border-color: color-mix(in srgb, var(--gold) 45%, transparent);
}

.lib-select {
  height: 32px;
  padding: 0 8px;
  border-radius: var(--r-sm);
  background: rgb(var(--ink) / 0.06);
  border: 1px solid var(--border);
  color: var(--fg-1);
  font-size: 12.5px;
  cursor: pointer;
}
.lib-select:focus { border-color: var(--gold); outline: none; }

.lib-view-toggle { display: flex; gap: 4px; }
.lib-view-toggle .mgr-btn-icon.active {
  color: var(--gold-bright);
  background: var(--gold-soft);
  border-color: color-mix(in srgb, var(--gold) 45%, transparent);
}

.lib-count {
  margin-bottom: 12px;
  font-size: 11px;
  color: var(--fg-3);
  font-family: var(--font-mono);
}

.lib-empty {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 28px 16px;
  color: var(--fg-2);
  font-size: 13px;
  background: var(--bg-2);
  border: 1px dashed var(--border);
  border-radius: var(--r-md);
}

.refreshing { opacity: 0.65; transition: opacity 0.15s; }

/* ── Poster cards (inside VirtualPosterGrid cells) ───────────────────── */
.lib-card { position: relative; min-width: 0; }
.lib-card-link { display: block; border-radius: var(--r-md); overflow: hidden; }
.lib-card-link :deep(.poster) { border-radius: var(--r-md); }
.lib-card-shade {
  position: absolute;
  inset: 0;
  /* Artwork scrim — stays literal by convention. */
  background: linear-gradient(to top, rgb(0 0 0 / 0.45) 0%, transparent 30%);
  pointer-events: none;
}
.lib-card.selected .lib-card-link { outline: 2px solid var(--gold); outline-offset: 2px; }

.lib-card-monitor,
.lib-card-select {
  position: absolute;
  top: 6px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  border-radius: 999px;
  border: 1px solid rgb(var(--ink) / 0.18);
  background: color-mix(in srgb, var(--bg-1) 78%, transparent);
  backdrop-filter: blur(6px);
  color: var(--fg-2);
  cursor: pointer;
  transition: color 0.12s, background 0.12s, opacity 0.12s;
}
.lib-card-monitor { left: 6px; }
.lib-card-monitor:hover { color: var(--fg-0); }
.lib-card-monitor.on { color: var(--gold-bright); }
.lib-card-select { right: 6px; opacity: 0; }
.lib-card:hover .lib-card-select,
.lib-card-select:focus-visible,
.lib-card-select.on { opacity: 1; }
.lib-card-select.on {
  color: var(--bg-0);
  background: var(--gold);
  border-color: var(--gold);
}

.lib-progress {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  height: 4px;
  background: rgb(var(--ink) / 0.25);
}
.lib-progress-fill {
  display: block;
  height: 100%;
  transition: width 0.2s;
}
.lib-progress.tone-good .lib-progress-fill { background: var(--good); }
.lib-progress.tone-warn .lib-progress-fill { background: var(--gold); }
.lib-progress.tone-bad .lib-progress-fill { background: var(--bad); }
.lib-progress.tone-none { display: none; }

.lib-card-meta { padding: 7px 2px 0; }
.lib-card-title {
  font-size: 12.5px;
  font-weight: 500;
  color: var(--fg-0);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.lib-card-sub {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 11px;
  color: var(--fg-3);
  font-family: var(--font-mono);
}
.lib-card-counts { margin-left: auto; color: var(--fg-2); }

/* ── Table (grid + RecycleScroller rows) ─────────────────────────────── */
/* No overflow-x here on purpose: any auto-overflow ancestor becomes the
   page-mode scroller's scroll parent and vertical scrolling dies. Narrow
   viewports drop columns (isPhone) rather than scroll sideways. */
.libt { min-width: 0; }
.libt-head {
  display: grid;
  align-items: center;
  gap: 0 10px;
  padding: 0 4px 8px;
}
.libt-head > * { min-width: 0; }
.lib-th {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  background: none;
  border: none;
  padding: 0;
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.14em;
  text-transform: uppercase;
  color: var(--fg-3);
  cursor: pointer;
  white-space: nowrap;
}
.lib-th.static { cursor: default; }
.lib-th.num { justify-content: flex-end; }
.lib-th:not(.static):hover,
.lib-th.active { color: var(--gold-bright); }

.libt-row {
  display: grid;
  align-items: center;
  gap: 0 10px;
  height: 44px;
  padding: 0 4px;
  border-top: 1px solid var(--border);
  background: var(--bg-2);
  transition: background 0.1s;
}
.libt-row:hover { background: color-mix(in srgb, var(--bg-2) 60%, rgb(var(--ink) / 0.05)); }
.libt-row.selected { background: color-mix(in srgb, var(--gold) 7%, var(--bg-2)); }
.libt-cell {
  min-width: 0;
  font-size: 12.5px;
  color: var(--fg-1);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.libt-cell.mono { font-family: var(--font-mono); font-size: 11.5px; color: var(--fg-2); }
.libt-cell.num { text-align: right; }
.libt-cell.title { font-weight: 500; }
.libt-cell.profile { font-size: 12px; color: var(--fg-2); }
.libt-cell.has-missing { color: var(--bad); font-weight: 600; }
.lib-row-title { color: var(--fg-0); font-weight: 500; text-decoration: none; }
.lib-row-title:hover { color: var(--gold-bright); }
.lib-status-chip {
  display: inline-flex;
  padding: 2px 8px;
  border-radius: 999px;
  background: rgb(var(--ink) / 0.07);
  border: 1px solid var(--border);
  font-family: var(--font-mono);
  font-size: 10px;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: var(--fg-2);
}
.lib-progress.table {
  position: relative;
  display: inline-block;
  vertical-align: middle;
  width: 70px;
  height: 5px;
  border-radius: 999px;
  overflow: hidden;
  margin-right: 8px;
  background: rgb(var(--ink) / 0.25);
}
.lib-progress-label { font-size: 11px; color: var(--fg-2); }
.lib-row-select,
.lib-row-monitor {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border-radius: 999px;
  border: 1px solid var(--border);
  background: none;
  color: var(--fg-3);
  cursor: pointer;
  transition: color 0.12s, background 0.12s;
}
.lib-row-select svg { opacity: 0; transition: opacity 0.1s; }
.lib-row-select:hover svg,
.lib-row-select.on svg { opacity: 1; }
.lib-row-select:hover { color: var(--fg-1); border-color: var(--fg-3); }
.lib-row-select.on {
  color: var(--bg-0);
  background: var(--gold);
  border-color: var(--gold);
}
.lib-row-monitor { border: none; }
.lib-row-monitor:hover { color: var(--fg-0); }
.lib-row-monitor.on { color: var(--gold-bright); }

/* ── Bulk bar ────────────────────────────────────────────────────────── */
.lib-bulkbar {
  position: sticky;
  bottom: 14px;
  z-index: 20;
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin-top: 16px;
  padding: 10px 14px;
  border-radius: var(--r-md);
  background: color-mix(in srgb, var(--bg-1) 92%, transparent);
  border: 1px solid color-mix(in srgb, var(--gold) 35%, var(--border));
  backdrop-filter: blur(10px);
  box-shadow: 0 8px 30px rgb(var(--ink) / 0.18);
}
.lib-bulk-count {
  font-family: var(--font-mono);
  font-size: 11.5px;
  font-weight: 600;
  color: var(--gold-bright);
  margin-right: 4px;
}
.lib-bulk-profile { display: flex; align-items: center; gap: 6px; }
.lib-bulk-clear { margin-left: auto; }

.bulkbar-enter-active,
.bulkbar-leave-active { transition: opacity 0.15s, transform 0.15s; }
.bulkbar-enter-from,
.bulkbar-leave-to { opacity: 0; transform: translateY(8px); }
</style>
