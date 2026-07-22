<template>
  <!-- Tone-follow: every descendant (hero buttons, ledger tone cells)
       inherits --tone/--tone-rgb/--tone-ink published here. -->
  <div class="ml" :style="toneStyle">
    <MusicCollectionDetail
      kind="Collection"
      title="Loved Songs"
      :images="artistArtUrls"
      :backdrop="firstAlbumCover"
      :ledger-cells="ledgerCells"
      :ledger-pending="pending"
      :tracks="tlRows"
      :tracks-pending="pending"
      :tracks-meta="`${(total ?? 0).toLocaleString()} ${(total ?? 0) === 1 ? 'track' : 'tracks'}`"
      :columns="columns"
      storage-key="loved"
      :context-items="contextItemsFor"
      :active-track-id="activeTrackId"
      :playing="playing"
      vu-meter-in="art"
      :art-play-icon-size="13"
      :duration-formatter="formatTime"
      :on-rating-change="onRatingChange"
      virtualized
      @image="currentBgArt = $event"
      @row-click="playFrom"
      @range="ensureRange"
    >
      <template #stats>
        <span>{{ (total ?? 0).toLocaleString() }} {{ (total ?? 0) === 1 ? 'track' : 'tracks' }}</span>
        <template v-if="stats && stats.total_duration > 0">
          <span class="dot">·</span>
          <span>{{ formatRunTime(stats.total_duration) }}</span>
        </template>
      </template>
      <template #actions>
        <button class="btn-play collection-half" :disabled="!total" @click="playAll(false)">
          <span class="tri" /> Play <small>{{ (total ?? 0).toLocaleString() }} {{ (total ?? 0) === 1 ? 'TRACK' : 'TRACKS' }}</small>
        </button>
        <button class="pill collection-half" :disabled="!total" @click="playAll(true)">
          <Icon name="shuffle" :size="15" /> Shuffle
        </button>
      </template>
      <template #loading>Loading…</template>
      <template #empty>
        <div class="ml-empty">
          <Icon name="star" :size="40" />
          <h3>No loved tracks yet</h3>
          <p>Heart or thumbs-up a track from the <NuxtLink to="/music/songs">Songs page</NuxtLink>, the player, or an album page. It'll appear here as soon as you love something.</p>
        </div>
      </template>
    </MusicCollectionDetail>
  </div>
</template>

<script setup lang="ts">
import type { Track } from '~/composables/usePlayer'
import type { TrackListColumn, TrackListRow } from '~/components/music/TrackList.vue'
import type { LedgerCell } from '~/components/ui/LedgerStrip.vue'
import type { RichTrackWire } from '~/utils/trackListMeta'
import type { ImageTone } from '~/composables/useImageTone'

definePageMeta({ layout: 'default' })

// Column order contract: title → artist → album → loved → everything
// else → duration.
const columns: TrackListColumn[] = [
  { key: 'idx', kind: 'index', label: '#', width: '40px' },
  { key: 'art', kind: 'art', width: '48px' },
  { key: 'title', kind: 'title', subtitle: 'artist-link', label: 'Title', width: 'minmax(200px, 1fr)', sortable: true },
  artistTrackColumn(),
  { key: 'album', kind: 'album', label: 'Album', width: 'minmax(160px, 1.2fr)', optional: true, defaultOn: true, sortable: true },
  { key: 'loved', kind: 'meta', label: 'Loved', width: '96px', optional: true, defaultOn: true, format: (r) => (r.rated_at ? formatShortDate(r.rated_at) : '—'), sortable: true, sortValue: (r) => (r.rated_at ? Date.parse(r.rated_at) || null : null), tooltip: (r) => formatFullDateTime(r.rated_at) },
  ...richTrackColumns(),
  { key: 'rating', kind: 'rating', label: 'Rating', width: '130px', sortable: true },
  { key: 'duration', kind: 'duration', headerIcon: 'clock', width: '60px', sortable: true },
]

interface RatedTrackRow extends RichTrackWire {
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
  rated_at?: string
  available?: boolean
}

