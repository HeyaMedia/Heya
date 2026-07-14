<template>
  <div v-if="loading" class="page-pad m-loading">Loading…</div>
  <div v-else-if="!detail" class="page-pad m-empty">
    <p>Failed to load this feed.</p>
    <NuxtLink to="/music/podcasts" class="pd-back">← Back to Podcasts</NuxtLink>
  </div>
  <div v-else class="pd-page">
    <header class="pd-hero">
      <div class="pd-hero-bg" :style="heroBgStyle" />
      <div class="pd-hero-tint" />
      <div class="pd-hero-content">
        <div class="pd-hero-art" :class="{ 'pd-hero-art-fallback': !detail.artwork_url }">
          <LoadingImage v-if="detail.artwork_url" :src="detail.artwork_url" :alt="detail.title" />
          <Icon v-else name="mic" :size="56" />
        </div>
        <div class="pd-hero-meta">
          <div class="pd-kind">Podcast</div>
          <h1 class="pd-title">{{ detail.title }}</h1>
          <div v-if="detail.author" class="pd-author">{{ detail.author }}</div>
          <div class="pd-stats">
            <span>{{ detail.episodes.length }} episodes</span>
            <span v-if="detail.language" class="dot">·</span>
            <span v-if="detail.language" class="mono">{{ detail.language.toUpperCase() }}</span>
          </div>
          <div class="pd-actions">
            <button
              v-if="latestEpisode"
              class="btn btn-primary"
              @click="actions.playEpisode(detail, latestEpisode)"
            >
              <Icon name="play" :size="16" /> Play Latest
            </button>
            <button
              class="btn"
              :class="{ active: subscribed }"
              :aria-pressed="subscribed"
              @click="toggleSubscribe"
            >
              <Icon :name="subscribed ? 'heartfill' : 'heart'" :size="14" />
              {{ subscribed ? 'Subscribed' : 'Subscribe' }}
            </button>
            <a v-if="detail.link" :href="detail.link" target="_blank" rel="noopener" class="btn btn-ghost">
              <Icon name="external-link" :size="14" /> Website
            </a>
          </div>
        </div>
      </div>
    </header>

    <section v-if="detail.description" class="pd-about page-pad">
      <p class="pd-desc" :class="{ collapsed: !descOpen && (detail.description.length > 480) }">
        {{ detail.description }}
      </p>
      <button v-if="detail.description.length > 480" class="pd-desc-toggle" @click="descOpen = !descOpen">
        {{ descOpen ? 'Show less' : 'Read more' }}
      </button>
    </section>

    <section class="pd-episodes page-pad">
      <h2 class="section-title-lg">Episodes</h2>
      <!-- Episode rows are description-bearing cards, not a dense track
           table — TrackList's fixed (non-slot) phone row can't carry the
           blurb or the pub-date/duration meta line, so desktop keeps this
           markup untouched and only phone gets TrackList (see script: the
           same split-render call as music/playlist/[id].vue, for a
           different reason — no reorder here, just richer per-row content). -->
      <div v-if="!isPhone" class="pd-episode-list">
        <article
          v-for="(ep, i) in detail.episodes"
          :key="ep.guid || `${ep.audio_url}-${i}`"
          class="pd-ep"
          role="button"
          tabindex="0"
          :aria-label="`Play ${ep.title}`"
          @click="actions.playEpisode(detail, ep)"
          @keydown="onEpisodeKeydown($event, ep)"
        >
          <div class="pd-ep-num mono">{{ ep.episode_number ?? (detail.episodes.length - i) }}</div>
          <div class="pd-ep-art">
            <LoadingImage
              v-if="ep.artwork_url || detail.artwork_url"
              :src="ep.artwork_url || detail.artwork_url"
              :alt="ep.title"
              loading="lazy"
            />
          </div>
          <div class="pd-ep-body">
            <div class="pd-ep-title">{{ ep.title }}</div>
            <div class="pd-ep-meta">
              <span v-if="ep.pub_date">{{ formatPubDate(ep.pub_date) }}</span>
              <span v-if="ep.duration_secs > 0" class="dot">·</span>
              <span v-if="ep.duration_secs > 0" class="mono">{{ formatDuration(ep.duration_secs) }}</span>
            </div>
            <p v-if="ep.description" class="pd-ep-desc">{{ ep.description }}</p>
          </div>
          <button class="pd-ep-play" @click.stop="actions.playEpisode(detail, ep)" title="Play">
            <Icon name="play" :size="16" />
          </button>
        </article>
      </div>
      <!-- Big feeds (daily shows run to thousands of episodes) window their
           rows; the data is already fully parsed from the RSS, so the
           scrollbar spans the whole feed either way. -->
      <TrackList
        v-else
        :tracks="tlRows"
        :columns="columns"
        grid-template-columns="28px 64px 1fr 40px"
        :show-header="false"
        :context-items="contextItemsFor"
        :duration-formatter="formatDuration"
        :virtualized="tlRows.length > 150"
        @row-click="onPhoneRowClick"
      />
    </section>
  </div>
</template>

