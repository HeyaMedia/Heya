<template>
  <div class="page-pad browse-page">
    <MusicPageHead title="Browse">
      <template #subtitle>
        <span>Explore your library by feel, genre, or tempo.</span>
        <template v-if="!loading && (moods.length || genres.length || tempo.length)">
          <span class="dot">·</span>
          <span>{{ moods.length }} moods · {{ genres.length }} genres · {{ tempo.length }} tempo bands</span>
        </template>
      </template>
    </MusicPageHead>

    <!-- Loading — skeleton tiles so the grid shape lands before the data. -->
    <section v-if="loading" class="bp-section" aria-hidden="true">
      <div class="bp-tiles">
        <div v-for="i in 8" :key="i" class="bp-tile bp-skeleton" />
      </div>
    </section>

    <!-- Moods -->
    <section v-if="moods.length" class="bp-section">
      <h2 class="section-title-lg">Moods <span class="bp-count">{{ moods.length }}</span></h2>
      <div class="bp-tiles">
        <NuxtLink
          v-for="(m, i) in moods"
          :key="m.key"
          :to="`/music/browse/mood/${m.key}`"
          class="bp-tile card-tile mood"
          :style="{ background: moodGradient(i) }"
        >
          <BrowseTileArt :artists="m.artists" :alt="m.label" />
          <div class="bp-tile-scrim" />
          <div class="bp-tile-body">
            <div class="bp-tile-label">{{ m.label }}</div>
            <div class="bp-tile-count">{{ m.track_count.toLocaleString() }} tracks</div>
          </div>
        </NuxtLink>
      </div>
    </section>

    <!-- Tempo -->
    <section v-if="tempo.length" class="bp-section">
      <h2 class="section-title-lg">Tempo <span class="bp-count">{{ tempo.length }}</span></h2>
      <div class="bp-tiles">
        <NuxtLink
          v-for="(b, i) in tempo"
          :key="b.key"
          :to="`/music/browse/tempo/${b.key}`"
          class="bp-tile card-tile tempo"
          :style="{ background: tempoGradient(i), '--bpm': midBpm(b) }"
        >
          <BrowseTileArt :artists="b.artists" :alt="b.label" />
          <div class="bp-tile-scrim" />
          <!-- The dot beats at the band's actual BPM — the tile demonstrates
               its own tempo. Pure CSS; the global reduced-motion reset stills
               it for users who've asked for that. -->
          <span class="bp-beat" aria-hidden="true" />
          <div class="bp-tile-body">
            <div class="bp-tile-label">{{ b.label }}</div>
            <div class="bp-tile-sub">{{ b.min_bpm }}–{{ b.max_bpm }} BPM</div>
            <div class="bp-tile-count">{{ b.track_count.toLocaleString() }} tracks</div>
          </div>
        </NuxtLink>
      </div>
    </section>

    <!-- Genres -->
    <section v-if="genres.length" class="bp-section">
      <h2 class="section-title-lg">Genres <span class="bp-count">{{ genres.length }}</span></h2>
      <div class="bp-tiles">
        <NuxtLink
          v-for="(g, i) in genres"
          :key="g.name"
          :to="`/music/browse/genre/${encodeURIComponent(g.name)}`"
          class="bp-tile card-tile genre"
          :style="{ background: genreGradient(i) }"
        >
          <BrowseTileArt :artists="g.artists" :alt="g.label" />
          <div class="bp-tile-scrim" />
          <div class="bp-tile-body">
            <div class="bp-tile-label">{{ g.label }}</div>
            <div v-if="g.parent" class="bp-tile-sub">{{ g.parent }}</div>
            <div class="bp-tile-count">{{ g.track_count.toLocaleString() }} tracks</div>
          </div>
        </NuxtLink>
      </div>
    </section>

    <MusicEmptyState
      v-if="!moods.length && !genres.length && !tempo.length && !loading"
      icon="pulse"
      title="Nothing to browse yet"
    >
      Moods, genres, and tempo come from sonic analysis — turn it on under
      <NuxtLink to="/settings/sonic">Settings → Intelligence</NuxtLink> and the
      tiles fill in as your catalog is analyzed.
    </MusicEmptyState>
  </div>