interface RatedTrackStats {
  track_count: number
  total_duration: number
  artist_count: number
  last_rated_at: string | null
}

const { play, queue, currentTrack, playing, formatTime, playTracks } = usePlayerBindings()
const { $heya } = useNuxtApp()
const trackRatings = useTrackRatings()
const ratings = trackRatings.ratings
const actions = useMusicActions()

const { total, pending, itemAt, ensureRange, loadedItems, reset } = useVirtualCatalog<RatedTrackRow>(() => ({
  key: 'me:rated:tracks:loved',
  pageSize: 500,
  fetch: async (offset, limit) => {
    const r = await $heya('/api/me/ratings/tracks', {
      query: { min_rating: 6, limit, offset },
    }) as unknown as { items: RatedTrackRow[]; total: number }
    const items = r.items ?? []
    trackRatings.primeMany(items.map((t) => [t.track_id, t.rating] as [number, number]))
    return { items, total: r.total ?? 0 }
  },
}))

// Eagerly materialize the whole list (loved sets top out in the low
// thousands — not the 280k Songs catalog) so header sorting has every row
// to work with; TrackList disables sort affordances while any row is
// pending. Past the cap we stay sparse and sorting stays off rather than
// lying. ~10 pages of 500 at the cap.
watch(total, (n) => {
  if (n && n > 0 && n <= 5000) ensureRange(0, n - 1)
}, { immediate: true })

// ── Hero ledger — band aggregates from the server (the sparse list only
// holds the pages the user has scrolled; runtime/artists need the lot). ──
const stats = ref<RatedTrackStats | null>(null)
async function loadStats() {
  try {
    stats.value = await $heya('/api/me/ratings/track-stats', {
      query: { min_rating: 6 },
    }) as unknown as RatedTrackStats
  } catch { /* strip renders from list totals only */ }
}
onMounted(loadStats)

const ledgerCells = computed<LedgerCell[]>(() => {
  const s = stats.value
  if (!s || !s.track_count) return []
  const cells: LedgerCell[] = [
    { k: 'Tracks', v: s.track_count.toLocaleString() },
    { k: 'Runtime', v: formatRunTime(s.total_duration) },
    { k: 'Artists', v: s.artist_count.toLocaleString() },
  ]
  if (s.last_rated_at) cells.push({ k: 'Last Loved', v: timeAgoShort(s.last_rated_at), tone: true })
  return cells
})

// ── Hero art + tone-follow ───────────────────────────────────────────
// Album art is only a fallback for the full-bleed background now; the
// foreground collage was intentionally removed to keep collection heroes
// focused on their title and actions.
const firstAlbumCover = computed(() => {
  const first = itemAt(0)
  return first ? useAlbumCoverUrl(first.artist_slug, first.album_slug) : null
})

// Current hero image (declared ahead of the tone watch below, which reads
// it with immediate: true). Driven by the rotation block further down.
const currentBgArt = ref<string | null>(null)

// ── Tone-follow: publish --tone/--tone-rgb/--tone-ink on the page root.
// Primary source is the AmbientBackdrop's own sampled tone (HeroCanvas
// claims the ambient layer with the hero image); local sample of the
// current hero image is the fallback — same pattern as the artist page.
const bgTone = useBackgroundTone()
const localTone = ref<ImageTone | null>(null)
let toneSeq = 0
watch(() => currentBgArt.value || firstAlbumCover.value, (src) => {
  const seq = ++toneSeq
  if (!src || !import.meta.client) { localTone.value = null; return }
  sampleImageTone(src).then((t) => {
    if (seq === toneSeq) localTone.value = t
  })
}, { immediate: true })

const { toneFollowEnabled } = useAppearance()
const toneStyle = computed(() => {
  if (!toneFollowEnabled.value) return undefined
  const t = bgTone.value || localTone.value
  return t ? toneStyleVars(t) : undefined
})

// ── Hero art — the loved artists (same rotation the playlist page runs;
// HeroCanvas mirrors each pick to the ambient layer, keeping the blur
// below the ledger seam in lockstep with the band). ──

