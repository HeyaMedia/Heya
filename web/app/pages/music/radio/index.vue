<template>
  <div class="page-pad radio-page">
    <MusicPageHead title="Internet Radio" :subtitle="stationCountSub" />
    <div class="ri-search">
      <Icon name="search" :size="16" class="ri-search-icon" />
      <input
        v-model="searchQuery"
        type="search"
        class="ri-search-input"
        placeholder="Search by name, tag, country…"
        aria-label="Search radio stations"
        @keydown.enter="runSearch"
      />
      <button v-if="searchQuery" class="ri-search-clear" @click="searchQuery = ''; searchResults = []">
        <Icon name="close" :size="14" />
      </button>
    </div>

    <!-- Discovery strip — quick access to the new browse surfaces.
         Hidden during search so results aren't pushed below the fold. -->
    <nav v-if="!searchResults.length" class="ri-strip">
      <NuxtLink to="/music/radio/countries" class="ri-strip-tile steer-glass">
        <Icon name="globe" :size="20" />
        <div>
          <div class="ri-strip-title">Browse by Country</div>
          <div class="ri-strip-sub">All 200+ countries with stations</div>
        </div>
      </NuxtLink>
      <NuxtLink to="/music/radio/tags" class="ri-strip-tile steer-glass">
        <Icon name="tag" :size="20" />
        <div>
          <div class="ri-strip-title">Browse by Tag</div>
          <div class="ri-strip-sub">Genres, eras, moods</div>
        </div>
      </NuxtLink>
      <NuxtLink to="/music/radio/favorites" class="ri-strip-tile steer-glass">
        <Icon name="heart" :size="20" />
        <div>
          <div class="ri-strip-title">Favorites</div>
          <div class="ri-strip-sub">Your saved stations</div>
        </div>
      </NuxtLink>
      <NuxtLink to="/music/radio/recents" class="ri-strip-tile steer-glass">
        <Icon name="clock" :size="20" />
        <div>
          <div class="ri-strip-title">Recently Played</div>
          <div class="ri-strip-sub">Last 30 stations</div>
        </div>
      </NuxtLink>
    </nav>

    <!-- Search results take over the grid when a query is active. -->
    <section v-if="searchResults.length" class="ri-section">
      <h2 class="section-title-lg">Results for "{{ lastSearch }}"</h2>
      <div class="ri-grid">
        <RadioStationCard
          v-for="s in searchResults"
          :key="`search-${s.stationuuid}`"
          :station="s"
          :favorited="radio.isFavorited(s.stationuuid)"
          :loading="radio.loadingStationUUID.value === s.stationuuid"
          @play="radio.playStation"
          @toggle-favorite="radio.toggleFavorite"
        />
      </div>
    </section>

    <template v-else>
      <!-- Favorites — only when the user has any. -->
      <section v-if="favorites.length" class="ri-section">
        <h2 class="section-title-lg">Your Favorites</h2>
        <div class="ri-grid">
          <RadioStationCard
            v-for="s in favorites"
            :key="`fav-${s.stationuuid}`"
            :station="favoriteToStation(s)"
            :favorited="true"
            :loading="radio.loadingStationUUID.value === s.stationuuid"
            @play="radio.playStation"
            @toggle-favorite="radio.toggleFavorite"
          />
        </div>
      </section>

      <!-- Recently played — also only when populated. -->
      <section v-if="recents.length" class="ri-section">
        <h2 class="section-title-lg">Recently Played</h2>
        <div class="ri-grid">
          <RadioStationCard
            v-for="s in recents"
            :key="`recent-${s.stationuuid}-${s.played_at}`"
            :station="recentToStation(s)"
            :favorited="radio.isFavorited(s.stationuuid)"
            :loading="radio.loadingStationUUID.value === s.stationuuid"
            @play="radio.playStation"
            @toggle-favorite="radio.toggleFavorite"
          />
        </div>
      </section>

      <!-- Trending — the always-on default content for fresh users. -->
      <section class="ri-section">
        <h2 class="section-title-lg">Most Popular</h2>
        <div v-if="topPending" class="ri-loading">Loading…</div>
        <div v-else class="ri-grid">
          <RadioStationCard
            v-for="s in topStations"
            :key="`top-${s.stationuuid}`"
            :station="s"
            :favorited="radio.isFavorited(s.stationuuid)"
            :loading="radio.loadingStationUUID.value === s.stationuuid"
            @play="radio.playStation"
            @toggle-favorite="radio.toggleFavorite"
          />
        </div>
      </section>

      <!-- Tags — clicking a tag jumps into a tag-filtered search. -->
      <section v-if="popularTags.length" class="ri-section">
        <h2 class="section-title-lg">Browse by Tag</h2>
        <div class="ri-tags">
          <button
            v-for="t in popularTags"
            :key="t.name"
            class="ri-tag steer-glass"
            @click="searchByTag(t.name)"
          >
            {{ t.name }}
            <span class="ri-tag-count mono">{{ t.stationcount.toLocaleString() }}</span>
          </button>
        </div>
      </section>
    </template>
  </div>
