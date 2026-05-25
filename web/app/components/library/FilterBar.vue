<template>
  <div class="filter-bar">
    <div class="filter-bar-top">
      <div class="filter-bar-left">
        <h1 class="filter-bar-title">{{ title }}</h1>
        <span class="filter-bar-count">{{ count }} titles</span>
      </div>
      <div class="filter-bar-right">
        <button class="btn-ghost-sm" :class="{ active: panelOpen }" @click="panelOpen = !panelOpen">
          <Icon name="filter" :size="14" />
          Filters
          <span v-if="activeCount > 0" class="filter-badge">{{ activeCount }}</span>
        </button>
        <div class="sort-wrap" ref="sortWrap">
          <button class="btn-ghost-sm" @click="sortOpen = !sortOpen">
            <Icon name="sort" :size="14" />
            {{ sortLabel }}
          </button>
          <div v-if="sortOpen" class="sort-menu">
            <div
              v-for="opt in sortOptions"
              :key="opt.value"
              class="sort-option"
              :class="{ active: sort === opt.value }"
              @click="$emit('sort', opt.value); sortOpen = false"
            >
              {{ opt.label }}
            </div>
          </div>
        </div>
        <div class="view-toggle">
          <button class="btn-icon" :class="{ active: view === 'grid' }" @click="$emit('view', 'grid')">
            <Icon name="grid" :size="16" />
          </button>
          <button class="btn-icon" :class="{ active: view === 'list' }" @click="$emit('view', 'list')">
            <Icon name="list" :size="16" />
          </button>
        </div>
      </div>
    </div>

    <!-- Active filter pills -->
    <div v-if="activeCount > 0" class="filter-pills">
      <button
        v-for="pill in activePills"
        :key="pill.key"
        class="filter-pill"
        @click="removePill(pill)"
      >
        {{ pill.label }} <Icon name="close" :size="10" />
      </button>
      <button class="filter-pill filter-pill-clear" @click="clearAll">Clear all</button>
      <button class="btn-ghost-sm save-smart" @click="$emit('save-list')">
        <Icon name="bookmark" :size="12" />
        Save as Smart List
      </button>
    </div>

    <!-- Filter panel — compact grid layout -->
    <div v-if="panelOpen" class="filter-panel">
      <div class="filter-grid">
        <!-- Genre chips — full width -->
        <div class="filter-cell full">
          <label class="filter-label">Genre</label>
          <div class="filter-chips">
            <button
              v-for="g in availableGenres"
              :key="g"
              class="chip"
              :class="{ active: local.genres.includes(g) }"
              @click="toggleGenre(g)"
            >{{ g }}</button>
          </div>
        </div>

        <!-- Year -->
        <div class="filter-cell">
          <label class="filter-label">Year</label>
          <div class="filter-range">
            <input type="number" class="range-input" placeholder="From" :value="local.yearMin"
              @input="local.yearMin = parseNum($event); emit()" />
            <span class="range-sep">–</span>
            <input type="number" class="range-input" placeholder="To" :value="local.yearMax"
              @input="local.yearMax = parseNum($event); emit()" />
          </div>
        </div>

        <!-- Rating -->
        <div class="filter-cell">
          <label class="filter-label">Rating</label>
          <div class="filter-range">
            <input type="number" class="range-input" placeholder="Min" step="0.5" min="0" max="10"
              :value="local.ratingMin"
              @input="local.ratingMin = parseFloat(($event.target as HTMLInputElement).value) || null; emit()" />
            <span class="range-sep">–</span>
            <input type="number" class="range-input" placeholder="Max" step="0.5" min="0" max="10"
              :value="local.ratingMax"
              @input="local.ratingMax = parseFloat(($event.target as HTMLInputElement).value) || null; emit()" />
          </div>
        </div>

        <!-- Resolution -->
        <div class="filter-cell">
          <label class="filter-label">Resolution</label>
          <div class="filter-chips">
            <button v-for="r in ['4k', '1080p', '720p', 'sd']" :key="r"
              class="chip" :class="{ active: local.resolutions.includes(r) }"
              @click="toggleResolution(r)"
            >{{ r === '4k' ? '4K' : r === 'sd' ? 'SD' : r }}</button>
          </div>
        </div>

        <!-- Watched -->
        <div class="filter-cell">
          <label class="filter-label">Watched</label>
          <div class="filter-chips">
            <button v-for="opt in [{ v: 'all', l: 'All' }, { v: 'watched', l: 'Watched' }, { v: 'unwatched', l: 'Unwatched' }]"
              :key="opt.v" class="chip" :class="{ active: local.watched === opt.v }"
              @click="local.watched = opt.v as any; emit()"
            >{{ opt.l }}</button>
          </div>
        </div>

        <!-- Language -->
        <div v-if="availableLanguages.length > 1" class="filter-cell">
          <label class="filter-label">Language</label>
          <div class="filter-chips">
            <button class="chip" :class="{ active: local.language === null }"
              @click="local.language = null; emit()">All</button>
            <button v-for="l in availableLanguages.slice(0, 8)" :key="l"
              class="chip" :class="{ active: local.language === l }"
              @click="local.language = local.language === l ? null : l; emit()"
            >{{ langName(l) }}</button>
          </div>
        </div>

        <!-- Person -->
        <div class="filter-cell">
          <label class="filter-label">Actor / Director</label>
          <div class="typeahead-wrap">
            <input type="text" class="typeahead-input" placeholder="Search people..."
              v-model="personQuery" @input="searchPeople" />
            <div v-if="personResults.length > 0" class="typeahead-dropdown">
              <div v-for="p in personResults" :key="p.id" class="typeahead-option"
                @click="addPerson(p)">{{ p.name }}</div>
            </div>
          </div>
          <div v-if="local.personNames.length" class="filter-chips" style="margin-top: 4px">
            <button v-for="(name, i) in local.personNames" :key="local.personIds[i]"
              class="chip active" @click="removePerson(i)"
            >{{ name }} <Icon name="close" :size="8" /></button>
          </div>
        </div>

        <!-- Studio -->
        <div class="filter-cell">
          <label class="filter-label">Studio</label>
          <div class="typeahead-wrap">
            <input type="text" class="typeahead-input" placeholder="Search studios..."
              v-model="studioQuery" @input="searchStudios" />
            <div v-if="studioResults.length > 0" class="typeahead-dropdown">
              <div v-for="s in studioResults" :key="s.id" class="typeahead-option"
                @click="addStudio(s)">{{ s.name }}</div>
            </div>
          </div>
          <div v-if="local.studioNames.length" class="filter-chips" style="margin-top: 4px">
            <button v-for="(name, i) in local.studioNames" :key="local.studioIds[i]"
              class="chip active" @click="removeStudio(i)"
            >{{ name }} <Icon name="close" :size="8" /></button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { FilterState } from '~~/shared/types'

