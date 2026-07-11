<template>
  <div class="page-pad rt-page">
    <header class="rt-head">
      <NuxtLink to="/music/radio" class="rt-back">
        <Icon name="chevleft" :size="14" /> Radio
      </NuxtLink>
      <MusicPageHead title="Browse by Tag" subtitle="Genres, formats, eras, moods — pick anything to drill into stations." />
    </header>

    <!-- Drilldown view: stations tagged with the selected tag. -->
    <template v-if="selectedTag">
      <div class="rt-drill-head">
        <button class="rt-drill-back" @click="clearSelection">
          <Icon name="chevleft" :size="16" /> All tags
        </button>
        <h2 class="section-title-lg rt-drill-title">#{{ selectedTag }}</h2>
      </div>
      <div v-if="stationsLoading" class="rt-loading">Loading stations…</div>
      <div v-else-if="!stations.length" class="rt-empty">No stations for this tag.</div>
      <div v-else class="rt-grid">
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

    <!-- Tag wall. -->
    <template v-else>
      <div v-if="!tags.length" class="rt-loading">Loading tags…</div>
      <div v-else class="rt-tag-cloud">
        <button
          v-for="t in tags"
          :key="t.name"
          class="rt-tag steer-glass"
          :style="{ fontSize: tagSize(t.stationcount) + 'px' }"
          @click="selectTag(t.name)"
        >
          {{ t.name }}
          <span class="rt-tag-count mono">{{ t.stationcount.toLocaleString() }}</span>
        </button>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import type { RadioStationView } from '~/composables/useRadio'
import { useQuery } from '@pinia/colada'

definePageMeta({ layout: 'default' })

interface TagRow { name: string; stationcount: number }

const route = useRoute()
const router = useRouter()
const radio = useRadioActions()
if (import.meta.client) radio.ensureFavoritesLoaded()

const { $heya } = useNuxtApp()

const tagsQuery = useQuery({
  key: ['radio', 'tags', { limit: 200 }],
  query: async () => ((await $heya('/api/radio/tags', { query: { limit: 200 } })) as { items: TagRow[] }).items ?? [],
  staleTime: 1000 * 60 * 60,
})
await waitForQuery(tagsQuery)
const tags = computed<TagRow[]>(() => tagsQuery.data.value ?? [])

const selectedTag = computed(() => (route.query.tag as string | undefined) ?? '')

const stationsQuery = useQuery({
  key: () => ['radio', 'search', { tag: selectedTag.value }],
  query: async () => ((await $heya('/api/radio/search', { query: { tag: selectedTag.value, limit: 60 } })) as { items: RadioStationView[] }).items ?? [],
  enabled: () => selectedTag.value.length > 0,
  staleTime: 1000 * 60 * 5,
})
const stations = computed<RadioStationView[]>(() => stationsQuery.data.value ?? [])
const stationsLoading = computed(() => stationsQuery.isLoading.value)

function selectTag(name: string) {
  router.replace({ query: { tag: name } })
}

function clearSelection() {
  router.replace({ query: {} })
}

// Variable-size tag cloud — popular tags appear bigger. log scale so the
// 12,000-station "pop" tag doesn't dwarf "free-form folk" into invisibility.
function tagSize(count: number) {
  const min = 11
  const max = 22
  if (count <= 0) return min
  const t = Math.min(1, Math.log10(count) / 4)
  return Math.round(min + (max - min) * t)
}
</script>

<style scoped>
.rt-page { padding-bottom: 80px; }
.rt-head { margin-bottom: 24px; }
.rt-back { color: var(--fg-3); font-size: 12px; text-decoration: none; display: inline-flex; align-items: center; gap: 4px; }
.rt-back:hover { color: var(--gold); }

.rt-tag-cloud {
  display: flex;
  flex-wrap: wrap;
  gap: 10px 14px;
  align-items: baseline;
  padding: 8px 0;
}
.rt-tag {
  display: inline-flex;
  align-items: baseline;
  gap: 6px;
  padding: 6px 12px;
  border-radius: 999px;
  font-family: inherit;
  font-weight: 500;
  color: var(--fg-1);
  cursor: pointer;
  transition: background 0.12s, border-color 0.12s, color 0.12s;
  text-transform: capitalize;
}
.rt-tag:hover {
  border-color: color-mix(in srgb, var(--gold) 30%, transparent);
}
.rt-tag-count { font-size: 10px; color: var(--fg-3); font-family: var(--font-mono); }

.rt-drill-head { display: flex; align-items: center; gap: 14px; margin-bottom: 20px; }
.rt-drill-back {
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
.rt-drill-back:hover { color: var(--gold); background: color-mix(in srgb, var(--gold) 6%, transparent); }
.rt-drill-title { margin: 0; text-transform: capitalize; }

.rt-loading, .rt-empty { color: var(--fg-3); padding: 24px 0; font-size: 13px; }

.rt-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(170px, 1fr));
  gap: 18px;
}
.mono { font-family: var(--font-mono); }

@media (max-width: 720px) {
  .rt-grid { grid-template-columns: repeat(auto-fill, minmax(110px, 1fr)); gap: 12px; }
  .page-pad { padding-left: 16px; padding-right: 16px; }
}
</style>
