<template>
  <div class="scroll" style="height: 100%">
    <HeroDeck
      v-if="showSection('hero')"
      :items="heroItems"
      :movies="movieDetails"
      :play-info="heroPlayInfo"
      :trailers="heroTrailers"
      :up-next-items="upNextItems"
      :tv-entries="tvQuery.data.value ?? []"
      :albums="recentAlbums"
      :artists="recentArtists"
      :pinned-mode="pinnedHeroMode"
      @play="onHeroPlay"
      @play-up-next="playUpNext"
      @pin="onPinHeroMode"
    />

    <!-- Sections render in user-configured order (Settings → Appearance)
         via CSS `order` — DOM stays static, only visual order moves. -->
    <div class="page-pad home-sections">
      <ContinueWatchingRow
        v-if="continueWatching.length && showSection('continue-watching')"
        :style="sectionStyle('continue-watching')"
        :items="continueWatching"
        @play="playContinue"
      />

      <UpNextRow
        v-if="upNextItems.length && showSection('up-next')"
        :style="sectionStyle('up-next')"
        :items="upNextItems"
        @play="playUpNext"
      />

      <ContentRow
        v-if="recommendedItems.length && showSection('for-you')"
        :style="sectionStyle('for-you')"
        title="For You"
        subtitle="From what you've watched & loved"
        :items="recommendedItems"
        :context-items="homeMediaContextItems"
        more="See all"
        @tile="(item) => navigateTo(mediaUrl(item))"
        @more="navigateTo('/movies/recommendations')"
      />

      <ContentRow
        v-if="recentMovies.length && showSection('recent-movies')"
        :style="sectionStyle('recent-movies')"
        title="Recently Added Films"
        subtitle="Across all libraries"
        :items="recentMovies"
        :context-items="homeMediaContextItems"
        more="See all"
        @tile="(item) => navigateTo(mediaUrl(item))"
        @more="navigateTo('/movies')"
      />

      <ContentRow
        v-if="recentTVItems.length && showSection('recent-tv')"
        :style="sectionStyle('recent-tv')"
        title="Recently Added TV"
        subtitle="New shows, seasons & episodes"
        :items="recentTVItems"
        :context-items="homeMediaContextItems"
        more="See all"
        @tile="(item) => navigateTo(mediaUrl(item))"
        @more="navigateTo('/tv')"
      />

      <ContentRow
        v-if="recentAlbums.length && showSection('recent-albums')"
        :style="sectionStyle('recent-albums')"
        title="Recently Added Albums"
        subtitle="Across all libraries"
        :items="recentAlbums"
        :context-items="homeAlbumContextItems"
        :aspect="'1/1'"
        :tile-width="168"
        more="See all"
        @tile="(item) => navigateTo(albumUrl(item))"
        @more="navigateTo('/music/albums')"
      />

      <ContentRow
        v-if="recentArtists.length && showSection('recent-artists')"
        :style="sectionStyle('recent-artists')"
        title="Recently Added Artists"
        subtitle="New & updated artists"
        :items="recentArtists"
        :context-items="homeArtistContextItems"
        :aspect="'1/1'"
        :tile-width="168"
        more="See all"
        @tile="(item) => navigateTo(mediaUrl(item))"
        @more="navigateTo('/music/artists')"
      />

      <ContentRow
        v-if="recentBooks.length && showSection('recent-books')"
        :style="sectionStyle('recent-books')"
        title="Recently Added Books"
        subtitle="Across all libraries"
        :items="recentBooks"
        :context-items="homeBookContextItems"
        more="See all"
        @tile="(item) => navigateTo(mediaUrl(item))"
        @more="navigateTo('/books')"
      />

      <div v-if="!loading && !hasContent" class="empty-home">
        <h2>Welcome to Heya</h2>
        <p>Add a library and scan it to see your media here.</p>
        <NuxtLink to="/libraries" class="btn btn-primary" style="margin-top: 16px">
          <Icon name="plus" :size="16" />
          Add Library
        </NuxtLink>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { ContextMenuItem, MediaItem, MediaDetail, Movie, UserList } from '~~/shared/types'
import type { ContinueWatchingItem } from '~/components/home/ContinueWatchingRow.vue'
import type { HeroPlayInfo } from '~/components/home/HeroA.vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'

const { $heya } = useNuxtApp()
const queryClient = useQueryClient()
const invalidateContinueWatching = useInvalidateContinueWatching()