</template>

<script setup lang="ts">
import type { RadioStationView } from '~/composables/useRadio'
import { useQuery } from '@pinia/colada'

definePageMeta({ layout: 'default' })

const radio = useRadioActions()
const { ensureSubscribed } = useRadioNowPlaying()
if (import.meta.client) {
  ensureSubscribed()
  radio.ensureFavoritesLoaded()
}

const { $heya } = useNuxtApp()

const topQuery = useQuery({
  key: ['radio', 'top', { category: 'topvote', count: 30 }],
  query: async () => ((await $heya('/api/radio/top', { query: { category: 'topvote', count: 30 } })) as { items: RadioStationView[] }).items ?? [],
  staleTime: 1000 * 60 * 10,
})
const topStations = computed<RadioStationView[]>(() => topQuery.data.value ?? [])
const topPending = computed(() => topQuery.isPending.value)

const tagsQuery = useQuery({
  key: ['radio', 'tags', { limit: 40 }],
  query: async () => ((await $heya('/api/radio/tags', { query: { limit: 40 } })) as { items: Array<{ name: string; stationcount: number }> }).items ?? [],
  staleTime: 1000 * 60 * 60,
})
const popularTags = computed(() => tagsQuery.data.value ?? [])

interface FavoriteRow {
  stationuuid: string; name: string; url: string; favicon: string; homepage: string
  country: string; countrycode: string; language: string; tags: string; codec: string; bitrate: number
}
interface RecentRow {
  stationuuid: string; name: string; url: string; favicon: string
  country: string; tags: string; codec: string; bitrate: number
  played_at: string
}

const favoritesQuery = useQuery({
  key: ['me', 'radio', 'favorites'],
  query: async () => ((await $heya('/api/me/radio/favorites')) as { items: FavoriteRow[] }).items ?? [],
  staleTime: 1000 * 30,
})
const favorites = computed<FavoriteRow[]>(() => favoritesQuery.data.value ?? [])

const recentsQuery = useQuery({
  key: ['me', 'radio', 'recents', { limit: 18 }],
  query: async () => ((await $heya('/api/me/radio/recents', { query: { limit: 18 } })) as { items: RecentRow[] }).items ?? [],
  staleTime: 1000 * 30,
})
await Promise.all([
  waitForQuery(topQuery),
  waitForQuery(tagsQuery),
  waitForQuery(favoritesQuery),
  waitForQuery(recentsQuery),
])
const recents = computed<RecentRow[]>(() => recentsQuery.data.value ?? [])

function favoriteToStation(f: FavoriteRow): RadioStationView {
  return { ...f, url_resolved: f.url, votes: 0, clickcount: 0 }
}
function recentToStation(r: RecentRow): RadioStationView {
  return {
    stationuuid: r.stationuuid, name: r.name, url: r.url, url_resolved: r.url,
    favicon: r.favicon, country: r.country, tags: r.tags, codec: r.codec,
    bitrate: r.bitrate, homepage: '', countrycode: '', language: '',
    votes: 0, clickcount: 0,
  }
}

// Search results aren't in Pinia Colada — they're transient, user-typed,
// and only fire on explicit Enter. Simpler to keep as a local ref.
const searchQuery = ref('')
const lastSearch = ref('')
const searchResults = ref<RadioStationView[]>([])