</template>

<script setup lang="ts">
import { useQuery } from '@pinia/colada'
import { musicBrowseGenresQuery, musicBrowseMoodsQuery, musicBrowseTempoQuery, type GenreBucket, type MoodBucket, type TempoBucket } from '~/queries/music'

definePageMeta({ layout: 'default' })

// Three independent, device-persisted queries fire in parallel (Pinia Colada
// handles concurrency natively) — see queries/music.ts. Each is cached
// separately so revisiting this page paints instantly from the last-known
// snapshot instead of refetching cold.
const moodsQuery = useQuery(musicBrowseMoodsQuery())
const genresQuery = useQuery(musicBrowseGenresQuery())
const tempoQuery = useQuery(musicBrowseTempoQuery())
await Promise.all([waitForQuery(moodsQuery), waitForQuery(genresQuery), waitForQuery(tempoQuery)])

const moods = computed<MoodBucket[]>(() => (moodsQuery.data.value ?? []).filter(x => x.track_count > 0))
const genres = computed<GenreBucket[]>(() => genresQuery.data.value ?? [])
const tempo = computed<TempoBucket[]>(() => (tempoQuery.data.value ?? []).filter(x => x.track_count > 0))
const loading = computed(() => moodsQuery.isPending.value || genresQuery.isPending.value || tempoQuery.isPending.value)

// The beat dot's tempo — band midpoint, clamped so open-ended bands ("Fast",
// max 999) don't strobe. Unitless: CSS divides 60s by it.
function midBpm(b: TempoBucket) {
  const mid = (b.min_bpm + Math.min(b.max_bpm, b.min_bpm + 60)) / 2
  return String(Math.max(50, Math.min(190, Math.round(mid))))
}

// Tile background gradients — deterministic per-index so the rail looks
// painted rather than random. Mood/tempo/genre each get their own palette.
// These are fixed categorical colors (like artwork), not canvas chrome —
// they stay literal in both themes so a mood/genre keeps the same hue.
const MOOD_GRADIENTS = [
  'linear-gradient(135deg, #f59e0b 0%, #d97706 100%)', // happy
  'linear-gradient(135deg, #ec4899 0%, #be185d 100%)', // party
  'linear-gradient(135deg, #ef4444 0%, #dc2626 100%)', // danceable
  'linear-gradient(135deg, #7c2d12 0%, #431407 100%)', // aggressive
  'linear-gradient(135deg, #6366f1 0%, #4338ca 100%)', // electronic
  'linear-gradient(135deg, #84cc16 0%, #4d7c0f 100%)', // acoustic
  'linear-gradient(135deg, #06b6d4 0%, #0e7490 100%)', // relaxed
  'linear-gradient(135deg, #475569 0%, #1e293b 100%)', // sad
  'linear-gradient(135deg, #a855f7 0%, #6b21a8 100%)', // vocal
]
const TEMPO_GRADIENTS = [
  'linear-gradient(135deg, #1e3a8a 0%, #1e293b 100%)', // slow
  'linear-gradient(135deg, #0e7490 0%, #155e75 100%)', // midtempo
  'linear-gradient(135deg, #16a34a 0%, #15803d 100%)', // house/pop
  'linear-gradient(135deg, #ea580c 0%, #c2410c 100%)', // dance
  'linear-gradient(135deg, #b91c1c 0%, #7f1d1d 100%)', // fast
]
const GENRE_GRADIENTS = [
  'linear-gradient(135deg, #0891b2 0%, #0e7490 100%)',
  'linear-gradient(135deg, #c026d3 0%, #86198f 100%)',
  'linear-gradient(135deg, #65a30d 0%, #4d7c0f 100%)',
  'linear-gradient(135deg, #ca8a04 0%, #a16207 100%)',
  'linear-gradient(135deg, #4f46e5 0%, #3730a3 100%)',
  'linear-gradient(135deg, #db2777 0%, #be185d 100%)',
]
function moodGradient(i: number) { return MOOD_GRADIENTS[i % MOOD_GRADIENTS.length] }
function tempoGradient(i: number) { return TEMPO_GRADIENTS[i % TEMPO_GRADIENTS.length] }
function genreGradient(i: number) { return GENRE_GRADIENTS[i % GENRE_GRADIENTS.length] }
</script>

