<template>
  <section class="hero-newin">
    <div class="newin-bg">
      <NuxtImg
        v-if="featured"
        :src="useBackdropUrl(featured.media_item_id) ?? undefined"
        :width="1920"
        :quality="75"
        class="newin-bg-img"
        @error="(e: Event | string) => { if (typeof e !== 'string') (e.target as HTMLImageElement).style.display = 'none' }"
      />
      <div class="newin-bg-gradient" />
    </div>

    <div class="newin-inner">
      <div class="newin-lead">
        <div class="newin-eyebrow">New in your library</div>
        <template v-if="featured">
          <NuxtLink :to="`/tv/${featured.slug}`" class="newin-title-link">
            <h1 class="newin-title">{{ featured.title }}</h1>
          </NuxtLink>
          <p class="newin-featured-sub">{{ entrySub(featured) }} · {{ relTime(featured.added_at) }}</p>
        </template>
        <p class="newin-sum">{{ summary }}</p>
      </div>

      <div class="newin-feed">
        <NuxtLink
          v-for="ev in feed"
          :key="ev.key"
          :to="ev.to"
          class="newin-row"
        >
          <img class="newin-row-art" :src="ev.art" alt="" @error="(e) => ((e.target as HTMLImageElement).style.visibility = 'hidden')">
          <div class="newin-row-body">
            <div class="newin-row-title">{{ ev.title }}</div>
            <div class="newin-row-sub">{{ ev.sub }}</div>
          </div>
          <div class="newin-row-side">
            <span class="newin-row-kind">{{ ev.kind }}</span>
            <span class="newin-row-time">{{ ev.time }}</span>
          </div>
        </NuxtLink>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
// "New" — the library pulse. One featured drop plus a compact feed of the
// latest arrivals across TV and music, each stamped with what it is and when
// it landed. Feeds entirely off data the page already fetched.
import type { MediaItem } from '~~/shared/types'

export interface RecentTVEntry {
  media_item_id: number
  title: string
  slug: string
  kind: 'series' | 'season' | 'episodes' | 'episode'
  season_number: number
  episode_number: number
  episode_title?: string
  season_count: number
  episode_count: number
  added_at: string
}

const props = defineProps<{
  tv: RecentTVEntry[]
  albums: (MediaItem & { sub?: string })[]
  artists: (MediaItem & { sub?: string })[]
}>()

const featured = computed(() => {
  // Biggest recent TV event wins the spotlight: prefer a whole new show,
  // then a new season, else just the newest entry.
  const tv = props.tv
  return tv.find(e => e.kind === 'series') ?? tv.find(e => e.kind === 'season') ?? tv[0]
})

function entrySub(e: RecentTVEntry): string {
  switch (e.kind) {
    case 'series': return e.season_count > 1 ? `New show · ${e.season_count} seasons` : `New show · ${e.episode_count} episode${e.episode_count === 1 ? '' : 's'}`
    case 'season': return `New season ${e.season_number} · ${e.episode_count} episode${e.episode_count === 1 ? '' : 's'}`
    case 'episodes': return `Season ${e.season_number} · ${e.episode_count} new episodes`
    case 'episode': {
      const code = `S${String(e.season_number).padStart(2, '0')}E${String(e.episode_number).padStart(2, '0')}`
      return e.episode_title ? `${code} · ${e.episode_title}` : code
    }
  }
}

function relTime(iso: string): string {
  const ms = Date.now() - new Date(iso).getTime()
  const h = Math.floor(ms / 3_600_000)
  if (h < 1) return 'just now'
  if (h < 24) return `${h}h ago`
  const d = Math.floor(h / 24)
  if (d < 7) return `${d}d ago`
  return `${Math.floor(d / 7)}w ago`
}

const summary = computed(() => {
  const parts: string[] = []
  const eps = props.tv.filter(e => e.kind === 'episode' || e.kind === 'episodes').length
  const seasons = props.tv.filter(e => e.kind === 'season').length
  const shows = props.tv.filter(e => e.kind === 'series').length
  if (shows) parts.push(`${shows} new show${shows === 1 ? '' : 's'}`)
  if (seasons) parts.push(`${seasons} new season${seasons === 1 ? '' : 's'}`)
  if (eps) parts.push(`${eps} episode drop${eps === 1 ? '' : 's'}`)
  if (props.artists.length) parts.push(`${props.artists.length} artist${props.artists.length === 1 ? '' : 's'}`)
  return parts.length ? `Lately: ${parts.join(' · ')}` : ''
})

