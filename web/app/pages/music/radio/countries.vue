<template>
  <div class="page-pad rc-page">
    <header class="rc-head">
      <NuxtLink to="/music/radio" class="rc-back">
        <Icon name="chevleft" :size="14" /> Radio
      </NuxtLink>
      <MusicPageHead title="Browse by Country" subtitle="Pick a country to see its highest-voted stations." />
    </header>

    <!-- Drilldown view: stations for the selected country. -->
    <template v-if="selectedCode">
      <div class="rc-drill-head">
        <button class="rc-drill-back" @click="clearSelection">
          <Icon name="chevleft" :size="16" /> All countries
        </button>
        <h2 class="section-title-lg rc-drill-title">
          <span class="rc-flag" v-if="selectedFlag">{{ selectedFlag }}</span>
          {{ selectedName }}
        </h2>
      </div>
      <div v-if="stationsLoading" class="rc-loading">Loading stations…</div>
      <div v-else-if="!stations.length" class="rc-empty">No stations for this country.</div>
      <div v-else class="rc-grid">
        <RadioStationCard
          v-for="s in stations"
          :key="s.stationuuid"
          :station="s"
          :favorited="radio.isFavorited(s.stationuuid)"
          :loading="radio.loadingStationUUID.value === s.stationuuid"
          @play="radio.playStation"
          @toggle-favorite="radio.toggleFavorite"
        />
      </div>
    </template>

    <!-- Country picker grid. -->
    <template v-else>
      <div v-if="!countries.length" class="rc-loading">Loading countries…</div>
      <div v-else class="rc-country-grid">
        <button
          v-for="c in countries"
          :key="c.iso_3166_1"
          class="rc-country-tile steer-glass"
          @click="selectCountry(c)"
        >
          <span class="rc-flag">{{ flag(c.iso_3166_1) }}</span>
          <span class="rc-country-name">{{ c.name }}</span>
          <span class="rc-country-count mono">{{ c.stationcount.toLocaleString() }}</span>
        </button>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import type { RadioStationView } from '~/composables/useRadio'
import { useQuery } from '@pinia/colada'

definePageMeta({ layout: 'default' })

interface CountryRow { name: string; iso_3166_1: string; stationcount: number }

const route = useRoute()
const router = useRouter()
const radio = useRadioActions()
if (import.meta.client) radio.ensureFavoritesLoaded()

const { $heya } = useNuxtApp()

const countriesQuery = useQuery({
  key: ['radio', 'countries'],
  query: async () => ((await $heya('/api/radio/countries')) as { items: CountryRow[] }).items ?? [],
  staleTime: 1000 * 60 * 60, // 1h — country list doesn't change often
})
await waitForQuery(countriesQuery)
const countries = computed<CountryRow[]>(() => countriesQuery.data.value ?? [])

// URL `?code=` drives the selection (and thus the stations query). Sharing
// a link with a code params auto-loads the drilldown view.
const selectedCode = computed(() => (route.query.code as string | undefined) ?? '')
const selectedName = computed(() => countries.value.find((c) => c.iso_3166_1 === selectedCode.value)?.name ?? '')
const selectedFlag = computed(() => flag(selectedCode.value))

const stationsQuery = useQuery({
  key: () => ['radio', 'search', { countrycode: selectedCode.value }],
  query: async () => ((await $heya('/api/radio/search', { query: { countrycode: selectedCode.value, limit: 50 } })) as { items: RadioStationView[] }).items ?? [],
  enabled: () => selectedCode.value.length > 0,
  staleTime: 1000 * 60 * 5,
})
const stations = computed<RadioStationView[]>(() => stationsQuery.data.value ?? [])
const stationsLoading = computed(() => stationsQuery.isLoading.value)

async function selectCountry(c: CountryRow) {
  router.replace({ query: { code: c.iso_3166_1 } })
}

function clearSelection() {
  router.replace({ query: {} })
}

// flag converts a country ISO-2 code into the Unicode regional-indicator
// flag emoji. Works for any ISO-3166-1 alpha-2 code; renders blank for
// codes shorter than 2 chars.
function flag(code: string) {
  if (!code || code.length < 2) return ''
  return code
    .toUpperCase()
    .split('')
    .map((c) => String.fromCodePoint(0x1f1e6 + c.charCodeAt(0) - 65))
    .join('')
}

// Pinia Colada auto-fires when selectedCode is non-empty — no manual onMounted.
</script>

<style scoped>
.rc-page { padding-bottom: 80px; }
.rc-head { margin-bottom: 24px; }
.rc-back { color: var(--fg-3); font-size: 12px; text-decoration: none; display: inline-flex; align-items: center; gap: 4px; }
.rc-back:hover { color: var(--gold); }

.rc-country-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: 8px;
}
.rc-country-tile {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  border-radius: var(--r-md);
  text-align: left;
  cursor: pointer;
  transition: border-color 0.15s, background 0.15s;
  font-family: inherit;
  color: inherit;
}
.rc-country-tile:hover {
  border-color: color-mix(in srgb, var(--gold) 30%, transparent);
}
.rc-flag { font-size: 20px; flex-shrink: 0; line-height: 1; }
.rc-country-name { flex: 1; min-width: 0; font-size: 13px; color: var(--fg-0); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.rc-country-count { font-size: 11px; color: var(--fg-3); font-family: var(--font-mono); }

.rc-drill-head { display: flex; align-items: center; gap: 14px; margin-bottom: 20px; }
.rc-drill-back {
  background: transparent;
  border: 0;
  font-size: 12px;
  color: var(--fg-2);
  cursor: pointer;
  padding: 6px 10px;
  border-radius: var(--r-sm);
  display: inline-flex;
  align-items: center;
  gap: 4px;
  transition: color 0.15s, background 0.15s;
}
.rc-drill-back:hover { color: var(--gold); background: color-mix(in srgb, var(--gold) 6%, transparent); }
.rc-drill-title { display: flex; align-items: center; gap: 10px; margin: 0; }

.rc-loading, .rc-empty { color: var(--fg-3); padding: 24px 0; font-size: 13px; }

.rc-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(170px, 1fr));
  gap: 18px;
}
.mono { font-family: var(--font-mono); }

@media (max-width: 720px) {
  .rc-country-grid { grid-template-columns: repeat(auto-fill, minmax(140px, 1fr)); gap: 8px; }
  .rc-grid { grid-template-columns: repeat(auto-fill, minmax(110px, 1fr)); gap: 12px; }
  .page-pad { padding-left: 16px; padding-right: 16px; }
}
</style>
