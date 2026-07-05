<template>
  <div class="page-pad rf-page">
    <header class="rf-head">
      <NuxtLink to="/music/radio" class="rf-back">
        <Icon name="chevleft" :size="14" /> Radio
      </NuxtLink>
      <h1 class="rf-title">Favorite Stations</h1>
      <p class="rf-sub">{{ subline }}</p>
    </header>

    <div v-if="loading" class="rf-loading">Loading…</div>
    <div v-else-if="!favorites.length" class="rf-empty">
      <Icon name="heart" :size="36" style="opacity: 0.4" />
      <h3>No favorites yet</h3>
      <p>Tap the heart icon on any station card to save it here.</p>
      <NuxtLink to="/music/radio" class="btn btn-primary">Browse stations</NuxtLink>
    </div>
    <div v-else class="rf-grid">
      <RadioStationCard
        v-for="s in favorites"
        :key="s.stationuuid"
        :station="rowToStation(s)"
        :favorited="true"
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

interface FavoriteRow {
  stationuuid: string; name: string; url: string; favicon: string; homepage: string
  country: string; countrycode: string; language: string; tags: string; codec: string; bitrate: number
}

const radio = useRadioActions()
if (import.meta.client) radio.ensureFavoritesLoaded()

const { $heya } = useNuxtApp()
const favoritesQuery = useQuery({
  queryKey: ['me', 'radio', 'favorites'],
  queryFn: async () => ((await $heya('/api/me/radio/favorites')) as { items: FavoriteRow[] }).items ?? [],
  staleTime: 1000 * 30,
})
const favorites = computed<FavoriteRow[]>(() => favoritesQuery.data.value ?? [])
const loading = computed(() => favoritesQuery.isPending.value)

// Favorites table mirrors only a subset of the upstream station shape;
// reconstruct what RadioStationCard needs (defaults stand in for fields
// we don't snapshot at favorite time).
function rowToStation(r: FavoriteRow): RadioStationView {
  return { ...r, url_resolved: r.url, votes: 0, clickcount: 0 }
}

const subline = computed(() => {
  if (!favorites.value.length) return ''
  return `${favorites.value.length} station${favorites.value.length === 1 ? '' : 's'} saved`
})
</script>

<style scoped>
.rf-page { padding-bottom: 80px; }
.rf-head { margin-bottom: 24px; }
.rf-back { color: var(--fg-3); font-size: 12px; text-decoration: none; display: inline-flex; align-items: center; gap: 4px; }
.rf-back:hover { color: var(--gold); }
.rf-title { font-size: 28px; font-weight: 700; margin-top: 6px; letter-spacing: -0.01em; }
.rf-sub { color: var(--fg-3); font-size: 13px; margin-top: 4px; }
.rf-loading { color: var(--fg-3); padding: 24px 0; font-size: 13px; }
.rf-empty {
  display: flex; flex-direction: column; align-items: center; gap: 8px;
  padding: 60px 0; text-align: center; color: var(--fg-2);
}
.rf-empty h3 { font-size: 16px; color: var(--fg-1); margin-top: 4px; }
.rf-empty p { font-size: 13px; color: var(--fg-3); max-width: 360px; margin-bottom: 12px; }
.rf-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(170px, 1fr));
  gap: 18px;
}

@media (max-width: 720px) {
  .rf-grid { grid-template-columns: repeat(auto-fill, minmax(110px, 1fr)); gap: 12px; }
  .page-pad { padding-left: 16px; padding-right: 16px; }
}
</style>
