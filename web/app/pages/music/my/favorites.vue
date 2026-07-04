<template>
  <div class="ms-fav page-pad">
    <header class="ms-fav-head">
      <div>
        <h1 class="ms-fav-title">My Favorites</h1>
        <div class="ms-fav-sub">Tracks you've rated above your threshold. Rate any track from anywhere — they'll show up here.</div>
      </div>
      <div class="ms-fav-meta">
        <div class="ms-fav-count">{{ totalFavorites.toLocaleString() }}</div>
        <div class="ms-fav-count-lbl">favorites</div>
      </div>
    </header>

    <!-- Threshold control -->
    <div class="ms-fav-controls">
      <div class="ms-fav-threshold">
        <label class="ms-fav-label">Favorites threshold</label>
        <StarRating :model-value="threshold" @update:model-value="setThreshold" size="md" />
        <span class="ms-fav-threshold-val">{{ thresholdStars }}★</span>
      </div>
      <div class="ms-fav-toggle">
        <button
          type="button"
          class="ms-fav-toggle-btn"
          :class="{ active: viewMode === 'favorites' }"
          @click="viewMode = 'favorites'"
        >Favorites only</button>
        <button
          type="button"
          class="ms-fav-toggle-btn"
          :class="{ active: viewMode === 'all' }"
          @click="viewMode = 'all'"
        >All rated</button>
      </div>
    </div>

    <div v-if="isLoading && !tracks.length" class="ms-fav-loading">Loading favorites…</div>

    <div v-else-if="!tracks.length" class="ms-fav-empty">
      <Icon name="star" :size="40" />
      <h3>No ratings yet</h3>
      <p>
        Rate a track from the <NuxtLink to="/music/songs">Songs page</NuxtLink>,
        the player, or anywhere you see <strong>★★★★★</strong>. They'll show up here
        once they pass your threshold.
      </p>
    </div>

    <ul v-else class="ms-fav-list">
      <AppContextMenu
        v-for="(t, i) in tracks"
        :key="`fav-${t.track_id}`"
        :items="actions.forTrack({ id: t.track_id, title: t.track_title, artist: t.artist_name, album: t.album_title, duration: t.duration, album_id: t.album_id, artist_id: t.artist_id, artist_slug: t.artist_slug, album_slug: t.album_slug, available: t.available })"
      >
      <li
        class="ms-fav-row"
        :class="{ 'ms-fav-missing': t.available === false }"
        @click="t.available !== false && playFrom(i)"
      >
        <div class="ms-fav-idx">{{ i + 1 }}</div>
        <div class="ms-fav-art">
          <img :src="useAlbumCoverUrl(t.artist_slug, t.album_slug) ?? ''" :alt="t.album_title" loading="lazy" />
          <div v-if="t.available !== false" class="ms-fav-play"><Icon name="play" :size="13" /></div>
          <div v-else class="ms-fav-play ms-fav-play-missing" title="Missing on disk"><Icon name="trash" :size="13" /></div>
        </div>
        <div class="ms-fav-meta-col">
          <div class="ms-fav-title-cell">{{ t.track_title }}</div>
          <div class="ms-fav-sub-cell">{{ t.artist_name }} · {{ t.album_title }}{{ t.album_year ? ' · ' + t.album_year : '' }}</div>
        </div>
        <div class="ms-fav-rating-cell" @click.stop>
          <StarRating
            :model-value="ratings.get(t.track_id) ?? t.rating"
            size="sm"
            @update:model-value="(v) => onRatingChange(t.track_id, v)"
          />
        </div>
        <div class="ms-fav-dur">{{ formatDuration(t.duration) }}</div>
      </li>
      </AppContextMenu>
    </ul>
  </div>
</template>

<script setup lang="ts">
import type { Track } from '~/composables/usePlayer'
import { useQuery } from '@tanstack/vue-query'

definePageMeta({ layout: 'default' })

