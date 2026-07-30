<script setup lang="ts">
definePageMeta({ layout: 'manager', middleware: 'admin' })

// The manager's release calendar. The CalendarAgenda / CalendarMonthGrid
// components are generic and reusable (the public sections will embed them
// later) — everything manager-specific happens here: fetching the admin
// endpoint, mapping API events into entries, and building manager links.
import { managerCalendarQuery, type CalendarEventView, type CalendarEventDetailView } from '~/queries/manager'
import { librariesQuery } from '~/queries/catalog'
import { managerLibraryIcon } from '~/composables/useManagerNav'
import { toCalendarDate, parseCalendarDate, type CalendarEntry, type CalendarEntryTag } from '~/components/calendar/calendarEntry'
import { fmtBytes } from '~/composables/useFormat'

// ── View state ───────────────────────────────────────────────────────────

type CalendarViewMode = 'week' | 'month' | 'agenda'
const view = ref<CalendarViewMode>('week')
onMounted(() => {
  const saved = localStorage.getItem('heya:manager:calendar-view:v2')
  if (saved === 'agenda' || saved === 'month' || saved === 'week') view.value = saved
})
watch(view, value => localStorage.setItem('heya:manager:calendar-view:v2', value))

const today = toCalendarDate(new Date())

function mondayOf(date: Date): Date {
  const monday = new Date(date)
  monday.setDate(date.getDate() - ((date.getDay() + 6) % 7))
  return monday
}

// Week cursor = the Monday of the visible week; month cursor = YYYY-MM.
// The nav arrows step whichever period the active view shows.
const weekCursor = ref(toCalendarDate(mondayOf(new Date())))
const monthCursor = ref(today.slice(0, 7))

function shiftPeriod(delta: number) {
  if (view.value === 'week') {
    const monday = parseCalendarDate(weekCursor.value)
    monday.setDate(monday.getDate() + delta * 7)
    weekCursor.value = toCalendarDate(monday)
    return
  }
  const [y, m] = monthCursor.value.split('-').map(Number)
  const d = new Date(y!, (m ?? 1) - 1 + delta, 1)
  monthCursor.value = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
}
function goToday() {
  weekCursor.value = toCalendarDate(mondayOf(new Date()))
  monthCursor.value = today.slice(0, 7)
}

const rangeFormat = new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'short' })
const periodLabel = computed(() => {
  if (view.value === 'week') {
    const monday = parseCalendarDate(weekCursor.value)
    const sunday = new Date(monday)
    sunday.setDate(monday.getDate() + 6)
    return `${rangeFormat.format(monday)} – ${rangeFormat.format(sunday)} ${sunday.getFullYear()}`
  }
  const [y, m] = monthCursor.value.split('-').map(Number)
  return new Intl.DateTimeFormat(undefined, { month: 'long', year: 'numeric' }).format(new Date(y!, (m ?? 1) - 1, 1))
})

const { isPhone } = useViewport()
// The grids need wide cells to be readable; phones get agenda only.
const effectiveView = computed<CalendarViewMode>(() => (isPhone.value ? 'agenda' : view.value))

const VIEW_OPTIONS: { key: CalendarViewMode, label: string }[] = [
  { key: 'week', label: 'Week' },
  { key: 'month', label: 'Month' },
  { key: 'agenda', label: 'Agenda' },
]

// The legend names the chip dressing — state must be readable without
// memorizing colors.
const LEGEND = [
  { state: 'ok', label: 'Downloaded' },
  { state: 'error', label: 'Missing' },
  { state: 'warn', label: 'Today' },
  { state: 'idle', label: 'Upcoming' },
] as const

// ── Filters ──────────────────────────────────────────────────────────────

const { data: libraries } = useQuery(librariesQuery())
const filterOptions = computed(() => (libraries.value ?? []).map(l => ({
  key: String(l.id),
  label: l.name,
  icon: managerLibraryIcon(l.media_type),
})))
const libraryFilter = ref<string[]>([])
const monitoredOnly = ref(false)

