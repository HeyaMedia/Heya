<template>
  <div class="ms-songs page-pad">
    <div class="ms-songs-head">
      <div>
        <h1 class="ms-songs-title">All Songs</h1>
        <div class="ms-songs-meta">
          <span v-if="total > 0">{{ total.toLocaleString() }} tracks</span>
          <span v-else-if="loading">Loading…</span>
          <span v-else>—</span>
          <span class="dot">·</span>
          <span>Page {{ page }} of {{ totalPages }}</span>
        </div>
      </div>
      <div class="ms-songs-actions">
        <button
          class="ms-page-btn"
          :disabled="page <= 1 || loading"
          @click="goPage(page - 1)"
          aria-label="Previous page"
        >
          <Icon name="chevleft" :size="14" />
          <span>Prev</span>
        </button>
        <button
          class="ms-page-btn"
          :disabled="page >= totalPages || loading"
          @click="goPage(page + 1)"
          aria-label="Next page"
        >
          <span>Next</span>
          <Icon name="chevright" :size="14" />
        </button>
      </div>
    </div>

    <div v-if="loading && !rows.length" class="ms-loading">Loading songs…</div>

    <TrackList
      v-else-if="tlRows.length"
      :tracks="tlRows"
      :columns="columns"
      grid-template-columns="48px 56px 1fr minmax(160px, 1.5fr) 70px 120px 60px"
      :context-items="contextItemsFor"
      :active-track-id="activeTrackId"
      :display-index="globalIndex"
      :on-rating-change="onRatingChange"
      :virtualized="tlRows.length > 200"
      @row-click="playFrom"
    />

    <div v-else class="ms-empty">
      <Icon name="music" :size="40" />
      <h3>Your library is empty</h3>
      <p>Scan a music library from <NuxtLink to="/settings/libraries">Settings → Libraries</NuxtLink>.</p>
    </div>

    <!-- Pagination footer -->
    <div v-if="totalPages > 1" class="ms-pagination">
      <button
        class="ms-page-btn"
        :disabled="page <= 1 || loading"
        @click="goPage(1)"
      >First</button>
      <button
        class="ms-page-btn"
        :disabled="page <= 1 || loading"
        @click="goPage(page - 1)"
      >
        <Icon name="chevleft" :size="14" />
      </button>
      <span class="ms-page-info">Page {{ page }} of {{ totalPages }}</span>
      <button
        class="ms-page-btn"
        :disabled="page >= totalPages || loading"
        @click="goPage(page + 1)"
      >
        <Icon name="chevright" :size="14" />
      </button>
      <button
        class="ms-page-btn"
        :disabled="page >= totalPages || loading"
        @click="goPage(totalPages)"
      >Last</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Track } from '~/composables/usePlayer'
import type { TrackListColumn, TrackListRow } from '~/components/music/TrackList.vue'
import { useQuery } from '@tanstack/vue-query'

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

const PAGE_SIZE = 1000

const route = useRoute()
const router = useRouter()
const { play, queue, currentTrack } = usePlayer()
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

interface PageBody {
  items: TrackRow[]
  total: number
  limit: number
  offset: number
}

const page = computed({
  get: () => {
    const p = parseInt((route.query.page as string | undefined) ?? '1', 10)
    return Number.isFinite(p) && p > 0 ? p : 1
  },
  set: (n: number) => {
    router.replace({ query: { ...route.query, page: n > 1 ? String(n) : undefined } })
  },
})
const offset = computed(() => (page.value - 1) * PAGE_SIZE)

const songsQuery = useQuery({
  queryKey: ['music', 'songs', offset],
  queryFn: async () => {
    const res = await $heya('/api/music/tracks', {
      query: { limit: PAGE_SIZE, offset: offset.value },
    }) as unknown as PageBody
    return res
  },
  staleTime: 1000 * 30,
  placeholderData: (prev) => prev,
})

const rows = computed<TrackRow[]>(() => songsQuery.data.value?.items ?? [])

// Normalized shape for TrackList — visual fields only. Business logic
// (contextItemsFor/playFrom/onRatingChange) still closes over `rows` by
// index, so it keeps the richer album_id/artist_id fields TrackList itself
// never needs.
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
  rating: ratings.value.get(t.track_id) ?? 0,
})))

function contextItemsFor(_track: TrackListRow, i: number) {
  return actions.forTrack(rowToTrackEntity(rows.value[i]!))
}

// Bulk-prime ratings for every visible track in one round-trip.
watch(rows, async (list) => {
  if (!list.length) return
  await trackRatings.primeBulk(list.map((t) => t.track_id))
}, { immediate: true })
const total = computed(() => songsQuery.data.value?.total ?? 0)
const totalPages = computed(() => Math.max(1, Math.ceil(total.value / PAGE_SIZE)))
const loading = computed(() => songsQuery.isFetching.value)

const activeTrackId = computed(() => currentTrack.value?.id ?? null)

function globalIndex(i: number) {
  return offset.value + i + 1
}

function goPage(n: number) {
  const clamped = Math.min(Math.max(1, n), totalPages.value)
  if (clamped === page.value) return
  page.value = clamped
  // Scroll back to the top so the user can see they're on a new page.
  if (import.meta.client) document.querySelector('.music-main')?.scrollTo({ top: 0, behavior: 'auto' })
}

async function playFrom(startIdx: number) {
  const clicked = rows.value[startIdx]
  if (!clicked || clicked.available === false) return
  // Queue only playable tracks; a removed-on-disk file can't enter the queue.
  const built: Track[] = rows.value
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
  queue.value = built
  await play(built.find((b) => b.id === clicked.track_id) ?? built[0]!)
}
</script>

<style scoped>
.ms-songs { max-width: 1400px; }

.ms-songs-head {
  display: flex; align-items: flex-end; justify-content: space-between; gap: 24px;
  margin-bottom: 24px;
}
.ms-songs-title { font-size: 28px; font-weight: 700; letter-spacing: -0.01em; }
.ms-songs-meta {
  margin-top: 4px;
  color: var(--fg-3); font-size: 12px;
  font-family: var(--font-mono); letter-spacing: 0.04em;
  display: flex; align-items: center; gap: 8px;
}
.ms-songs-meta .dot { opacity: 0.4; }
.ms-songs-actions { display: flex; gap: 8px; align-items: center; }

.ms-page-btn {
  display: inline-flex; align-items: center; gap: 4px;
  padding: 8px 12px;
  background: rgb(var(--ink) / 0.04);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  color: var(--fg-1);
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.15s;
}
.ms-page-btn:hover:not(:disabled) { background: rgb(var(--ink) / 0.08); border-color: var(--fg-3); }
.ms-page-btn:disabled { opacity: 0.35; cursor: default; }

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

.ms-pagination {
  display: flex; align-items: center; justify-content: center; gap: 8px;
  margin-top: 32px;
  padding: 16px 0;
}
.ms-page-info {
  margin: 0 8px;
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--fg-3);
  letter-spacing: 0.04em;
}
</style>
