<template>
  <div class="page-pad ys-page">
    <h1 class="ys-title">Your Sound</h1>
    <p class="ys-sub">
      A picture of your listening, derived by joining every scrobble against the sonic-analysis facets.
      <span v-if="stats">Based on {{ stats.total_plays.toLocaleString() }} {{ stats.total_plays === 1 ? 'play' : 'plays' }}.</span>
    </p>

    <div v-if="loading" class="m-loading">Loading…</div>

    <div v-else-if="!stats || stats.total_plays === 0" class="ys-empty">
      <Icon name="music" :size="40" class="m-empty-icon" />
      <h3>No listening data yet</h3>
      <p>Play a few tracks for at least 30 seconds each and this page will fill in. Stats refresh once a minute.</p>
    </div>

    <div v-else class="ys-grid">
      <!-- Top genres — horizontal bars ranked by play count -->
      <section class="ys-card">
        <h2 class="ys-card-title">Top Genres</h2>
        <p v-if="!stats.top_genres.length" class="ys-card-empty">
          No analyzed plays yet — the sonic analyzer hasn't tagged any of the tracks in your history.
        </p>
        <div v-else class="ys-bars">
          <div v-for="g in stats.top_genres.slice(0, 10)" :key="g.genre_name" class="ys-bar-row">
            <div class="ys-bar-label" :title="g.genre_name">{{ genreLabel(g.genre_name) }}</div>
            <div class="ys-bar-track">
              <div class="ys-bar-fill" :style="{ width: barPct(g.play_count, topGenrePlays) + '%' }" />
            </div>
            <div class="ys-bar-count mono">{{ g.play_count.toLocaleString() }}</div>
          </div>
        </div>
      </section>

      <!-- Mood profile — horizontal bars showing avg classifier score in [0..1] -->
      <section class="ys-card">
        <h2 class="ys-card-title">Mood Profile</h2>
        <p v-if="!stats.mood_avg.length" class="ys-card-empty">
          No mood-tagged plays yet.
        </p>
        <div v-else class="ys-bars">
          <div v-for="m in stats.mood_avg" :key="m.mood_key" class="ys-bar-row">
            <div class="ys-bar-label">{{ moodLabel(m.mood_key) }}</div>
            <div class="ys-bar-track">
              <div class="ys-bar-fill mood" :style="{ width: (m.avg_score * 100).toFixed(0) + '%' }" />
            </div>
            <div class="ys-bar-count mono">{{ (m.avg_score * 100).toFixed(0) }}</div>
          </div>
        </div>
      </section>

      <!-- Tempo histogram — vertical bars, one per BPM band -->
      <section class="ys-card ys-tempo">
        <h2 class="ys-card-title">Tempo Histogram</h2>
        <p v-if="!stats.tempo_histogram.length" class="ys-card-empty">
          No BPM-tagged plays yet.
        </p>
        <div v-else class="ys-tempo-chart">
          <div v-for="b in stats.tempo_histogram" :key="b.band" class="ys-tempo-col">
            <div class="ys-tempo-bar" :style="{ height: (b.play_count / maxTempoPlays * 100) + '%' }">
              <span class="ys-tempo-count">{{ b.play_count }}</span>
            </div>
            <div class="ys-tempo-band">{{ tempoLabel(b.band) }}</div>
          </div>
        </div>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useQuery } from '@tanstack/vue-query'

definePageMeta({ layout: 'default' })

interface ListeningStats {
  total_plays: number
  top_genres: Array<{ genre_name: string; play_count: number }>
  mood_avg: Array<{ mood_key: string; avg_score: number; sample_count: number }>
  tempo_histogram: Array<{ band: string; play_count: number }>
}

const { $heya } = useNuxtApp()
const statsQuery = useQuery({
  queryKey: ['me', 'listening-stats'],
  queryFn: async () => (await $heya('/api/me/listening-stats')) as ListeningStats,
  staleTime: 1000 * 60,
})
const stats = computed<ListeningStats | null>(() => statsQuery.data.value ?? null)
const loading = computed(() => statsQuery.isPending.value)

// Pre-compute max-of-bar so each bar group scales independently.
const topGenrePlays = computed(() => stats.value?.top_genres[0]?.play_count ?? 1)
const maxTempoPlays = computed(() => Math.max(1, ...(stats.value?.tempo_histogram.map(b => b.play_count) ?? [1])))