const libraryById = computed(() => {
  const map = new Map<number, { name: string, mediaType: string }>()
  for (const l of libraries.value ?? []) map.set(l.id, { name: l.name, mediaType: l.media_type })
  return map
})

// ── Window + data ────────────────────────────────────────────────────────
// Agenda: rolling week-back/month-ahead — recent gaps beside what's coming.
// Month: the padded Monday-grid range for the cursor month.

const window_ = computed(() => {
  // Follows effectiveView, not the persisted preference: a phone forces the
  // agenda, and it must get the rolling window — not a month-shaped range
  // left over from a desktop month view.
  if (effectiveView.value === 'agenda') {
    const from = new Date()
    from.setDate(from.getDate() - 7)
    const to = new Date()
    to.setDate(to.getDate() + 30)
    return { from: toCalendarDate(from), to: toCalendarDate(to) }
  }
  if (effectiveView.value === 'week') {
    const monday = parseCalendarDate(weekCursor.value)
    const sunday = new Date(monday)
    sunday.setDate(monday.getDate() + 6)
    return { from: toCalendarDate(monday), to: toCalendarDate(sunday) }
  }
  const [y, m] = monthCursor.value.split('-').map(Number)
  const first = new Date(y!, (m ?? 1) - 1, 1)
  const start = new Date(first)
  start.setDate(first.getDate() - ((first.getDay() + 6) % 7))
  const last = new Date(y!, (m ?? 1), 0)
  const end = new Date(last)
  end.setDate(last.getDate() + (6 - ((last.getDay() + 6) % 7)))
  return { from: toCalendarDate(start), to: toCalendarDate(end) }
})

const eventsQuery = useQuery(() => managerCalendarQuery({
  from: window_.value.from,
  to: window_.value.to,
  libraries: libraryFilter.value.map(Number),
  monitored: monitoredOnly.value,
}))
const events = computed(() => eventsQuery.data.value ?? [])
const loading = computed(() => eventsQuery.isLoading.value && !eventsQuery.data.value)
// Month navigation keeps the previous window on screen as placeholder; the
// dim signals the swap the same way the library list does.
const refreshing = computed(() => eventsQuery.isLoading.value && !!eventsQuery.data.value)
const loadError = computed(() => eventsQuery.error.value)

// ── API events → generic calendar entries ────────────────────────────────

const KIND_ICONS: Record<string, string> = { episode: 'tv', movie: 'film', album: 'music', book: 'book' }

function badgeFor(date: string, hasFile: boolean): CalendarEntry['badge'] {
  if (hasFile) return { label: 'downloaded', state: 'ok' }
  if (date === today) return { label: 'today', state: 'warn' }
  if (date < today) return { label: 'missing', state: 'error' }
  return { label: 'upcoming', state: 'idle' }
}

function libraryRef(ev: CalendarEventView): CalendarEntry['library'] {
  const lib = libraryById.value.get(ev.library_id)
  return lib ? { id: ev.library_id, label: lib.name } : undefined
}

const epCode = (season: number, episode: number) =>
  `S${String(season).padStart(2, '0')}E${String(episode).padStart(2, '0')}`

// Milestones wear arr vocabulary: premieres lean positive, a season finale is
// an occasion, a series finale is the end of the road.
const MILESTONE_TAGS: Record<string, CalendarEntryTag> = {
  series_premiere: { label: 'Series premiere', tone: 'good' },
  season_premiere: { label: 'Season premiere', tone: 'good' },
  season_finale: { label: 'Season finale', tone: 'gold' },
  series_finale: { label: 'Series finale', tone: 'bad' },
}