const { play, queue } = usePlayer()
const { $heya } = useNuxtApp()
const trackRatings = useTrackRatings()
const ratings = trackRatings.ratings
const actions = useMusicActions()

interface RatedTrackRow {
  track_id: number
  track_title: string
  duration: number
  album_id: number
  album_title: string
  album_slug: string
  album_year: string
  artist_id: number
  artist_name: string
  artist_slug: string
  rating: number
  available?: boolean
}
interface ListBody { items: RatedTrackRow[]; total: number }

// View mode: filter by threshold or show every rated track
const viewMode = ref<'favorites' | 'all'>('favorites')

// Threshold (1..10) lives server-side per user
const thresholdQuery = useQuery({
  queryKey: ['me', 'ratings', 'threshold'],
  queryFn: async () => (await $heya('/api/me/ratings/threshold')) as unknown as { rating: number },
  staleTime: 1000 * 60,
})
const threshold = computed(() => thresholdQuery.data.value?.rating ?? 7)
const thresholdStars = computed(() => (threshold.value / 2).toFixed(1).replace(/\.0$/, ''))

async function setThreshold(v: number) {
  if (v < 1 || v > 10) return
  await $heya('/api/me/ratings/threshold', { method: 'PUT', body: { rating: v } })
  thresholdQuery.refetch()
  ratedQuery.refetch()
}

const effectiveMin = computed(() => (viewMode.value === 'favorites' ? threshold.value : 1))

// Paginated rated-tracks list — keyed on threshold + mode so the filter
// re-runs when either changes.
const ratedQuery = useQuery({
  queryKey: ['me', 'ratings', 'list', effectiveMin] as const,
  queryFn: async () => {
    const r = await $heya('/api/me/ratings/tracks', {
      query: { min_rating: effectiveMin.value, limit: 200 },
    }) as unknown as ListBody
    // Prime the shared rating cache from the response.
    trackRatings.primeMany(r.items.map((it) => [it.track_id, it.rating] as [number, number]))
    return r
  },
  staleTime: 1000 * 30,
})

const tracks = computed<RatedTrackRow[]>(() => ratedQuery.data.value?.items ?? [])
const totalFavorites = computed(() => ratedQuery.data.value?.total ?? 0)
const isLoading = computed(() => ratedQuery.isLoading.value)

async function onRatingChange(trackId: number, v: number) {
  try {
    await trackRatings.set(trackId, v)
    // If the new rating no longer satisfies the filter, re-fetch so the row
    // disappears (or appears) without the user wondering why nothing happened.
    if ((v === 0 && viewMode.value === 'favorites')
      || (v > 0 && v < effectiveMin.value && viewMode.value === 'favorites')) {
      ratedQuery.refetch()
    }
  } catch {
    // optimistic rollback already happened in useTrackRatings
  }
}

async function playFrom(i: number) {
  const clicked = tracks.value[i]
  if (!clicked || clicked.available === false) return
  const built: Track[] = tracks.value.filter((t) => t.available !== false).map((t) => ({
    id: t.track_id, title: t.track_title, artist: t.artist_name, album: t.album_title, duration: t.duration,
    stream_url: `/api/music/tracks/${t.track_id}/stream`,
    album_id: t.album_id, artist_id: t.artist_id, artist_slug: t.artist_slug, album_slug: t.album_slug,
    poster: useAlbumCoverUrl(t.artist_slug, t.album_slug) ?? undefined,
    source: 'favorites',
    available: t.available,
  }))
  if (!built.length) return
  queue.value = built
  await play(built.find((b) => b.id === clicked.track_id) ?? built[0]!)
}
</script>

<style scoped>
.ms-fav { max-width: 1300px; }