const artistArtUrls = computed(() => {
  const seen = new Set<string>()
  const urls: string[] = []
  const n = Math.min(total.value ?? 0, 100)
  for (let i = 0; i < n; i++) {
    const t = itemAt(i)
    if (!t || !t.artist_slug || seen.has(t.artist_slug)) continue
    seen.add(t.artist_slug)
    urls.push(`/api/media/${t.artist_slug}/image/poster`)
    if (urls.length === 12) break
  }
  return urls
})

// MusicCollectionHero owns the rotation (CycleControls owns the clock;
// prev/pause/next/expand live in its tools cluster) — this page just
// supplies the pool above and mirrors the shown image via @image for the
// tone fallback sample.

// ── Rows ─────────────────────────────────────────────────────────────
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
          rated_at: t.rated_at,
          ...pickRichFields(t),
        }
      : { id: -(i + 1), pending: true, title: '', artist: '', album: '', duration: 0 }
  }
  return out
})

function contextItemsFor(_track: TrackListRow, i: number) {
  const t = itemAt(i)
  if (!t) return []
  const items = actions.forTrack({ id: t.track_id, title: t.track_title, artist: t.artist_name, album: t.album_title, duration: t.duration, album_id: t.album_id, artist_id: t.artist_id, artist_slug: t.artist_slug, album_slug: t.album_slug, available: t.available })
  return [
    ...items,
    { label: '', separator: true },
    { label: 'Remove from Loved Songs', icon: 'close', action: () => unlove(t.track_id) },
  ]
}

const activeTrackId = computed(() => currentTrack.value?.id ?? null)

async function unlove(trackId: number) {
  try {
    await trackRatings.set(trackId, 0)
    reset()
    loadStats()
  } catch { /* optimistic rollback handled by composable */ }
}

async function onRatingChange(trackId: number, v: number) {
  try {
    await trackRatings.set(trackId, v)
    // Dropping below the loved band removes the row — reset the catalog so
    // indexes/total stay honest rather than leaving a hole.
    if (v < 9) {
      reset()
      loadStats()
    }
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

// Queue every LOADED playable track in list order — the pages the user has
// actually scrolled through.
function loadedPlayable(): Track[] {
  return loadedItems()
    .map(({ item }) => item)
    .filter((r) => r.available !== false)
    .map(toPlayable)
}

async function playAll(shuffle: boolean) {
  let built = loadedPlayable()
  if (shuffle) built = [...built].sort(() => Math.random() - 0.5)
  if (!built.length) return
  await playTracks(built)
}

async function playFrom(i: number) {
  const clicked = itemAt(i)
  if (!clicked || clicked.available === false) return
  const built = loadedPlayable()
  if (!built.length) return
  await playTracks(built, built.find((b) => b.id === clicked.track_id))
}
</script>

<style scoped>
.ml { padding-bottom: 0; }

.dot { opacity: 0.4; }

.ml-empty {
  text-align: center; padding: 80px 20px; color: var(--fg-3);
}
.ml-empty :deep(svg) { color: var(--fg-3); margin-bottom: 12px; }
.ml-empty h3 { font-size: 18px; color: var(--fg-1); margin-bottom: 8px; font-weight: 600; }
.ml-empty p { font-size: 13px; line-height: 1.6; max-width: 440px; margin: 0 auto; }
.ml-empty a { color: var(--gold); text-decoration: none; }
.ml-empty a:hover { text-decoration: underline; }

/* TrackList's baseline CSS matches music/songs.vue exactly — this page's
   numbers differ in a handful of spots, so layer the deltas on via :deep()
   rather than duplicating the whole table. TrackList isn't portaled, so
   scoped :deep() reaches its internals fine (docs/ui.md gotcha #2 only
   applies to portaled content). */
:deep(.tl-body) { gap: 2px; }
:deep(.tl-c-art) { width: 44px; height: 44px; }
:deep(.tl-c-index) { font-size: 11px; }
:deep(.tl-c-duration) { font-size: 11px; letter-spacing: 0.04em; }
/* songs.vue tints its index column gold on the active row; loved.vue never did. */
:deep(.tl-track.tl-active .tl-c-index) { color: var(--fg-3); }
</style>
