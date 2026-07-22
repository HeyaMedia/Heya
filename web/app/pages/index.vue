<template>
  <!-- `hero-flush` opts the home page out of the .app-main topbar offset so the
       hero deck's art rides up under the glass topbar. The tone vars
       (--tone/--tone-rgb/--tone-ink) publish on the scroll root — the same
       pattern as the movie/series ports + the playbar's --pb-accent — so the
       library-pulse ledger and the rail section counts follow the featured
       (or active-mode) hero's dominant tone. -->
  <div class="scroll hero-flush" :style="toneStyle" style="height: 100%">
    <HeroDeck
      v-if="showSection('hero')"
      :items="heroItems"
      :movies="movieDetails"
      :play-info="heroPlayInfo"
      :trailers="heroTrailers"
      :up-next-items="upNextItems"
      :tv-entries="tvEntries"
      :albums="recentAlbums"
      :artists="recentArtists"
      :pinned-mode="pinnedHeroMode"
      @play="onHeroPlay"
      @play-up-next="playUpNext"
      @pin="onPinHeroMode"
    />

    <!-- Library pulse — the signature 2.0 ledger at the hero's hard-clip seam.
         USER-FACING facts only (session + recent arrivals), derived entirely
         from queries the page already made; no server/ops telemetry, no new
         endpoints (PLAN cardinal rule 2). -->
    <LedgerStrip
      v-if="showSection('hero') && (ledgerCells.length || pulsePending)"
      :cells="ledgerCells"
      :pending="pulsePending"
    />

    <!-- Sections render in user-configured order (Settings → Appearance)
         via CSS `order` — DOM stays static, only visual order moves. -->
    <div class="page-pad home-sections">
      <ContinueWatchingRow
        v-if="continueWatching.length && showSection('continue-watching')"
        :style="sectionStyle('continue-watching')"
        :items="continueWatching"
        :has-more="continueWatchingQuery.hasNextPage.value"
        :loading-more="continueWatchingQuery.asyncStatus.value === 'loading'"
        @play="playContinue"
        @load-more="loadMoreContinueWatching"
      />

      <UpNextRow
        v-if="upNextItems.length && showSection('up-next')"
        :style="sectionStyle('up-next')"
        :items="upNextItems"
        :has-more="upNextHasMore"
        :loading-more="upNextLoadingMore"
        @play="playUpNext"
        @load-more="loadMoreUpNext"
      />

      <ContentRow
        v-if="(recommendedItems.length || forYouQuery.isPending.value) && showSection('for-you')"
        :style="sectionStyle('for-you')"
        title="For You"
        subtitle="From what you've watched & loved"
        :items="recommendedItems"
        :context-items="homeMediaContextItems"
        :has-more="forYouQuery.hasNextPage.value"
        :loading-more="forYouQuery.asyncStatus.value === 'loading'"
        :pending="forYouQuery.isPending.value"
        more="Show all"
        @tile="(item) => navigateTo(mediaUrl(item))"
        @intent="prefetchMediaIntent"
        @more="navigateTo('/movies/recommendations')"
        @load-more="loadMoreForYou"
      />

      <ContentRow
        v-if="(recentMovies.length || moviesQuery.isPending.value) && showSection('recent-movies')"
        :style="sectionStyle('recent-movies')"
        title="Recently Added Films"
        subtitle="Across all libraries"
        :items="recentMovies"
        :context-items="homeMediaContextItems"
        :has-more="moviesQuery.hasNextPage.value"
        :loading-more="moviesQuery.asyncStatus.value === 'loading'"
        :pending="moviesQuery.isPending.value"
        show-added
        more="Show all"
        @tile="(item) => navigateTo(mediaUrl(item))"
        @intent="prefetchMediaIntent"
        @more="navigateTo('/movies/all?sort=added')"
        @load-more="loadMoreMovies"
      />

      <ContentRow
        v-if="(recentTVItems.length || tvQuery.isPending.value) && showSection('recent-tv')"
        :style="sectionStyle('recent-tv')"
        title="Recently Added TV"
        subtitle="New shows, seasons & episodes"
        :items="recentTVItems"
        :context-items="homeMediaContextItems"
        :has-more="tvQuery.hasNextPage.value"
        :loading-more="tvQuery.asyncStatus.value === 'loading'"
        :pending="tvQuery.isPending.value"
        show-added
        more="Show all"
        @tile="(item) => navigateTo(mediaUrl(item))"
        @intent="prefetchMediaIntent"
        @more="navigateTo('/tv/all?sort=added')"
        @load-more="loadMoreTV"
      />

      <ContentRow
        v-if="(recentAlbums.length || albumsQuery.isPending.value) && showSection('recent-albums')"
        :style="sectionStyle('recent-albums')"
        title="Recently Added Albums"
        subtitle="Across all libraries"
        :items="recentAlbums"
        :context-items="homeAlbumContextItems"
        :aspect="'1/1'"
        :tile-width="168"
        :has-more="albumsQuery.hasNextPage.value"
        :loading-more="albumsQuery.asyncStatus.value === 'loading'"
        :pending="albumsQuery.isPending.value"
        show-added
        more="Show all"
        @tile="(item) => navigateTo(albumUrl(item))"
        @more="navigateTo('/music/albums')"
        @load-more="loadMoreAlbums"
      />

      <ContentRow
        v-if="(recentArtists.length || artistsQuery.isPending.value) && showSection('recent-artists')"
        :style="sectionStyle('recent-artists')"
        title="Recently Added Artists"
        subtitle="New & updated artists"
        :items="recentArtists"
        :context-items="homeArtistContextItems"
        :aspect="'1/1'"
        :tile-width="168"
        :has-more="artistsQuery.hasNextPage.value"
        :loading-more="artistsQuery.asyncStatus.value === 'loading'"
        :pending="artistsQuery.isPending.value"
        show-added
        more="Show all"
        @tile="(item) => navigateTo(mediaUrl(item))"
        @intent="prefetchMediaIntent"
        @more="navigateTo('/music/artists')"
        @load-more="loadMoreArtists"
      />

      <ContentRow
        v-if="recentBooks.length && showSection('recent-books')"
        :style="sectionStyle('recent-books')"
        title="Recently Added Books"
        subtitle="Across all libraries"
        :items="recentBooks"
        :context-items="homeBookContextItems"
        :has-more="booksQuery.hasNextPage.value"
        :loading-more="booksQuery.asyncStatus.value === 'loading'"
        show-added
        more="Show all"
        @tile="(item) => navigateTo(mediaUrl(item))"
        @intent="prefetchMediaIntent"
        @more="navigateTo('/books?sort=added')"
        @load-more="loadMoreBooks"
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
import type { ContextMenuItem, MediaItem, MediaDetail, Movie } from '~~/shared/types'
import type { ContinueWatchingItem } from '~/types/home'
import type { HeroPlayInfo } from '~/components/home/HeroA.vue'
import type { ImageTone } from '~/composables/useImageTone'
import type { LedgerCell } from '~/components/ui/LedgerStrip.vue'
import { useInfiniteQuery, useQuery, useQueryCache } from '@pinia/colada'
import { meSettingsQuery, type UserSettingsBlob } from '~/queries/user'
import { mediaUserStateQuery, movieUserStateQuery, seriesUserStateQuery, userListsQuery as userListsOptions } from '~/queries/catalog'
import { mediaDetailQuery, mediaDetailTarget } from '~/queries/media'
import { continueWatchingInfinite } from '~/queries/activity'
import {
  forYouInfinite,
  recentArtistsInfinite,
  recentAlbumsInfinite,
  recentMediaInfinite,
  recentTVInfinite,
  type RecentAlbumRow,
  type RecentArtistEntry,
  type RecentTVEntry,
} from '~/queries/rails'

