<template>
  <div class="ml page-pad">
    <MusicPageHead title="Loved Songs">
      <template #subtitle>
        <span>Every track you've hearted. Tap the heart on any track to add it — tap again to remove.</span>
        <span class="dot">·</span>
        <span>{{ total.toLocaleString() }} tracks</span>
      </template>
    </MusicPageHead>

    <div v-if="pending && !rows.length" class="ml-loading">Loading…</div>

    <div v-else-if="!rows.length" class="ml-empty">
      <Icon name="star" :size="40" />
      <h3>No rated tracks yet</h3>
      <p>Heart a track from the <NuxtLink to="/music/songs">Songs page</NuxtLink>, the player, or an album page. It'll appear here as soon as you love something.</p>
    </div>

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

const lovedQuery = useQuery({
  key: ['me', 'ratings', 'loved-list'],
  query: async () => {
    const r = await $heya('/api/me/ratings/tracks', { query: { min_rating: 9, limit: 500 } }) as unknown as { items: RatedTrackRow[]; total: number }
    trackRatings.primeMany(r.items.map((t) => [t.track_id, t.rating] as [number, number]))
    return r
  },
  staleTime: 1000 * 30,
})
await waitForQuery(lovedQuery)
const pending = computed(() => lovedQuery.isPending.value)
const rows = computed(() => lovedQuery.data.value?.items ?? [])
const total = computed(() => lovedQuery.data.value?.total ?? 0)

const tlRows = computed<TrackListRow[]>(() => rows.value.map((t) => ({
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
  const t = rows.value[i]!
  return actions.forTrack({ id: t.track_id, title: t.track_title, artist: t.artist_name, album: t.album_title, duration: t.duration, album_id: t.album_id, artist_id: t.artist_id, artist_slug: t.artist_slug, album_slug: t.album_slug, available: t.available })
}

const activeTrackId = computed(() => currentTrack.value?.id ?? null)

async function onRatingChange(trackId: number, v: number) {
  try {
    await trackRatings.set(trackId, v)
    // Clearing the rating drops the track out of this view; refetch so the
    // row disappears rather than lingering with empty stars.
    if (v === 0) lovedQuery.refetch()
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
  const clicked = rows.value[i]
  if (!clicked || clicked.available === false) return
  const built = rows.value.filter((r) => r.available !== false).map(toPlayable)
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
