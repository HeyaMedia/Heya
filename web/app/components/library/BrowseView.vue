<template>
  <div class="rec-view scroll">
    <div class="rec-pad">
      <!-- Activity rows (bespoke tiles) come first, composed here from their
           own endpoints; the server-ranked discovery rails follow. -->
      <ContinueWatchingRow
        v-if="continueItems.length"
        :items="continueItems"
        @play="playContinue"
      />

      <UpNextRow
        v-if="section === 'tv' && upNextItems.length"
        :items="upNextItems"
        @play="playUpNext"
      />

      <ContentRow
        v-if="forYouItems.length"
        title="For You"
        subtitle="Ranked by your taste"
        :items="forYouItems"
        more="See all"
        @tile="go"
        @more="navigateTo(section === 'movie' ? '/movies/recommendations' : '/tv/recommendations')"
      />

      <ContentRow
        v-if="recentAdded.length"
        :title="section === 'tv' ? 'Recently Added TV' : 'Recently Added Films'"
        :subtitle="section === 'tv' ? 'New shows, seasons & episodes' : 'Across all libraries'"
        :items="recentAdded"
        @tile="go"
      />

      <ContentRow
        v-if="recentWatched.length"
        :title="section === 'tv' ? 'Recently Watched' : 'Recently Watched Films'"
        subtitle="Pick up where you left off"
        :items="recentWatched"
        @tile="go"
      />

      <ContentRow
        v-for="rail in rails"
        :key="rail.key"
        :title="rail.title"
        :subtitle="rail.subtitle"
        :items="toRow(rail.items)"
        @tile="go"
      />

      <div v-if="!loading && isEmpty" class="rec-empty">
        <Icon :name="section === 'tv' ? 'tv' : 'film'" :size="30" class="rec-empty-icon" />
        <p>Nothing to recommend yet. Watch a few titles and this fills in.</p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { MediaItem } from '~~/shared/types'
import type { ContinueWatchingItem } from '~/components/home/ContinueWatchingRow.vue'
import { useQuery } from '@tanstack/vue-query'

const props = defineProps<{ section: 'movie' | 'tv' }>()

const { $heya } = useNuxtApp()

// Server-ranked discovery rails (genre/actor affinity, top-unwatched,
// rediscover, local TMDB recs). One typed shape mirroring service.RecRailItem.
interface RailItem {
  id: number
  title: string
  slug: string
  year?: string
  sub?: string
  media_type: string
  rating?: number
  available: boolean
}
interface Rail { key: string; title: string; subtitle?: string; items: RailItem[] }

const railsQuery = useQuery({
  queryKey: ['recommended', props.section],
  queryFn: async () => (await $heya('/api/me/recommended/{section}', {
    path: { section: props.section },
  })) as { rails: Rail[] },
  staleTime: 1000 * 60 * 5,
})
const rails = computed<Rail[]>(() => railsQuery.data.value?.rails ?? [])

// Personalized "For You" — the taste-vector + TMDB-graph engine, section-scoped.
const forYouQuery = useQuery({
  queryKey: ['for-you', props.section],
  queryFn: async () => (await $heya('/api/me/recommendations', {
    query: { type: props.section, limit: 20 },
  })) as { items: RailItem[]; has_signal: boolean },
  staleTime: 1000 * 60 * 5,
})
const forYouItems = computed<MediaItem[]>(() => toRow(forYouQuery.data.value?.items ?? []))

