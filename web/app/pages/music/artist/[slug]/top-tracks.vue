<script setup lang="ts">
import type { ArtistTopTrackRow, TrackView } from '~~/shared/types'
import type { Track } from '~/composables/usePlayer'
import { useQuery } from '@pinia/colada'
import { musicArtistDetailQuery } from '~/queries/music'

definePageMeta({ layout: 'default' })

const route = useRoute()
const slug = computed(() => route.params.slug as string)
const { $heya } = useNuxtApp()
const { playTracks, currentTrack, playing, formatTime } = usePlayerBindings()
const trackMenuActions = useMusicActions()
const { isCoarse } = useViewport()

// Shares the artist page's cache entry — usually already warm from the
// detail page the user navigated here from.
const detailQuery = useQuery(() => musicArtistDetailQuery(slug.value))
await waitForQuery(detailQuery)
watch(detailQuery.error, (err) => {
  if (err) navigateTo('/music')
}, { immediate: true })

const artist = computed(() => detailQuery.data.value?.artist ?? null)
const albums = computed(() => detailQuery.data.value?.albums ?? [])

// The full persisted chart — upstream caps each provider at its top 100.
const chartQuery = useQuery({
  key: () => ['music', 'artist', 'top-tracks', slug.value, { limit: 200 }],
  query: async () => ((await $heya('/api/music/artists/{slug}/top-tracks', { path: { slug: slug.value }, query: { limit: 200 } })) as { items: ArtistTopTrackRow[] }).items ?? [],
  enabled: () => slug.value.length > 0,
  staleTime: 1000 * 60 * 30,
  retry: 0,
})

// Providers overlap heavily (Deezer's chart is mostly Last.fm's) — dedupe by
// resolved local track first, folded title second, keeping chart order
// (Last.fm block leads).
const rows = computed<ArtistTopTrackRow[]>(() => {
  const seen = new Set<string>()
  const out: ArtistTopTrackRow[] = []
  for (const t of chartQuery.data.value ?? []) {
    const key = t.local_track_id ? `id:${t.local_track_id}` : `t:${t.title.trim().toLowerCase()}`
    if (seen.has(key)) continue
    seen.add(key)
    out.push(t)
  }
  return out
})

const providers = computed(() => [...new Set((chartQuery.data.value ?? []).map((t) => t.provider || 'lastfm'))])

// Popularity bar — playcounts are only comparable within Last.fm (Deezer's
// "playcount" carries a rank number), so the bar normalizes against the
// Last.fm max and Deezer-only rows simply go barless.
const maxPlaycount = computed(() => Math.max(1, ...rows.value.filter((t) => (t.provider || 'lastfm') === 'lastfm').map((t) => t.playcount || 0)))
function barWidth(t: ArtistTopTrackRow): string {
  if ((t.provider || 'lastfm') !== 'lastfm' || !t.playcount) return '0%'
  return `${Math.max(2, Math.round((t.playcount / maxPlaycount.value) * 100))}%`
}

// Playability mirrors the artist page: a chart row is playable when its
// matched local track has a live file in the (already loaded) discography.
const playableTrackIds = computed(() => {
  const s = new Set<number>()
  for (const al of albums.value) for (const t of al.tracks as TrackView[]) if (t.files.length > 0) s.add(t.id)
  return s
})
function isPlayable(t: ArtistTopTrackRow) {
  return !!t.local_track_id && playableTrackIds.value.has(t.local_track_id)
}
function isActive(t: ArtistTopTrackRow) {
  const id = currentTrack.value?.id
  return id != null && id === t.local_track_id
}

function toTrack(t: ArtistTopTrackRow): Track {
  return {
    id: t.local_track_id!,
    title: t.title,
    artist: artist.value?.name ?? '',
    album: t.local_album_title ?? '',
    duration: t.local_duration ?? 0,
    stream_url: `/api/music/tracks/${t.local_track_id}/stream`,
    album_id: t.local_album_id ?? 0,
    artist_id: artist.value?.id,
    poster: useAlbumCoverUrl(slug.value, t.local_album_slug ?? '') ?? undefined,
  }
}

