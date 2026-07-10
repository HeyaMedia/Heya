<!--
  FilterBar — sticky toolbar for the library browse pages (movies / tv).

  Title + count on the left; a uniform control row on the right: Reset
  (only when sort/filters are dirty), Filters (anchored popover panel),
  Sort (dropdown menu) and the grid/detail/list view toggle. Active
  filters render as removable pills under the toolbar.

  The filter panel is a reka Popover (not DropdownMenu) so the inputs and
  typeaheads inside it can take focus without closing the panel. All panel
  styles live in the unscoped style block — the popover portals to <body>,
  out of reach of scoped CSS.
-->
<template>
  <div class="filter-bar">
    <div class="filter-bar-top">
      <div class="filter-bar-left">
        <h1 class="filter-bar-title">{{ title }}</h1>
        <span class="filter-bar-count">{{ count }} {{ countLabel ?? 'titles' }}</span>
      </div>
      <div class="filter-bar-right">
        <!-- The section sidebar (library/loved/lists/franchises) opens from
             AppTopBar's burger on phone + the compact band — no per-page
             button here. The "Filters" popover below is the separate
             attribute-filter panel (genre/year/rating/...). -->
        <!-- Poster-size slider (grid view only, when the page wires it up).
             Hidden on phones via the SAME isPhone signal usePosterGrid uses:
             the phone grid pins its own minimum card width, so the slider
             would render but do nothing there. -->
        <div v-if="tileSize !== undefined && view === 'grid' && !isPhone" class="fb-size" title="Poster size">
          <Icon name="grid" :size="12" class="fb-size-icon" />
          <AppSlider v-model="tileProxy" :min="TILE_SIZE_MIN" :max="TILE_SIZE_MAX" :step="10" aria-label="Poster size" />
        </div>

        <button v-if="dirty && !hideFilters" class="btn-ghost-sm steer-glass fb-reset" title="Reset filters and sorting" @click="$emit('reset')">
          <Icon name="undo" :size="14" />
          Reset
        </button>

        <PopoverRoot v-if="!hideFilters" v-model:open="panelOpen">
          <PopoverTrigger as-child>
            <button class="btn-ghost-sm steer-glass" :class="{ active: activeCount > 0 || panelOpen }">
              <Icon name="filter" :size="14" />
              Filters
              <span v-if="activeCount > 0" class="filter-badge">{{ activeCount }}</span>
              <Icon name="chevdown" :size="10" class="fb-caret" :class="{ open: panelOpen }" />
            </button>
          </PopoverTrigger>
          <PopoverPortal>
            <PopoverContent class="surface fb-pop" align="end" :side-offset="8" :collision-padding="16">
              <div class="fb-pop-scroll scroll">
                <div class="fb-sec">
                  <div class="fb-sec-label">Genre</div>
                  <div class="fb-chips">
                    <button
                      v-for="g in availableGenres"
                      :key="g"
                      class="fb-chip"
                      :class="{ active: local.genres.includes(g) }"
                      @click="toggleGenre(g)"
                    >
                      {{ g }}<span v-if="genreCounts?.[g]" class="fb-chip-count">{{ genreCounts?.[g] }}</span>
                    </button>
                  </div>
                </div>

                <div class="fb-sec fb-sec-cols">
                  <div>
                    <div class="fb-sec-label">Year</div>
                    <div class="fb-range">
                      <input
                        type="number" class="fb-input" placeholder="From" :value="local.yearMin"
                        @input="local.yearMin = parseNum($event); emitFilters()"
                      >
                      <span class="fb-range-sep">–</span>
                      <input
                        type="number" class="fb-input" placeholder="To" :value="local.yearMax"
                        @input="local.yearMax = parseNum($event); emitFilters()"
                      >
                    </div>
                  </div>
                  <div>
                    <div class="fb-sec-label">Rating</div>
                    <div class="fb-range">
                      <input
                        type="number" class="fb-input" placeholder="Min" step="0.5" min="0" max="10" :value="local.ratingMin"
                        @input="local.ratingMin = parseFloat(($event.target as HTMLInputElement).value) || null; emitFilters()"
                      >
                      <span class="fb-range-sep">–</span>
                      <input
                        type="number" class="fb-input" placeholder="Max" step="0.5" min="0" max="10" :value="local.ratingMax"
                        @input="local.ratingMax = parseFloat(($event.target as HTMLInputElement).value) || null; emitFilters()"
                      >
                    </div>
                  </div>
                </div>

                <div class="fb-sec fb-sec-cols">
                  <div>
                    <div class="fb-sec-label">Resolution</div>
                    <div class="fb-chips">
                      <button
                        v-for="r in ['4k', '1080p', '720p', 'sd']" :key="r"
                        class="fb-chip" :class="{ active: local.resolutions.includes(r) }"
                        @click="toggleResolution(r)"
                      >{{ r === '4k' ? '4K' : r === 'sd' ? 'SD' : r }}</button>
                    </div>
                  </div>
                  <div>
                    <div class="fb-sec-label">Watched</div>
                    <div class="fb-seg">
                      <button
                        v-for="opt in [{ v: 'all', l: 'All' }, { v: 'watched', l: 'Seen' }, { v: 'unwatched', l: 'Unseen' }]"
                        :key="opt.v" :class="{ active: local.watched === opt.v }"
                        @click="local.watched = opt.v as any; emitFilters()"
                      >{{ opt.l }}</button>
                    </div>
                  </div>
                </div>

                <div v-if="availableLanguages.length > 1" class="fb-sec">
                  <div class="fb-sec-label">Language</div>
                  <div class="fb-chips">
                    <button class="fb-chip" :class="{ active: local.language === null }" @click="local.language = null; emitFilters()">All</button>
                    <button
                      v-for="l in availableLanguages.slice(0, 12)" :key="l"
                      class="fb-chip" :class="{ active: local.language === l }"
                      @click="local.language = local.language === l ? null : l; emitFilters()"
                    >{{ langName(l) }}</button>
                  </div>
                </div>

                <div class="fb-sec fb-sec-cols">
                  <div>
                    <div class="fb-sec-label">Actor / Director</div>
                    <div class="fb-ta">
                      <input v-model="personQuery" type="text" class="fb-input fb-ta-input" placeholder="Search people..." @input="searchPeople">
                      <div v-if="personResults.length > 0" class="fb-ta-drop">
                        <div v-for="p in personResults" :key="p.id" class="fb-ta-opt" @click="addPerson(p)">{{ p.name }}</div>
                      </div>
                    </div>
                    <div v-if="local.personNames.length" class="fb-chips" style="margin-top: 6px">
                      <button
                        v-for="(name, i) in local.personNames" :key="local.personIds[i]"
                        class="fb-chip active" @click="removePerson(i)"
                      >{{ name }} <Icon name="close" :size="8" /></button>
                    </div>
                  </div>
                  <div>
                    <div class="fb-sec-label">Studio</div>
                    <div class="fb-ta">
                      <input v-model="studioQuery" type="text" class="fb-input fb-ta-input" placeholder="Search studios..." @input="searchStudios">
                      <div v-if="studioResults.length > 0" class="fb-ta-drop">
                        <div v-for="st in studioResults" :key="st.id" class="fb-ta-opt" @click="addStudio(st)">{{ st.name }}</div>
                      </div>
                    </div>
                    <div v-if="local.studioNames.length" class="fb-chips" style="margin-top: 6px">
                      <button
                        v-for="(name, i) in local.studioNames" :key="local.studioIds[i]"
                        class="fb-chip active" @click="removeStudio(i)"
                      >{{ name }} <Icon name="close" :size="8" /></button>
                    </div>
                  </div>
                </div>
              </div>

              <div class="fb-pop-foot">
                <button class="fb-foot-btn" :disabled="activeCount === 0" @click="clearAll">Clear filters</button>
                <button class="fb-foot-btn gold" :disabled="activeCount === 0" @click="$emit('save-list')">
                  <Icon name="bookmark" :size="12" />
                  Save as Smart List
                </button>
              </div>
            </PopoverContent>
          </PopoverPortal>
        </PopoverRoot>

        <AppMenu trigger-class="btn-ghost-sm steer-glass" :width="210" align="end">
          <template #trigger>
            <Icon name="sort" :size="14" />
            {{ sortLabel }}
            <Icon name="chevdown" :size="10" class="fb-caret" />
          </template>
          <DropdownMenuItem
            v-for="opt in sortMenu"
            :key="opt.value"
            class="surface-item fb-sort-item"
            :class="{ active: sort === opt.value }"
            @select="$emit('sort', opt.value)"
          >
            {{ opt.label }}
            <Icon v-if="sort === opt.value" name="check" :size="12" class="fb-sort-check" />
          </DropdownMenuItem>
        </AppMenu>

        <div class="view-toggle">
          <AppTooltip v-for="v in viewOptions" :key="v.value" :label="v.label">
            <button
              class="view-toggle-btn"
              :class="{ active: view === v.value }"
              :aria-label="v.label"
              @click="$emit('view', v.value)"
            >
              <Icon :name="v.icon" :size="15" />
            </button>
          </AppTooltip>
        </div>
      </div>
    </div>

    <!-- Active filter pills -->
    <div v-if="activeCount > 0 && !hideFilters" class="filter-pills">
      <button
        v-for="pill in activePills"
        :key="pill.key"
        class="filter-pill"
        @click="removePill(pill)"
      >
        {{ pill.label }} <Icon name="close" :size="10" />
      </button>
      <button class="filter-pill filter-pill-clear" @click="clearAll">Clear all</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { FilterState } from '~~/shared/types'
