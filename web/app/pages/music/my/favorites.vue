<template>
  <div class="ms-fav page-pad">
    <MusicPageHead title="My Favorites">
      <template #subtitle>
        <span>Everything you've reacted to. Tap 👍 or ❤️ anywhere — it shows up here.</span>
        <span class="dot">·</span>
        <span>{{ (total ?? 0).toLocaleString() }} tracks</span>
      </template>
    </MusicPageHead>

    <!-- Reaction-band selector: default view is thumbs-up and higher. -->
    <div class="ms-fav-controls">
      <div class="ms-fav-toggle">
        <button
          v-for="band in BANDS"
          :key="band.key"
          type="button"
          class="ms-fav-toggle-btn steer-glass"
          :class="{ active: view === band.key }"
          :aria-pressed="view === band.key"
          @click="view = band.key"
        >
          <Icon :name="band.icon" :size="13" />
          {{ band.label }}
        </button>
      </div>
    </div>

    <div v-if="isLoading" class="ms-fav-loading">Loading…</div>

    <MusicEmptyState v-else-if="!total" icon="heart" :title="emptyTitle">
      {{ emptyBody }} React from the <NuxtLink to="/music/songs">Songs page</NuxtLink>,
      the player, or anywhere you see the reactions.
    </MusicEmptyState>

    <!-- Sparse full-length list per reaction band — each band pages its own
         random-access catalog server-side (min/max rating), so the scrollbar
         spans the whole band and the 500-cap is gone. -->
    <TrackList
      v-else
      :tracks="tlRows"
      :columns="columns"
      grid-template-columns="28px 44px 1fr 130px 60px"
      :show-header="false"
      :context-items="contextItemsFor"
      :art-play-icon-size="13"
      :on-rating-change="onRatingChange"
      virtualized
      @row-click="playFrom"
      @range="ensureRange"
    />
  </div>
</template>

<script setup lang="ts">
import type { Track } from '~/composables/usePlayer'
import type { TrackListColumn, TrackListRow } from '~/components/music/TrackList.vue'

definePageMeta({ layout: 'default' })

const columns: TrackListColumn[] = [
  { key: 'idx', kind: 'index' },
  { key: 'art', kind: 'art' },
  { key: 'title', kind: 'title', subtitle: 'artist-album-year' },
  { key: 'rating', kind: 'rating' },
  { key: 'duration', kind: 'duration' },
]

const { play, queue, playTracks } = usePlayerBindings()
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

// Reaction bands over the stored 1–10 ratings — same cuts ReactionControl
// renders: down ≤3, up 6–8, heart ≥9. Default view = thumbs up and higher.
// Each band is a [min, max] the SERVER filters on: the list is random-access
// paged, so band membership can't be computed client-side any more.
type BandKey = 'positive' | 'heart' | 'up' | 'down'
const BANDS: { key: BandKey; label: string; icon: string; min: number; max: number }[] = [
  { key: 'positive', label: 'Liked & loved', icon: 'thumbsup', min: 6, max: 10 },
  { key: 'heart', label: 'Loved', icon: 'heart', min: 9, max: 10 },
  { key: 'up', label: 'Liked', icon: 'thumbsup', min: 6, max: 8 },
  { key: 'down', label: 'Not for me', icon: 'thumbsdown', min: 1, max: 3 },
]
const view = ref<BandKey>('positive')
const activeBand = computed(() => BANDS.find((b) => b.key === view.value)!)

const { total, pending: isLoading, itemAt, ensureRange, loadedItems, reset } = useVirtualCatalog<RatedTrackRow>(() => ({
  key: `me:rated:tracks:${activeBand.value.min}-${activeBand.value.max}`,
  pageSize: 100,
  fetch: async (offset, limit) => {
    const r = await $heya('/api/me/ratings/tracks', {
      query: { min_rating: activeBand.value.min, max_rating: activeBand.value.max, limit, offset },
    }) as unknown as ListBody
    const items = r.items ?? []
    trackRatings.primeMany(items.map((it) => [it.track_id, it.rating] as [number, number]))
    return { items, total: r.total ?? 0 }
  },
}))