function barPct(n: number, max: number) {
  if (max <= 0) return 0
  return Math.max(2, Math.round((n / max) * 100))
}

// Strip the "Parent---Leaf" hierarchy down to the leaf for chart labels;
// the full path is on the title attr for hover.
function genreLabel(raw: string) {
  const parts = raw.split('---')
  return parts[parts.length - 1] ?? raw
}

function moodLabel(key: string) {
  const map: Record<string, string> = {
    mood_happy: 'Happy', mood_sad: 'Melancholic', mood_aggressive: 'Aggressive',
    mood_relaxed: 'Relaxed', mood_party: 'Party', mood_electronic: 'Electronic',
    mood_acoustic: 'Acoustic', danceability: 'Danceable', voice: 'Vocal',
  }
  return map[key] ?? key
}

function tempoLabel(band: string) {
  // "0-90" → "<90", "150-300" → "150+", others stay as "a–b".
  if (band.startsWith('0-')) return '<' + band.split('-')[1]
  if (band.endsWith('-300')) return band.split('-')[0] + '+'
  return band.replace('-', '–')
}
</script>

<style scoped>
.ys-page { padding-bottom: 80px; max-width: 1100px; }
.ys-title { font-size: 30px; font-weight: 700; margin-bottom: 8px; letter-spacing: -0.01em; }
.ys-sub { color: var(--fg-3); font-size: 13px; margin-bottom: 32px; max-width: 700px; }
.m-loading { color: var(--fg-3); padding: 24px 0; font-size: 13px; }

.ys-empty {
  display: flex; flex-direction: column; align-items: center;
  text-align: center; padding: 80px 0;
  color: var(--fg-2);
  gap: 8px;
}
.ys-empty h3 { font-size: 18px; font-weight: 600; color: var(--fg-1); margin-top: 8px; }
.ys-empty p { font-size: 13px; max-width: 420px; color: var(--fg-3); }
.m-empty-icon { color: var(--fg-3); }

.ys-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 18px;
}
.ys-tempo { grid-column: 1 / -1; }

.ys-card {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 18px 20px 22px;
}
.ys-card-title {
  font-size: 13px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--fg-2);
  margin-bottom: 18px;
}
.ys-card-empty { color: var(--fg-3); font-size: 12px; }

.ys-bars { display: flex; flex-direction: column; gap: 10px; }
.ys-bar-row {
  display: grid;
  grid-template-columns: 120px 1fr 50px;
  align-items: center;
  gap: 12px;
}
.ys-bar-label {
  font-size: 12px;
  color: var(--fg-1);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ys-bar-track {
  height: 18px;
  background: rgba(255,255,255,0.04);
  border-radius: 6px;
  overflow: hidden;
}
.ys-bar-fill {
  height: 100%;
  background: linear-gradient(90deg, var(--gold) 0%, #f59e0b 100%);
  transition: width 0.4s ease;
}
.ys-bar-fill.mood {
  background: linear-gradient(90deg, #6366f1 0%, #ec4899 100%);
}
.ys-bar-count { font-size: 11px; color: var(--fg-3); text-align: right; }

.ys-tempo-chart {
  display: flex;
  align-items: flex-end;
  gap: 16px;
  height: 220px;
  padding-bottom: 24px;
  position: relative;
}
.ys-tempo-col {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  height: 100%;
  position: relative;
}
.ys-tempo-bar {
  width: 100%;
  background: linear-gradient(180deg, var(--gold) 0%, #ea580c 100%);
  border-radius: 6px 6px 0 0;
  position: relative;
  min-height: 4px;
  margin-top: auto;
  transition: height 0.4s ease;
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding-top: 4px;
}
.ys-tempo-count {
  font-size: 11px;
  font-family: var(--font-mono);
  color: var(--bg-0);
  font-weight: 700;
}
.ys-tempo-band {
  font-size: 11px;
  font-family: var(--font-mono);
  color: var(--fg-3);
  position: absolute;
  bottom: -22px;
  white-space: nowrap;
}
.mono { font-family: var(--font-mono); }

@media (max-width: 800px) {
  .ys-grid { grid-template-columns: 1fr; }
}
</style>