import { DropdownMenuItem, PopoverRoot, PopoverTrigger, PopoverPortal, PopoverContent } from 'reka-ui'

const props = defineProps<{
  title: string
  count: number
  sort: string
  view: string
  filters: FilterState
  availableGenres: string[]
  availableLanguages: string[]
  /** Per-genre item counts shown in the filter panel chips. */
  genreCounts?: Record<string, number>
  /** True when sort or filters differ from defaults — shows the Reset button. */
  dirty?: boolean
  /** Override the sort menu (default = the movie sort set below). */
  sortOptions?: { label: string; value: string }[]
  /** Hide the attribute-filter controls (Filters popover, Reset, active pills).
   *  The Franchises view sorts + toggles view but has no movie-level filters. */
  hideFilters?: boolean
  /** Noun shown after the count (default 'titles'). */
  countLabel?: string
  /** Grid poster size (px). Providing it renders the size slider. */
  tileSize?: number
}>()

const emits = defineEmits<{
  sort: [value: string]
  view: [value: string]
  'update:filters': [filters: FilterState]
  'save-list': []
  reset: []
  'tile-size': [value: number]
}>()

// AppSlider wants a v-model; proxy it onto the tile-size prop/emit pair.
const tileProxy = computed({
  get: () => props.tileSize ?? TILE_SIZE_DEFAULT,
  set: (v: number) => emits('tile-size', v),
})

