<template>
  <div class="page-pad browse-page">
    <h1 class="bp-title">Browse</h1>
    <p class="bp-sub">Explore your library by feel, genre, or tempo. Each tile counts what's been analyzed so far — the lists fill in as the sonic-analysis scheduler chews through your catalog.</p>

    <!-- Moods -->
    <section v-if="moods.length" class="bp-section">
      <h2 class="section-title-lg">Moods</h2>
      <div class="bp-tiles">
        <NuxtLink
          v-for="(m, i) in moods"
          :key="m.key"
          :to="`/music/browse/mood/${m.key}`"
          class="bp-tile card-tile mood"
          :style="{ background: moodGradient(i) }"
        >
          <div class="bp-tile-body">
            <div class="bp-tile-label">{{ m.label }}</div>
            <div class="bp-tile-count">{{ m.track_count.toLocaleString() }} tracks</div>
          </div>
        </NuxtLink>
      </div>
    </section>

    <!-- Tempo -->
    <section v-if="tempo.length" class="bp-section">
      <h2 class="section-title-lg">Tempo</h2>
      <div class="bp-tiles">
        <NuxtLink
          v-for="(b, i) in tempo"
          :key="b.key"
          :to="`/music/browse/tempo/${b.key}`"
          class="bp-tile card-tile tempo"
          :style="{ background: tempoGradient(i) }"
        >
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
      <h2 class="section-title-lg">Genres</h2>
      <div class="bp-tiles">
        <NuxtLink
          v-for="(g, i) in genres"
          :key="g.name"
          :to="`/music/browse/genre/${encodeURIComponent(g.name)}`"
          class="bp-tile card-tile genre"
          :style="{ background: genreGradient(i) }"
        >
          <div class="bp-tile-body">
            <div class="bp-tile-label">{{ g.label }}</div>
            <div v-if="g.parent" class="bp-tile-sub">{{ g.parent }}</div>
            <div class="bp-tile-count">{{ g.track_count.toLocaleString() }} tracks</div>
          </div>
        </NuxtLink>
      </div>
    </section>

    <div v-if="!moods.length && !genres.length && !tempo.length && !loading" class="bp-empty">
      Nothing to browse yet — the sonic analyzer hasn't tagged any tracks. Enable it under
      <NuxtLink to="/settings/server" class="bp-empty-link">Settings → Server</NuxtLink>
      to start.
    </div>
  </div>
</template>

<script setup lang="ts">
import { useQuery } from '@tanstack/vue-query'

definePageMeta({ layout: 'default' })

interface MoodBucket { key: string; label: string; threshold: number; track_count: number }
interface GenreBucket { name: string; label: string; parent: string; track_count: number }
interface TempoBucket { key: string; label: string; min_bpm: number; max_bpm: number; track_count: number }

const { $heya } = useNuxtApp()

// Three independent queries fire in parallel (vue-query handles concurrency
// natively). Each is cached separately so revisiting this page is instant.
const moodsQuery = useQuery({
  queryKey: ['music', 'browse', 'moods'],
  queryFn: async () => ((await $heya('/api/music/browse/moods')) as { items: MoodBucket[] }).items ?? [],
  staleTime: 1000 * 60 * 5,
})
const genresQuery = useQuery({
  queryKey: ['music', 'browse', 'genres'],
  queryFn: async () => ((await $heya('/api/music/browse/genres')) as { items: GenreBucket[] }).items ?? [],
  staleTime: 1000 * 60 * 5,
})
const tempoQuery = useQuery({
  queryKey: ['music', 'browse', 'tempo'],
  queryFn: async () => ((await $heya('/api/music/browse/tempo')) as { items: TempoBucket[] }).items ?? [],
  staleTime: 1000 * 60 * 5,
})

const moods = computed<MoodBucket[]>(() => (moodsQuery.data.value ?? []).filter(x => x.track_count > 0))
const genres = computed<GenreBucket[]>(() => genresQuery.data.value ?? [])
const tempo = computed<TempoBucket[]>(() => (tempoQuery.data.value ?? []).filter(x => x.track_count > 0))
const loading = computed(() => moodsQuery.isPending.value || genresQuery.isPending.value || tempoQuery.isPending.value)

// Tile background gradients — deterministic per-index so the rail looks
// painted rather than random. Mood/tempo/genre each get their own palette.
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
.bp-title { font-size: 30px; font-weight: 700; margin-bottom: 8px; letter-spacing: -0.01em; }
.bp-sub { color: var(--fg-3); font-size: 13px; max-width: 600px; margin-bottom: 32px; }

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
  color: #fff;
  display: flex;
  align-items: flex-end;
  padding: 14px 16px;
  transition: transform 0.15s ease, box-shadow 0.15s ease;
  box-shadow: 0 6px 16px rgba(0, 0, 0, 0.35);
}
.bp-tile:hover {
  transform: translateY(-2px);
  box-shadow: 0 12px 24px rgba(0, 0, 0, 0.5);
}
.bp-tile-body { width: 100%; }
.bp-tile-label { font-size: 17px; font-weight: 700; letter-spacing: -0.005em; }
.bp-tile-sub { font-size: 11px; opacity: 0.85; font-family: var(--font-mono); margin-top: 3px; }
.bp-tile-count { font-size: 11px; opacity: 0.7; margin-top: 6px; }

.bp-empty {
  color: var(--fg-3);
  font-size: 13px;
  padding: 40px 0;
  max-width: 520px;
}
.bp-empty-link { color: var(--gold); text-decoration: underline; }
</style>
