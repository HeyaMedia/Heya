<template>
  <div class="ms-station page-pad">
    <header class="ms-st-head">
      <div class="ms-st-icon-wrap" :style="{ background: meta.iconBg }">
        <Icon :name="meta.icon" :size="22" />
      </div>
      <div class="ms-st-text">
        <h1 class="ms-st-title">{{ meta.title }}</h1>
        <div class="ms-st-sub">{{ meta.subtitle }}</div>
      </div>
    </header>

    <!-- Time Travel decade picker -->
    <div v-if="slug === 'time-travel'" class="ms-st-decade-row">
      <button
        v-for="d in decades"
        :key="d.value"
        type="button"
        class="ms-st-decade-btn"
        :class="{ active: activeDecade === d.value }"
        @click="setDecade(d.value)"
      >{{ d.label }}</button>
    </div>

    <StationResults
      :label="label"
      :tracks="tracks"
      :loading="isLoading"
      :error="errorMsg"
      :reroll-label="meta.rerollLabel"
      :save-label="meta.title"
      @reroll="refetch"
    />
  </div>
</template>

<script setup lang="ts">
import { useQuery } from '@tanstack/vue-query'
import type { StationTrack } from '~/components/music/StationResults.vue'

definePageMeta({ layout: 'default' })

const route = useRoute()
const slug = computed(() => (route.params.slug as string) ?? '')
const { $heya } = useNuxtApp()

interface StationMeta {
  title: string
  subtitle: string
  icon: string
  iconBg: string
  rerollLabel: string
}

const META: Record<string, StationMeta> = {
  library: {
    title: 'Library Radio',
    subtitle: 'A random walk through everything you own. Tap re-roll for a fresh shuffle.',
    icon: 'radio',
    iconBg: 'linear-gradient(135deg, #2a1d4a, #5b3aa1)',
    rerollLabel: 'Re-roll',
  },
  'deep-cuts': {
    title: 'Deep Cuts',
    subtitle: 'Tracks you own but barely play. The forgotten gems.',
    icon: 'compass',
    iconBg: 'linear-gradient(135deg, #1d3a4a, #3a8aa1)',
    rerollLabel: 'Re-roll',
  },
  'time-travel': {
    title: 'Time Travel',
    subtitle: 'Drop into a decade. Pick a band to start.',
    icon: 'clock',
    iconBg: 'linear-gradient(135deg, #4a2d1d, #a16d3a)',
    rerollLabel: 'Re-roll',
  },
  'random-album': {
    title: 'Random Album',
    subtitle: 'One album, end-to-end. Tap to draw another.',
    icon: 'music',
    iconBg: 'linear-gradient(135deg, #4a1d3a, #a13a7d)',
    rerollLabel: 'Pick another album',
  },
}

const meta = computed<StationMeta>(() => META[slug.value] ?? {
  title: 'Station',
  subtitle: 'Unknown station.',
  icon: 'compass',
  iconBg: 'linear-gradient(135deg, #444, #666)',
  rerollLabel: 'Re-roll',
})

// Time Travel decade picker
const decades = [
  { value: 1960, label: '60s' },
  { value: 1970, label: '70s' },
  { value: 1980, label: '80s' },
  { value: 1990, label: '90s' },
  { value: 2000, label: '00s' },
  { value: 2010, label: '10s' },
  { value: 2020, label: '20s' },
]
const activeDecade = ref<number>(2020)
function setDecade(d: number) {
  activeDecade.value = d
}

// Single query, branches on slug
interface StationBody {
  kind: string
  label: string
  tracks: StationTrack[]
}

const stationQuery = useQuery({
  queryKey: ['music', 'stations', slug, activeDecade] as const,
  queryFn: async (): Promise<StationBody> => {
    if (slug.value === 'library') {
      return await $heya('/api/music/stations/library-radio', { query: { limit: 30 } }) as unknown as StationBody
    }
    if (slug.value === 'deep-cuts') {
      return await $heya('/api/music/stations/deep-cuts', { query: { limit: 30 } }) as unknown as StationBody
    }
    if (slug.value === 'time-travel') {
      return await $heya('/api/music/stations/time-travel', {
        query: { min_year: activeDecade.value, max_year: activeDecade.value + 9, limit: 30 },
      }) as unknown as StationBody
    }
    if (slug.value === 'random-album') {
      return await $heya('/api/music/stations/random-album') as unknown as StationBody
    }
    return { kind: '', label: '', tracks: [] }
  },
  enabled: () => !!slug.value && slug.value in META,
  staleTime: 0,
  refetchOnWindowFocus: false,
})

const tracks = computed<StationTrack[]>(() => stationQuery.data.value?.tracks ?? [])
const label = computed(() => stationQuery.data.value?.label ?? meta.value.title)
const isLoading = computed(() => stationQuery.isFetching.value)
const errorMsg = computed(() => {
  const e = stationQuery.error.value as { data?: { error?: string }; message?: string } | null
  if (!e) return null
  return e.data?.error ?? e.message ?? null
})

function refetch() { stationQuery.refetch() }
</script>

<style scoped>
.ms-station { max-width: 1100px; }

.ms-st-head {
  display: flex; align-items: center; gap: 18px;
  margin-bottom: 24px;
}
.ms-st-icon-wrap {
  width: 64px; height: 64px;
  border-radius: var(--r-md);
  display: flex; align-items: center; justify-content: center;
  color: #fff;
  flex-shrink: 0;
  box-shadow: 0 6px 16px rgba(0, 0, 0, 0.35);
}
.ms-st-text { flex: 1; min-width: 0; }
.ms-st-title { font-size: 30px; font-weight: 700; letter-spacing: -0.01em; }
.ms-st-sub { color: var(--fg-3); font-size: 13px; margin-top: 4px; max-width: 640px; }

.ms-st-decade-row {
  display: flex; gap: 4px;
  margin-bottom: 24px;
  padding: 4px;
  background: rgba(255,255,255,0.03);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  width: fit-content;
}
.ms-st-decade-btn {
  padding: 8px 16px;
  background: transparent;
  border: 0;
  border-radius: var(--r-sm);
  color: var(--fg-2);
  font-size: 13px;
  font-weight: 700;
  cursor: pointer;
  transition: all 0.15s;
  font-family: var(--font-mono);
  letter-spacing: 0.04em;
}
.ms-st-decade-btn:hover { color: var(--fg-0); }
.ms-st-decade-btn.active {
  background: var(--gold-soft);
  color: var(--gold);
}

@media (max-width: 720px) {
  .ms-st-icon-wrap { width: 52px; height: 52px; }
  .ms-st-title { font-size: 24px; }

  /* 7 decade pills at `width: fit-content` overflow a 390px viewport and
     blow out the page's scrollWidth — let the strip scroll horizontally
     within itself instead of the whole page gaining a scrollbar. */
  .ms-st-decade-row {
    width: 100%;
    max-width: 100%;
    overflow-x: auto;
    -webkit-overflow-scrolling: touch;
    scrollbar-width: none;
  }
  .ms-st-decade-row::-webkit-scrollbar { display: none; }
  .ms-st-decade-btn { flex-shrink: 0; }
}
</style>