const panelOpen = ref(false)
const { isPhone } = useViewport()

const local = reactive<FilterState>({ ...props.filters })

watch(() => props.filters, (f) => {
  Object.assign(local, f)
}, { deep: true })

function emitFilters() {
  emits('update:filters', { ...local })
}

const DEFAULT_SORT_OPTIONS = [
  { label: 'Title A→Z', value: 'title' },
  { label: 'Recently Added', value: 'added' },
  { label: 'Year (Newest)', value: 'year-desc' },
  { label: 'Year (Oldest)', value: 'year-asc' },
  { label: 'Rating', value: 'rating' },
]
const sortMenu = computed(() => props.sortOptions ?? DEFAULT_SORT_OPTIONS)

const viewOptions = [
  { value: 'grid', label: 'Grid view', icon: 'grid' },
  { value: 'detail', label: 'Detail view', icon: 'rows' },
  { value: 'list', label: 'List view', icon: 'list' },
]

const sortLabel = computed(() => sortMenu.value.find(o => o.value === props.sort)?.label || 'Sort')

const activeCount = computed(() => {
  let c = 0
  if (local.genres.length) c += local.genres.length
  if (local.yearMin !== null) c++
  if (local.yearMax !== null) c++
  if (local.ratingMin !== null) c++
  if (local.ratingMax !== null) c++
  if (local.resolutions.length) c += local.resolutions.length
  if (local.watched !== 'all') c++
  if (local.personIds.length) c += local.personIds.length
  if (local.studioIds.length) c += local.studioIds.length
  if (local.language !== null) c++
  return c
})