// One pass produces the display entries AND remembers which raw event ids a
// collapsed run covers, so the modal can expand every episode of the run.
const mapped = computed<{ entries: CalendarEntry[], runIds: Map<string, string[]> }>(() => {
  const list = events.value
  const out: CalendarEntry[] = []
  const runIds = new Map<string, string[]>()
  let i = 0
  while (i < list.length) {
    const ev = list[i]!
    if (ev.kind === 'episode') {
      // Same-day multi-episode drops collapse into one row per series+season
      // (the API sorts date → title → season → episode, so runs are
      // contiguous). Any missing episode drives the aggregate badge.
      let j = i + 1
      while (
        j < list.length && list[j]!.kind === 'episode'
        && list[j]!.date === ev.date && list[j]!.media_item_id === ev.media_item_id
        && list[j]!.season === ev.season
      ) j++
      const run = list.slice(i, j)
      const allHave = run.every(e => e.has_file)
      const last = run[run.length - 1]!
      // A run spans both boundaries when a full season drops at once — the
      // premiere from its first episode, the finale from its last.
      const tags: CalendarEntryTag[] = []
      const premiere = run.find(e => e.milestone?.includes('premiere'))
      const finale = [...run].reverse().find(e => e.milestone?.includes('finale'))
      if (premiere?.milestone) tags.push(MILESTONE_TAGS[premiere.milestone]!)
      if (finale?.milestone) tags.push(MILESTONE_TAGS[finale.milestone]!)
      runIds.set(ev.id, run.map(e => e.id))
      out.push({
        id: ev.id,
        date: ev.date,
        title: ev.title,
        subtitle: run.length === 1
          ? `${epCode(ev.season ?? 0, ev.episode ?? 0)}${ev.episode_name ? ` · ${ev.episode_name}` : ''}`
          : `${epCode(ev.season ?? 0, ev.episode ?? 0)}–E${String(last.episode ?? 0).padStart(2, '0')} · ${run.length} episodes`,
        to: `/manager/library/${ev.library_id}/${ev.media_item_id}`,
        icon: KIND_ICONS[ev.kind],
        library: libraryRef(ev),
        badge: badgeFor(ev.date, allHave),
        tags: tags.length ? tags : undefined,
      })
      i = j
      continue
    }

    const entry: CalendarEntry = {
      id: ev.id,
      date: ev.date,
      title: ev.title,
      to: `/manager/library/${ev.library_id}/${ev.media_item_id}`,
      icon: KIND_ICONS[ev.kind] ?? 'folder',
      library: libraryRef(ev),
      badge: badgeFor(ev.date, ev.has_file),
    }
    if (ev.kind === 'album') {
      entry.subtitle = ev.album_title
      // The release bucket as a tag ("EP", "Single", "Live") — plain albums
      // stay untagged, the icon already says music.
      if (ev.album_type && ev.album_type !== 'album') {
        entry.tags = [{ label: ev.album_type, tone: 'neutral' }]
      }
      if (ev.album_ref) entry.to = `/manager/library/${ev.library_id}/${ev.media_item_id}/${ev.album_ref}`
    }
    out.push(entry)
    i++
  }
  return { entries: out, runIds }
})
const entries = computed(() => mapped.value.entries)

// ── Details modal ────────────────────────────────────────────────────────
// Any event click opens it — an episode run expands into every episode.

const { $heya } = useNuxtApp()
const modalOpen = ref(false)
const modalEntry = ref<CalendarEntry | null>(null)
const modalDetails = ref<CalendarEventDetailView[] | null>(null)
const modalError = ref('')

async function openEvent(entry: CalendarEntry) {
  modalEntry.value = entry
  modalDetails.value = null
  modalError.value = ''
  modalOpen.value = true
  const ids = mapped.value.runIds.get(entry.id) ?? [entry.id]
  try {
    modalDetails.value = await $heya('/api/manager/calendar/events', {
      query: { ids: ids.join(',') },
    }) as CalendarEventDetailView[]
  } catch (e: any) {
    modalError.value = e?.data?.detail ?? 'Couldn\'t load details.'
  }
}

