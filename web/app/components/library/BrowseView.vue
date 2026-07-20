<template>
  <div class="rec-view scroll">
    <!-- Library head + signature ledger sit edge-to-edge above the rails
         (LedgerStrip needs no side gutter; LibHead carries its own). Rendered
         only once the page hands us the facts — the catalog loads in the
         background, so the rails paint immediately and the ledger fills in. -->
    <LibHead v-if="libTitle" :title="libTitle" :crumbs="libCrumbs" />
    <LedgerStrip v-if="ledgerCells.length" :cells="ledgerCells" canvas />
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
        :context-items="contextItemsFor"
        :has-more="forYouQuery.hasNextPage.value || forYouQuery.asyncStatus.value === 'loading'"
        :loading-more="forYouQuery.asyncStatus.value === 'loading'"
        more="Show all"
        @tile="go"
        @more="navigateTo(section === 'movie' ? '/movies/recommendations' : '/tv/recommendations')"
        @load-more="loadMoreForYou"
      />

      <ContentRow
        v-if="recentAdded.length"
        :title="section === 'tv' ? 'Recently Added TV' : 'Recently Added Films'"
        :subtitle="section === 'tv' ? 'New shows, seasons & episodes' : 'Across all libraries'"
        :items="recentAdded"
        :context-items="contextItemsFor"
        :has-more="recentAddedHasMore"
        :loading-more="recentAddedLoading"
        show-added
        more="Show all"
        @tile="go"
        @more="navigateTo(section === 'movie' ? '/movies/all?sort=added' : '/tv/all?sort=added')"
        @load-more="loadMoreRecentAdded"
      />

      <ContentRow
        v-if="recentWatched.length"
        :title="section === 'tv' ? 'Recently Watched' : 'Recently Watched Films'"
        subtitle="Pick up where you left off"
        :items="recentWatched"
        :context-items="contextItemsFor"
        :has-more="recentWatchedHasMore"
        :loading-more="recentWatchedLoading"
        @tile="go"
        @load-more="loadMoreRecentWatched"
      />

      <DiscoveryRail
        v-for="rail in rails"
        :key="rail.key"
        :section="section"
        :rail="rail"
        :context-items="contextItemsFor"
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
import type { ContinueWatchingItem } from '~/types/home'
import type { LedgerCell } from '~/components/ui/LedgerStrip.vue'
import type { Crumb } from '~/components/library/LibHead.vue'
import { useInfiniteQuery, useQuery, useQueryCache } from '@pinia/colada'
import { movieUserStateQuery, seriesUserStateQuery, userListsQuery as userListsOptions } from '~/queries/catalog'
import { continueWatchingQuery } from '~/queries/activity'
import {
  forYouInfinite,
  recentEpisodesInfinite,
  recentMediaInfinite,
  recentTVInfinite,
  recentWatchedInfinite,
  type Rail,
  type RailItem,
  type RecentEpisodeRow,
  type RecentTVEntry,
} from '~/queries/rails'

const props = withDefaults(defineProps<{
  section: 'movie' | 'tv'
  /** Archivo library title shown in the LibHead (omit to hide the head). */
  libTitle?: string
  /** Mono breadcrumb segments above the title. */
  libCrumbs?: Crumb[]
  /** Signature ledger facts (empty until the page's catalog resolves). */
  ledgerCells?: LedgerCell[]
}>(), {
  libTitle: '',
  libCrumbs: () => [],
  ledgerCells: () => [],
})

const { $heya } = useNuxtApp()
const queryClient = useQueryCache()
const invalidateContinueWatching = useInvalidateContinueWatching()
const { buildItems: buildCardCtxItems } = useCardContextItems()

// Server-ranked discovery rails (genre/actor affinity, top-unwatched,
// rediscover, local TMDB recs). The bundle gives each rail its 24-item head;
// DiscoveryRail pages the rest on demand.
const railsQuery = useQuery({
  key: () => ['recommended', props.section],
  query: async () => (await $heya('/api/me/recommended/{section}', {
    path: { section: props.section },
  })) as { rails: Rail[] },
  staleTime: 1000 * 60 * 5,
})
const rails = computed<Rail[]>(() => railsQuery.data.value?.rails ?? [])