async function playRow(t: ArtistTopTrackRow) {
  if (!isPlayable(t)) return
  await playTracks([toTrack(t)])
}
async function playAll(shuffle: boolean) {
  const owned = rows.value.filter(isPlayable).map(toTrack)
  if (!owned.length) return
  await playTracks(owned, undefined, { shuffle })
}
const hasPlayable = computed(() => rows.value.some(isPlayable))

function rowMenuItems(t: ArtistTopTrackRow) {
  if (!t.local_track_id) return []
  return trackMenuActions.forTrack({
    id: t.local_track_id,
    title: t.title,
    artist: artist.value?.name ?? '',
    album: t.local_album_title ?? '',
    duration: t.local_duration ?? 0,
    artist_slug: slug.value,
    album_slug: t.local_album_slug,
    available: isPlayable(t),
  })
}

function formatBigInt(n: number): string {
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(1).replace(/\.0$/, '')}B`
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1).replace(/\.0$/, '')}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1).replace(/\.0$/, '')}K`
  return n.toLocaleString()
}

useHead(() => ({ title: artist.value ? `${artist.value.name} — Top Tracks` : 'Top Tracks' }))
</script>

<template>
  <div class="ttp">
    <header class="ttp-head">
      <Poster :idx="0" :src="usePosterUrl(detailQuery.data.value?.media_item)" aspect="1/1" :width="128" class="ttp-avatar" />
      <div class="ttp-text">
        <NuxtLink :to="`/music/artist/${slug}`" class="ttp-eyebrow">
          <Icon name="chevleft" :size="12" /> {{ artist?.name ?? 'Artist' }}
        </NuxtLink>
        <h1 class="ttp-title">Top Tracks</h1>
        <div class="ttp-sub">
          <span>{{ rows.length }} tracks</span>
          <span class="dot">&middot;</span>
          <span>ranked by global plays</span>
          <template v-if="providers.length">
            <span class="dot">&middot;</span>
            <span v-for="p in providers" :key="p" class="ttp-src">{{ providerLabel(p) }}</span>
          </template>
        </div>
      </div>
      <div class="ttp-actions">
        <button class="ttp-pill" :disabled="!hasPlayable" @click="playAll(false)">
          <Icon name="play" :size="12" /><span>Play all</span>
        </button>
        <button class="ttp-pill ttp-pill-ghost" :disabled="!hasPlayable" @click="playAll(true)">
          <Icon name="shuffle" :size="12" /><span>Shuffle</span>
        </button>
      </div>
    </header>

    <div v-if="chartQuery.isPending.value" class="ttp-state">Loading…</div>
    <div v-else-if="!rows.length" class="ttp-state">No chart data yet — refresh the artist's metadata.</div>

    <div v-if="rows.length" class="ttp-cols" aria-hidden="true">
      <span class="ttp-col-rank">#</span>
      <span>Track</span>
      <span>Popularity</span>
      <span class="ttp-col-r">Plays</span>
      <span class="ttp-col-r">Listeners</span>
      <span class="ttp-col-r">Time</span>
      <span class="ttp-col-r">Source</span>
    </div>
    <ol v-if="rows.length" class="ttp-list">
      <AppContextMenu
        v-for="(t, idx) in rows"
        :key="`${t.provider}-${t.rank}-${idx}`"
        :items="rowMenuItems(t)"
        :disabled="!t.local_track_id"
      >
        <li
          class="ttp-row"
          role="button"
          :class="{ 'ttp-missing': !isPlayable(t), 'ttp-active': isActive(t), 'ttp-podium': idx < 3 }"
          @click="playRow(t)"
        >
          <div class="ttp-rank">
            <VuMeter v-if="isActive(t)" :playing="playing" />
            <template v-else>{{ idx + 1 }}</template>
          </div>

          <div class="ttp-meta">
            <div class="ttp-line">
              <span class="ttp-t">{{ t.title }}</span>
              <a
                v-if="!t.local_track_id && t.url"
                :href="t.url"
                target="_blank"
                rel="noopener"
                class="ttp-ext"
                :title="`Open on ${providerLabel(t.provider || 'lastfm')}`"
                @click.stop
              ><Icon name="link" :size="11" /></a>
            </div>
            <NuxtLink
              v-if="t.local_album_title && t.local_album_slug"
              :to="`/music/artist/${slug}/${t.local_album_slug}`"
              class="ttp-al"
              @click.stop
            >{{ t.local_album_title }}</NuxtLink>
            <span v-else class="ttp-al ttp-al-none">{{ t.local_track_id ? '' : 'not in library' }}</span>
          </div>

          <div class="ttp-bar-cell">
            <div class="ttp-bar"><span :style="{ width: barWidth(t) }" /></div>
          </div>

          <div class="ttp-num">{{ t.playcount && (t.provider || 'lastfm') === 'lastfm' ? formatBigInt(t.playcount) : '' }}</div>
          <div class="ttp-num ttp-listeners">{{ t.listeners ? formatBigInt(t.listeners) : '' }}</div>
          <div class="ttp-dur">{{ t.local_duration ? formatTime(t.local_duration) : '' }}</div>
          <div class="ttp-prov">{{ providerLabel(t.provider || 'lastfm') }}</div>
        </li>
      </AppContextMenu>
    </ol>

    <div v-if="rows.length" class="ttp-legend">
      Plays &amp; listeners from {{ providers.map(providerLabel).join(' + ') }} via heya.media.
      Dimmed tracks aren't in the library.
    </div>
  </div>