<script setup lang="ts">
import type { PodcastDetail, PodcastEpisode } from '~/composables/usePodcasts'
import type { TrackListColumn, TrackListRow } from '~/components/music/TrackList.vue'
import type { ContextMenuItem } from '~~/shared/types'
import { useQuery } from '@pinia/colada'

definePageMeta({ layout: 'default' })

const { isPhone } = useViewport()

const route = useRoute()
const feedURL = computed(() => (route.query.feed as string | undefined) ?? '')

const actions = usePodcastActions()
if (import.meta.client) actions.ensureSubscriptionsLoaded()

const { $heya } = useNuxtApp()
const detailQuery = useQuery({
  key: () => ['podcasts', 'feed', feedURL.value],
  query: async () => (await $heya('/api/podcasts/feed', { query: { url: feedURL.value } })) as PodcastDetail,
  enabled: () => feedURL.value.length > 0,
  staleTime: 1000 * 60 * 5,
})
await waitForQuery(detailQuery)
const detail = computed<PodcastDetail | null>(() => detailQuery.data.value ?? null)
const loading = computed(() => detailQuery.isPending.value)
const descOpen = ref(false)

const subscribed = computed(() => detail.value ? actions.isSubscribed(detail.value.feed_url) : false)
const latestEpisode = computed<PodcastEpisode | null>(() => detail.value?.episodes[0] ?? null)

async function toggleSubscribe() {
  if (!detail.value) return
  if (subscribed.value) {
    await actions.unsubscribe(detail.value.feed_url)
  } else {
    await actions.subscribe({
      feed_url: detail.value.feed_url,
      title: detail.value.title,
      author: detail.value.author,
      artwork_url: detail.value.artwork_url,
    })
  }
}

// Hero backdrop uses a heavily-blurred + darkened artwork so the eye lands
// on the foreground meta block. Mirrors the album page treatment.
const heroBgStyle = computed(() => {
  if (!detail.value?.artwork_url) return {}
  return { backgroundImage: `url(${detail.value.artwork_url})` }
})