const { $heya } = useNuxtApp()
const queryClient = useQueryCache()
const invalidateContinueWatching = useInvalidateContinueWatching()

function prefetchMediaIntent(item: MediaItem) {
  const to = mediaUrl(item)
  void preloadRouteComponents(to)
  const entry = queryClient.ensure(mediaDetailQuery(mediaDetailTarget(item)))
  void queryClient.refresh(entry).catch(() => {})
}

// Music rows show recent ALBUMS plus recent ARTISTS. Items are normalized to
// MediaItem-ish so ContentRow renders them, with poster_src set to the
// album-cover endpoint and the click handler routing to album detail.
type AlbumRowItem = MediaItem & { artist_slug: string; album_slug: string; artist_name?: string }
type ArtistRowItem = MediaItem & { artist_id: number }

// The TV rail is Plex-style grouped file arrivals, not bare shows: a brand-new
// show is one "New show" card, a season drop one "New season" card, and a
// lone episode an episode card. The backend derives the grouping; the FE only
// formats subtitles. (RecentTVEntry lives in queries/rails.ts, shared with
// the TV browse landing.)

// One Pinia Colada per rail — infinite where the backend pages (the rail
// keeps loading as you scroll right), plain where it doesn't (artists, whose
// grouped events are window-bound). Each caches independently so cross-page
// navigation returns instantly; event-bus listeners below invalidate by key.
const moviesQuery = useInfiniteQuery(() => recentMediaInfinite('movie'))
const tvQuery = useInfiniteQuery(() => recentTVInfinite())
const booksQuery = useInfiniteQuery(() => recentMediaInfinite('book'))
const albumsQuery = useInfiniteQuery(() => recentAlbumsInfinite())
const loadMoreMovies = railLoadMore(moviesQuery)
const loadMoreTV = railLoadMore(tvQuery)
const loadMoreBooks = railLoadMore(booksQuery)
const loadMoreAlbums = railLoadMore(albumsQuery)