// Personalized "For You" — the taste-vector + TMDB-graph engine, section-scoped
// and offset-paged (depth ends at the engine's re-rank pool).
const forYouQuery = useInfiniteQuery(() => forYouInfinite({ section: props.section }))
const forYouItems = computed<MediaItem[]>(() =>
  toRow((forYouQuery.data.value?.pages ?? []).flatMap(p => p.items as RailItem[])))
const loadMoreForYou = railLoadMore(forYouQuery)

// ── Recently Added ────────────────────────────────────────────────────────
// The TV rail is Plex-style grouped file arrivals (new show / season / episode);
// movies are a flat newest-first list. Both infinite: page 0 is the cheap
// recent window, deeper pages walk the full arrival history. Query keys are
// shared with the home page so caches stay warm across navigation.
const recentMoviesQuery = useInfiniteQuery(() => ({
  ...recentMediaInfinite('movie'),
  enabled: props.section === 'movie',
}))
const recentTVQuery = useInfiniteQuery(() => ({
  ...recentTVInfinite(),
  enabled: props.section === 'tv',
}))
const recentAddedHasMore = computed(() => (props.section === 'movie' ? recentMoviesQuery : recentTVQuery).hasNextPage.value)
const recentAddedLoading = computed(() => (props.section === 'movie' ? recentMoviesQuery : recentTVQuery).asyncStatus.value === 'loading')

const recentAdded = computed<MediaItem[]>(() => {
  if (props.section === 'movie') return (recentMoviesQuery.data.value?.pages ?? []).flat()
  return (recentTVQuery.data.value?.pages ?? []).flat().map(tvEntryToRowItem)
})
const loadMoreRecentMovies = railLoadMore(recentMoviesQuery)
const loadMoreRecentTV = railLoadMore(recentTVQuery)
const loadMoreRecentAdded = () => (props.section === 'movie' ? loadMoreRecentMovies() : loadMoreRecentTV())

// ── Continue Watching / Recently Watched ──────────────────────────────────
const continueQuery = useQuery(continueWatchingQuery())
const continueItems = computed<ContinueWatchingItem[]>(() =>
  (continueQuery.data.value ?? []).filter(i => mediaTypeInSection(i.media_type, props.section)),
)

// Movies: one tile per watched movie (deduped to the item). TV: one tile per
// watched EPISODE, each painted with the show's poster and an "S02E03 · Title"
// subtitle. Both page back through the full watch history.
const recentMoviesWatchedQuery = useInfiniteQuery(() => ({
  ...recentWatchedInfinite(),
  enabled: props.section === 'movie',
}))
const recentEpisodesQuery = useInfiniteQuery(() => ({
  ...recentEpisodesInfinite(),
  enabled: props.section === 'tv',
}))
const recentWatchedHasMore = computed(() => (props.section === 'movie' ? recentMoviesWatchedQuery : recentEpisodesQuery).hasNextPage.value)
const recentWatchedLoading = computed(() => (props.section === 'movie' ? recentMoviesWatchedQuery : recentEpisodesQuery).asyncStatus.value === 'loading')

const recentWatched = computed<MediaItem[]>(() => {
  if (props.section === 'movie') {
    return (recentMoviesWatchedQuery.data.value?.pages ?? []).flat()
      .filter(r => r.media_type === 'movie')
      .map(r => ({ id: r.media_item_id, title: r.title, slug: r.slug, media_type: r.media_type, available: true } as unknown as MediaItem))
  }
  return (recentEpisodesQuery.data.value?.pages ?? []).flat().map(episodeToRowItem)
})
const loadMoreWatchedMovies = railLoadMore(recentMoviesWatchedQuery)
const loadMoreWatchedEpisodes = railLoadMore(recentEpisodesQuery)
const loadMoreRecentWatched = () => (props.section === 'movie' ? loadMoreWatchedMovies() : loadMoreWatchedEpisodes())