interface Pill { key: string; label: string; type: string; index?: number }

const activePills = computed(() => {
  const pills: Pill[] = []
  for (const g of local.genres) pills.push({ key: `genre-${g}`, label: g, type: 'genre' })
  if (local.yearMin !== null) pills.push({ key: 'yearMin', label: `From ${local.yearMin}`, type: 'yearMin' })
  if (local.yearMax !== null) pills.push({ key: 'yearMax', label: `To ${local.yearMax}`, type: 'yearMax' })
  if (local.ratingMin !== null) pills.push({ key: 'ratingMin', label: `≥ ${local.ratingMin}★`, type: 'ratingMin' })
  if (local.ratingMax !== null) pills.push({ key: 'ratingMax', label: `≤ ${local.ratingMax}★`, type: 'ratingMax' })
  for (const r of local.resolutions) pills.push({ key: `res-${r}`, label: r === '4k' ? '4K' : r.toUpperCase(), type: 'resolution' })
  if (local.watched !== 'all') pills.push({ key: 'watched', label: local.watched === 'watched' ? 'Watched' : 'Unwatched', type: 'watched' })
  for (let i = 0; i < local.personNames.length; i++) {
    const label = local.personNames[i]
    if (label !== undefined) pills.push({ key: `person-${local.personIds[i]}`, label, type: 'person', index: i })
  }
  for (let i = 0; i < local.studioNames.length; i++) {
    const label = local.studioNames[i]
    if (label !== undefined) pills.push({ key: `studio-${local.studioIds[i]}`, label, type: 'studio', index: i })
  }
  if (local.language !== null) pills.push({ key: 'language', label: langName(local.language), type: 'language' })
  return pills
})

function removePill(pill: Pill) {
  switch (pill.type) {
    case 'genre': local.genres = local.genres.filter(g => g !== pill.label); break
    case 'yearMin': local.yearMin = null; break
    case 'yearMax': local.yearMax = null; break
    case 'ratingMin': local.ratingMin = null; break
    case 'ratingMax': local.ratingMax = null; break
    case 'resolution': local.resolutions = local.resolutions.filter(r => {
      const display = r === '4k' ? '4K' : r.toUpperCase()
      return display !== pill.label
    }); break
    case 'watched': local.watched = 'all'; break
    case 'person': if (pill.index !== undefined) removePerson(pill.index); return
    case 'studio': if (pill.index !== undefined) removeStudio(pill.index); return
    case 'language': local.language = null; break
  }
  emitFilters()
}

function clearAll() {
  Object.assign(local, defaultFilters())
  emitFilters()
}

function toggleGenre(g: string) {
  const idx = local.genres.indexOf(g)
  if (idx >= 0) local.genres.splice(idx, 1)
  else local.genres.push(g)
  emitFilters()
}

function toggleResolution(r: string) {
  const idx = local.resolutions.indexOf(r)
  if (idx >= 0) local.resolutions.splice(idx, 1)
  else local.resolutions.push(r)
  emitFilters()
}