function formatPubDate(s: string) {
  if (!s) return ''
  try {
    return new Date(s).toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })
  } catch { return s }
}
function formatDuration(seconds: number) {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)} min`
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  return `${h}h ${m}m`
}

// Phone-only TrackList render (see template comment). `id` has no natural
// numeric source (episodes are keyed by `guid`) — the list index is stable
// for a single render of one feed, which is all TrackList needs it for.
// This mounts only at phone width, where TrackList always uses its fixed
// 2-line row (not this desktop column config) — the `art` kind column
// only exists so TrackList's `hasArt` check shows episode thumbnails
// instead of a bare index there.
const columns: TrackListColumn[] = [
  { key: 'idx', kind: 'index' },
  { key: 'art', kind: 'art' },
  { key: 'title', kind: 'title' },
  { key: 'duration', kind: 'duration' },
]

const tlRows = computed<TrackListRow[]>(() => (detail.value?.episodes ?? []).map((ep, i) => ({
  id: i,
  title: ep.title,
  // Reuses the title column's "artist" subtitle slot to show the pub date
  // (episodes have no artist/album) — the description itself doesn't fit
  // TrackList's fixed phone row and is dropped there, same tradeoff every
  // migrated page makes for secondary metadata at phone width.
  artist: ep.pub_date ? formatPubDate(ep.pub_date) : '',
  album: '',
  duration: ep.duration_secs || 0,
  poster: ep.artwork_url || detail.value?.artwork_url || null,
})))

function contextItemsFor(_row: TrackListRow, i: number): ContextMenuItem[] {
  const ep = detail.value?.episodes[i]
  if (!ep || !detail.value) return []
  return [{ label: 'Play Episode', icon: 'play', action: () => actions.playEpisode(detail.value!, ep) }]
}

function onPhoneRowClick(i: number) {
  const ep = detail.value?.episodes[i]
  if (ep && detail.value) actions.playEpisode(detail.value, ep)
}

// Keyboard mirror of the desktop episode row's @click (playbook item 1).
// Guarded on target===currentTarget so Enter/Space on the nested "Play"
// button doesn't double-fire playEpisode.
function onEpisodeKeydown(e: KeyboardEvent, ep: PodcastEpisode) {
  if (e.target !== e.currentTarget) return
  if (e.key !== 'Enter' && e.key !== ' ') return
  e.preventDefault()
  if (detail.value) actions.playEpisode(detail.value, ep)
}
</script>

<style scoped>
.m-loading, .m-empty { color: var(--fg-3); padding: 32px 40px; font-size: 13px; }
.pd-back { color: var(--gold); text-decoration: underline; font-size: 12px; display: inline-block; margin-top: 12px; }

.pd-page { padding-bottom: 80px; }
.pd-hero { position: relative; overflow: hidden; border-radius: 0 0 var(--r-md) var(--r-md); min-height: 280px; }
.pd-hero-bg {
  position: absolute; inset: 0;
  background-size: cover;
  background-position: center;
  filter: blur(60px) brightness(0.4) saturate(2);
  transform: scale(1.4);
  z-index: 0;
}
.pd-hero-tint {
  position: absolute; inset: 0;
  /* scrim over the blurred backdrop art — stays literal */
  background: linear-gradient(180deg, rgba(0,0,0,0.25) 0%, rgba(0,0,0,0.5) 100%);
  z-index: 0;
}
.pd-hero-content {
  position: relative;
  z-index: 1;
  padding: 36px 40px;
  display: flex;
  align-items: flex-end;
  gap: 28px;
}
.pd-hero-art {
  width: 180px;
  height: 180px;
  border-radius: var(--r-md);
  overflow: hidden;
  box-shadow: 0 20px 40px rgba(0, 0, 0, 0.55); /* cast onto the always-dark blurred backdrop — stays literal */
  flex-shrink: 0;
  background: var(--bg-3);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-3);
}
.pd-hero-art-fallback { background: linear-gradient(135deg, rgba(99, 102, 241, 0.2), rgba(99, 102, 241, 0.06)); }
.pd-hero-art img { width: 100%; height: 100%; object-fit: cover; }
.pd-hero-meta { min-width: 0; }
/* .pd-kind..pd-stats sit on the blurred hero backdrop art — colors below
   stay literal white/black (artwork doesn't theme). */
.pd-kind {
  font-size: 11px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.12em;
  color: rgba(255, 255, 255, 0.7);
  margin-bottom: 6px;
}
.pd-title {
  font-size: 38px;
  font-weight: 700;
  letter-spacing: -0.01em;
  color: #fff;
  margin: 0 0 6px;
  text-shadow: 0 2px 12px rgba(0, 0, 0, 0.7);
}
.pd-author { font-size: 14px; color: rgba(255, 255, 255, 0.85); margin-bottom: 8px; }
.pd-stats { font-size: 12px; color: rgba(255, 255, 255, 0.75); display: flex; align-items: center; gap: 8px; }
.pd-stats .dot { color: rgba(255, 255, 255, 0.4); }
.pd-actions { display: flex; gap: 10px; margin-top: 18px; }

.pd-about { padding-top: 24px; max-width: 800px; }
.pd-desc { font-size: 14px; color: var(--fg-1); line-height: 1.65; white-space: pre-wrap; }
.pd-desc.collapsed { display: -webkit-box; -webkit-line-clamp: 4; -webkit-box-orient: vertical; overflow: hidden; }
.pd-desc-toggle { background: transparent; border: 0; color: var(--gold); font-size: 12px; cursor: pointer; margin-top: 8px; padding: 0; }

.pd-episodes { padding-top: 32px; }
.pd-episodes .section-title-lg { margin-bottom: 16px; }
.pd-episode-list { display: flex; flex-direction: column; gap: 8px; }
.pd-ep {
  display: grid;
  grid-template-columns: 28px 64px 1fr 40px;
  gap: 14px;
  padding: 12px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  cursor: pointer;
  transition: border-color 0.15s, background 0.15s;
  align-items: center;
}
.pd-ep:hover { border-color: color-mix(in srgb, var(--gold) 30%, transparent); background: color-mix(in srgb, var(--gold) 4%, transparent); }
.pd-ep-num { color: var(--fg-3); font-size: 11px; text-align: right; }
.pd-ep-art {
  width: 56px;
  height: 56px;
  border-radius: var(--r-sm);
  overflow: hidden;
  background: var(--bg-3);
  flex-shrink: 0;
}
.pd-ep-art img { width: 100%; height: 100%; object-fit: cover; }
.pd-ep-body { min-width: 0; }
.pd-ep-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--fg-0);
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 1;
  -webkit-box-orient: vertical;
  margin-bottom: 4px;
}
.pd-ep-meta { font-size: 11px; color: var(--fg-3); display: flex; align-items: center; gap: 6px; }
.pd-ep-meta .dot { color: var(--fg-4); }
.pd-ep-desc {
  font-size: 12px;
  color: var(--fg-2);
  margin-top: 6px;
  line-height: 1.5;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.pd-ep-play {
  width: 36px;
  height: 36px;
  border-radius: 50%;
  background: var(--gold);
  color: var(--bg-0);
  border: 0;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  opacity: 0;
  transition: opacity 0.15s, transform 0.15s;
}
.pd-ep:hover .pd-ep-play,
.pd-ep-play:focus-visible { opacity: 1; }
.pd-ep-play:hover { transform: scale(1.1); }
.mono { font-family: var(--font-mono); }

/* Phone (<=720px): stack the hero, center the art, wrap the action row.
   The episode list itself swaps to TrackList at this width (see template). */
@media (max-width: 720px) {
  .pd-hero { min-height: 0; }
  .pd-hero-content {
    flex-direction: column;
    align-items: center;
    text-align: center;
    padding: 24px 20px 20px;
    gap: 14px;
  }
  .pd-hero-art { width: min(55vw, 240px); height: min(55vw, 240px); }
  .pd-hero-meta { width: 100%; }
  .pd-stats { justify-content: center; }
  .pd-actions { justify-content: center; flex-wrap: wrap; }
}
</style>