const emptyTitle = computed(() => {
  switch (view.value) {
    case 'down': return 'Nothing marked "not for me"'
    case 'heart': return 'Nothing loved yet'
    case 'up': return 'Nothing liked yet'
    default: return 'No reactions yet'
  }
})
const emptyBody = computed(() =>
  view.value === 'down'
    ? 'Thumbs-down anything you never want mixed in again.'
    : 'Like or love a few tracks and your taste profile starts learning.')

// Sparse full-length rows — unloaded stretches render as skeletons.
const tlRows = computed<TrackListRow[]>(() => {
  const n = total.value ?? 0
  const out: TrackListRow[] = new Array(n)
  for (let i = 0; i < n; i++) {
    const t = itemAt(i)
    out[i] = t
      ? {
          id: t.track_id,
          title: t.track_title,
          artist: t.artist_name,
          artist_slug: t.artist_slug,
          album: t.album_title,
          album_slug: t.album_slug,
          album_year: t.album_year,
          duration: t.duration,
          available: t.available,
          poster: useAlbumCoverUrl(t.artist_slug, t.album_slug),
          rating: ratings.value.get(t.track_id) ?? t.rating,
        }
      : { id: -(i + 1), pending: true, title: '', artist: '', album: '', duration: 0 }
  }
  return out
})

function contextItemsFor(_track: TrackListRow, i: number) {
  const t = itemAt(i)
  if (!t) return []
  return actions.forTrack({ id: t.track_id, title: t.track_title, artist: t.artist_name, album: t.album_title, duration: t.duration, album_id: t.album_id, artist_id: t.artist_id, artist_slug: t.artist_slug, album_slug: t.album_slug, available: t.available })
}

async function onRatingChange(trackId: number, v: number) {
  try {
    await trackRatings.set(trackId, v)
    // Any reaction change can move the row out of the current band — reset
    // the band's catalog so indexes/total stay honest.
    reset()
  } catch {
    // optimistic rollback already happened in useTrackRatings
  }
}

async function playFrom(i: number) {
  const clicked = itemAt(i)
  if (!clicked || clicked.available === false) return
  // Queue every LOADED playable track in band order — the pages the user has
  // actually scrolled through.
  const built: Track[] = loadedItems()
    .map(({ item }) => item)
    .filter((t) => t.available !== false)
    .map((t) => ({
      id: t.track_id, title: t.track_title, artist: t.artist_name, album: t.album_title, duration: t.duration,
      stream_url: `/api/music/tracks/${t.track_id}/stream`,
      album_id: t.album_id, artist_id: t.artist_id, artist_slug: t.artist_slug, album_slug: t.album_slug,
      poster: useAlbumCoverUrl(t.artist_slug, t.album_slug) ?? undefined,
      source: 'favorites',
      available: t.available,
    }))
  if (!built.length) return
  await playTracks(built, built.find((b) => b.id === clicked.track_id))
}
</script>

<style scoped>
.ms-fav { max-width: 1300px; }

.dot { opacity: 0.4; }

.ms-fav-controls {
  display: flex; align-items: center; gap: 24px;
  margin-bottom: 28px;
  padding: 10px 12px;
  background: color-mix(in oklab, var(--bg-2) 85%, transparent);
  -webkit-backdrop-filter: blur(12px);
  backdrop-filter: blur(12px);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  box-shadow: var(--shadow-el);
}

.ms-fav-toggle {
  display: flex; gap: 2px;
  padding: 3px;
  background: rgb(var(--ink) / 0.04);
  border-radius: var(--r-sm);
}
.ms-fav-toggle-btn {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 6px 14px;
  border-radius: 4px;
  color: var(--fg-2);
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.15s;
}
.ms-fav-toggle-btn.active { background: var(--gold-soft); color: var(--gold); }

.ms-fav-loading {
  color: var(--fg-2);
  text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1);
  font-size: 13px; padding: 40px 0; text-align: center;
}

/* Deltas from TrackList's songs.vue-shaped baseline — see loved.vue for the
   same pattern. This page never had an active-row treatment (no
   activeTrackId passed above), so no tint override is needed here. */
:deep(.tl-body) { gap: 2px; }
:deep(.tl-c-art) { width: 44px; height: 44px; }
:deep(.tl-c-index) { font-size: 11px; }
:deep(.tl-c-duration) { font-size: 11px; letter-spacing: 0.04em; }
</style>