const props = defineProps<{
  title: string
  count: number
  sort: string
  view: string
  filters: FilterState
  availableGenres: string[]
  availableLanguages: string[]
}>()

const emits = defineEmits<{
  sort: [value: string]
  view: [value: string]
  'update:filters': [filters: FilterState]
  'save-list': []
}>()

const panelOpen = ref(false)
const sortOpen = ref(false)
const sortWrap = ref<HTMLElement>()

const local = reactive<FilterState>({ ...props.filters })

watch(() => props.filters, (f) => {
  Object.assign(local, f)
}, { deep: true })

function emit() {
  emits('update:filters', { ...local })
}

const sortOptions = [
  { label: 'Title A→Z', value: 'title' },
  { label: 'Recently Added', value: 'added' },
  { label: 'Year (Newest)', value: 'year-desc' },
  { label: 'Year (Oldest)', value: 'year-asc' },
  { label: 'Rating', value: 'rating' },
]

const sortLabel = computed(() => sortOptions.find(o => o.value === props.sort)?.label || 'Sort')

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
  emit()
}

function clearAll() {
  Object.assign(local, defaultFilters())
  emit()
}

function toggleGenre(g: string) {
  const idx = local.genres.indexOf(g)
  if (idx >= 0) local.genres.splice(idx, 1)
  else local.genres.push(g)
  emit()
}

function toggleResolution(r: string) {
  const idx = local.resolutions.indexOf(r)
  if (idx >= 0) local.resolutions.splice(idx, 1)
  else local.resolutions.push(r)
  emit()
}

function parseNum(e: Event): number | null {
  const v = parseInt((e.target as HTMLInputElement).value)
  const result = isNaN(v) ? null : v
  emit()
  return result
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
  emit()
}

function removePerson(i: number) {
  local.personIds.splice(i, 1)
  local.personNames.splice(i, 1)
  emit()
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
  emit()
}