.ms-fav-head {
  display: flex; align-items: flex-end; justify-content: space-between; gap: 32px;
  margin-bottom: 24px;
}
.ms-fav-title { font-size: 30px; font-weight: 700; letter-spacing: -0.01em; }
.ms-fav-sub { color: var(--fg-3); font-size: 13px; margin-top: 4px; max-width: 540px; }
.ms-fav-meta { text-align: right; }
.ms-fav-count {
  font-size: 28px; font-weight: 700;
  color: var(--gold);
}
.ms-fav-count-lbl {
  font-size: 10px; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.1em;
  color: var(--fg-3);
  margin-top: -2px;
}

.ms-fav-controls {
  display: flex; align-items: center; justify-content: space-between; gap: 24px;
  margin-bottom: 28px;
  padding: 14px 18px;
  background: rgba(255,255,255,0.03);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.ms-fav-threshold { display: flex; align-items: center; gap: 12px; }
.ms-fav-label {
  font-size: 11px; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.1em;
  color: var(--fg-3);
}
.ms-fav-threshold-val {
  font-family: var(--font-mono); font-size: 13px;
  color: var(--gold); font-weight: 700;
}

.ms-fav-toggle {
  display: flex; gap: 2px;
  padding: 3px;
  background: rgba(255,255,255,0.04);
  border-radius: var(--r-sm);
}
.ms-fav-toggle-btn {
  padding: 6px 14px;
  background: transparent;
  border: 0;
  border-radius: 4px;
  color: var(--fg-2);
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.15s;
}
.ms-fav-toggle-btn:hover { color: var(--fg-0); }
.ms-fav-toggle-btn.active { background: var(--gold-soft); color: var(--gold); }

.ms-fav-loading { color: var(--fg-3); font-size: 13px; padding: 40px 0; text-align: center; }
.ms-fav-empty {
  text-align: center;
  padding: 80px 20px;
  color: var(--fg-3);
}
.ms-fav-empty :deep(svg) { color: var(--fg-3); margin-bottom: 12px; }
.ms-fav-empty h3 { font-size: 18px; color: var(--fg-1); margin-bottom: 8px; font-weight: 600; }
.ms-fav-empty p { font-size: 13px; line-height: 1.6; max-width: 440px; margin: 0 auto; }
.ms-fav-empty a { color: var(--gold); text-decoration: none; }
.ms-fav-empty a:hover { text-decoration: underline; }
.ms-fav-empty strong { color: var(--gold); font-weight: 700; }

.ms-fav-list { display: flex; flex-direction: column; gap: 2px; }
.ms-fav-row {
  display: grid;
  grid-template-columns: 28px 44px 1fr 130px 60px;
  gap: 12px;
  align-items: center;
  padding: 6px 10px;
  border-radius: var(--r-sm);
  cursor: pointer;
  transition: background 0.15s;
}
.ms-fav-row:hover { background: rgba(255,255,255,0.04); }
.ms-fav-idx { text-align: right; font-family: var(--font-mono); font-size: 11px; color: var(--fg-3); }
.ms-fav-art {
  position: relative;
  width: 44px; height: 44px;
  border-radius: 4px; overflow: hidden;
  background: var(--bg-3);
}
.ms-fav-art img { width: 100%; height: 100%; object-fit: cover; display: block; }
.ms-fav-play {
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center;
  background: rgba(0,0,0,0.55);
  color: #fff;
  opacity: 0;
  transition: opacity 0.15s;
}
.ms-fav-row:hover .ms-fav-play { opacity: 1; }
.ms-fav-play-missing { opacity: 1; color: #d96b6b; }
.ms-fav-missing { opacity: 0.5; cursor: default; }
.ms-fav-missing:hover { background: transparent; }
.ms-fav-meta-col { min-width: 0; }
.ms-fav-title-cell {
  font-size: 14px; font-weight: 500;
  color: var(--fg-0);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.ms-fav-sub-cell {
  font-size: 12px;
  color: var(--fg-3);
  margin-top: 2px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.ms-fav-rating-cell { display: flex; align-items: center; }
.ms-fav-dur {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--fg-3);
  letter-spacing: 0.04em;
  text-align: right;
}
</style>
