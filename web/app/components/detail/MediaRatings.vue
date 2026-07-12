<script setup lang="ts">
import type { ExternalRating } from '~~/shared/types'

const props = defineProps<{ ratings?: ExternalRating[] | null }>()

interface SourceMeta { label: string; max: number; unit: 'ten' | 'pct' | 'five' }

const SOURCE_META: Record<string, SourceMeta> = {
  imdb: { label: 'IMDb', max: 10, unit: 'ten' },
  tmdb: { label: 'TMDB', max: 10, unit: 'ten' },
  rotten_tomatoes: { label: 'Rotten Tomatoes', max: 100, unit: 'pct' },
  rottentomatoes: { label: 'Rotten Tomatoes', max: 100, unit: 'pct' },
  metacritic: { label: 'Metacritic', max: 100, unit: 'pct' },
  letterboxd: { label: 'Letterboxd', max: 5, unit: 'five' },
  trakt: { label: 'Trakt', max: 10, unit: 'ten' },
  anidb: { label: 'AniDB', max: 10, unit: 'ten' },
  tvdb: { label: 'TVDB', max: 10, unit: 'ten' },
  tvmaze: { label: 'TVmaze', max: 10, unit: 'ten' },
  fanart: { label: 'Fanart', max: 10, unit: 'ten' },
}

function sourceLabel(s: string): string { return SOURCE_META[s]?.label || s }

const RATING_LOGOS: Record<string, string> = {
  imdb: '/logos/imdb.svg',
  rotten_tomatoes: '/logos/rotten_tomatoes.svg',
  rottentomatoes: '/logos/rotten_tomatoes.svg',
  metacritic: '/logos/metacritic.svg',
  tmdb: '/logos/tmdb.svg',
  letterboxd: '/logos/letterboxd.svg',
  trakt: '/logos/trakt.svg',
  tvdb: '/logos/tvdb.png',
  tvmaze: '/logos/tvmaze.png',
  fanart: '/logos/fanart.png',
  anidb: '/logos/anidb.png',
}

const missing = reactive(new Set<string>())
function logo(source: string): string | null {
  if (missing.has(source)) return null
  return RATING_LOGOS[source] || null
}
function onMissing(source: string) { missing.add(source) }

function parseNum(v: any): number | null {
  if (v == null || v === '') return null
  const m = String(v).match(/-?\d+(\.\d+)?/)
  if (!m) return null
  const n = parseFloat(m[0])
  return isNaN(n) ? null : n
}

function percentOf(source: string, value: any): number {
  const max = SOURCE_META[source]?.max ?? 10
  const n = parseNum(value)
  if (n == null) return 0
  return Math.max(0, Math.min(100, (n / max) * 100))
}

function formatValue(source: string, value: any): string {
  const meta = SOURCE_META[source]
  const n = parseNum(value)
  if (n == null) return String(value || '—')
  if (meta?.unit === 'pct') return `${Math.round(n)}%`
  if (meta?.unit === 'five') return `${n.toFixed(1)}/5`
  return `${n.toFixed(1)}/10`
}

function scoreClass(pct: number): string {
  if (pct >= 85) return 'great'
  if (pct >= 70) return 'good'
  if (pct >= 50) return 'ok'
  return 'low'
}

// Merge anidb_permanent + anidb_temporary into a single averaged anidb rating.
const displayRatings = computed<ExternalRating[]>(() => {
  const list = props.ratings || []
  const out: ExternalRating[] = []
  const anidbParts: { rating: ExternalRating; num: number }[] = []

  for (const r of list) {
    if (r.source === 'anidb_permanent' || r.source === 'anidb_temporary') {
      const n = parseNum(r.value)
      if (n !== null) anidbParts.push({ rating: r, num: n })
      else out.push({ ...r, source: 'anidb' })
      continue
    }
    out.push(r)
  }

  if (anidbParts.length === 1) {
    out.push({ ...anidbParts[0]!.rating, source: 'anidb' })
  } else if (anidbParts.length > 1) {
    const avg = anidbParts.reduce((s, p) => s + p.num, 0) / anidbParts.length
    const base = anidbParts[0]!.rating
    const totalVotes = anidbParts.reduce((s, p) => s + (p.rating.votes || 0), 0)
    out.push({
      ...base,
      source: 'anidb',
      value: avg.toFixed(2),
      votes: totalVotes || base.votes,
    })
  }

  return out
})
</script>

