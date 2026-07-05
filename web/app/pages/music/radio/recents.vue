<template>
  <div class="page-pad rr-page">
    <header class="rr-head">
      <NuxtLink to="/music/radio" class="rr-back">
        <Icon name="chevleft" :size="14" /> Radio
      </NuxtLink>
      <h1 class="rr-title">Recently Played</h1>
      <p class="rr-sub">Your last 30 stations — same row twice means you came back, the dedup hides the duplicates.</p>
    </header>

    <div v-if="loading" class="rr-loading">Loading…</div>
    <div v-else-if="!recents.length" class="rr-empty">
      <Icon name="radio" :size="36" style="opacity: 0.4" />
      <h3>No recent plays yet</h3>
      <p>Stations land here once you start one — even the ones you skip.</p>
    </div>
    <div v-else class="rr-grid">
      <RadioStationCard
        v-for="(s, i) in recents"
        :key="`${s.stationuuid}-${i}`"
        :station="rowToStation(s)"
        :favorited="radio.isFavorited(s.stationuuid)"
        :loading="radio.loadingStationUUID.value === s.stationuuid"
        @play="radio.playStation"
        @toggle-favorite="radio.toggleFavorite"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import type { RadioStationView } from '~/composables/useRadio'
import { useQuery } from '@tanstack/vue-query'

definePageMeta({ layout: 'default' })

interface RecentRow {
  stationuuid: string; name: string; url: string; favicon: string
  country: string; tags: string; codec: string; bitrate: number
  played_at: string
}

const radio = useRadioActions()
if (import.meta.client) radio.ensureFavoritesLoaded()

const { $heya } = useNuxtApp()
const recentsQuery = useQuery({
  queryKey: ['me', 'radio', 'recents', { limit: 30 }],
  queryFn: async () => ((await $heya('/api/me/radio/recents', { query: { limit: 30 } })) as { items: RecentRow[] }).items ?? [],
  staleTime: 1000 * 30,
})
const recents = computed<RecentRow[]>(() => recentsQuery.data.value ?? [])
const loading = computed(() => recentsQuery.isPending.value)

function rowToStation(r: RecentRow): RadioStationView {
  return {
    stationuuid: r.stationuuid, name: r.name, url: r.url, url_resolved: r.url,
    favicon: r.favicon, country: r.country, tags: r.tags, codec: r.codec,
    bitrate: r.bitrate, homepage: '', countrycode: '', language: '',
    votes: 0, clickcount: 0,
  }
}
</script>

<style scoped>
.rr-page { padding-bottom: 80px; }
.rr-head { margin-bottom: 24px; }
.rr-back { color: var(--fg-3); font-size: 12px; text-decoration: none; display: inline-flex; align-items: center; gap: 4px; }
.rr-back:hover { color: var(--gold); }
.rr-title { font-size: 28px; font-weight: 700; margin-top: 6px; letter-spacing: -0.01em; }
.rr-sub { color: var(--fg-3); font-size: 13px; margin-top: 4px; max-width: 600px; }
.rr-loading { color: var(--fg-3); padding: 24px 0; font-size: 13px; }
.rr-empty {
  display: flex; flex-direction: column; align-items: center; gap: 8px;
  padding: 60px 0; text-align: center; color: var(--fg-2);
}
.rr-empty h3 { font-size: 16px; color: var(--fg-1); margin-top: 4px; }
.rr-empty p { font-size: 13px; color: var(--fg-3); max-width: 360px; }
.rr-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(170px, 1fr));
  gap: 18px;
}

@media (max-width: 720px) {
  .rr-grid { grid-template-columns: repeat(auto-fill, minmax(110px, 1fr)); gap: 12px; }
  .page-pad { padding-left: 16px; padding-right: 16px; }
}
</style>