// Music rows show recent ALBUMS plus recent ARTISTS. Items are normalized to
// MediaItem-ish so ContentRow renders them, with poster_src set to the
// album-cover endpoint and the click handler routing to album detail.
type AlbumRowItem = MediaItem & { artist_slug: string; album_slug: string; artist_name?: string }
type ArtistRowItem = MediaItem & { artist_id: number }

// The TV rail is Plex-style grouped file arrivals, not bare shows: a brand-new
// show is one "New show" card, a season drop one "New season" card, and a
// lone episode an episode card. The backend derives the grouping; the FE only
// formats subtitles.
interface RecentTVEntry {
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

interface RecentArtistEntry {
  id: number
  media_item_id: number
  media_item_public_id?: string
  name: string
  slug: string
  album_count: number
  track_count: number
  kind: 'new' | 'updated'
  new_album_count: number
  latest_album_title?: string
  latest_album_slug?: string
  added_at: string
}

// One vue-query per rail. Each caches independently so cross-page navigation
// returns instantly. Event-bus listeners below invalidate by key to refresh.
const moviesQuery = useQuery({
  queryKey: ['media', 'recent', 'movie'],
  queryFn: async () => (await $heya('/api/media', { query: { type: 'movie', sort: 'added', limit: 20 } })) as MediaItem[],
  staleTime: 1000 * 60,
})
const tvQuery = useQuery({
  queryKey: ['media', 'recent', 'tv'],
  queryFn: async () => (await $heya('/api/media/tv/recently-added', { query: { limit: 20 } })) as RecentTVEntry[],
  staleTime: 1000 * 60,
})
const booksQuery = useQuery({
  queryKey: ['media', 'recent', 'book'],
  queryFn: async () => (await $heya('/api/media', { query: { type: 'book', sort: 'added', limit: 20 } })) as MediaItem[],
  staleTime: 1000 * 60,
})
const musicHomeQuery = useQuery({
  queryKey: ['home', 'recent-albums'],
  queryFn: async () => {
    const home = await $heya('/api/music/home', { query: { limit: 20 } }) as {
      recent_albums: Array<{
        id: number; title: string; year: string; artist_name: string; artist_slug: string; slug: string; available?: boolean
      }>
      recent_artists: RecentArtistEntry[]
    }
    return {
      albums: (home.recent_albums ?? []).map(albumToRowItem),
      artists: (home.recent_artists ?? []).map(artistToRowItem),
    }
  },
  staleTime: 1000 * 60,
})
const continueWatchingQuery = useQuery({
  queryKey: ['me', 'watch', 'continue'],
  queryFn: async () => (await $heya('/api/me/watch/continue')) as ContinueWatchingItem[],
  staleTime: 1000 * 30,
})
const recentWatchedQuery = useQuery({
  queryKey: ['me', 'watch', 'recent'],
  queryFn: async () => (await $heya('/api/me/watch/recent')) as Array<{
    media_item_id: number; title: string; poster_path: string; slug: string; media_type: string
  }>,
  staleTime: 1000 * 30,
})
// Personalized "For You" — the taste-vector + TMDB-graph engine. Excludes
// seeds (hearts / watched) server-side, so no client-side filtering needed.
const forYouQuery = useQuery({
  queryKey: ['for-you', { limit: 20 }],
  queryFn: async () => (await $heya('/api/me/recommendations', { query: { limit: 20 } })) as {
    items: { id: number; public_id?: string; title: string; slug: string; year?: string; media_type: string; reason?: string; available: boolean }[]
    has_signal: boolean
  },
  staleTime: 1000 * 60 * 10,
})

const userListsQuery = useQuery({
  queryKey: ['me', 'lists'],
  queryFn: async () => (await $heya('/api/me/lists')) as UserList[],
  staleTime: 1000 * 60,
})
const movieStateQuery = useQuery({
  queryKey: ['me', 'state', 'movies'],
  queryFn: async () => fetchUserState('movies'),
  staleTime: 1000 * 30,
})
const seriesStateQuery = useQuery({
  queryKey: ['me', 'state', 'series'],
  queryFn: async () => fetchUserState('series'),
  staleTime: 1000 * 30,
})
const mediaStateQuery = useQuery({
  queryKey: ['me', 'media-state'],
  queryFn: async () => (await $heya('/api/me/media-state')) as { watched: number[]; favorited: number[] },
  staleTime: 1000 * 30,
})

const recentMovies = computed<MediaItem[]>(() => moviesQuery.data.value ?? [])
const recentBooks = computed<MediaItem[]>(() => booksQuery.data.value ?? [])
const recentAlbums = computed<AlbumRowItem[]>(() => musicHomeQuery.data.value?.albums ?? [])
const recentArtists = computed<ArtistRowItem[]>(() => musicHomeQuery.data.value?.artists ?? [])

// Rail items: one card per grouped TV event (a show may appear twice).
const recentTVItems = computed<MediaItem[]>(() => (tvQuery.data.value ?? []).map(tvEntryToRowItem))
// Deduped MediaItem-ish shows for hero / favorites / recommendations, which
// think in shows, not events.
const recentTVShows = computed<MediaItem[]>(() => {
  const seen = new Set<number>()
  const out: MediaItem[] = []
  for (const e of tvQuery.data.value ?? []) {
    if (seen.has(e.media_item_id)) continue
    seen.add(e.media_item_id)
    out.push({
      id: e.media_item_id,
      public_id: e.media_item_public_id,
      title: e.title,
      slug: e.slug,
      media_type: 'tv',
      created_at: e.added_at,
      available: true,
    } as unknown as MediaItem)
  }
  return out
})

const continueWatching = computed<ContinueWatchingItem[]>(() => continueWatchingQuery.data.value ?? [])

// Hero/Up Next/Favorites/Recommendations are derived from the queries above.
// Up Next needs an extra per-show /up-next round-trip; keep that imperative
// since it depends on the recent-watched query landing first.
const movieDetails = ref<Record<number, Movie>>({})
const heroPlayInfo = ref<Record<number, HeroPlayInfo>>({})
const heroTrailers = ref<Record<number, number>>({})

// Up Next + player navigation are shared with the Movies/TV Recommended
// landings — see useUpNext / usePlaybackNav.
const { upNextItems } = useUpNext(() => recentWatchedQuery.data.value)
const { playContinue, playUpNext } = usePlaybackNav()

// Pinned hero mode — server-persisted in user settings so it follows the
// user across devices. The deck itself mirrors to localStorage for instant
// paint; this query is the authority.
interface MeSettings { playback?: Record<string, unknown>; ui?: { pinned_hero_mode?: string } }
const settingsQuery = useQuery({
  queryKey: ['me', 'settings'],
  queryFn: async () => (await $heya('/api/me/settings')) as MeSettings,
  staleTime: 1000 * 60 * 5,
})
const pinnedHeroMode = computed(() => settingsQuery.data.value?.ui?.pinned_hero_mode ?? undefined)

// Section visibility + order (Settings → Appearance). Rides the same
// ['me','settings'] query as pinnedHeroMode; hidden sections skip render,
// order lands as CSS `order` on the flex column below.
const { isVisible: showSection, orderOf } = useHomeSections()
const sectionStyle = (id: string) => ({ order: orderOf(id) })

async function onPinHeroMode(mode: string) {
  const current = settingsQuery.data.value ?? {}
  const next: MeSettings = { ...current, ui: { ...current.ui, pinned_hero_mode: mode } }
  try {
    await $heya('/api/me/settings', { method: 'PUT', body: next as never })
    queryClient.invalidateQueries({ queryKey: ['me', 'settings'] })
  } catch { /* localStorage mirror still holds it for this device */ }
}

// No longer rendered as its own row — kept only so Recommended For You can
// exclude titles the user already favorited (the Loved sidebar views cover
// browsing favorites).
const recommendedItems = computed<MediaItem[]>(() =>
  (forYouQuery.data.value?.items ?? []).map(it => ({
    id: it.id,
    public_id: it.public_id,
    title: it.title,
    slug: it.slug,
    year: it.year ?? '',
    media_type: it.media_type,
    available: it.available,
  }) as unknown as MediaItem),
)

const loading = computed(() =>
  moviesQuery.isPending.value || tvQuery.isPending.value || booksQuery.isPending.value || musicHomeQuery.isPending.value
)

// Chip per TV show: what the newest grouped event for that show was, so the
// hero slide can say WHY it's featured ("New season", "New episode", …).
const tvChipByShow = computed<Record<number, string>>(() => {
  const out: Record<number, string> = {}
  for (const e of tvQuery.data.value ?? []) {
    if (out[e.media_item_id]) continue
    out[e.media_item_id]
      = e.kind === 'series' ? 'New show'
        : e.kind === 'season' ? `New season ${e.season_number}`
          : e.kind === 'episodes' ? 'New episodes' : 'New episode'
  }
  return out
})

const heroItems = computed(() => {
  // Hero only spotlights playable titles — never feature something whose
  // files were removed from disk.
  const combined = [
    ...recentMovies.value.filter(i => i.available !== false).map(i => ({ ...i, chip: 'New film', _sort: new Date(i.created_at).getTime() })),
    ...recentTVShows.value.map(i => ({ ...i, chip: tvChipByShow.value[i.id], _sort: new Date(i.created_at).getTime() })),
  ]
  combined.sort((a, b) => b._sort - a._sort)
  return combined.slice(0, 5)
})

const hasContent = computed(() =>
  recentMovies.value.length + recentTVItems.value.length + recentAlbums.value.length + recentBooks.value.length > 0
)

// Albums route to /music/artist/{aslug}/{album_slug}. Falls back to the
// generic mediaUrl shape so this works even if the ContentRow item is a
// vanilla MediaItem (e.g. dev/build noise) — we always have at least an id.
function albumUrl(item: AlbumRowItem | MediaItem) {
  const al = item as AlbumRowItem
  if (al.artist_slug && al.album_slug) return `/music/artist/${al.artist_slug}/${al.album_slug}`
  return mediaUrl(item as MediaItem)
}

// Normalize a raw recent-album row into the ContentRow item shape. The
// double cast through unknown is intentional — AlbumRowItem extends MediaItem
// which has a wide field surface (library_id, sort_title, …) we don't have or
// need for the rail.
function albumToRowItem(al: {
  id: number; title: string; year: string; artist_name: string; artist_slug: string; slug: string; available?: boolean
}): AlbumRowItem {
  return {
    id: al.id,
    title: al.title,
    year: al.year,
    sub: al.artist_name,
    media_type: 'music',
    slug: al.slug,
    artist_slug: al.artist_slug,
    album_slug: al.slug,
    artist_name: al.artist_name,
    available: al.available,
    poster_src: useAlbumCoverUrl(al.artist_slug, al.slug) ?? undefined,
  } as unknown as AlbumRowItem
}

// Grouped TV event → rail card. Poster is the show's; the subtitle carries
// the event ("New show", "New season 3 · 8 episodes", "S05E12 · Title").
// `year` stays empty so ContentRow falls through to `sub`, and `key` keeps
// v-for happy when one show has two event cards.
function tvEntryToRowItem(e: RecentTVEntry): MediaItem {
  return {
    id: e.media_item_id,
    public_id: e.media_item_public_id,
    key: `${e.media_item_id}-${e.kind}-${e.season_number}-${e.episode_number}-${e.added_at}`,
    title: e.title,
    year: '',
    sub: tvEntrySub(e),
    media_type: 'tv',
    slug: e.slug,
    created_at: e.added_at,
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

// Artist event → rail card. id is the artist's media item id so the default
// /api/media/{id}/image/poster lookup and mediaUrl routing both work.
function artistToRowItem(ar: RecentArtistEntry): ArtistRowItem {
  const sub = ar.kind === 'new'
    ? 'New artist'
    : ar.new_album_count > 1
      ? `${ar.new_album_count} new releases`
      : `New: ${ar.latest_album_title || 'release'}`
  return {
    id: ar.media_item_id,
    public_id: ar.media_item_public_id,
    title: ar.name,
    year: '',
    sub,
    media_type: 'music',
    slug: ar.slug,
    artist_id: ar.id,
    available: true,
  } as unknown as ArtistRowItem
}

const homeMovieWatchedSet = ref<Set<number>>(new Set())
const homeShowWatchedSet = ref<Set<number>>(new Set())
const homeFavoritedSet = ref<Set<number>>(new Set())
const { buildItems: buildCardCtxItems } = useCardContextItems()
const musicActions = useMusicActions()

watchEffect(() => {
  homeMovieWatchedSet.value = new Set(movieStateQuery.data.value?.watched ?? [])
  homeShowWatchedSet.value = new Set((seriesStateQuery.data.value?.shows ?? [])
    .filter(s => s.total_episodes > 0 && s.watched_episodes >= s.total_episodes)
    .map(s => s.media_item_id))
  homeFavoritedSet.value = new Set([
    ...(mediaStateQuery.data.value?.favorited ?? []),
    ...(movieStateQuery.data.value?.favorited ?? []),
    ...(seriesStateQuery.data.value?.favorited ?? []),
  ])
})

function watchedSetFor(item: MediaItem): Set<number> | undefined {
  if (item.media_type === 'movie') return homeMovieWatchedSet.value
  if (item.media_type === 'tv' || item.media_type === 'anime') return homeShowWatchedSet.value
  return undefined
}

function homeMediaContextItems(item: MediaItem): ContextMenuItem[] {
  const watchedSet = watchedSetFor(item)
  return buildCardCtxItems(item, {
    watchedSet,
    favoritedSet: homeFavoritedSet.value,
    userLists: userListsQuery.data.value ?? [],
    onToggleWatched: watchedSet ? toggleHomeWatched : undefined,
    onToggleFavorite: toggleHomeFavorite,
    onAddToList: addHomeItemToList,
  })
}

function homeBookContextItems(item: MediaItem): ContextMenuItem[] {
  return buildCardCtxItems(item, {
    favoritedSet: homeFavoritedSet.value,
    userLists: userListsQuery.data.value ?? [],
    onToggleFavorite: toggleHomeFavorite,
    onAddToList: addHomeItemToList,
  })
}

function homeAlbumContextItems(item: MediaItem & { artist_slug?: string; album_slug?: string; artist_name?: string; sub?: string }): ContextMenuItem[] {
  if (!item.artist_slug || !item.album_slug) return []
  return musicActions.forAlbum({
    id: item.id,
    title: item.title,
    artist_slug: item.artist_slug,
    album_slug: item.album_slug,
    artist_name: item.artist_name ?? item.sub,
    available: item.available,
  })
}

function homeArtistContextItems(item: MediaItem & { artist_id?: number }): ContextMenuItem[] {
  if (!item.artist_id) return []
  return musicActions.forArtist({
    id: item.artist_id,
    name: item.title,
    slug: item.slug,
    media_item_id: item.id,
    available: item.available,
  })
}

async function toggleHomeWatched(id: number, watched: boolean, item: MediaItem) {
  const setRef = item.media_type === 'movie' ? homeMovieWatchedSet : homeShowWatchedSet
  try {
    await $heya('/api/me/watched/media/{id}', {
      method: 'POST',
      path: { id },
      body: { watched } as any,
    })
    const next = new Set(setRef.value)
    if (watched) next.add(id)
    else next.delete(id)
    setRef.value = next
    invalidateContinueWatching()
    queryClient.invalidateQueries({ queryKey: ['me', 'state'] })
  } catch { /* ignore */ }
}

async function toggleHomeFavorite(id: number, favorited: boolean) {
  try {
    await $heya('/api/me/favorites', {
      method: 'POST',
      body: { entity_type: 'media_item', entity_id: id } as any,
    })
    const next = new Set(homeFavoritedSet.value)
    if (favorited) next.add(id)
    else next.delete(id)
    homeFavoritedSet.value = next
    queryClient.invalidateQueries({ queryKey: ['me', 'media-state'] })
    queryClient.invalidateQueries({ queryKey: ['me', 'state'] })
  } catch { /* ignore */ }
}

async function addHomeItemToList(listId: number, mediaId: number) {
  try {
    await $heya('/api/me/lists/{id}/items', {
      method: 'POST',
      path: { id: listId },
      body: { media_item_id: mediaId } as any,
    })
  } catch { /* ignore */ }
}

// Hero details — resolves movie/tv detail for each hero tile so the
// HeroA component can render genres/rating/play button. Recomputed when
// the underlying movie/tv lists refresh.
async function rebuildHeroDetails() {
  for (const item of heroItems.value) {
    if (movieDetails.value[item.id]) continue // already fetched in this session
    try {
      const detail = await $heya('/api/media/{id}', { path: { id: String(item.id) } }) as MediaDetail
      // Local trailer file → hero trailer takeover for this slide.
      const trailer = detail.extras?.find(x => x.extra_type === 'trailer' && x.file_path)
      if (trailer) heroTrailers.value[item.id] = trailer.id
      if (detail.movie) {
        movieDetails.value[item.id] = detail.movie
        const fileId = detail.files?.[0]?.public_id || detail.files?.[0]?.id || null
        if (fileId) heroPlayInfo.value[item.id] = { fileId }
      } else if (detail.tv_series) {
        movieDetails.value[item.id] = {
          id: 0, media_item_id: item.id,
          runtime_minutes: 0, tagline: '', genres: detail.tv_series.genres || [],
          rating: detail.tv_series.rating, release_date: detail.tv_series.first_air_date,
          original_title: '', original_language: '', budget: 0, revenue: 0,
        }
        try {
          const up = await $heya('/api/media/{id}/up-next', { path: { id: item.id as never } }) as {
            has_next: boolean; file_id?: number; file_public_id?: string; episode_id?: number
            season_number?: number; episode_number?: number; episode_title?: string
          }
          const fileId = up?.file_public_id || up?.file_id
          if (up?.has_next && fileId) {
            const s = String(up.season_number ?? 0).padStart(2, '0')
            const e = String(up.episode_number ?? 0).padStart(2, '0')
            const base = `S${s}E${e}`
            const label = up.episode_title ? `${base} - ${up.episode_title}` : base
            heroPlayInfo.value[item.id] = { fileId, label, episodeId: up.episode_id }
          }
        } catch { /* empty */ }
      }
    } catch { /* empty */ }
  }
}
watch(heroItems, rebuildHeroDetails, { immediate: true })

function onHeroPlay(item: MediaItem) {
  const info = heroPlayInfo.value[item.id]
  if (!info?.fileId) return
  const titleSuffix = info.label ? ` - ${info.label}` : ''
  const params = new URLSearchParams({
    media_item_id: String(item.id),
    title: `${item.title}${titleSuffix}`,
  })
  // Hero plays a *movie* by default; TV entries also flow through here
  // (heroPlayInfo carries a file id for the next-unwatched episode).
  // Tag the entity type so the activity panel can format the title
  // correctly. info.episodeId is set when the hero target is a TV series.
  if (info.episodeId) {
    params.set('entity_type', 'episode')
    params.set('entity_id', String(info.episodeId))
  } else {
    params.set('entity_type', 'movie')
    params.set('entity_id', String(item.id))
  }
  navigateTo(`/watch/${info.fileId}?${params}`)
}

// Live refresh: media.added (file just matched) / media.updated (enrich
// landed — new seasons/episodes/albums included) map to each rail's query
// key by media_type. See useLiveRefresh for the coalescing rationale — a
// scan matching hundreds of files must not trigger hundreds of refetches.
useLiveRefresh([
  { events: ['media.added', 'media.updated'], filter: byMediaType('movie'), keys: [['media', 'recent', 'movie']] },
  { events: ['media.added', 'media.updated'], filter: byMediaType('tv', 'anime'), keys: [['media', 'recent', 'tv']] },
  { events: ['media.added', 'media.updated'], filter: byMediaType('book'), keys: [['media', 'recent', 'book']] },
  { events: ['media.added', 'media.updated'], filter: byMediaType('music'), keys: [['home', 'recent-albums']] },
  { events: ['media.watched'], keys: [['me', 'state'], ['me', 'media-state'], ['me', 'watch', 'continue'], ['me', 'watch', 'recent']] },
])
</script>

<style scoped>
/* Flex column purely so children's CSS `order` (user section order) takes
   effect; visual layout is unchanged for the default order. */
.home-sections {
  display: flex;
  flex-direction: column;
}
.home-sections > .empty-home { order: 99; }

.empty-home {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 80px 0;
  text-align: center;
  color: var(--fg-2);
}
.empty-home h2 {
  font-size: 28px;
  font-weight: 600;
  color: var(--fg-0);
  margin-bottom: 8px;
}
.empty-home p {
  font-size: 15px;
}

/* Phone (W3a): match the 16px-side .page-pad override already used by the
   music pages (see music/artists.vue) — the shared heya.css rule only tapers
   to 24px at <=1100px. ContentRow / ContinueWatchingRow / UpNextRow are
   untouched this package (touch-fixed already in W2b), so their shared
   `.section-title-lg` heading and `.more` "See all" link get a page-scoped
   :deep() override here instead of editing those component files. */
@media (max-width: 720px) {
  .page-pad { padding-left: 16px; padding-right: 16px; }
  .page-pad :deep(.section-title-lg) { font-size: 18px; }
  .page-pad :deep(.more) {
    padding: 10px 6px;
    margin: -10px -6px;
  }
}
</style>