const pad2 = (n: number) => String(n).padStart(2, '0')
const stillUrl = (d: CalendarEventDetailView) =>
  `/api/media/${d.media_item_id}/image/still?label=s${pad2(d.season ?? 0)}e${pad2(d.episode ?? 0)}`

function detailCover(d: CalendarEventDetailView): string | null {
  if (d.kind === 'episode') return stillUrl(d)
  if (d.kind === 'album') {
    if (d.album_slug) return useAlbumCoverUrl(d.slug, d.album_slug)
    return metadataImageProxyUrl(d.cover_url ?? '') || null
  }
  return usePosterUrl({ id: d.media_item_id })
}

function detailHeading(d: CalendarEventDetailView): string {
  if (d.kind === 'episode') return `${epCode(d.season ?? 0, d.episode ?? 0)}${d.episode_name ? ` · ${d.episode_name}` : ''}`
  if (d.kind === 'album') return d.album_title || d.title
  return d.title
}

function detailMeta(d: CalendarEventDetailView): string {
  const parts: string[] = []
  if (d.date) parts.push(releaseDateLabel(d.date, d.date.slice(0, 4)))
  if (d.runtime_minutes) parts.push(`${d.runtime_minutes} min`)
  if (d.rating) parts.push(`★ ${d.rating.toFixed(1)}`)
  if (d.kind === 'album' && d.tracks_total) parts.push(`${d.tracks_have}/${d.tracks_total} tracks`)
  if (d.has_file && d.quality) parts.push(d.quality)
  if (d.has_file && d.size_bytes) parts.push(fmtBytes(d.size_bytes))
  return parts.join(' · ')
}

const modalPrimary = computed(() => modalDetails.value?.[0] ?? null)
const managerLink = computed(() => {
  const d = modalPrimary.value
  if (!d) return null
  if (d.kind === 'album' && d.album_ref) return `/manager/library/${d.library_id}/${d.media_item_id}/${d.album_ref}`
  return `/manager/library/${d.library_id}/${d.media_item_id}`
})
const publicLink = computed(() => {
  const d = modalPrimary.value
  if (!d) return null
  switch (d.media_type) {
    case 'movie': return `/movies/${d.slug}`
    case 'tv':
    case 'anime': return `/tv/${d.slug}`
    case 'music': return d.album_slug ? `/music/artist/${d.slug}/${d.album_slug}` : `/music/artist/${d.slug}`
    case 'book': return `/books/${d.slug}`
    default: return null
  }
})

// Honest sparseness: music dates only exist once artists have re-enriched —
// an empty music-only view needs that context or it looks broken.
const emptyText = computed(() => {
  const selected = libraryFilter.value.map(Number)
  const onlyMusic = selected.length > 0
    && selected.every(id => libraryById.value.get(id)?.mediaType === 'music')
  if (onlyMusic) {
    return 'No dated releases in this range. Music release dates fill in as artists are enriched — refresh an artist from its manager page to pull its catalog.'
  }
  return 'No dated releases in this range.'
})

</script>

