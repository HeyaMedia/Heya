<template>
  <div v-if="pending" class="page-pad m-loading">Loading…</div>
  <div v-else-if="!total" class="page-pad">
    <MusicEmptyState icon="pulse" :title="`No tracks in ${heading} yet`">
      This bucket doesn't have any matches right now.
      <NuxtLink to="/music/browse">← Back to Browse</NuxtLink>
    </MusicEmptyState>
  </div>
  <div v-else class="bd-page">
    <header class="bd-hero" :style="heroStyle">
      <div class="bd-hero-tint" />
      <div class="bd-hero-content">
        <NuxtLink to="/music/browse" class="bd-back-link">
          <Icon name="chevleft" :size="16" /> Browse
        </NuxtLink>
        <div class="bd-kind">{{ kindLabel }}</div>
        <h1 class="bd-title">{{ heading }}</h1>
        <div class="bd-stats">
          <span>{{ total.toLocaleString() }} tracks</span>
          <span v-if="totalDuration > 0" class="dot">·</span>
          <span v-if="totalDuration > 0">{{ formatRunTime(totalDuration) }}</span>
        </div>
        <div class="bd-actions">
          <button class="btn btn-primary" @click="playAll(false)">
            <Icon name="play" :size="16" /> Play
          </button>
          <button class="btn" @click="playAll(true)">
            <Icon name="shuffle" :size="16" /> Shuffle
          </button>
        </div>
      </div>
    </header>

    <!-- Sparse full-length list — the scrollbar spans the bucket's full
         track count; pages stream in as skeleton rows when the user scrubs. -->
    <section class="bd-tracks page-pad">
      <TrackList
        :tracks="tlRows"
        :columns="columns"
        grid-template-columns="40px 2.5fr 1.5fr 80px"
        :context-items="contextItemsFor"
        :active-track-id="activeTrackId"
        :playing="playing"
        vu-meter-in="title"
        :duration-formatter="formatTime"
        virtualized
        @row-click="playFrom"
        @range="ensureRange"
      />
    </section>
  </div>
</template>

<script setup lang="ts">
import type { Track } from '~/composables/usePlayer'
import type { TrackListColumn, TrackListRow } from '~/components/music/TrackList.vue'
import type { MusicBrowseKind, MusicBrowseTrack } from '~/queries/music'

definePageMeta({ layout: 'default' })

const columns: TrackListColumn[] = [
  { key: 'idx', kind: 'index', label: '#' },
  { key: 'title', kind: 'title', label: 'Title', inlineArt: true, inlineArtSize: 40, subtitle: 'artist-plain' },
  { key: 'album', kind: 'album', label: 'Album' },
  { key: 'duration', kind: 'duration', label: 'Duration' },
]

// The /music/browse/[kind]/[key] route dispatches three flavors via `kind`:
//   mood   → /api/music/browse/moods/{key}/tracks
//   genre  → /api/music/browse/genres/{key}/tracks  (key is URL-encoded)
//   tempo  → /api/music/browse/tempo/{key}/tracks
//
// All three return {items, total}, so the random-access catalog + sparse
// TrackList rendering is shared.
const route = useRoute()
const kind = computed(() => route.params.kind as MusicBrowseKind)
const bucketKey = computed(() => route.params.key as string)

const { $heya } = useNuxtApp()
const { play, queue, currentTrack, playing, formatTime } = usePlayerBindings()
const actions = useMusicActions()

const PAGE = 100

const { total, pending, itemAt, ensureRange, loadedItems } = useVirtualCatalog<MusicBrowseTrack>(() => ({
  key: `music:browse:${kind.value}:${bucketKey.value}`,
  pageSize: PAGE,
  fetch: async (offset, limit) => {
    const query = { limit, offset }
    let res: { items: MusicBrowseTrack[]; total: number }
    if (kind.value === 'mood') {
      res = await $heya('/api/music/browse/moods/{mood}/tracks', {
        path: { mood: bucketKey.value }, query,
      }) as { items: MusicBrowseTrack[]; total: number }
    } else if (kind.value === 'genre') {
      res = await $heya('/api/music/browse/genres/{name}/tracks', {
        path: { name: bucketKey.value }, query,
      }) as { items: MusicBrowseTrack[]; total: number }
    } else {
      res = await $heya('/api/music/browse/tempo/{band}/tracks', {
        path: { band: bucketKey.value }, query,
      }) as { items: MusicBrowseTrack[]; total: number }
    }
    return { items: res.items ?? [], total: res.total ?? 0 }
  },
}))

// MusicBrowseTrack has no `available` field (the browse endpoints don't
// report it) — tlRows/contextItemsFor both omit it, which TrackList
// treats as always-available, matching today's unconditional playFrom/menu.
// Unloaded stretches become pending skeleton rows.
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
          duration: t.duration,
          poster: useAlbumCoverUrl(t.artist_slug, t.album_slug),
        }
      : { id: -(i + 1), pending: true, title: '', artist: '', album: '', duration: 0 }
  }
  return out
})

function contextItemsFor(_track: TrackListRow, i: number) {
  const t = itemAt(i)
  if (!t) return []
  return actions.forTrack({ id: t.track_id, title: t.track_title, artist: t.artist_name, album: t.album_title, duration: t.duration, album_id: t.album_id, artist_id: t.artist_id, artist_slug: t.artist_slug, album_slug: t.album_slug })
}

const activeTrackId = computed(() => currentTrack.value?.id ?? null)

