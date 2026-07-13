<template>
  <div v-if="loading" class="page-pad m-loading">Loading mix…</div>
  <div v-else-if="!mix" class="page-pad m-empty">
    Mix not found. Mixes are seeded from your recent listening — play a few
    tracks and check back in a minute.
  </div>
  <div v-else class="mix-page">
    <header class="mix-hero">
      <!-- 4-up mosaic of the first four tracks' covers, mirroring the home
           tile so the page identity carries over. -->
      <div class="mix-hero-art">
        <NuxtImg
          v-for="(t, i) in mix.tracks.slice(0, 4)"
          :key="t.track_id"
          :src="useAlbumCoverUrl(t.artist_slug, t.album_slug) || ''"
          :alt="t.album_title"
          :class="['mix-hero-cell', `s${i}`]"
          :width="160"
          :quality="80"
          densities="1x 2x"
          loading="lazy"
          @error="onImgError"
        />
      </div>
      <div class="mix-hero-meta">
        <div class="m-kind">Mix</div>
        <h1 class="m-title">{{ mix.name }}</h1>
        <p class="m-sub">
          Sonic-similar tracks from the artists you've been listening to.
          Refreshes every hour.
        </p>
        <div class="mix-hero-stats">
          <NuxtLink :to="`/music/artist/${mix.seed_artist_slug}`" class="mix-seed-link">
            Seeded from {{ mix.seed_artist_name }}
          </NuxtLink>
          <span class="dot">·</span>
          <span>{{ mix.tracks.length }} tracks</span>
        </div>
        <div class="m-actions">
          <button class="btn btn-primary" :disabled="!mix.tracks.length" @click="playAll(false)">
            <Icon name="play" :size="16" /> Play
          </button>
          <button class="btn" :disabled="!mix.tracks.length" @click="playAll(true)">
            <Icon name="shuffle" :size="16" /> Shuffle
          </button>
        </div>
      </div>
    </header>

    <section class="page-pad mix-tracks">
      <div class="list-rows">
        <div v-if="!isPhone" class="list-row list-row-head mix-cols">
          <div>#</div>
          <div>Title</div>
          <div>Album</div>
          <div>Artist</div>
          <div style="text-align: right">Duration</div>
        </div>
        <TrackList
          :tracks="tlRows"
          :columns="columns"
          grid-template-columns="32px 1fr 1fr 1fr 80px"
          :show-header="false"
          :context-items="contextItemsFor"
          :active-track-id="currentTrack?.id ?? null"
          :duration-formatter="formatTime"
          @row-click="playFrom"
        >
          <template #cell-artist="{ index }">
            <div class="mix-artist">
              <NuxtLink :to="`/music/artist/${mix!.tracks[index]!.artist_slug}`" class="mix-link" @click.stop>{{ mix!.tracks[index]!.artist_name }}</NuxtLink>
            </div>
          </template>
        </TrackList>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import type { Track } from '~/composables/usePlayer'
import type { TrackListColumn, TrackListRow } from '~/components/music/TrackList.vue'
import { useQuery } from '@pinia/colada'
import { musicMixesQuery, type MusicMix as Mix, type MusicMixTrack as MixTrack } from '~/queries/music'

definePageMeta({ layout: 'default' })

const { isPhone } = useViewport()

const route = useRoute()
const slug = computed(() => String(route.params.slug ?? ''))

const { $heya } = useNuxtApp()
const { play, currentTrack, queue, playTracks } = usePlayerBindings()
const actions = useMusicActions()

// Shares the cache key with MusicHome's mixes-for-you query — opening
// a mix detail page from the home shelf reads from the same cache slot,
// no refetch needed. The 1h staleTime matches the home shelf.
const mixesQuery = useQuery(musicMixesQuery())
await waitForQuery(mixesQuery)
const mix = computed<Mix | null>(() => (mixesQuery.data.value ?? []).find(m => m.seed_artist_slug === slug.value) ?? null)
const loading = computed(() => mixesQuery.isPending.value)

function mixTrackToTrack(t: MixTrack): Track {
  return {
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
    source: 'mix',
  }
}