// ── Recently Added ────────────────────────────────────────────────────────
// The TV rail is Plex-style grouped file arrivals (new show / season / episode);
// movies are a flat newest-first list. Shares query keys with the home page so
// the caches are warm across navigation.
interface RecentTVEntry {
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

const recentMoviesQuery = useQuery({
  queryKey: ['media', 'recent', 'movie'],
  queryFn: async () => (await $heya('/api/media', { query: { type: 'movie', sort: 'added', limit: 24 } })) as MediaItem[],
  staleTime: 1000 * 60,
  enabled: props.section === 'movie',
})
const recentTVQuery = useQuery({
  queryKey: ['media', 'recent', 'tv'],
  queryFn: async () => (await $heya('/api/media/tv/recently-added', { query: { limit: 24 } })) as RecentTVEntry[],
  staleTime: 1000 * 60,
  enabled: props.section === 'tv',
})

const recentAdded = computed<MediaItem[]>(() => {
  if (props.section === 'movie') return recentMoviesQuery.data.value ?? []
  return (recentTVQuery.data.value ?? []).map(tvEntryToRowItem)
})

// ── Continue Watching / Recently Watched ──────────────────────────────────
const continueQuery = useQuery({
  queryKey: ['me', 'watch', 'continue'],
  queryFn: async () => (await $heya('/api/me/watch/continue')) as ContinueWatchingItem[],
  staleTime: 1000 * 30,
})
const continueItems = computed<ContinueWatchingItem[]>(() =>
  (continueQuery.data.value ?? []).filter(i => mediaTypeInSection(i.media_type, props.section)),
)

// Movies: one tile per watched movie (/watch/recent, deduped to the item).
const recentMoviesWatchedQuery = useQuery({
  queryKey: ['me', 'watch', 'recent'],
  queryFn: async () => (await $heya('/api/me/watch/recent')) as Array<{
    media_item_id: number; title: string; slug: string; media_type: string
  }>,
  staleTime: 1000 * 30,
  enabled: props.section === 'movie',
})

// TV: one tile per watched EPISODE (not deduped to the show), each painted with
// the show's poster and an "S02E03 · Title" subtitle.
interface RecentEpisode {
  episode_id: number
  media_item_id: number
  series_title: string
  series_slug: string
  season_number: number
  episode_number: number
  episode_title: string
}
const recentEpisodesQuery = useQuery({
  queryKey: ['me', 'watch', 'recent-episodes'],
  queryFn: async () => (await $heya('/api/me/watch/recent-episodes')) as RecentEpisode[],
  staleTime: 1000 * 30,
  enabled: props.section === 'tv',
})

const recentWatched = computed<MediaItem[]>(() => {
  if (props.section === 'movie') {
    return (recentMoviesWatchedQuery.data.value ?? [])
      .filter(r => r.media_type === 'movie')
      .map(r => ({ id: r.media_item_id, title: r.title, slug: r.slug, media_type: r.media_type, available: true } as unknown as MediaItem))
  }
  return (recentEpisodesQuery.data.value ?? []).map(episodeToRowItem)
})

// Watched episode → rail card: show poster (media_item_id) + episode subtitle;
// `key` is the episode id so the same show can appear once per watched episode.
function episodeToRowItem(e: RecentEpisode): MediaItem {
  const code = `S${String(e.season_number).padStart(2, '0')}E${String(e.episode_number).padStart(2, '0')}`
  return {
    id: e.media_item_id,
    key: `ep-${e.episode_id}`,
    title: e.series_title,
    year: '',
    sub: e.episode_title ? `${code} · ${e.episode_title}` : code,
    media_type: 'tv',
    slug: e.series_slug,
    available: true,
  } as unknown as MediaItem
}

const loading = computed(() =>
  railsQuery.isPending.value
  || (props.section === 'movie' ? recentMoviesQuery.isPending.value : recentTVQuery.isPending.value),
)
const isEmpty = computed(() =>
  !continueItems.value.length && !upNextItems.value.length && !recentAdded.value.length
  && !recentWatched.value.length && !rails.value.length && !forYouItems.value.length,
)

// ── Up Next (TV) + player navigation ──────────────────────────────────────
// Shared with the Home page. Up Next is fed from the episode-level watch feed;
// useUpNext dedupes to unique series internally, so several watched episodes of
// one show still yield a single Up Next tile.
const { upNextItems } = useUpNext(() => props.section === 'tv'
  ? (recentEpisodesQuery.data.value ?? []).map(e => ({
    media_item_id: e.media_item_id, title: e.series_title, slug: e.series_slug, media_type: 'tv',
  }))
  : [])
const { playContinue, playUpNext } = usePlaybackNav()

// ContentRow types its tiles as MediaItem-ish; RailItem carries just the subset
// it reads (id for the poster, title/year/sub for labels, slug+media_type for
// the click-through), so widen it for the prop.
function toRow(items: RailItem[]): MediaItem[] {
  return items as unknown as MediaItem[]
}

function go(item: MediaItem | RailItem) {
  navigateTo(mediaUrl(item as MediaItem))
}

// Grouped TV event → rail card. Poster is the show's; the subtitle carries the
// event; `key` keeps v-for happy when one show has two event cards.
function tvEntryToRowItem(e: RecentTVEntry): MediaItem {
  return {
    id: e.media_item_id,
    key: `${e.media_item_id}-${e.kind}-${e.season_number}-${e.episode_number}-${e.added_at}`,
    title: e.title,
    year: '',
    sub: tvEntrySub(e),
    media_type: 'tv',
    slug: e.slug,
    available: true,
  } as unknown as MediaItem
}

function tvEntrySub(e: RecentTVEntry): string {
  const eps = (n: number, word = 'episode') => `${n} ${word}${n === 1 ? '' : 's'}`
  switch (e.kind) {
    case 'series':
      return e.season_count > 1 ? `New show · ${e.season_count} seasons` : `New show · ${eps(e.episode_count)}`
    case 'season':
      return e.season_number === 0 ? `New · ${eps(e.episode_count, 'special')}` : `New season ${e.season_number} · ${eps(e.episode_count)}`
    case 'episodes':
      return e.season_number === 0 ? `${eps(e.episode_count, 'new special')}` : `Season ${e.season_number} · ${e.episode_count} new episodes`
    case 'episode': {
      const code = `S${String(e.season_number).padStart(2, '0')}E${String(e.episode_number).padStart(2, '0')}`
      return e.episode_title ? `${code} · ${e.episode_title}` : code
    }
  }
}

// Live refresh: a new/updated file for this section refreshes the affected
// rails. The bundle re-runs too (a newly-added title changes top-unwatched /
// recommended), coalesced by useLiveRefresh so a big scan is one refetch.
useLiveRefresh([
  {
    events: ['media.added', 'media.updated'],
    filter: props.section === 'tv' ? byMediaType('tv', 'anime') : byMediaType(props.section),
    keys: [
      ['recommended', props.section],
      ['media', 'recent', props.section],
    ],
  },
  { events: ['media.watched'], keys: [['me', 'watch', 'continue'], ['me', 'watch', 'recent'], ['me', 'watch', 'recent-episodes'], ['recommended', props.section], ['for-you', props.section]] },
])

function mediaTypeInSection(mediaType: string, section: 'movie' | 'tv') {
  if (section === 'tv') return mediaType === 'tv' || mediaType === 'anime'
  return mediaType === section
}
</script>

<style scoped>
.rec-view { height: 100%; }
.rec-pad { padding: 24px 32px 80px; }

.rec-empty {
  display: flex; flex-direction: column; align-items: center; gap: 14px;
  padding: 90px 32px; text-align: center; color: var(--fg-2); font-size: 15px;
}
.rec-empty p { margin: 0; max-width: 360px; }
.rec-empty-icon { opacity: 0.35; }

@media (max-width: 720px) {
  .rec-pad { padding: 16px 16px 90px; }
  .rec-pad :deep(.section-title-lg) { font-size: 18px; }
}
</style>