const userListsQuery = useQuery(userListsOptions())
const moviesStateQuery = useQuery(() => ({ ...movieUserStateQuery(), enabled: props.section === 'movie' }))
const seriesStateQuery = useQuery(() => ({ ...seriesUserStateQuery(), enabled: props.section === 'tv' }))

const watchedSet = ref<Set<number>>(new Set())
const favoritedSet = ref<Set<number>>(new Set())

watchEffect(() => {
  if (props.section === 'movie') {
    watchedSet.value = new Set(moviesStateQuery.data.value?.watched ?? [])
    favoritedSet.value = new Set(moviesStateQuery.data.value?.favorited ?? [])
    return
  }
  watchedSet.value = new Set((seriesStateQuery.data.value?.shows ?? [])
    .filter(s => s.total_episodes > 0 && s.watched_episodes >= s.total_episodes)
    .map(s => s.media_item_id))
  favoritedSet.value = new Set(seriesStateQuery.data.value?.favorited ?? [])
})

function contextItemsFor(item: MediaItem) {
  return buildCardCtxItems(item, {
    watchedSet: watchedSet.value,
    favoritedSet: favoritedSet.value,
    userLists: userListsQuery.data.value ?? [],
    onToggleWatched: async (id: number, watched: boolean) => {
      try {
        await $heya('/api/me/watched/media/{id}', {
          method: 'POST',
          path: { id },
          body: { watched } as any,
        })
        const next = new Set(watchedSet.value)
        if (watched) next.add(id)
        else next.delete(id)
        watchedSet.value = next
        invalidateContinueWatching()
        queryClient.invalidateQueries({ key: ['me', 'state'] })
      } catch { /* ignore */ }
    },
    onToggleFavorite: async (id: number, favorited: boolean) => {
      try {
        await $heya('/api/me/favorites', {
          method: 'POST',
          body: { entity_type: 'media_item', entity_id: id } as any,
        })
        const next = new Set(favoritedSet.value)
        if (favorited) next.add(id)
        else next.delete(id)
        favoritedSet.value = next
        queryClient.invalidateQueries({ key: ['me', 'state'] })
      } catch { /* ignore */ }
    },
    onAddToList: async (listId: number, mediaId: number) => {
      try {
        await $heya('/api/me/lists/{id}/items', {
          method: 'POST',
          path: { id: listId },
          body: { media_item_id: mediaId } as any,
        })
      } catch { /* ignore */ }
    },
  })
}

// Watched episode → rail card: show poster (media_item_id) + episode subtitle;
// `key` is the episode id so the same show can appear once per watched episode.
function episodeToRowItem(e: RecentEpisodeRow): MediaItem {
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
  || (props.section === 'movie' ? recentMoviesQuery : recentTVQuery).isPending.value,
)
const isEmpty = computed(() =>
  !continueItems.value.length && !upNextItems.value.length && !recentAdded.value.length
  && !recentWatched.value.length && !rails.value.length && !forYouItems.value.length,
)

// ── Up Next (TV) + player navigation ──────────────────────────────────────
// Shared with the Home page — the server owns the derivation (see useUpNext);
// only the TV landing renders the rail, so the fetch is gated to it.
const { upNextItems } = useUpNext(() => props.section === 'tv')
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
// event; `key` keeps v-for happy when one show has two event cards. added_at
// feeds the "3d ago" corner chip on the Recently Added rail.
function tvEntryToRowItem(e: RecentTVEntry): MediaItem {
  return {
    id: e.media_item_id,
    key: `${e.media_item_id}-${e.kind}-${e.season_number}-${e.episode_number}-${e.added_at}`,
    title: e.title,
    year: '',
    sub: tvEntrySub(e),
    media_type: 'tv',
    slug: e.slug,
    added_at: e.added_at,
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
  { events: ['media.watched'], keys: [['me', 'watch', 'continue'], ['me', 'watch', 'recent'], ['me', 'watch', 'recent-episodes'], ['me', 'up-next'], ['recommended', props.section], ['for-you', props.section]] },
  { events: ['media.watched', 'media.favorited'], keys: [['me', 'state', props.section === 'movie' ? 'movies' : 'series']] },
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
