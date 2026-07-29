<script setup lang="ts">
definePageMeta({ layout: 'manager', middleware: 'admin' })

// The manager's release calendar. The CalendarAgenda / CalendarMonthGrid
// components are generic and reusable (the public sections will embed them
// later) — everything manager-specific happens here: fetching the admin
// endpoint, mapping API events into entries, and building manager links.
import { managerCalendarQuery, type CalendarEventView } from '~/queries/manager'
import { librariesQuery } from '~/queries/catalog'
import { managerLibraryIcon } from '~/composables/useManagerNav'
import { toCalendarDate, parseCalendarDate, type CalendarEntry, type CalendarEntryTag } from '~/components/calendar/calendarEntry'

// ── View state ───────────────────────────────────────────────────────────

const view = ref<'agenda' | 'month'>('agenda')
onMounted(() => {
  const saved = localStorage.getItem('heya:manager:calendar-view')
  if (saved === 'agenda' || saved === 'month') view.value = saved
})
watch(view, value => localStorage.setItem('heya:manager:calendar-view', value))

const today = toCalendarDate(new Date())
// Month cursor, YYYY-MM. Agenda ignores it (rolling window).
const monthCursor = ref(today.slice(0, 7))
const selectedDate = ref<string | null>(null)
watch([view, monthCursor], () => { selectedDate.value = null })

function shiftMonth(delta: number) {
  const [y, m] = monthCursor.value.split('-').map(Number)
  const d = new Date(y!, (m ?? 1) - 1 + delta, 1)
  monthCursor.value = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
}
const monthLabel = computed(() => {
  const [y, m] = monthCursor.value.split('-').map(Number)
  return new Intl.DateTimeFormat(undefined, { month: 'long', year: 'numeric' }).format(new Date(y!, (m ?? 1) - 1, 1))
})

const { isPhone } = useViewport()
// The grid needs ~180px cells to be readable; phones get agenda only.
const effectiveView = computed(() => (isPhone.value ? 'agenda' : view.value))

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

const entries = computed<CalendarEntry[]>(() => {
  const list = events.value
  const out: CalendarEntry[] = []
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
  return out
})

const selectedDayEntries = computed(() =>
  selectedDate.value ? entries.value.filter(e => e.date === selectedDate.value) : [])

const selectedDayLabel = computed(() => {
  if (!selectedDate.value) return ''
  return new Intl.DateTimeFormat(undefined, { weekday: 'long', day: '2-digit', month: 'long' })
    .format(parseCalendarDate(selectedDate.value))
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
          <button type="button" class="mgr-btn-icon" :class="{ active: view === 'agenda' }" aria-label="Agenda view" @click="view = 'agenda'">
            <Icon name="rows" :size="15" />
          </button>
          <button type="button" class="mgr-btn-icon" :class="{ active: view === 'month' }" aria-label="Month view" @click="view = 'month'">
            <Icon name="calendar" :size="15" />
          </button>
        </div>
      </div>
    </div>

    <div v-if="effectiveView === 'month'" class="calp-monthnav">
      <button type="button" class="mgr-btn-icon" aria-label="Previous month" @click="shiftMonth(-1)">
        <Icon name="chevleft" :size="14" />
      </button>
      <button type="button" class="mgr-btn calp-today-btn" @click="monthCursor = today.slice(0, 7)">Today</button>
      <button type="button" class="mgr-btn-icon" aria-label="Next month" @click="shiftMonth(1)">
        <Icon name="chevright" :size="14" />
      </button>
      <span class="calp-month-label">{{ monthLabel }}</span>
    </div>

    <div v-if="loadError" class="mgr-flash err" role="alert">
      <Icon name="warning" :size="13" /> Couldn't load the calendar. <button type="button" class="mgr-btn calp-retry" @click="eventsQuery.refetch()">Retry</button>
    </div>

    <div v-else-if="loading" class="calp-skel" role="status" aria-label="Loading calendar">
      <div v-for="i in 6" :key="i" class="calp-skel-row" />
    </div>

    <template v-else-if="effectiveView === 'month'">
      <div :class="{ refreshing }">
        <CalendarMonthGrid v-model:selected-date="selectedDate" :month="monthCursor" :entries="entries" />
      </div>
      <div v-if="selectedDate" class="calp-day-panel">
        <div class="calp-day-panel-head">{{ selectedDayLabel }}</div>
        <CalendarAgenda :entries="selectedDayEntries" empty-text="Nothing on this day." />
      </div>
    </template>

    <div v-else :class="{ refreshing }">
      <CalendarAgenda :entries="entries" :empty-text="emptyText" />
    </div>
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

.calp-view-toggle { display: flex; gap: 4px; }
.calp-view-toggle .mgr-btn-icon.active {
  color: var(--gold-bright);
  background: var(--gold-soft);
  border-color: color-mix(in srgb, var(--gold) 45%, transparent);
}

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

.calp-day-panel { margin-top: 18px; }
.calp-day-panel-head {
  font-family: var(--font-mono);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--fg-3);
  margin-bottom: 10px;
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