function parseNum(e: Event): number | null {
  const v = parseInt((e.target as HTMLInputElement).value)
  return isNaN(v) ? null : v
}

const personQuery = ref('')
const personResults = ref<{ id: number; name: string; profile_path: string }[]>([])
let personDebounce: ReturnType<typeof setTimeout>

function searchPeople() {
  clearTimeout(personDebounce)
  if (!personQuery.value.trim()) { personResults.value = []; return }
  personDebounce = setTimeout(async () => {
    try {
      const { $heya } = useNuxtApp()
      personResults.value = await $heya('/api/people/search', {
        query: { q: personQuery.value, limit: 8 },
      }) as any
    } catch { personResults.value = [] }
  }, 200)
}

function addPerson(p: { id: number; name: string }) {
  if (local.personIds.includes(p.id)) return
  local.personIds.push(p.id)
  local.personNames.push(p.name)
  personQuery.value = ''
  personResults.value = []
  emitFilters()
}

function removePerson(i: number) {
  local.personIds.splice(i, 1)
  local.personNames.splice(i, 1)
  emitFilters()
}

const studioQuery = ref('')
const studioResults = ref<{ id: number; name: string; logo_path: string }[]>([])
let studioDebounce: ReturnType<typeof setTimeout>

function searchStudios() {
  clearTimeout(studioDebounce)
  if (!studioQuery.value.trim()) { studioResults.value = []; return }
  studioDebounce = setTimeout(async () => {
    try {
      const { $heya } = useNuxtApp()
      studioResults.value = await $heya('/api/studios/search', {
        query: { q: studioQuery.value, limit: 8 },
      }) as any
    } catch { studioResults.value = [] }
  }, 200)
}

function addStudio(s: { id: number; name: string }) {
  if (local.studioIds.includes(s.id)) return
  local.studioIds.push(s.id)
  local.studioNames.push(s.name)
  studioQuery.value = ''
  studioResults.value = []
  emitFilters()
}

function removeStudio(i: number) {
  local.studioIds.splice(i, 1)
  local.studioNames.splice(i, 1)
  emitFilters()
}

const LANG_NAMES: Record<string, string> = {
  en: 'English', ja: 'Japanese', ko: 'Korean', fr: 'French', de: 'German',
  es: 'Spanish', it: 'Italian', pt: 'Portuguese', zh: 'Chinese', ru: 'Russian',
  hi: 'Hindi', ar: 'Arabic', th: 'Thai', sv: 'Swedish', da: 'Danish',
  no: 'Norwegian', fi: 'Finnish', nl: 'Dutch', pl: 'Polish', tr: 'Turkish',
  cs: 'Czech', hu: 'Hungarian', ro: 'Romanian', el: 'Greek', he: 'Hebrew',
  id: 'Indonesian', ms: 'Malay', vi: 'Vietnamese', uk: 'Ukrainian', bg: 'Bulgarian',
  hr: 'Croatian', sk: 'Slovak', sl: 'Slovenian', sr: 'Serbian', lt: 'Lithuanian',
  lv: 'Latvian', et: 'Estonian', tl: 'Tagalog', te: 'Telugu', ta: 'Tamil',
  ml: 'Malayalam', kn: 'Kannada', bn: 'Bengali', cn: 'Cantonese',
}

function langName(code: string) {
  return LANG_NAMES[code] || code.toUpperCase()
}

</script>

<style scoped>
/* Sticky so view/sort/filter controls stay reachable mid-scroll. Safe to
   blur here: the filter panel portals to <body>, so no descendant carries
   its own backdrop-filter (see docs/ui.md gotcha #4). */