</template>

<style scoped>
.ttp { max-width: 1100px; margin: 0 auto; padding: 36px 32px 96px; }

.ttp-head {
  display: flex;
  align-items: center;
  gap: 20px;
  margin-bottom: 28px;
  padding-bottom: 20px;
  border-bottom: 1px solid var(--hair-strong);
}
.ttp-avatar { width: 64px; height: 64px; border-radius: 50%; flex-shrink: 0; }
.ttp-text { min-width: 0; flex: 1; }
.ttp-eyebrow {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font: 600 11.5px var(--font-mono);
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: var(--fg-2);
  text-decoration: none;
  transition: color 0.15s;
}
.ttp-eyebrow:hover { color: var(--gold); }
/* Section-title halo — the head sits over the ambient art pool. */
.ttp-eyebrow, .ttp-title, .ttp-sub { text-shadow: 0 1px 2px var(--bg-1), 0 0 10px var(--bg-1), 0 0 24px var(--bg-1); }
.ttp-title { font: 700 30px/1.1 var(--font-display, inherit); margin: 4px 0 6px; color: var(--fg-0); }
.ttp-sub {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px 10px;
  font: 500 12px var(--font-mono);
  color: var(--fg-2);
}
.ttp-sub .dot { color: var(--gold); }
.ttp-src { color: var(--fg-1); }
.ttp-actions { display: flex; gap: 8px; flex-shrink: 0; }
.ttp-pill {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 5px 13px;
  border-radius: 999px;
  border: 0;
  background: var(--gold);
  color: var(--bg-0);
  font: 700 11.5px var(--font-sans);
  cursor: pointer;
  transition: filter 0.12s;
}
.ttp-pill:hover:not([disabled]) { filter: brightness(1.1); }
.ttp-pill[disabled] { opacity: 0.4; cursor: not-allowed; }
.ttp-pill-ghost { background: rgb(var(--ink) / 0.07); color: rgb(var(--ink) / 0.82); }
.ttp-pill-ghost:hover:not([disabled]) { background: rgb(var(--ink) / 0.12); filter: none; }

.ttp-state { padding: 48px 0; color: var(--fg-3); font: 500 13px var(--font-mono); }

/* Column labels — mirrors the row grid (plus the panel's 10px side pad). */
.ttp-cols {
  display: grid;
  grid-template-columns: 44px minmax(0, 1.5fr) minmax(80px, 1fr) 76px 76px 52px 76px;
  gap: 14px;
  padding: 0 20px 7px;
  font: 600 10px var(--font-mono);
  letter-spacing: 0.14em;
  text-transform: uppercase;
  color: var(--fg-2);
  text-shadow: 0 1px 2px var(--bg-1), 0 0 10px var(--bg-1);
}
.ttp-col-rank { text-align: center; }
.ttp-col-r { text-align: right; }

