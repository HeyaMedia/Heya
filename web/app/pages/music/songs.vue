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

    <!-- Sticky column header -->
    <div class="ms-row ms-row-head">
      <div class="ms-c-idx">#</div>
      <div class="ms-c-art"></div>
      <div class="ms-c-title">Title</div>
      <div class="ms-c-album">Album</div>
      <div class="ms-c-year">Year</div>
      <div class="ms-c-rating">Rating</div>
      <div class="ms-c-dur"><Icon name="clock" :size="13" /></div>
    </div>

    <div v-if="loading && !rows.length" class="ms-loading">Loading songs…</div>

    <ul v-else-if="rows.length" class="ms-list">
      <AppContextMenu
        v-for="(t, i) in rows"
        :key="t.track_id"
        :items="actions.forTrack(rowToTrackEntity(t))"
      >
      <li
        class="ms-row ms-row-track"
        :class="{ playing: nowPlayingId === t.track_id, 'ms-row-missing': t.available === false }"
        @dblclick="t.available !== false && playFrom(i)"
        @click="t.available !== false && playFrom(i)"
      >
        <div class="ms-c-idx">{{ globalIndex(i) }}</div>
        <div class="ms-c-art">
          <img :src="useAlbumCoverUrl(t.artist_slug, t.album_slug) ?? ''" :alt="t.album_title" loading="lazy" />
          <div v-if="t.available !== false" class="ms-art-play"><Icon name="play" :size="14" /></div>
          <div v-else class="ms-art-missing" title="Missing on disk"><Icon name="trash" :size="14" /></div>
        </div>
        <div class="ms-c-title">
          <div class="ms-title">{{ t.track_title }}</div>
          <NuxtLink
            :to="`/music/artist/${t.artist_slug}`"
            class="ms-artist"
            @click.stop
          >{{ t.artist_name }}</NuxtLink>
        </div>
        <div class="ms-c-album">
          <NuxtLink
            :to="`/music/artist/${t.artist_slug}/${t.album_slug}`"
            class="ms-album-link"
            @click.stop
          >{{ t.album_title }}</NuxtLink>
        </div>
        <div class="ms-c-year">{{ t.album_year || '—' }}</div>
        <div class="ms-c-rating" @click.stop>
          <StarRating
            :model-value="ratings.get(t.track_id) ?? 0"
            size="sm"
            @update:model-value="(v) => onRatingChange(t.track_id, v)"
          />
        </div>
        <div class="ms-c-dur">{{ formatDuration(t.duration) }}</div>
      </li>
      </AppContextMenu>
    </ul>

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
import { useQuery } from '@tanstack/vue-query'

definePageMeta({ layout: 'default' })

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

// Bulk-prime ratings for every visible track in one round-trip.
watch(rows, async (list) => {
  if (!list.length) return
  await trackRatings.primeBulk(list.map((t) => t.track_id))
}, { immediate: true })
const total = computed(() => songsQuery.data.value?.total ?? 0)
const totalPages = computed(() => Math.max(1, Math.ceil(total.value / PAGE_SIZE)))
const loading = computed(() => songsQuery.isFetching.value)

const nowPlayingId = computed(() => currentTrack.value?.id ?? -1)

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

function formatDuration(sec: number): string {
  if (!sec || sec < 0) return ''
  const m = Math.floor(sec / 60)
  const s = Math.floor(sec % 60)
  return `${m}:${s.toString().padStart(2, '0')}`
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
  background: rgba(255,255,255,0.04);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  color: var(--fg-1);
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.15s;
}
.ms-page-btn:hover:not(:disabled) { background: rgba(255,255,255,0.08); border-color: var(--fg-3); }
.ms-page-btn:disabled { opacity: 0.35; cursor: default; }

/* Table-ish row layout. Grid keeps cells aligned across rows without table
   markup, so hover/animation are easy. */
.ms-row {
  display: grid;
  grid-template-columns: 48px 56px 1fr minmax(160px, 1.5fr) 70px 120px 60px;
  gap: 12px;
  align-items: center;
  padding: 6px 10px;
}
.ms-row-head {
  position: sticky; top: 0; z-index: 4;
  padding: 8px 10px;
  background: var(--bg-1);
  color: var(--fg-3);
  font-size: 10px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  border-bottom: 1px solid var(--border);
}
.ms-c-idx { text-align: right; color: var(--fg-3); font-family: var(--font-mono); font-size: 12px; }
.ms-c-dur { text-align: right; color: var(--fg-3); font-family: var(--font-mono); font-size: 12px; }
.ms-c-year { color: var(--fg-3); font-family: var(--font-mono); font-size: 12px; }

.ms-c-art {
  width: 48px; height: 48px;
  position: relative;
  border-radius: 4px; overflow: hidden;
  background: var(--bg-3);
  justify-self: center;
}
.ms-c-art img { width: 100%; height: 100%; object-fit: cover; display: block; }
.ms-art-play {
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center;
  background: rgba(0,0,0,0.55);
  color: #fff;
  opacity: 0;
  transition: opacity 0.15s;
}
.ms-art-missing {
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center;
  background: rgba(0,0,0,0.55);
  color: #d96b6b;
}
.ms-row-missing { opacity: 0.5; cursor: default; }
.ms-row-missing:hover { background: transparent; }

.ms-list { display: flex; flex-direction: column; gap: 1px; }
.ms-row-track {
  border-radius: var(--r-sm);
  cursor: pointer;
  transition: background 0.15s;
}
.ms-row-track:hover { background: rgba(255,255,255,0.04); }
.ms-row-track:hover .ms-art-play { opacity: 1; }
.ms-row-track.playing { background: var(--gold-soft); }
.ms-row-track.playing .ms-title,
.ms-row-track.playing .ms-c-idx { color: var(--gold); }

.ms-c-title { min-width: 0; }
.ms-title {
  font-size: 14px;
  font-weight: 500;
  color: var(--fg-0);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.ms-artist {
  font-size: 12px;
  color: var(--fg-3);
  text-decoration: none;
  display: inline-block;
  margin-top: 1px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  max-width: 100%;
}
.ms-artist:hover { color: var(--fg-1); text-decoration: underline; }

.ms-c-album { min-width: 0; }
.ms-album-link {
  font-size: 13px;
  color: var(--fg-2);
  text-decoration: none;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  display: block;
}
.ms-album-link:hover { color: var(--fg-0); text-decoration: underline; }

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
