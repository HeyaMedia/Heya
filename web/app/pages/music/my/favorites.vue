<template>
  <div class="ms-fav page-pad">
    <MusicPageHead title="My Favorites">
      <template #subtitle>
        <span>Everything you've reacted to. Tap 👍 or ❤️ anywhere — it shows up here.</span>
        <span class="dot">·</span>
        <span>{{ tracks.length.toLocaleString() }} tracks</span>
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

    <div v-if="isLoading && !tracks.length" class="ms-fav-loading">Loading…</div>

    <MusicEmptyState v-else-if="!tracks.length" icon="heart" :title="emptyTitle">
      {{ emptyBody }} React from the <NuxtLink to="/music/songs">Songs page</NuxtLink>,
      the player, or anywhere you see the reactions.
    </MusicEmptyState>

    <TrackList
      v-else
      :tracks="tlRows"
      :columns="columns"
      grid-template-columns="28px 44px 1fr 130px 60px"
      :show-header="false"
      :context-items="contextItemsFor"
      :art-play-icon-size="13"
      :on-rating-change="onRatingChange"
      @row-click="playFrom"
    />
  </div>
</template>

<script setup lang="ts">
import type { Track } from '~/composables/usePlayer'
import type { TrackListColumn, TrackListRow } from '~/components/music/TrackList.vue'
import { useQuery } from '@pinia/colada'

definePageMeta({ layout: 'default' })

const columns: TrackListColumn[] = [
  { key: 'idx', kind: 'index' },
  { key: 'art', kind: 'art' },
  { key: 'title', kind: 'title', subtitle: 'artist-album-year' },
  { key: 'rating', kind: 'rating' },
  { key: 'duration', kind: 'duration' },
]

const { play, queue } = usePlayerBindings()
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
type BandKey = 'positive' | 'heart' | 'up' | 'down'
const BANDS: { key: BandKey; label: string; icon: string; match: (r: number) => boolean }[] = [
  { key: 'positive', label: 'Liked & loved', icon: 'thumbsup', match: (r) => r >= 6 },
  { key: 'heart', label: 'Loved', icon: 'heart', match: (r) => r >= 9 },
  { key: 'up', label: 'Liked', icon: 'thumbsup', match: (r) => r >= 6 && r <= 8 },
  { key: 'down', label: 'Not for me', icon: 'thumbsdown', match: (r) => r >= 1 && r <= 3 },
]
const view = ref<BandKey>('positive')
const activeBand = computed(() => BANDS.find((b) => b.key === view.value)!)

// One fetch of everything rated; band filtering is client-side (the list is
// capped at 500 — plenty until real pagination is warranted).
const ratedQuery = useQuery({
  key: ['me', 'ratings', 'reactions-list'],
  query: async () => {
    const r = await $heya('/api/me/ratings/tracks', {
      query: { min_rating: 1, limit: 500 },
    }) as unknown as ListBody
    trackRatings.primeMany(r.items.map((it) => [it.track_id, it.rating] as [number, number]))
    return r
  },
  staleTime: 1000 * 30,
})
await waitForQuery(ratedQuery)

const allRated = computed<RatedTrackRow[]>(() => ratedQuery.data.value?.items ?? [])
const isLoading = computed(() => ratedQuery.isLoading.value)

// Live-filter through the shared ratings cache so a reaction change moves the
// row between bands without a refetch.
const tracks = computed<RatedTrackRow[]>(() =>
  allRated.value.filter((t) => activeBand.value.match(ratings.value.get(t.track_id) ?? t.rating)))

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

const tlRows = computed<TrackListRow[]>(() => tracks.value.map((t) => ({
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
})))

function contextItemsFor(_track: TrackListRow, i: number) {
  const t = tracks.value[i]!
  return actions.forTrack({ id: t.track_id, title: t.track_title, artist: t.artist_name, album: t.album_title, duration: t.duration, album_id: t.album_id, artist_id: t.artist_id, artist_slug: t.artist_slug, album_slug: t.album_slug, available: t.available })
}

async function onRatingChange(trackId: number, v: number) {
  try {
    await trackRatings.set(trackId, v)
    if (v > 0 && !allRated.value.some((t) => t.track_id === trackId)) {
      ratedQuery.refetch() // newly rated — pull it into the base list
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