async function playAll(shuffle: boolean) {
  if (!mix.value?.tracks.length) return
  let tracks = mix.value.tracks.map(mixTrackToTrack)
  if (shuffle) {
    tracks = [...tracks].sort(() => Math.random() - 0.5)
  }
  await playTracks(tracks)
}

async function playFrom(index: number) {
  if (!mix.value?.tracks.length) return
  const tracks = mix.value.tracks.map(mixTrackToTrack)
  await playTracks(tracks, tracks[index])
}

// TrackList migration (W2c) — the header row stays hand-rolled above (see
// template) since this grid was already correctly aligned; TrackList's own
// sticky, solid-background header would be a visual change, not a parity fix.
const columns: TrackListColumn[] = [
  { key: 'idx', kind: 'index' },
  { key: 'title', kind: 'title', inlineArt: true, inlineArtSize: 40, subtitle: 'none' },
  { key: 'album', kind: 'album' },
  { key: 'artist', kind: 'custom' },
  { key: 'duration', kind: 'duration' },
]

const tlRows = computed<TrackListRow[]>(() => (mix.value?.tracks ?? []).map((t) => ({
  id: t.track_id,
  title: t.track_title,
  artist: t.artist_name,
  artist_slug: t.artist_slug,
  album: t.album_title,
  album_slug: t.album_slug,
  duration: t.duration,
  poster: useAlbumCoverUrl(t.artist_slug, t.album_slug),
})))

// The old hand-rolled rows had no context menu at all — TrackList requires
// one, so this is a small net-new affordance (right-click a mix row to
// play/queue/rate that track) rather than a preserved behavior.
function contextItemsFor(_row: TrackListRow, i: number) {
  const t = mix.value!.tracks[i]!
  return actions.forTrack({
    id: t.track_id,
    title: t.track_title,
    artist: t.artist_name,
    album: t.album_title,
    duration: t.duration,
    album_id: t.album_id,
    artist_id: t.artist_id,
    artist_slug: t.artist_slug,
    album_slug: t.album_slug,
  })
}

// NuxtImg types its `error` payload as `string | Event`; narrow before use.
function onImgError(e: Event | string) {
  if (typeof e === 'string') return
  const img = e.target as HTMLImageElement
  img.style.visibility = 'hidden'
}

function formatTime(seconds: number): string {
  if (!seconds || seconds < 0) return '—'
  const m = Math.floor(seconds / 60)
  const s = Math.floor(seconds % 60)
  return `${m}:${String(s).padStart(2, '0')}`
}
</script>

<style scoped>
.m-loading, .m-empty { color: var(--fg-3); font-size: 14px; padding: 32px 0; }

.mix-page { padding-bottom: 80px; }

.mix-hero {
  display: flex;
  gap: 32px;
  align-items: flex-end;
  padding: 32px 32px 24px;
  margin-bottom: 16px;
  background: linear-gradient(180deg, color-mix(in srgb, var(--gold) 4%, transparent), transparent);
  /* No border-bottom: a divider re-splits the page into stacked panels;
     spacing + the glass track table below define the edge on their own. */
}
.mix-hero-art {
  flex-shrink: 0;
  width: 220px;
  height: 220px;
  border-radius: var(--r-md);
  overflow: hidden;
  background: var(--bg-3);
  display: grid;
  grid-template-columns: 1fr 1fr;
  grid-template-rows: 1fr 1fr;
  /* Hero-poster shadow formula (matches the detail pages). */
  box-shadow: 0 24px 60px rgb(var(--shade) / 0.5), 0 0 0 1px rgb(var(--ink) / 0.06);
}
.mix-hero-cell { width: 100%; height: 100%; object-fit: cover; }
.mix-hero-cell.s0 { grid-area: 1 / 1; }
.mix-hero-cell.s1 { grid-area: 1 / 2; }
.mix-hero-cell.s2 { grid-area: 2 / 1; }
.mix-hero-cell.s3 { grid-area: 2 / 2; }