<template>
  <div>
    <SettingsContextHero
      title="Calendar"
      icon="calendar"
      eyebrow="Manager · Calendar"
      description="Dated releases across your libraries — episodes airing, albums dropping, movies and books releasing."
    />

    <div class="calp-toolbar">
      <ManagerLibraryFilter v-model="libraryFilter" :options="filterOptions" />

      <div class="calp-toolbar-right">
        <label class="calp-monitored">
          <AppSwitch v-model="monitoredOnly" size="sm" aria-label="Monitored only" />
          <span>Monitored only</span>
        </label>
        <div v-if="!isPhone" class="calp-view-toggle" role="group" aria-label="Calendar view">
          <button
            v-for="option in VIEW_OPTIONS"
            :key="option.key"
            type="button"
            class="calp-view-btn"
            :class="{ active: view === option.key }"
            :aria-pressed="view === option.key"
            @click="view = option.key"
          >{{ option.label }}</button>
        </div>
      </div>
    </div>

    <div v-if="effectiveView !== 'agenda'" class="calp-monthnav">
      <button type="button" class="mgr-btn-icon" :aria-label="view === 'week' ? 'Previous week' : 'Previous month'" @click="shiftPeriod(-1)">
        <Icon name="chevleft" :size="14" />
      </button>
      <button type="button" class="mgr-btn calp-today-btn" @click="goToday">Today</button>
      <button type="button" class="mgr-btn-icon" :aria-label="view === 'week' ? 'Next week' : 'Next month'" @click="shiftPeriod(1)">
        <Icon name="chevright" :size="14" />
      </button>
      <span class="calp-month-label">{{ periodLabel }}</span>
      <div class="calp-legend" aria-hidden="true">
        <span v-for="item in LEGEND" :key="item.state" class="calp-legend-item">
          <span class="calp-legend-swatch" :class="`st-${item.state}`" />{{ item.label }}
        </span>
      </div>
    </div>

    <div v-if="loadError" class="mgr-flash err" role="alert">
      <Icon name="warning" :size="13" /> Couldn't load the calendar. <button type="button" class="mgr-btn calp-retry" @click="eventsQuery.refetch()">Retry</button>
    </div>

    <div v-else-if="loading" class="calp-skel" role="status" aria-label="Loading calendar">
      <div v-for="i in 6" :key="i" class="calp-skel-row" />
    </div>

    <div v-else-if="effectiveView === 'week'" :class="{ refreshing }">
      <CalendarMonthGrid :week="weekCursor" :entries="entries" @select="openEvent" />
    </div>

    <div v-else-if="effectiveView === 'month'" :class="{ refreshing }">
      <CalendarMonthGrid :month="monthCursor" :entries="entries" @select="openEvent" />
    </div>

    <div v-else :class="{ refreshing }">
      <CalendarAgenda :entries="entries" :empty-text="emptyText" interactive="select" @select="openEvent" />
    </div>

    <AppDialog v-model="modalOpen" :title="modalEntry?.title ?? ''" size="md">
      <div v-if="modalError" class="mgr-flash err" role="alert">
        <Icon name="warning" :size="13" /> {{ modalError }}
      </div>
      <div v-else-if="!modalDetails" class="calm-loading">
        <Icon name="spinner" :size="16" /> Loading…
      </div>
      <div v-else class="calm-list">
        <div v-for="d in modalDetails" :key="d.id" class="calm-item">
          <NuxtImg
            v-if="detailCover(d)"
            :src="detailCover(d)!"
            :width="d.kind === 'episode' ? 220 : 120"
            class="calm-image"
            :class="`kind-${d.kind}`"
            :alt="detailHeading(d)"
          />
          <div class="calm-body">
            <div class="calm-heading">
              {{ detailHeading(d) }}
              <span v-if="d.kind === 'album' && d.album_type && d.album_type !== 'album'" class="calm-type">{{ d.album_type }}</span>
            </div>
            <div class="calm-meta">
              <span>{{ detailMeta(d) }}</span>
              <StatusBadge :state="d.has_file ? 'ok' : (d.date && d.date < today ? 'error' : 'idle')">
                {{ d.has_file ? 'downloaded' : (d.date && d.date < today ? 'missing' : 'upcoming') }}
              </StatusBadge>
            </div>
            <p v-if="d.overview" class="calm-overview">{{ d.overview }}</p>
          </div>
        </div>
      </div>
      <template #footer>
        <NuxtLink v-if="managerLink" :to="managerLink" class="mgr-btn">Open in manager</NuxtLink>
        <NuxtLink v-if="publicLink" :to="publicLink" class="mgr-btn">View in library</NuxtLink>
        <button type="button" class="mgr-btn-gold" @click="modalOpen = false">Close</button>
      </template>
    </AppDialog>
  </div>