async function runSearch() {
  const q = searchQuery.value.trim()
  if (!q) { searchResults.value = []; return }
  lastSearch.value = q
  try {
    const res = await $heya('/api/radio/search', { query: { name: q, limit: 60 } }) as { items: RadioStationView[] }
    searchResults.value = res.items ?? []
  } catch { searchResults.value = [] }
}

async function searchByTag(tag: string) {
  searchQuery.value = tag
  lastSearch.value = tag
  try {
    const res = await $heya('/api/radio/search', { query: { tag, limit: 60 } }) as { items: RadioStationView[] }
    searchResults.value = res.items ?? []
  } catch { searchResults.value = [] }
}

const stationCountSub = computed(() => {
  if (searchResults.value.length) return `${searchResults.value.length} match${searchResults.value.length === 1 ? '' : 'es'}`
  return 'Tens of thousands of live stations from around the world.'
})
</script>

<style scoped>
.radio-page { padding-bottom: 80px; }

.ri-search {
  position: relative;
  display: flex;
  align-items: center;
  max-width: 720px;
  margin-bottom: 28px;
}
.ri-search-icon { position: absolute; left: 14px; color: var(--fg-3); }
.ri-search-input {
  width: 100%;
  padding: 12px 36px 12px 38px;
  font-size: 14px;
  background: color-mix(in oklab, var(--bg-2) 82%, transparent);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  border: 1px solid var(--border);
  box-shadow: var(--shadow-el);
  border-radius: var(--r-md);
  color: var(--fg-0);
  outline: none;
  transition: border-color 0.15s;
}
.ri-search-input:focus { border-color: var(--gold); }
.ri-search-clear {
  position: absolute;
  right: 10px;
  background: transparent;
  border: 0;
  color: var(--fg-3);
  padding: 6px;
  cursor: pointer;
  border-radius: var(--r-sm);
}
.ri-search-clear:hover { color: var(--gold); }

.ri-strip {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 10px;
  margin-bottom: 36px;
}
.ri-strip-tile {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
  border-radius: var(--r-md);
  text-decoration: none;
  color: inherit;
  transition: border-color 0.15s, background 0.15s, transform 0.15s;
}
.ri-strip-tile:hover {
  border-color: color-mix(in srgb, var(--gold) 40%, transparent);
  transform: translateY(-1px);
}
.ri-strip-tile > :first-child { color: var(--gold); flex-shrink: 0; }
.ri-strip-title { font-size: 13px; font-weight: 600; color: var(--fg-0); }
.ri-strip-sub { font-size: 11px; color: var(--fg-3); margin-top: 2px; }

.ri-section { margin-bottom: 36px; }
.ri-section .section-title-lg { margin-bottom: 16px; }

.ri-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(170px, 1fr));
  gap: 18px;
}
/* Bare-on-ambient status text — halo so it survives the bright pool backdrop. */
.ri-loading { color: var(--fg-3); padding: 20px 0; text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1); }

.ri-tags { display: flex; flex-wrap: wrap; gap: 8px; }
.ri-tag {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 12px;
  border-radius: 999px;
  font-size: 12px;
  color: var(--fg-1);
  cursor: pointer;
  transition: background 0.15s, border-color 0.15s, color 0.15s;
  text-transform: capitalize;
  font-family: inherit;
}
.ri-tag:hover {
  border-color: color-mix(in srgb, var(--gold) 30%, transparent);
}
.ri-tag-count {
  color: var(--fg-3);
  font-size: 10px;
  font-family: var(--font-mono);
}
.mono { font-family: var(--font-mono); }

@media (pointer: coarse) {
  .ri-tag { min-height: 44px; }
}

@media (max-width: 720px) {
  /* music.vue's phone header already reads "Internet Radio" directly
     above this page — the live sub line + search box both stay. */
  :deep(.mhd-title) { display: none; }
  :deep(.mhd) { margin-bottom: 20px; }
  .ri-grid { grid-template-columns: repeat(auto-fill, minmax(110px, 1fr)); gap: 12px; }
  .page-pad { padding-left: 16px; padding-right: 16px; }
}
</style>