.mix-hero-meta { flex: 1; min-width: 0; }
.m-kind {
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.12em;
  color: var(--gold);
  margin-bottom: 6px;
}
/* Halos — the mix hero sits over the shell's ambient pool. */
.m-title {
  font-size: 38px; font-weight: 800; letter-spacing: -0.02em; margin-bottom: 6px;
  text-shadow: 0 1px 2px var(--bg-1), 0 0 10px var(--bg-1), 0 0 24px var(--bg-1);
}
.m-sub {
  color: var(--fg-1); font-size: 13px; max-width: 560px; margin-bottom: 14px;
  text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1);
}

.mix-hero-stats {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--fg-1);
  font-size: 13px;
  margin-bottom: 18px;
  text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1);
}
.mix-seed-link {
  color: var(--fg-1);
  text-decoration: none;
  font-weight: 600;
  transition: color 0.15s;
}
.mix-seed-link:hover { color: var(--gold); }
.dot { color: var(--fg-3); }

.m-actions { display: flex; gap: 10px; align-items: center; }
.btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 8px 16px;
  background: color-mix(in oklab, var(--bg-2) 82%, transparent);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  box-shadow: var(--shadow-el);
  color: var(--fg-1);
  font-size: 13px;
  cursor: pointer;
  font-family: inherit;
  transition: background 0.15s, border-color 0.15s;
}
.btn:hover { background: var(--bg-3); }
.btn-primary {
  background: var(--gold);
  color: var(--bg-0);
  border-color: var(--gold);
  font-weight: 600;
}
.btn-primary:hover { background: var(--gold); filter: brightness(1.1); }
.btn:disabled { opacity: 0.4; cursor: not-allowed; }

.mix-tracks { margin-top: 8px; }

/* `.list-row`/`.list-row-head` back the hand-rolled header only now — the
   body rows moved to TrackList (`.tl-*`), overridden via :deep() below. */
.list-rows { display: flex; flex-direction: column; }
.list-row {
  display: grid;
  align-items: center;
  gap: 12px;
  padding: 8px 12px;
  border-radius: var(--r-sm);
  cursor: pointer;
  transition: background 0.1s;
}
.list-row-head {
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  /* Renders outside TrackList's glass panel, straight over the ambient
     art — fg-2 + halo instead of fg-3 so it survives bright washes. */
  color: var(--fg-2);
  text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1);
  font-weight: 600;
  cursor: default;
  border-bottom: 0;
  margin-bottom: 4px;
}
/* Cancels heya.css's global `.list-row:hover` background — this scoped
   block wins on specificity, so without this the (non-interactive) header
   would still pick up the global hover tint. */
.list-row-head:hover { background: transparent; }

.mix-cols { grid-template-columns: 32px 1fr 1fr 1fr 80px; }
.mix-artist { font-size: 12px; color: var(--fg-2); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.mix-link { color: inherit; text-decoration: none; transition: color 0.15s; }
.mix-link:hover { color: var(--gold); }

/* TrackList body-row deltas vs. this page's old (already fairly close)
   `.list-row` numbers: only the row padding, the index alignment, the
   inline-art gap, and the album-link color/size/hover actually differ. */
:deep(.tl-body) { gap: 0; }
:deep(.tl-track) { padding: 8px 12px; }
:deep(.tl-c-index) { text-align: center; }
:deep(.tl-track.tl-active .tl-c-index) { color: var(--fg-3); }
:deep(.tl-title-inline-art) { gap: 10px; }
:deep(.tl-c-album) { font-size: 12px; }
:deep(.tl-album-link) { font-size: 12px; }
:deep(.tl-album-link:hover) { color: var(--gold); }

/* Phone (<=720px): stack the hero, center the mosaic, wrap the action row.
   TrackList's own isPhone branch handles the tracklist. */
@media (max-width: 720px) {
  .mix-hero {
    flex-direction: column;
    align-items: center;
    text-align: center;
    padding: 24px 20px 20px;
    gap: 14px;
  }
  .mix-hero-art { width: min(55vw, 240px); height: min(55vw, 240px); }
  .mix-hero-meta { width: 100%; }
  .mix-hero-stats { justify-content: center; flex-wrap: wrap; }
  .m-actions { justify-content: center; flex-wrap: wrap; }
}
</style>