const artistsQuery = useInfiniteQuery(() => recentArtistsInfinite())
const continueWatchingQuery = useInfiniteQuery(() => continueWatchingInfinite())
const loadMoreArtists = railLoadMore(artistsQuery)
const loadMoreContinueWatching = railLoadMore(continueWatchingQuery)
// Personalized "For You" — the taste-vector + TMDB-graph engine. Excludes
// seeds (hearts / watched) server-side, so no client-side filtering needed.
const forYouQuery = useInfiniteQuery(() => forYouInfinite({ section: 'all' }))
const loadMoreForYou = railLoadMore(forYouQuery)

const userListsQuery = useQuery(userListsOptions())
const movieStateQuery = useQuery(movieUserStateQuery())
const seriesStateQuery = useQuery(seriesUserStateQuery())
const mediaStateQuery = useQuery(mediaUserStateQuery())

const recentMovies = computed<MediaItem[]>(() => (moviesQuery.data.value?.pages ?? []).flat())
const recentBooks = computed<MediaItem[]>(() => (booksQuery.data.value?.pages ?? []).flat())
const recentAlbums = computed<AlbumRowItem[]>(() => (albumsQuery.data.value?.pages ?? []).flat().map(albumToRowItem))
const recentArtists = computed<ArtistRowItem[]>(() =>
  (artistsQuery.data.value?.pages ?? []).flat().map(artistToRowItem))

// Flattened grouped-TV events across loaded pages — rail, hero and chips all
// derive from this one list.
const tvEntries = computed<RecentTVEntry[]>(() => (tvQuery.data.value?.pages ?? []).flat())

// Rail items: one card per grouped TV event (a show may appear twice).
const recentTVItems = computed<MediaItem[]>(() => tvEntries.value.map(tvEntryToRowItem))
// Deduped MediaItem-ish shows for hero / favorites / recommendations, which
// think in shows, not events.
const recentTVShows = computed<MediaItem[]>(() => {
  const seen = new Set<number>()
  const out: MediaItem[] = []
  for (const e of tvEntries.value) {
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
      description: e.description ?? '',
    } as unknown as MediaItem)
  }
  return out
})

const continueWatching = computed<ContinueWatchingItem[]>(() =>
  (continueWatchingQuery.data.value?.pages ?? []).flat())

// Hero/Up Next/Favorites/Recommendations are derived from the queries above.
const movieDetails = ref<Record<number, Movie>>({})
const heroPlayInfo = ref<Record<number, HeroPlayInfo>>({})
const heroTrailers = ref<Record<number, number>>({})

// Up Next + player navigation are shared with the Movies/TV Recommended
// landings — see useUpNext / usePlaybackNav.
const {
  upNextItems,
  isPending: upNextPending,
  hasMore: upNextHasMore,
  loadingMore: upNextLoadingMore,
  loadMore: loadMoreUpNext,
} = useUpNext()
const { playContinue, playUpNext } = usePlaybackNav()

// Pinned hero mode — server-persisted in user settings so it follows the
// user across devices. The deck itself mirrors to localStorage for instant
// paint; this query is the authority.
const settingsQuery = useQuery(meSettingsQuery())
const pinnedHeroMode = computed(() => settingsQuery.data.value?.ui?.pinned_hero_mode ?? undefined)

// Section visibility + order (Settings → Appearance). Rides the same
// ['me','settings'] query as pinnedHeroMode; hidden sections skip render,
// order lands as CSS `order` on the flex column below.
const { isVisible: showSection, orderOf } = useHomeSections()
const sectionStyle = (id: string) => ({ order: orderOf(id) })

async function onPinHeroMode(mode: string) {
  const current = settingsQuery.data.value ?? {}
  const next: UserSettingsBlob = { ...current, ui: { ...current.ui, pinned_hero_mode: mode } }
  try {
    await $heya('/api/me/settings', { method: 'PUT', body: next as never })
    queryClient.invalidateQueries({ key: ['me', 'settings'] })
  } catch { /* localStorage mirror still holds it for this device */ }
}