/* Glass panel — the page sits over the music shell's rotating ambient
   pool, and bare rows painted straight onto busy art are unreadable
   (same story as TrackList's .tl surface). */
.ttp-list {
  list-style: none;
  margin: 0;
  padding: 6px 10px 10px;
  background: color-mix(in oklab, var(--bg-2) 82%, transparent);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  box-shadow: var(--shadow-el);
}
.ttp-row {
  display: grid;
  grid-template-columns: 44px minmax(0, 1.5fr) minmax(80px, 1fr) 76px 76px 52px 76px;
  gap: 14px;
  align-items: center;
  padding: 9px 10px;
  border-bottom: 1px solid var(--hair);
  border-radius: var(--r-sm);
  cursor: pointer;
  transition: background 0.12s;
}
.ttp-row:last-child { border-bottom: 0; }
.ttp-row:hover { background: rgb(var(--ink) / 0.05); }
/* Dim via ink strength, not whole-row opacity — opacity let the ambient
   art bleed through and the row vanished. */
.ttp-missing { cursor: default; }
.ttp-missing .ttp-t { color: rgb(var(--ink) / 0.5); font-weight: 500; }
.ttp-missing .ttp-rank { color: rgb(var(--ink) / 0.3); }
.ttp-missing .ttp-bar span { opacity: 0.45; }
.ttp-missing .ttp-num, .ttp-missing .ttp-dur, .ttp-missing .ttp-prov { color: rgb(var(--ink) / 0.35); }
.ttp-active .ttp-t { color: var(--gold); }

.ttp-rank {
  font: 700 15px var(--font-mono);
  color: rgb(var(--ink) / 0.45);
  text-align: center;
  font-variant-numeric: tabular-nums;
}
.ttp-podium .ttp-rank { color: var(--gold); font-size: 17px; }

.ttp-meta { min-width: 0; }
.ttp-line { display: flex; align-items: center; gap: 6px; min-width: 0; }
.ttp-t {
  font-size: 14px;
  font-weight: 600;
  color: var(--fg-0);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.ttp-ext { color: var(--fg-3); display: inline-flex; transition: color 0.15s; }
.ttp-ext:hover { color: var(--gold); }
.ttp-al {
  display: block;
  font-size: 11.5px;
  color: var(--fg-3);
  text-decoration: none;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  margin-top: 2px;
}
.ttp-al:hover { color: var(--fg-1); }
.ttp-al-none { font-style: italic; }

.ttp-bar-cell { min-width: 0; }
.ttp-bar {
  height: 4px;
  border-radius: 2px;
  background: rgb(var(--ink) / 0.08);
  overflow: hidden;
}
.ttp-bar span {
  display: block;
  height: 100%;
  border-radius: 2px;
  background: linear-gradient(90deg, color-mix(in oklab, var(--gold) 55%, transparent), var(--gold));
  transition: width 0.3s;
}

.ttp-num {
  font: 500 12px var(--font-mono);
  color: var(--fg-2);
  text-align: right;
  font-variant-numeric: tabular-nums;
}
.ttp-listeners { color: var(--fg-3); }
.ttp-dur { font: 500 12px var(--font-mono); color: var(--fg-3); text-align: right; }
.ttp-prov {
  font: 550 10px var(--font-mono);
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: rgb(var(--ink) / 0.4);
  text-align: right;
}

.ttp-legend { margin-top: 18px; font: 500 11.5px var(--font-mono); color: var(--fg-3); }

@media (max-width: 820px) {
  .ttp { padding: 24px 16px 96px; }
  .ttp-cols { display: none; }
  .ttp-row { grid-template-columns: 34px minmax(0, 1fr) 70px; }
  .ttp-bar-cell, .ttp-listeners, .ttp-dur, .ttp-prov { display: none; }
  .ttp-head { flex-wrap: wrap; }
}
</style>