// Only honest once everything's loaded — a partial sum under-reports and
// the old page's number was silently capped at 500 anyway.
const totalDuration = computed(() => {
  const loaded = loadedItems()
  if (total.value === null || loaded.length < total.value) return 0
  return loaded.reduce((s, { item }) => s + (item.duration || 0), 0)
})

const kindLabel = computed(() => ({
  mood:  'Mood',
  genre: 'Genre',
  tempo: 'Tempo',
}[kind.value] || 'Browse'))

const heading = computed(() => {
  const k = bucketKey.value
  if (kind.value === 'mood') {
    // Convert "mood_happy" → "Happy", "danceability" → "Danceable".
    const map: Record<string, string> = {
      mood_happy: 'Happy', mood_sad: 'Melancholic', mood_aggressive: 'Aggressive',
      mood_relaxed: 'Relaxed', mood_party: 'Party', mood_electronic: 'Electronic',
      mood_acoustic: 'Acoustic', danceability: 'Danceable', voice: 'Vocal',
    }
    return map[k] ?? k
  }
  if (kind.value === 'genre') {
    // Strip "Parent---Leaf" hierarchy for the headline; full path shown in
    // small-print sub.
    const parts = k.split('---')
    return parts[parts.length - 1] ?? k
  }
  if (kind.value === 'tempo') {
    return k.replace('-', '–') + ' BPM'
  }
  return k
})

// Hero accent — picked by the kind so each browse drilldown reads visually
// distinct from the others.
const heroStyle = computed(() => {
  const grad = {
    mood:  'linear-gradient(135deg, #ec4899 0%, #6366f1 100%)',
    genre: 'linear-gradient(135deg, #06b6d4 0%, #6366f1 100%)',
    tempo: 'linear-gradient(135deg, #ea580c 0%, #b91c1c 100%)',
  }[kind.value] || 'linear-gradient(135deg, #4f46e5 0%, #3730a3 100%)'
  return { background: grad }
})

function rowToTrack(r: MusicBrowseTrack): Track {
  return {
    id: r.track_id,
    title: r.track_title,
    artist: r.artist_name,
    album: r.album_title,
    duration: r.duration,
    stream_url: `/api/music/tracks/${r.track_id}/stream`,
    album_id: r.album_id,
    artist_id: r.artist_id,
    poster: useAlbumCoverUrl(r.artist_slug, r.album_slug) ?? undefined,
    source: 'browse',
  }
}

// Queues every LOADED track in list order — with the sparse list that's the
// pages the user has actually seen, mirroring the old capped-fetch behavior.
async function playAll(shuffle: boolean) {
  let list = loadedItems().map(({ item }) => rowToTrack(item))
  if (shuffle) list = [...list].sort(() => Math.random() - 0.5)
  if (!list.length) return
  queue.value = list
  await play(list[0])
}

async function playFrom(idx: number) {
  const target = itemAt(idx)
  if (!target) return
  queue.value = loadedItems().map(({ item }) => rowToTrack(item))
  await play(rowToTrack(target))
}

</script>

<style scoped>
.m-loading {
  color: var(--fg-2);
  text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1);
  padding: 32px 40px; font-size: 13px;
}

.bd-page { padding-bottom: 80px; }

.bd-hero {
  position: relative;
  padding: 36px 40px 28px;
  overflow: hidden;
  border-radius: 0 0 var(--r-md) var(--r-md);
  color: #fff; /* on the fixed per-kind gradient — stays literal, see .bd-hero-tint..bd-back-link below */
}
.bd-hero-tint {
  position: absolute; inset: 0;
  background: linear-gradient(180deg, rgba(0,0,0,0) 50%, rgba(0,0,0,0.4) 100%);
  pointer-events: none;
}
.bd-hero-content { position: relative; z-index: 1; }
.bd-back-link {
  display: inline-flex; align-items: center; gap: 4px;
  color: rgba(255,255,255,0.85);
  font-size: 12px;
  text-decoration: none;
  margin-bottom: 14px;
}
.bd-back-link:hover { color: #fff; }
.bd-kind {
  font-size: 11px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.12em;
  opacity: 0.8;
}
.bd-title { font-size: 38px; font-weight: 700; margin: 4px 0 8px; letter-spacing: -0.01em; }
.bd-stats { font-size: 12px; opacity: 0.85; display: flex; align-items: center; gap: 8px; }
.bd-stats .dot { opacity: 0.6; }
.bd-actions { display: flex; gap: 10px; margin-top: 20px; }

.bd-tracks { margin-top: 24px; }

/* This page used the global `.list-row`/`.list-row-head` chrome (heya.css)
   instead of a bespoke row implementation, so its deltas from TrackList's
   songs.vue-shaped baseline are: bigger padding, r-md radius (not r-sm), a
   stronger hover tint, an inset row divider, a non-sticky/inline header,
   13px index (inherited from `.list-row` before), regular-weight title, and
   a gold (not underline) album-link hover. See loved.vue for the pattern. */
:deep(.tl-track) {
  padding: 9px 12px;
  border-radius: var(--r-md);
  box-shadow: inset 0 -1px 0 rgb(var(--ink) / 0.035);
}
:deep(.tl-track:hover) { background: rgb(var(--ink) / 0.045); }
:deep(.tl-body) { gap: 0; }
:deep(.tl-head) {
  position: static;
  padding: 9px 12px 6px;
  margin-bottom: 4px;
  background: transparent;
}
:deep(.tl-c-index) { font-size: 13px; }
:deep(.tl-title) { font-weight: 400; }
:deep(.tl-album-link:hover) { color: var(--gold); text-decoration: none; }
</style>