interface FeedRow { key: string; to: string; art: string; title: string; sub: string; kind: string; time: string }

const feed = computed<FeedRow[]>(() => {
  const rows: FeedRow[] = []
  for (const e of props.tv.slice(0, 8)) {
    if (featured.value && e === featured.value) continue
    rows.push({
      key: `tv-${e.media_item_id}-${e.kind}-${e.season_number}-${e.episode_number}-${e.added_at}`,
      to: `/tv/${e.slug}`,
      art: usePosterUrl(e.media_item_id) ?? '',
      title: e.title,
      sub: entrySub(e),
      kind: e.kind === 'series' ? 'SHOW' : e.kind === 'season' ? 'SEASON' : 'EPISODE',
      time: relTime(e.added_at),
    })
  }
  for (const a of props.artists.slice(0, 3)) {
    rows.push({
      key: `artist-${a.id}`,
      to: mediaUrl(a),
      art: usePosterUrl(a.id) ?? '',
      title: a.title,
      sub: (a as MediaItem & { sub?: string }).sub ?? '',
      kind: 'ARTIST',
      time: '',
    })
  }
  return rows.slice(0, 5)
})
</script>

<style scoped>
.hero-newin { position: relative; height: 100%; }
.newin-bg { position: absolute; inset: 0; }
.newin-bg-img {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.newin-bg-gradient {
  position: absolute;
  inset: 0;
  background:
    linear-gradient(to right, var(--bg-1) 0%, rgba(12,12,16,0.72) 45%, rgba(12,12,16,0.3) 100%),
    linear-gradient(to top, var(--bg-1) 0%, transparent 40%);
}
.newin-inner {
  position: relative;
  z-index: 2;
  display: grid;
  grid-template-columns: minmax(300px, 1fr) minmax(0, 560px);
  align-items: center;
  gap: 48px;
  height: 100%;
  padding: 48px 40px;
  max-width: 1240px;
}
.newin-eyebrow {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: var(--gold);
  margin-bottom: 10px;
}
.newin-title-link { color: inherit; text-decoration: none; }
.newin-title-link:hover .newin-title { color: var(--gold); }
.newin-title {
  font-size: 44px;
  font-weight: 600;
  letter-spacing: -0.025em;
  line-height: 1.05;
  margin: 0 0 8px;
  text-wrap: balance;
  transition: color 0.15s;
}
.newin-featured-sub {
  font-family: var(--font-mono);
  font-size: 13px;
  color: var(--fg-1);
  margin: 0 0 18px;
}
.newin-sum {
  font-size: 13px;
  color: var(--fg-2);
  margin: 0;
}
.newin-feed {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.newin-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 12px 8px 8px;
  border-radius: var(--r-md);
  background: rgba(7, 7, 10, 0.5);
  border: 1px solid var(--border);
  color: inherit;
  text-decoration: none;
  transition: background 0.15s, border-color 0.15s;
}
.newin-row:hover {
  background: rgba(19, 19, 24, 0.75);
  border-color: var(--border-strong);
}
.newin-row-art {
  width: 34px;
  height: 50px;
  object-fit: cover;
  border-radius: var(--r-xs);
  background: var(--bg-3);
  flex-shrink: 0;
}
.newin-row-body { min-width: 0; flex: 1; }
.newin-row-title {
  font-size: 13.5px;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.newin-row-sub {
  font-size: 12px;
  color: var(--fg-2);
  margin-top: 2px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.newin-row-side {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 3px;
  flex-shrink: 0;
}
.newin-row-kind {
  font-family: var(--font-mono);
  font-size: 9.5px;
  letter-spacing: 0.12em;
  color: var(--gold);
  border: 1px solid rgba(230, 185, 74, 0.3);
  border-radius: 999px;
  padding: 2px 7px;
}
.newin-row-time {
  font-family: var(--font-mono);
  font-size: 10.5px;
  color: var(--fg-3);
}
@media (max-width: 900px) {
  .newin-inner { grid-template-columns: 1fr; gap: 18px; padding: 24px 20px; align-content: center; }
  .newin-title { font-size: 32px; }
  .newin-row:nth-child(n+4) { display: none; }
}
</style>
