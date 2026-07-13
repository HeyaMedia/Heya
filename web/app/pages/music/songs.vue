<template>
  <div class="ms-songs page-pad">
    <MusicPageHead title="All Songs">
      <template #subtitle>
        <span v-if="total !== null">{{ total.toLocaleString() }} tracks</span>
        <span v-else>Loading…</span>
      </template>
    </MusicPageHead>

    <div v-if="pending" class="ms-loading">Loading songs…</div>

    <!-- Sparse full-length list: TrackList's scroller is sized to the total
         track count, so the scrollbar spans the whole library — drag it to
         any point and the pages covering that window stream in as skeleton
         rows fill. Replaces the old Prev/Next 1000-per-page buttons. -->
    <TrackList
      v-else-if="(total ?? 0) > 0"
      :tracks="tlRows"
      :columns="columns"
      grid-template-columns="48px 56px 1fr minmax(160px, 1.5fr) 70px 120px 60px"
      :context-items="contextItemsFor"
      :active-track-id="activeTrackId"
      :on-rating-change="onRatingChange"
      virtualized
      @row-click="playFrom"
      @range="ensureRange"
    />

    <div v-else class="ms-empty">
      <Icon name="music" :size="40" />
      <h3>Your library is empty</h3>
      <p>Scan a music library from <NuxtLink to="/settings/libraries">Settings → Libraries</NuxtLink>.</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Track } from '~/composables/usePlayer'
import type { TrackListColumn, TrackListRow } from '~/components/music/TrackList.vue'

definePageMeta({ layout: 'default' })

const columns: TrackListColumn[] = [
  { key: 'idx', kind: 'index', label: '#' },
  { key: 'art', kind: 'art', label: '' },
  { key: 'title', kind: 'title', label: 'Title', subtitle: 'artist-link' },
  { key: 'album', kind: 'album', label: 'Album' },
  { key: 'year', kind: 'year', label: 'Year' },
  { key: 'rating', kind: 'rating', label: 'Rating' },
  { key: 'duration', kind: 'duration', headerIcon: 'clock' },
]

const { play, queue, currentTrack, playTracks } = usePlayerBindings()
const { $heya } = useNuxtApp()
const trackRatings = useTrackRatings()
const ratings = trackRatings.ratings
const actions = useMusicActions()

async function onRatingChange(trackId: number, v: number) {
  try { await trackRatings.set(trackId, v) } catch { /* rollback handled */ }
}

// Adapts a Songs-page row to the shared TrackEntity shape so the same
// context-menu builder can be reused across pages.
function rowToTrackEntity(t: TrackRow) {
  return {
    id: t.track_id,
    title: t.track_title,
    artist: t.artist_name,
    album: t.album_title,
    duration: t.duration,
    album_id: t.album_id,
    artist_id: t.artist_id,
    artist_slug: t.artist_slug,
    album_slug: t.album_slug,
    available: t.available,
  }
}

interface TrackRow {
  track_id: number
  track_title: string
  duration: number
  disc_number: number
  track_number: number
  album_id: number
  album_title: string
  album_slug: string
  album_cover_path: string
  album_year: string
  artist_id: number
  artist_name: string
  artist_slug: string
  available?: boolean
}

const { total, pending, itemAt, ensureRange, loadedItems } = useVirtualCatalog<TrackRow>(() => ({
  key: 'music:songs:list',
  pageSize: 200,
  fetch: async (offset, limit) => {
    const res = await $heya('/api/music/tracks', {
      query: { limit, offset },
    }) as unknown as { items: TrackRow[]; total: number }
    const items = res.items ?? []
    // Prime ratings per landed page — one round-trip per 200 tracks, same
    // batching the paged version had.
    if (items.length) void trackRatings.primeBulk(items.map((t) => t.track_id))
    return { items, total: res.total ?? 0 }
  },
}))

// Full-length sparse rows: loaded indexes map to real rows, everything else
// is a pending placeholder (unique negative id for the v-for key) that
// TrackList renders as a skeleton.
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
          rating: ratings.value.get(t.track_id) ?? 0,
        }
      : { id: -(i + 1), pending: true, title: '', artist: '', album: '', duration: 0 }
  }
  return out
})

function contextItemsFor(_track: TrackListRow, i: number) {
  const t = itemAt(i)
  return t ? actions.forTrack(rowToTrackEntity(t)) : []
}

const activeTrackId = computed(() => currentTrack.value?.id ?? null)

async function playFrom(startIdx: number) {
  const clicked = itemAt(startIdx)
  if (!clicked || clicked.available === false) return
  // Queue every LOADED playable track in library order — with a sparse list
  // that's the pages the user has actually scrolled through, which mirrors
  // the old behavior of queueing the visible page.
  const built: Track[] = loadedItems()
    .map(({ item }) => item)
    .filter((t) => t.available !== false)
    .map((t) => ({
      id: t.track_id,
      title: t.track_title,
      artist: t.artist_name,
      album: t.album_title,
      duration: t.duration,
      stream_url: `/api/music/tracks/${t.track_id}/stream`,
      album_id: t.album_id,
      artist_id: t.artist_id,
      artist_slug: t.artist_slug,
      album_slug: t.album_slug,
      poster: useAlbumCoverUrl(t.artist_slug, t.album_slug) ?? undefined,
      source: 'songs',
      available: t.available,
    }))
  if (!built.length) return
  await playTracks(built, built.find((b) => b.id === clicked.track_id))
}
</script>

<style scoped>
.ms-songs { max-width: 1400px; }

.ms-loading { color: var(--fg-3); font-size: 13px; padding: 40px 0; text-align: center; }

.ms-empty {
  text-align: center;
  padding: 80px 20px;
  color: var(--fg-3);
}
.ms-empty :deep(svg) { color: var(--fg-3); margin-bottom: 12px; }
.ms-empty h3 { font-size: 16px; color: var(--fg-1); margin-bottom: 8px; font-weight: 600; }
.ms-empty a { color: var(--gold); text-decoration: none; }
.ms-empty a:hover { text-decoration: underline; }
</style>
