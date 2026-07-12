<template>
  <div class="ml page-pad">
    <MusicPageHead title="Loved Songs">
      <template #subtitle>
        <span>Every track you've hearted. Tap the heart on any track to add it — tap again to remove.</span>
        <span class="dot">·</span>
        <span>{{ (total ?? 0).toLocaleString() }} tracks</span>
      </template>
    </MusicPageHead>

    <div v-if="pending" class="ml-loading">Loading…</div>

    <div v-else-if="!total" class="ml-empty">
      <Icon name="star" :size="40" />
      <h3>No rated tracks yet</h3>
      <p>Heart a track from the <NuxtLink to="/music/songs">Songs page</NuxtLink>, the player, or an album page. It'll appear here as soon as you love something.</p>
    </div>

    <!-- Sparse full-length list — the scrollbar spans every loved track;
         pages stream in wherever it's dragged (500-cap gone). -->
    <TrackList
      v-else
      :tracks="tlRows"
      :columns="columns"
      grid-template-columns="32px 44px 1fr minmax(160px, 1.2fr) 130px 60px"
      :show-header="false"
      :context-items="contextItemsFor"
      :active-track-id="activeTrackId"
      :playing="playing"
      vu-meter-in="art"
      :art-play-icon-size="13"
      :duration-formatter="formatTime"
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
  { key: 'title', kind: 'title', subtitle: 'artist-link' },
  { key: 'album', kind: 'album' },
  { key: 'rating', kind: 'rating' },
  { key: 'duration', kind: 'duration' },
]

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

const { play, queue, currentTrack, playing, formatTime } = usePlayerBindings()
const { $heya } = useNuxtApp()
const trackRatings = useTrackRatings()
const ratings = trackRatings.ratings
const actions = useMusicActions()

const { total, pending, itemAt, ensureRange, loadedItems, reset } = useVirtualCatalog<RatedTrackRow>(() => ({
  key: 'me:rated:tracks:loved',
  pageSize: 100,
  fetch: async (offset, limit) => {
    const r = await $heya('/api/me/ratings/tracks', {
      query: { min_rating: 9, limit, offset },
    }) as unknown as { items: RatedTrackRow[]; total: number }
    const items = r.items ?? []
    trackRatings.primeMany(items.map((t) => [t.track_id, t.rating] as [number, number]))
    return { items, total: r.total ?? 0 }
  },
}))

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

const activeTrackId = computed(() => currentTrack.value?.id ?? null)

async function onRatingChange(trackId: number, v: number) {
  try {
    await trackRatings.set(trackId, v)
    // Dropping below the loved band removes the row — reset the catalog so
    // indexes/total stay honest rather than leaving a hole.
    if (v < 9) reset()
  } catch {
    // optimistic rollback handled by composable
  }
}

function toPlayable(row: RatedTrackRow): Track {
  return {
    id: row.track_id,
    title: row.track_title,
    artist: row.artist_name,
    album: row.album_title,
    duration: row.duration,
    stream_url: `/api/music/tracks/${row.track_id}/stream`,
    album_id: row.album_id,
    artist_id: row.artist_id,
    poster: useAlbumCoverUrl(row.artist_slug, row.album_slug) ?? undefined,
    source: 'loved',
    available: row.available,
  }
}

async function playFrom(i: number) {
  const clicked = itemAt(i)
  if (!clicked || clicked.available === false) return
  // Queue every LOADED playable track in list order — the pages the user has
  // actually scrolled through.
  const built = loadedItems()
    .map(({ item }) => item)
    .filter((r) => r.available !== false)
    .map(toPlayable)
  if (!built.length) return
  queue.value = built
  await play(built.find((b) => b.id === clicked.track_id) ?? built[0]!)
}
</script>

<style scoped>
.ml { max-width: 1300px; }

.dot { opacity: 0.4; }

.ml-loading { color: var(--fg-3); font-size: 13px; padding: 40px 0; text-align: center; }
.ml-empty {
  text-align: center; padding: 80px 20px; color: var(--fg-3);
}
.ml-empty :deep(svg) { color: var(--fg-3); margin-bottom: 12px; }
.ml-empty h3 { font-size: 18px; color: var(--fg-1); margin-bottom: 8px; font-weight: 600; }
.ml-empty p { font-size: 13px; line-height: 1.6; max-width: 440px; margin: 0 auto; }
.ml-empty a { color: var(--gold); text-decoration: none; }
.ml-empty a:hover { text-decoration: underline; }

/* TrackList's baseline CSS matches music/songs.vue exactly (48px art, 12px
   index, 1px list gap, gold-tinted index on the active row, no duration
   letter-spacing) — this page's numbers differ in a handful of spots, so
   layer the deltas on via :deep() rather than duplicating the whole table.
   TrackList isn't portaled, so scoped :deep() reaches its internals fine
   (docs/ui.md gotcha #2 only applies to portaled content). */
:deep(.tl-body) { gap: 2px; }
:deep(.tl-c-art) { width: 44px; height: 44px; }
:deep(.tl-c-index) { font-size: 11px; }
:deep(.tl-c-duration) { font-size: 11px; letter-spacing: 0.04em; }
/* songs.vue tints its index column gold on the active row; loved.vue never did. */
:deep(.tl-track.tl-active .tl-c-index) { color: var(--fg-3); }
</style>