<style scoped>
.browse-page { padding-bottom: 80px; }

.bp-count {
  font-size: 11px;
  font-family: var(--font-mono);
  font-weight: 600;
  color: var(--fg-2);
  vertical-align: 3px;
  margin-left: 6px;
  letter-spacing: 0.06em;
}

.bp-section { margin-bottom: 40px; }
.bp-section .section-title-lg { margin-bottom: 16px; }

.bp-tiles {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 14px;
}
.bp-tile {
  aspect-ratio: 16 / 10;
  border-radius: var(--r-md);
  overflow: hidden;
  position: relative;
  text-decoration: none;
  color: #fff; /* on the fixed gradient tile — stays literal */
  display: flex;
  align-items: flex-end;
  padding: 14px 16px;
  transition: transform 0.15s ease, box-shadow 0.15s ease;
  box-shadow: 0 6px 16px rgb(var(--shade) / 0.35);
}
.bp-tile:hover {
  transform: translateY(-2px);
  box-shadow: 0 12px 24px rgb(var(--shade) / 0.5);
}
/* Literal dark scrim over the (optional) BrowseTileArt cycler — allowed
   exception for artwork per CLAUDE.md. Sits above the art layer, below the
   label text; a no-op when the tile has no artists (art layer is unmounted,
   the gradient background alone still reads fine under it). */
.bp-tile-scrim {
  position: absolute;
  inset: 0;
  z-index: 1;
  background: linear-gradient(0deg, rgba(0, 0, 0, 0.75) 0%, rgba(0, 0, 0, 0.32) 45%, rgba(0, 0, 0, 0.08) 75%, transparent 100%);
  pointer-events: none;
}

.bp-tile-body { position: relative; z-index: 2; width: 100%; }
.bp-tile-label {
  font-size: 17px; font-weight: 700; letter-spacing: -0.005em;
  text-shadow: 0 1px 3px rgba(0, 0, 0, 0.35); /* on gradient — literal */
}
.bp-tile-sub { font-size: 11px; opacity: 0.85; font-family: var(--font-mono); margin-top: 3px; }
.bp-tile-count { font-size: 11px; opacity: 0.75; margin-top: 6px; }

/* The metronome dot — one beat per its band's BPM. calc(60s / --bpm) turns
   the band midpoint into a period; sharp attack, soft decay reads as a
   pulse rather than a blink. Stilled by the global reduced-motion reset. */
.bp-beat {
  position: absolute;
  z-index: 2;
  top: 14px;
  right: 14px;
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.9); /* on gradient — literal */
  box-shadow: 0 0 8px rgba(255, 255, 255, 0.6);
  animation: bp-beat calc(60s / var(--bpm, 100)) ease-out infinite;
}
@keyframes bp-beat {
  0% { transform: scale(1.7); opacity: 1; }
  45% { transform: scale(1); opacity: 0.55; }
  100% { transform: scale(1); opacity: 0.55; }
}

/* Skeletons — quiet glass placeholders, no shimmer theatrics. */
.bp-skeleton {
  background: color-mix(in oklab, var(--bg-2) 70%, transparent);
  border: 1px solid var(--border);
  box-shadow: none;
}

@media (max-width: 720px) {
  /* music.vue's phone header already reads "Browse" directly above this
     page — MusicPageHead's title is redundant weight; the subtitle line
     (counts) stays. */
  :deep(.mhd-title) { display: none; }
  .bp-tiles { grid-template-columns: repeat(auto-fill, minmax(100px, 1fr)); gap: 10px; }
  .page-pad { padding-left: 16px; padding-right: 16px; }

  /* The fixed 16/10 aspect-ratio was sized for >=180px desktop tiles — at
     ~110px phone width it shrinks to ~70px tall, too short for the
     tempo/genre tiles' 3 lines of text (label + sub + count), so
     overflow:hidden was clipping the label's top edge. Let content dictate
     height instead of forcing a ratio. */
  .bp-tile { aspect-ratio: auto; min-height: 92px; padding: 10px 12px; }
  .bp-tile-label { font-size: 15px; }
}
</style>