.filter-bar {
  position: sticky;
  top: 0;
  z-index: 20;
  padding: 18px 32px 14px;
  /* IDENTICAL fixed-pixel ramp to the LibrarySidebar's (hold --chrome
     14px, reach the translucent glass at 110px): both panels start at the
     same viewport y, so matching px stops mean matching color at every
     shared row — a %-based ramp tied to the bar's own (variable) height
     put the two panels at different points in their fades and drew a
     vertical seam at their join. Posters scrolling under the bar still
     ghost through the lower glass and melt away near the top. */
  background: linear-gradient(to bottom,
    var(--chrome) 0,
    var(--chrome) 14px,
    color-mix(in srgb, var(--bg-2) 55%, transparent) 110px);
  backdrop-filter: blur(24px);
  -webkit-backdrop-filter: blur(24px);
  box-shadow: 0 10px 28px rgb(var(--shade) / 0.14);
  /* The shadow may ONLY fall downward: its 28px blur otherwise bleeds past
     the bar's left edge onto the sidebar, shading pixels at the join and
     re-drawing the very seam the matched gradients erase. */
  clip-path: inset(0 0 -48px 0);
}
/* Firefox draws visible seam lines at backdrop-filter region boundaries in
   this stacked-panel arrangement (Safari/Chrome composite it cleanly) —
   trade the blur for slightly more solid glass there. */
@supports (-moz-appearance: none) {
  .filter-bar {
    backdrop-filter: none;
    /* S-curve stops: Firefox's weaker gradient dithering shows Mach-band
       lines at the ramp's slope discontinuities — ease in and out of the
       fade so there is no knee to see. MUST stay identical to the
       sidebar's Firefox ramp. */
    background: linear-gradient(to bottom,
      var(--chrome) 0,
      var(--chrome) 14px,
      color-mix(in srgb, var(--chrome) 96%, color-mix(in srgb, var(--bg-2) 84%, transparent)) 26px,
      color-mix(in srgb, var(--chrome) 50%, color-mix(in srgb, var(--bg-2) 84%, transparent)) 62px,
      color-mix(in srgb, var(--chrome) 4%, color-mix(in srgb, var(--bg-2) 84%, transparent)) 98px,
      color-mix(in srgb, var(--bg-2) 84%, transparent) 110px);
  }
}
.filter-bar-title { text-shadow: 0 1px 2px var(--bg-1), 0 0 10px var(--bg-1); }