// The home For You rail, flattened across loaded pages.
const recommendedItems = computed<MediaItem[]>(() =>
  (forYouQuery.data.value?.pages ?? []).flatMap(p => p.items).map(it => ({
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
  moviesQuery.isPending.value || tvQuery.isPending.value || booksQuery.isPending.value
  || albumsQuery.isPending.value || artistsQuery.isPending.value
)

// Ledger shell: while the cell-feeding queries are still pending on a truly
// cold cache, the strip renders ghost cells at its final height instead of
// popping in later and shoving every rail down. Hydrated boots never see it.
const pulsePending = computed(() =>
  loading.value || continueWatchingQuery.isPending.value || upNextPending.value,
)

// Chip per TV show: what the newest grouped event for that show was, so the
// hero slide can say WHY it's featured ("New season", "New episode", …).
const tvChipByShow = computed<Record<number, string>>(() => {
  const out: Record<number, string> = {}
  for (const e of tvEntries.value) {
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
  return combined.slice(0, 10)
})

const hasContent = computed(() =>
  recentMovies.value.length + recentTVItems.value.length + recentAlbums.value.length + recentBooks.value.length > 0
)

// ── Tone follow ─────────────────────────────────────────────────────────────
// Publish --tone/--tone-rgb/--tone-ink on the scroll root so the ledger + the
// rail section counts pick up the hero's dominant color. Primary source is the
// AmbientBackdrop's own sampled tone (useBackgroundTone) — in ambient mode each
// hero claims its art, so this follows the ACTIVE mode's current slide and
// re-samples on every crossfade. A direct sample of the featured backdrop is
// the ambient-off fallback (sequence-guarded, the playbar's --pb-accent
// pattern).
const bgTone = useBackgroundTone()
const localTone = ref<ImageTone | null>(null)
let toneSeq = 0
const featuredBackdrop = computed(() => {
  const it = heroItems.value[0]
  return it ? useBackdropUrl(it) : null
})
watch(featuredBackdrop, (src) => {
  const seq = ++toneSeq
  if (!src) { localTone.value = null; return }
  sampleImageTone(src).then((t) => { if (seq === toneSeq) localTone.value = t })
}, { immediate: true })
const { toneFollowEnabled } = useAppearance()
const toneStyle = computed(() => {
  if (!toneFollowEnabled.value) return undefined
  const t = bgTone.value || localTone.value
  if (!t) return undefined
  const m = t.main.match(/\d+/g)
  if (!m) return undefined
  return { '--tone': t.main, '--tone-rgb': m.slice(0, 3).join(' '), '--tone-ink': t.ink }
})

// ── Library pulse ledger (user-facing facts only) ───────────────────────────
// Everything here comes from queries the page already ran. Recency counts read
// the loaded first pages of each "recently added" rail — since those rails are
// newest-first, all this-week arrivals live in the first page, so the counts
// stay stable as more (older) pages load. No totals: the library-count
// endpoint doesn't exist and the rule forbids inventing one.
const WEEK_MS = 7 * 24 * 60 * 60 * 1000
type Datedish = { added_at?: unknown; created_at?: unknown }
function toMs(v: unknown): number {
  if (!v) return NaN
  const iso = typeof v === 'string' ? v : (v as { Time?: string })?.Time
  return iso ? new Date(iso).getTime() : NaN
}
function addedThisWeek(items: Datedish[]): number {
  const cut = Date.now() - WEEK_MS
  return items.reduce((n, it) => {
    const t = toMs(it.added_at) || toMs(it.created_at)
    return !isNaN(t) && t >= cut ? n + 1 : n
  }, 0)
}

const ledgerCells = computed<LedgerCell[]>(() => {
  const cells: LedgerCell[] = []

  const cw = continueWatching.value.length
  if (cw) cells.push({ k: 'Continue', v: String(cw), unit: 'in progress', tone: true })

  const up = upNextItems.value[0]
  if (up) {
    const s = String(up.season_number ?? 0).padStart(2, '0')
    const e = String(up.episode_number ?? 0).padStart(2, '0')
    const code = up.season_number ? `S${s}E${e}` : ''
    if (code) cells.push({ k: 'Up next', v: code, sub: up.title, tone: true })
    else cells.push({ k: 'Up next', v: String(upNextItems.value.length), unit: 'waiting', tone: true })
  }

  const films = addedThisWeek(recentMovies.value as Datedish[])
  if (films) cells.push({ k: 'New films', v: String(films) })
  const tv = addedThisWeek(tvEntries.value as Datedish[])
  if (tv) cells.push({ k: 'New TV', v: String(tv) })
  const albums = addedThisWeek(recentAlbums.value as Datedish[])
  if (albums) cells.push({ k: 'New albums', v: String(albums) })
  const books = addedThisWeek(recentBooks.value as Datedish[])
  if (books) cells.push({ k: 'New books', v: String(books) })

  return cells
})

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
// need for the rail. added_at (min file arrival) feeds the corner chip.
function albumToRowItem(al: RecentAlbumRow): AlbumRowItem {
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
    added_at: al.added_at,
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
    added_at: ar.added_at,
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
    queryClient.invalidateQueries({ key: ['me', 'state'] })
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
    queryClient.invalidateQueries({ key: ['me', 'media-state'] })
    queryClient.invalidateQueries({ key: ['me', 'state'] })
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

// Copy one detail response into the hero's presentation maps. Keeping this
// synchronous is important on Back: the Colada entry is already warm, so the
// hero should paint its rating/genres/Play state in the first returned frame
// instead of briefly reverting to its data-less shape.
function applyHeroDetail(item: MediaItem, detail: MediaDetail) {
  const trailer = detail.extras?.find(x => x.extra_type === 'trailer' && x.file_path)
  if (trailer) heroTrailers.value[item.id] = trailer.id
  if (detail.movie) {
    movieDetails.value[item.id] = detail.movie
    const fileId = detail.files?.[0]?.public_id || detail.files?.[0]?.id || null
    if (fileId) heroPlayInfo.value[item.id] = { fileId }
    return
  }
  if (!detail.tv_series) return
  movieDetails.value[item.id] = {
    id: 0, media_item_id: item.id,
    runtime_minutes: 0, tagline: '', genres: detail.tv_series.genres || [],
    rating: detail.tv_series.rating, release_date: detail.tv_series.first_air_date,
    original_title: '', original_language: '', budget: 0, revenue: 0,
  }

  // The shared Up Next query is device-persisted and normally has the Play
  // target already. Reuse it before falling back to the per-series request.
  const up = upNextItems.value.find(candidate => candidate.id === item.id)
  const fileId = up?.play_file_public_id || up?.play_file_id
  if (up && fileId) {
    const s = String(up.season_number ?? 0).padStart(2, '0')
    const e = String(up.episode_number ?? 0).padStart(2, '0')
    const base = `S${s}E${e}`
    const separator = up.episode_label.indexOf(' · ')
    const episodeTitle = separator >= 0 ? up.episode_label.slice(separator + 3) : ''
    heroPlayInfo.value[item.id] = {
      fileId,
      label: episodeTitle ? `${base} - ${episodeTitle}` : base,
      episodeId: up.episode_id,
    }
  }
}

async function resolveHeroTVPlayInfo(item: MediaItem) {
  if (heroPlayInfo.value[item.id]) return
  try {
    const up = await $heya('/api/media/{id}/up-next', { path: { id: item.id as never } }) as {
      has_next: boolean; file_id?: number; file_public_id?: string; episode_id?: number
      season_number?: number; episode_number?: number; episode_title?: string
    }
    const fileId = up?.file_public_id || up?.file_id
    if (!up?.has_next || !fileId) return
    const s = String(up.season_number ?? 0).padStart(2, '0')
    const e = String(up.episode_number ?? 0).padStart(2, '0')
    const base = `S${s}E${e}`
    const label = up.episode_title ? `${base} - ${up.episode_title}` : base
    heroPlayInfo.value[item.id] = { fileId, label, episodeId: up.episode_id }
  } catch { /* empty */ }
}

// Hero details — paint warm Colada data synchronously, then revalidate it in
// place. A cold entry still resolves concurrently with the other slides.
async function rebuildHeroDetails() {
  await Promise.allSettled(heroItems.value.map(async (item) => {
    try {
      const entry = queryClient.ensure(mediaDetailQuery(mediaDetailTarget(item)))
      const cached = entry.state.value.data as MediaDetail | undefined
      if (cached) applyHeroDetail(item, cached)

      const detail = (await queryClient.refresh(entry)).data
      if (!detail) return
      applyHeroDetail(item, detail as MediaDetail)
      if (detail.tv_series) await resolveHeroTVPlayInfo(item)
    } catch { /* empty */ }
  }))
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
  { events: ['media.added', 'media.updated'], filter: byMediaType('music'), keys: [['home', 'recent-albums'], ['home', 'recent-artists']] },
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