<template>
  <div v-if="displayRatings.length" class="ratings">
    <div
      v-for="r in displayRatings"
      :key="r.source"
      class="rating-card"
      :title="sourceLabel(r.source)"
    >
      <div class="rating-head">
        <NuxtImg
          v-if="logo(r.source)"
          :src="logo(r.source)!"
          :alt="sourceLabel(r.source)"
          class="rating-logo"
          @error="onMissing(r.source)"
        />
        <div v-else class="rating-source">{{ sourceLabel(r.source) }}</div>
        <div class="rating-value">{{ formatValue(r.source, r.value) }}</div>
      </div>
      <div class="rating-meter">
        <div class="rating-bar">
          <div
            class="rating-bar-fill"
            :class="scoreClass(percentOf(r.source, r.value))"
            :style="{ width: percentOf(r.source, r.value) + '%' }"
          />
        </div>
        <span class="rating-pct">{{ Math.round(percentOf(r.source, r.value)) }}%</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ratings { display: flex; flex-direction: column; gap: 6px; }

.rating-card {
  /* Floats over the hero backdrop (see movies/tv detail pages' .hero-side).
     Theme-aware glass: the old literal dark glass was an unreadable black
     slab on the light theme's paper. */
  display: flex; flex-direction: column; gap: 7px;
  padding: 10px 12px 11px;
  background: color-mix(in oklab, var(--bg-2) 80%, transparent);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  box-shadow: var(--shadow-card);
  transition: border-color 0.15s, background 0.15s;
}
.rating-card:hover {
  border-color: var(--border-strong);
  background: color-mix(in oklab, var(--bg-2) 92%, transparent);
}

.rating-head {
  display: flex; align-items: center; justify-content: space-between;
  gap: 10px;
}
.rating-logo {
  height: 16px; width: auto; max-width: 92px;
  display: block; opacity: 0.95; flex-shrink: 0;
}
.rating-source {
  font-size: 10px; color: var(--fg-2); font-family: var(--font-mono);
  font-weight: 700; text-transform: uppercase; letter-spacing: 0.06em;
}
.rating-value {
  font-size: 17px; font-weight: 700; color: var(--fg-0);
  line-height: 1; letter-spacing: -0.01em; font-variant-numeric: tabular-nums;
}

.rating-meter {
  display: flex; align-items: center; gap: 8px;
}
.rating-bar {
  flex: 1; height: 4px; border-radius: 999px;
  background: rgb(var(--ink) / 0.10); overflow: hidden;
}
.rating-bar-fill {
  height: 100%; border-radius: 999px;
  transition: width 0.5s cubic-bezier(0.2, 0.8, 0.2, 1);
}
.rating-bar-fill.great { background: linear-gradient(90deg, #4ade80, #22c55e); box-shadow: 0 0 8px rgba(34,197,94,0.45); }
.rating-bar-fill.good  { background: linear-gradient(90deg, #fbbf24, #f59e0b); box-shadow: 0 0 8px rgba(245,158,11,0.4); }
.rating-bar-fill.ok    { background: linear-gradient(90deg, #fb923c, #f97316); box-shadow: 0 0 6px rgba(249,115,22,0.35); }
.rating-bar-fill.low   { background: linear-gradient(90deg, #f87171, #ef4444); box-shadow: 0 0 6px rgba(239,68,68,0.35); }

.rating-pct {
  font-size: 10px; font-weight: 700; font-family: var(--font-mono);
  color: var(--fg-2); letter-spacing: 0.04em;
  min-width: 30px; text-align: right; font-variant-numeric: tabular-nums;
}
</style>