</template>

<style scoped>
.calp-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  margin-bottom: 18px;
}
.calp-toolbar-right { display: flex; align-items: center; gap: 14px; }

.calp-monitored {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 12.5px;
  color: var(--fg-2);
  cursor: pointer;
}

.calp-view-toggle {
  display: flex;
  gap: 2px;
  padding: 2px;
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
}
.calp-view-btn {
  padding: 4px 12px;
  border: 0;
  border-radius: calc(var(--r-sm) - 2px);
  background: none;
  color: var(--fg-2);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: background 0.12s, color 0.12s;
}
.calp-view-btn:hover { color: var(--fg-0); }
.calp-view-btn.active {
  color: var(--gold-bright);
  background: var(--gold-soft);
}

.calp-legend {
  display: flex;
  align-items: center;
  gap: 14px;
  margin-left: auto;
  font-size: 11px;
  color: var(--fg-2);
}
.calp-legend-item { display: inline-flex; align-items: center; gap: 6px; }
.calp-legend-swatch {
  width: 10px;
  height: 10px;
  border-radius: 3px;
  border-left: 3px solid rgb(var(--ink) / 0.18);
  background: rgb(var(--ink) / 0.06);
}
.calp-legend-swatch.st-ok { border-left-color: var(--good); background: color-mix(in srgb, var(--good) 13%, transparent); }
.calp-legend-swatch.st-error { border-left-color: var(--bad); background: color-mix(in srgb, var(--bad) 13%, transparent); }
.calp-legend-swatch.st-warn { border-left-color: var(--gold); background: var(--gold-soft); }

.calp-monthnav {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 14px;
}
.calp-month-label {
  font-family: var(--font-display);
  font-variation-settings: "wdth" 100;
  font-size: 17px;
  font-weight: 700;
  color: var(--fg-0);
  margin-left: 6px;
}

.calp-retry { margin-left: 10px; }

/* ── Details modal ───────────────────────────────────────────────────── */
.calm-loading {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--fg-2);
  font-size: 13px;
  padding: 18px 0;
}
.calm-list { display: flex; flex-direction: column; gap: 16px; }
.calm-item { display: flex; gap: 14px; align-items: flex-start; }
.calm-image {
  flex-shrink: 0;
  border-radius: var(--r-sm);
  background: var(--bg-3);
  object-fit: cover;
}
.calm-image.kind-episode { width: 168px; aspect-ratio: 16 / 9; }
.calm-image.kind-movie,
.calm-image.kind-book { width: 84px; aspect-ratio: 2 / 3; }
.calm-image.kind-album { width: 96px; aspect-ratio: 1; }
.calm-body { min-width: 0; flex: 1; display: flex; flex-direction: column; gap: 6px; }
.calm-heading {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  font-size: 14px;
  font-weight: 600;
  color: var(--fg-0);
}
.calm-type {
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--fg-2);
  padding: 1px 7px;
  border-radius: 999px;
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
}
.calm-meta {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  font-size: 12px;
  color: var(--fg-2);
  font-family: var(--font-mono);
}
.calm-overview {
  margin: 0;
  font-size: 12.5px;
  line-height: 1.55;
  color: var(--fg-1);
  display: -webkit-box;
  -webkit-line-clamp: 5;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
@media (max-width: 560px) {
  .calm-item { flex-direction: column; }
  .calm-image.kind-episode { width: 100%; }
}

.refreshing { opacity: 0.65; transition: opacity 0.15s; }

.calp-skel { display: flex; flex-direction: column; gap: 8px; }
.calp-skel-row {
  height: 54px;
  border-radius: var(--r-md);
  background: var(--bg-3);
  animation: calp-pulse 1.5s ease-in-out infinite;
}
@keyframes calp-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.55; }
}
</style>