/* Poster-size slider */
.fb-size {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 132px;
  padding: 0 4px;
}
.fb-size-icon { color: var(--fg-3); flex-shrink: 0; }
.filter-bar-top { display: flex; align-items: center; justify-content: space-between; }
.filter-bar-left { display: flex; align-items: baseline; gap: 12px; min-width: 0; }
.filter-bar-title {
  font-size: 26px; font-weight: 600; letter-spacing: -0.02em; margin: 0;
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.filter-bar-count { font-family: var(--font-mono); font-size: 12px; color: var(--fg-3); white-space: nowrap; }
.filter-bar-right { display: flex; align-items: center; gap: 8px; flex-shrink: 0; }

.btn-ghost-sm.active { color: var(--gold); border-color: color-mix(in srgb, var(--gold) 35%, transparent); }

.fb-reset { color: var(--fg-2); }
.fb-reset:hover { color: var(--bad); border-color: color-mix(in srgb, var(--bad) 35%, transparent); }

.fb-caret { opacity: 0.45; margin-left: -2px; transition: transform 0.15s ease; }
.fb-caret.open { transform: rotate(180deg); }

.filter-badge {
  display: inline-flex; align-items: center; justify-content: center;
  min-width: 18px; height: 18px; padding: 0 5px;
  border-radius: 100px; font-size: 10px; font-weight: 700;
  background: var(--gold); color: var(--bg-0);
}

/* Segmented grid / detail / list toggle — glass so it reads over ambient
   artwork (the bare ink wash vanished there). */
.view-toggle {
  display: flex; align-items: center; gap: 2px;
  height: 32px; padding: 2px;
  background: color-mix(in oklab, var(--bg-2) 82%, transparent);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  box-shadow: var(--shadow-el);
}
.view-toggle-btn {
  display: inline-flex; align-items: center; justify-content: center;
  width: 32px; height: 26px; border-radius: 4px;
  color: var(--fg-2);
  transition: background 0.12s ease, color 0.12s ease;
}
.view-toggle-btn:hover { color: var(--fg-0); background: rgb(var(--ink) / 0.06); }
.view-toggle-btn.active { background: var(--gold-soft); color: var(--gold); }

/* Filter pills */
.filter-pills {
  display: flex; flex-wrap: wrap; align-items: center; gap: 6px;
  margin-top: 12px;
}
.filter-pill {
  display: inline-flex; align-items: center; gap: 5px;
  padding: 4px 10px; border-radius: 100px; font-size: 12px; font-weight: 500;
  background: var(--bg-3); border: 1px solid var(--border);
  color: var(--fg-1); cursor: pointer; transition: all 0.15s;
  box-shadow: var(--shadow-el);
}
.filter-pill:hover { border-color: var(--gold); color: var(--gold); }
.filter-pill-clear { color: var(--fg-3); border-color: transparent; background: none; }
.filter-pill-clear:hover { color: var(--bad); }

/* ── Phone (<=720px) ─────────────────────────────────────────────────
   Title above, controls below (wrapping onto a second line rather than
   scroll-hunting — Reset/Filters/Sort/view-toggle can add up).
   Buttons carrying `.btn-ghost-sm`/`.view-toggle-btn` are literal elements
   in this template (not AppMenu/Popover-rendered), so the scoped selector
   reaches them fine — see the unscoped block below for the one trigger
   AppMenu itself renders. */
@media (max-width: 720px) {
  .filter-bar { padding: 14px 16px 12px; }
  .filter-bar-top { flex-direction: column; align-items: stretch; gap: 12px; }
  .filter-bar-left { justify-content: space-between; }
  .filter-bar-title { font-size: 21px; }
  .filter-bar-right { flex-wrap: wrap; gap: 8px; }
  .btn-ghost-sm { min-height: 44px; }
  .view-toggle { height: 40px; }
  .view-toggle-btn { width: 40px; height: 34px; }

  /* Active-filter pills can run long (person/studio names) — scroll the
     strip internally instead of letting it wrap forever or blow out page
     width (same fix as the station decade pills / builder seed tabs). */
  .filter-pills {
    flex-wrap: nowrap;
    overflow-x: auto;
    -webkit-overflow-scrolling: touch;
    scrollbar-width: none;
  }
  .filter-pills::-webkit-scrollbar { display: none; }
  .filter-pill, .filter-pill-clear { flex-shrink: 0; }
}
</style>

<style>
/* Everything below lands inside portaled reka content (filter popover, sort
   menu) — unreachable from the scoped block above. */
.fb-pop {
  width: 480px;
  max-width: calc(100vw - 32px);
  display: flex;
  flex-direction: column;
}
.fb-pop-scroll {
  max-height: min(66vh, 620px);
  padding: 6px 0 10px;
}

.fb-sec { padding: 10px 16px 4px; }
.fb-sec-cols { display: grid; grid-template-columns: 1fr 1fr; gap: 0 20px; }
.fb-sec-label {
  font-size: 10px; font-weight: 700; color: var(--fg-3);
  text-transform: uppercase; letter-spacing: 0.08em;
  font-family: var(--font-mono);
  margin-bottom: 7px;
}

.fb-chips { display: flex; flex-wrap: wrap; gap: 4px; }
.fb-chip {
  padding: 4px 11px; border-radius: 100px; font-size: 11.5px; font-weight: 500;
  background: rgb(var(--ink) / 0.04); border: 1px solid var(--border);
  color: var(--fg-1); cursor: pointer; transition: all 0.15s;
  display: inline-flex; align-items: center; gap: 5px;
}
.fb-chip:hover { border-color: var(--fg-3); color: var(--fg-0); }
.fb-chip.active { background: var(--gold-soft); border-color: color-mix(in srgb, var(--gold) 50%, transparent); color: var(--gold-bright); }
.fb-chip-count { font-size: 9.5px; font-family: var(--font-mono); color: var(--fg-3); }
.fb-chip.active .fb-chip-count { color: color-mix(in srgb, var(--gold) 65%, transparent); }

.fb-range { display: flex; align-items: center; gap: 6px; }
.fb-range-sep { color: var(--fg-3); font-size: 13px; }
.fb-input {
  width: 76px; padding: 6px 9px; border-radius: var(--r-sm);
  background: rgb(var(--ink) / 0.05); border: 1px solid var(--border);
  color: var(--fg-0); font-size: 12px; font-family: var(--font-mono);
  transition: border-color 0.15s;
}
.fb-input:focus { border-color: var(--gold); outline: none; }
.fb-input::placeholder { color: var(--fg-3); }

/* Watched segmented control */
.fb-seg {
  display: inline-flex; gap: 2px; padding: 2px;
  background: rgb(var(--ink) / 0.04);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
}
.fb-seg button {
  padding: 4px 11px; border-radius: 4px; font-size: 11.5px; font-weight: 500;
  color: var(--fg-2); cursor: pointer;
  transition: background 0.12s ease, color 0.12s ease;
}
.fb-seg button:hover { color: var(--fg-0); }
.fb-seg button.active { background: var(--gold-soft); color: var(--gold-bright); }

/* Typeahead */
.fb-ta { position: relative; }
.fb-ta-input { width: 100%; font-family: var(--font-sans); }
.fb-ta-drop {
  position: absolute; top: calc(100% + 4px); left: 0; right: 0;
  background: var(--bg-3); border: 1px solid var(--border-strong);
  border-radius: var(--r-md); padding: 4px; z-index: 30; box-shadow: var(--shadow-2);
  max-height: 180px; overflow-y: auto;
}
.fb-ta-opt {
  padding: 6px 9px; font-size: 12px; border-radius: var(--r-sm);
  cursor: pointer; color: var(--fg-1);
}
.fb-ta-opt:hover { background: rgb(var(--ink) / 0.06); color: var(--fg-0); }

.fb-pop-foot {
  display: flex; align-items: center; justify-content: space-between;
  padding: 10px 16px;
  border-top: 1px solid var(--border);
  background: rgb(var(--shade) / 0.15);
}
.fb-foot-btn {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 6px 12px; border-radius: var(--r-sm);
  font-size: 12px; font-weight: 500; color: var(--fg-2);
  transition: background 0.12s ease, color 0.12s ease;
}
.fb-foot-btn:hover:not(:disabled) { background: rgb(var(--ink) / 0.06); color: var(--fg-0); }
.fb-foot-btn:disabled { opacity: 0.35; cursor: default; }
.fb-foot-btn.gold { color: var(--gold); }
.fb-foot-btn.gold:hover:not(:disabled) { background: var(--gold-soft); color: var(--gold-bright); }

/* Sort menu items (AppMenu portals these out of scope too) */
.fb-sort-item { justify-content: space-between; }
.fb-sort-item.active { color: var(--gold); }
.fb-sort-check { color: var(--gold); }

@media (max-width: 720px) {
  /* The Sort button is rendered BY AppMenu (a real <button>, not as-child),
     so it carries AppMenu's own scope, not FilterBar's — the scoped phone
     rule above can't reach it (docs/ui.md "Scoped CSS doesn't reach
     portaled/child-owned elements"). Bump it from here instead. */
  .app-menu-trigger.btn-ghost-sm { min-height: 44px; }

  /* Two-column sections (Year/Rating, Resolution/Watched, Actor/Studio) get
     tight at a ~360px popover width — stack to one column. */
  .fb-sec-cols { grid-template-columns: 1fr; gap: 14px 0; }
  .fb-input { width: 100px; }
}
</style>