function removeStudio(i: number) {
  local.studioIds.splice(i, 1)
  local.studioNames.splice(i, 1)
  emit()
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

onMounted(() => {
  document.addEventListener('click', (e) => {
    if (sortWrap.value && !sortWrap.value.contains(e.target as Node)) sortOpen.value = false
  })
})
</script>

<style scoped>
.filter-bar { padding: 24px 32px 0; }
.filter-bar-top { display: flex; align-items: center; justify-content: space-between; }
.filter-bar-left { display: flex; align-items: baseline; gap: 12px; }
.filter-bar-title { font-size: 30px; font-weight: 600; letter-spacing: -0.02em; margin: 0; }
.filter-bar-count { font-family: var(--font-mono); font-size: 12px; color: var(--fg-3); }
.filter-bar-right { display: flex; align-items: center; gap: 8px; }

.btn-ghost-sm.active { color: var(--gold); }

.filter-badge {
  display: inline-flex; align-items: center; justify-content: center;
  min-width: 18px; height: 18px; padding: 0 5px;
  border-radius: 100px; font-size: 10px; font-weight: 700;
  background: var(--gold); color: var(--bg-0);
  margin-left: 4px;
}

.sort-wrap { position: relative; }
.sort-menu {
  position: absolute; top: calc(100% + 6px); right: 0; min-width: 200px;
  background: var(--bg-3); border: 1px solid var(--border-strong);
  border-radius: var(--r-md); padding: 4px; z-index: 20; box-shadow: var(--shadow-2);
}
.sort-option {
  padding: 8px 12px; font-size: 13px; border-radius: var(--r-sm); cursor: pointer; color: var(--fg-1);
}
.sort-option:hover { background: rgba(255,255,255,0.06); }
.sort-option.active { color: var(--gold); }
.view-toggle { display: flex; gap: 2px; }

/* Filter pills */
.filter-pills {
  display: flex; flex-wrap: wrap; align-items: center; gap: 6px;
  margin-top: 14px; padding-bottom: 4px;
}
.filter-pill {
  display: inline-flex; align-items: center; gap: 5px;
  padding: 4px 10px; border-radius: 100px; font-size: 12px; font-weight: 500;
  background: var(--bg-3); border: 1px solid var(--border);
  color: var(--fg-1); cursor: pointer; transition: all 0.15s;
}
.filter-pill:hover { border-color: var(--gold); color: var(--gold); }
.filter-pill-clear { color: var(--fg-3); border-color: transparent; background: none; }
.filter-pill-clear:hover { color: var(--bad); }
.save-smart { margin-left: auto; color: var(--gold); font-size: 12px; }

/* Filter panel — compact grid */
.filter-panel {
  margin-top: 14px; padding: 14px 0 6px;
  border-top: 1px solid var(--border);
}
.filter-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px 24px;
}
.filter-cell { display: flex; flex-direction: column; gap: 5px; }
.filter-cell.full { grid-column: 1 / -1; }
.filter-label {
  font-size: 10px; font-weight: 700; color: var(--fg-4);
  text-transform: uppercase; letter-spacing: 0.08em;
}
.filter-chips { display: flex; flex-wrap: wrap; gap: 4px; }
.chip {
  padding: 3px 10px; border-radius: 100px; font-size: 11px; font-weight: 500;
  background: var(--bg-3); border: 1px solid var(--border);
  color: var(--fg-2); cursor: pointer; transition: all 0.15s;
  display: inline-flex; align-items: center; gap: 4px;
}
.chip:hover { border-color: var(--fg-3); color: var(--fg-1); }
.chip.active { background: var(--gold-soft, rgba(212,175,55,0.15)); border-color: var(--gold); color: var(--gold); }

.filter-range { display: flex; align-items: center; gap: 6px; }
.range-input {
  width: 72px; padding: 4px 8px; border-radius: var(--r-sm);
  background: var(--bg-3); border: 1px solid var(--border);
  color: var(--fg-1); font-size: 12px; font-family: var(--font-mono);
}
.range-input:focus { border-color: var(--gold); outline: none; }
.range-sep { color: var(--fg-3); font-size: 13px; }

/* Typeahead */
.typeahead-wrap { position: relative; max-width: 220px; }
.typeahead-input {
  width: 100%; padding: 4px 8px; border-radius: var(--r-sm);
  background: var(--bg-3); border: 1px solid var(--border);
  color: var(--fg-1); font-size: 12px;
}
.typeahead-input:focus { border-color: var(--gold); outline: none; }
.typeahead-dropdown {
  position: absolute; top: calc(100% + 4px); left: 0; right: 0;
  background: var(--bg-3); border: 1px solid var(--border-strong);
  border-radius: var(--r-md); padding: 4px; z-index: 30; box-shadow: var(--shadow-2);
  max-height: 180px; overflow-y: auto;
}
.typeahead-option {
  padding: 5px 8px; font-size: 12px; border-radius: var(--r-sm);
  cursor: pointer; color: var(--fg-1);
}
.typeahead-option:hover { background: rgba(255,255,255,0.06); }
</style>
