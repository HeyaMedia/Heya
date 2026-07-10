<template>
  <section class="hero-newin">
    <div class="newin-bg" :class="{ 'ambient-extended': ambientEnabled }">
      <NuxtImg
        v-if="featured"
        :src="bgUrl ?? undefined"
        :width="1920"
        :quality="75"
        class="newin-bg-img"
        @error="(e: Event | string) => { if (typeof e !== 'string') (e.target as HTMLImageElement).style.display = 'none' }"
      />
      <div class="newin-bg-gradient" />
    </div>

    <div class="newin-inner">
      <div class="newin-lead">
        <div>
          <div class="newin-eyebrow">New in your library</div>
          <template v-if="featured">
            <NuxtLink :to="`/tv/${featured.slug}`" class="newin-title-link">
              <h1 class="newin-title">{{ featured.title }}</h1>
            </NuxtLink>
            <p class="newin-featured-sub">{{ entrySub(featured) }} · {{ relTime(featured.added_at) }}</p>
          </template>
        </div>
        <p class="newin-sum">{{ summary }}</p>
      </div>

      <div class="newin-feed">
        <NuxtLink
          v-for="ev in feed"
          :key="ev.key"
          :to="ev.to"
          class="newin-card"
        >
          <div class="newin-card-art">
            <NuxtImg
              :src="ev.art"
              :width="240"
              :quality="80"
              densities="1x 2x"
              alt=""
              @error="(e: Event | string) => { if (typeof e !== 'string') (e.target as HTMLImageElement).style.visibility = 'hidden' }"
            />
            <span class="newin-card-kind">{{ ev.kind }}</span>
          </div>
          <div class="newin-card-title">{{ ev.title }}</div>
          <div class="newin-card-sub">{{ ev.sub }}</div>
          <div class="newin-card-time">{{ ev.time }}</div>
        </NuxtLink>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
// "New" — the library pulse. A featured drop up top, the latest arrivals as
// a horizontal shelf of cards along the bottom, each stamped with what it is
// and when it landed. Feeds entirely off data the page already fetched.
import type { MediaItem } from '~~/shared/types'

export interface RecentTVEntry {
  media_item_id: number
  media_item_public_id?: string
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

const bgUrl = computed(() => (featured.value
  ? useBackdropUrl({ id: featured.value.media_item_id, public_id: featured.value.media_item_public_id })
  : null) || null)

// Ambient extension: with the ambient background on, the featured entry's
// backdrop becomes the full-page layer — the local `.newin-bg-img` hides via
// .ambient-extended and the AmbientBackdrop layer follows the feed through
// this watcher.
const { ambientEnabled } = useAppearance()
const ambientArt = useAmbientArt()
watch([bgUrl, ambientEnabled], ([url, on]) => {
  if (on && url) ambientArt.set(url)
  else ambientArt.clear()
}, { immediate: true })

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
  for (const e of props.tv.slice(0, 10)) {
    if (featured.value && e === featured.value) continue
    rows.push({
      key: `tv-${e.media_item_id}-${e.kind}-${e.season_number}-${e.episode_number}-${e.added_at}`,
      to: `/tv/${e.slug}`,
      art: usePosterUrl({ id: e.media_item_id, public_id: e.media_item_public_id }) ?? '',
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
      art: usePosterUrl(a) ?? '',
      title: a.title,
      sub: (a as MediaItem & { sub?: string }).sub ?? '',
      kind: 'ARTIST',
      time: '',
    })
  }
  return rows.slice(0, 8)
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
    linear-gradient(to right, var(--bg-1) 0%, color-mix(in srgb, var(--bg-1) 72%, transparent) 45%, color-mix(in srgb, var(--bg-1) 30%, transparent) 100%),
    linear-gradient(to top, var(--bg-1) 0%, color-mix(in srgb, var(--bg-1) 75%, transparent) 30%, transparent 60%);
}
/* Ambient extension: the AmbientBackdrop layer shows the featured entry's
   backdrop full-page (see the ambientArt watcher), so the local copy hides —
   its different crop would seam at the hero edges — and the fade softens so
   the artwork continues past the hero bottom instead of ending at solid
   canvas. */
.newin-bg.ambient-extended .newin-bg-img { display: none; }
.newin-bg.ambient-extended .newin-bg-gradient {
  background:
    linear-gradient(to right,
      color-mix(in srgb, var(--bg-1) 68%, transparent) 0%,
      color-mix(in srgb, var(--bg-1) 40%, transparent) 45%,
      color-mix(in srgb, var(--bg-1) 16%, transparent) 100%),
    linear-gradient(to top,
      color-mix(in srgb, var(--bg-1) 24%, transparent) 0%,
      color-mix(in srgb, var(--bg-1) 50%, transparent) 30%,
      transparent 60%);
}
.newin-inner {
  position: relative;
  z-index: 2;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  height: 100%;
  padding: 44px 40px 24px;
}
.newin-lead {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 24px;
}
.newin-eyebrow {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: var(--gold);
  margin-bottom: 8px;
}
.newin-title-link { color: inherit; text-decoration: none; }
.newin-title-link:hover .newin-title { color: var(--gold); }
.newin-title {
  font-size: 38px;
  font-weight: 600;
  letter-spacing: -0.025em;
  line-height: 1.05;
  margin: 0 0 6px;
  text-wrap: balance;
  transition: color 0.15s;
}
.newin-featured-sub {
  font-family: var(--font-mono);
  font-size: 12.5px;
  color: var(--fg-1);
  margin: 0;
}
.newin-sum {
  font-size: 12.5px;
  color: var(--fg-2);
  margin: 0 0 4px;
  text-align: right;
  flex-shrink: 0;
}
.newin-feed {
  display: flex;
  gap: 14px;
  overflow-x: auto;
  scrollbar-width: none;
  padding-top: 16px;
}
.newin-feed::-webkit-scrollbar { display: none; }
.newin-card {
  width: 118px;
  flex-shrink: 0;
  color: inherit;
  text-decoration: none;
}
.newin-card-art {
  position: relative;
  width: 118px;
  aspect-ratio: 2 / 3;
  border-radius: var(--r-sm);
  overflow: hidden;
  background: var(--bg-3);
  border: 1px solid var(--border);
  transition: transform 0.15s, border-color 0.15s;
}
.newin-card:hover .newin-card-art {
  transform: translateY(-2px);
  border-color: var(--border-strong);
}
.newin-card-art img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}
.newin-card-kind {
  position: absolute;
  top: 6px;
  left: 6px;
  font-family: var(--font-mono);
  font-size: 8.5px;
  letter-spacing: 0.1em;
  color: var(--gold);
  background: rgba(7, 7, 10, 0.8); /* on artwork — stays literal */
  border: 1px solid color-mix(in srgb, var(--accent) 35%, transparent);
  border-radius: 999px;
  padding: 2px 6px;
}
.newin-card-title {
  font-size: 12px;
  font-weight: 600;
  margin-top: 7px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.newin-card-sub {
  font-size: 10.5px;
  color: var(--fg-2);
  margin-top: 2px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.newin-card-time {
  font-family: var(--font-mono);
  font-size: 9.5px;
  color: var(--fg-3);
  margin-top: 2px;
}
@media (max-width: 900px) {
  .newin-inner { padding: 20px; }
  .newin-title { font-size: 28px; }
  .newin-sum { display: none; }
}
</style>
