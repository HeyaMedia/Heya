<template>
  <div class="scroll" style="height: 100%">
    <HeroDeck
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
      @play-up-next="onPlayUpNext"
      @pin="onPinHeroMode"
    />

    <div class="page-pad">
      <ContinueWatchingRow
        v-if="continueWatching.length"
        :items="continueWatching"
        @play="onPlayContinue"
      />

      <UpNextRow
        v-if="upNextItems.length"
        :items="upNextItems"
        @play="onPlayUpNext"
      />

      <ContentRow
        v-if="recommendedItems.length"
        title="Recommended For You"
        subtitle="Based on your library"
        :items="recommendedItems"
        @tile="(item) => navigateTo(mediaUrl(item))"
      />

      <ContentRow
        v-if="recentMovies.length"
        title="Recently Added Films"
        subtitle="Across all libraries"
        :items="recentMovies"
        more="See all"
        @tile="(item) => navigateTo(mediaUrl(item))"
        @more="navigateTo('/movies')"
      />

      <ContentRow
        v-if="recentTVItems.length"
        title="Recently Added TV"
        subtitle="New shows, seasons & episodes"
        :items="recentTVItems"
        more="See all"
        @tile="(item) => navigateTo(mediaUrl(item))"
        @more="navigateTo('/tv')"
      />

      <ContentRow
        v-if="recentAlbums.length"
        title="Recently Added Albums"
        subtitle="Across all libraries"
        :items="recentAlbums"
        :aspect="'1/1'"
        :tile-width="168"
        more="See all"
        @tile="(item) => navigateTo(albumUrl(item))"
        @more="navigateTo('/music/albums')"
      />

      <ContentRow
        v-if="recentArtists.length"
        title="Recently Added Artists"
        subtitle="New & updated artists"
        :items="recentArtists"
        :aspect="'1/1'"
        :tile-width="168"
        more="See all"
        @tile="(item) => navigateTo(mediaUrl(item))"
        @more="navigateTo('/music/artists')"
      />

      <ContentRow
        v-if="recentBooks.length"
        title="Recently Added Books"
        subtitle="Across all libraries"
        :items="recentBooks"
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
import type { MediaItem, MediaDetail, Movie } from '~~/shared/types'
import type { ContinueWatchingItem } from '~/components/home/ContinueWatchingRow.vue'
import type { HeroPlayInfo } from '~/components/home/HeroA.vue'
import type { UpNextItem } from '~/components/home/UpNextRow.vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'

const { $heya } = useNuxtApp()
const queryClient = useQueryClient()

// Music rows show recent ALBUMS plus recent ARTISTS. Items are normalized to
// MediaItem-ish so ContentRow renders them, with poster_src set to the
// album-cover endpoint and the click handler routing to album detail.
type AlbumRowItem = MediaItem & { artist_slug: string; album_slug: string }

// The TV rail is Plex-style grouped file arrivals, not bare shows: a brand-new
// show is one "New show" card, a season drop one "New season" card, and a
// lone episode an episode card. The backend derives the grouping; the FE only
// formats subtitles.
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

interface RecentArtistEntry {
  id: number
  media_item_id: number
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
const favoritesStateQuery = useQuery({
  queryKey: ['me', 'state', { scope: 'movies' }],
  queryFn: async () => (await $heya('/api/me/state', {
    method: 'POST',
    body: { scope: 'movies' } as never,
  })) as { favorited: number[] },
  staleTime: 1000 * 60,
})
const recsQuery = useQuery({
  queryKey: ['recommendations', { limit: 20 }],
  queryFn: async () => (await $heya('/api/recommendations', { query: { limit: 20 } })) as { local_media_item_id: number | null }[],
  staleTime: 1000 * 60 * 10,
})

const recentMovies = computed<MediaItem[]>(() => moviesQuery.data.value ?? [])
const recentBooks = computed<MediaItem[]>(() => booksQuery.data.value ?? [])
const recentAlbums = computed<AlbumRowItem[]>(() => musicHomeQuery.data.value?.albums ?? [])
const recentArtists = computed<MediaItem[]>(() => musicHomeQuery.data.value?.artists ?? [])

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
const upNextItems = ref<UpNextItem[]>([])

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
const favoriteItems = computed<MediaItem[]>(() => {
  const favIDs = new Set(favoritesStateQuery.data.value?.favorited ?? [])
  if (favIDs.size === 0) return []
  return [...recentMovies.value, ...recentTVShows.value].filter(m => favIDs.has(m.id) && m.available !== false)
})

const recommendedItems = computed<MediaItem[]>(() => {
  const recs = recsQuery.data.value ?? []
  if (!recs.length) return []
  const mediaMap = new Map([...recentMovies.value, ...recentTVShows.value].map(m => [m.id, m]))
  const local = recs
    .filter(r => r.local_media_item_id !== null)
    .map(r => mediaMap.get(r.local_media_item_id as number))
    .filter((m): m is MediaItem => !!m && m.available !== false)
  const existing = new Set([
    ...favoriteItems.value.map(m => m.id),
    ...upNextItems.value.map(m => m.id),
  ])
  return local.filter(m => !existing.has(m.id)).slice(0, 20)
})

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
function artistToRowItem(ar: RecentArtistEntry): MediaItem {
  const sub = ar.kind === 'new'
    ? 'New artist'
    : ar.new_album_count > 1
      ? `${ar.new_album_count} new releases`
      : `New: ${ar.latest_album_title || 'release'}`
  return {
    id: ar.media_item_id,
    title: ar.name,
    year: '',
    sub,
    media_type: 'music',
    slug: ar.slug,
    available: true,
  } as unknown as MediaItem
}

function onPlayContinue(item: ContinueWatchingItem) {
  // No file_id resolved → fall back to opening the detail page (rare,
  // happens when the underlying library file was deleted or never matched).
  if (!item.file_id) {
    navigateTo(mediaUrl({ id: item.media_item_id, title: item.title, slug: item.slug, media_type: item.media_type } as MediaItem))
    return
  }
  // Navigate straight into the player; VideoPlayer discovers the saved
  // resume position itself and asks the user via its own in-player modal.
  // Keeping the modal in the player avoids the cross-page transition
  // positioning glitch and means a single source of truth for "ask resume?".
  const params = new URLSearchParams({
    media_item_id: String(item.media_item_id),
    title: item.title,
  })
  if (item.entity_type) params.set('entity_type', item.entity_type)
  if (item.entity_id) params.set('entity_id', String(item.entity_id))
  navigateTo(`/watch/${item.file_id}?${params}`)
}

// Up Next: for each unique TV series in recently-watched, resolve the next
// unwatched episode. Imperative because it depends on the recentWatched
// query landing first AND iterates over the result set. Recomputed via
// `watch` whenever the underlying data refreshes.
async function rebuildUpNext() {
  const recent = recentWatchedQuery.data.value
  if (!recent?.length) { upNextItems.value = []; return }
  type RecentlyWatchedRow = { media_item_id: number; title: string; slug: string; media_type: string }
  const tvSeries = new Map<number, RecentlyWatchedRow>()
  for (const row of recent) {
    if (row.media_type !== 'tv') continue
    if (!tvSeries.has(row.media_item_id)) tvSeries.set(row.media_item_id, row as RecentlyWatchedRow)
  }
  const resolved = await Promise.allSettled(
    Array.from(tvSeries.values()).map(async row => {
      const up = await $heya('/api/media/{id}/up-next', { path: { id: row.media_item_id as never } }) as {
        has_next: boolean; file_id?: number; episode_id?: number
        season_number?: number; episode_number?: number; episode_title?: string
        runtime?: number
      }
      return { row, up }
    })
  )
  const entries: UpNextItem[] = []
  for (const r of resolved) {
    if (r.status !== 'fulfilled') continue
    const { row, up } = r.value
    if (!up?.has_next || !up.file_id) continue
    const sNum = up.season_number ?? 0
    const eNum = up.episode_number ?? 0
    const s = String(sNum).padStart(2, '0')
    const e = String(eNum).padStart(2, '0')
    const label = up.episode_title ? `S${s}E${e} · ${up.episode_title}` : `S${s}E${e}`
    entries.push({
      id: row.media_item_id, title: row.title, slug: row.slug,
      season_number: sNum, episode_number: eNum, episode_label: label,
      play_file_id: up.file_id,
      episode_id: up.episode_id,
      runtime_minutes: up.runtime,
    })
  }
  upNextItems.value = entries.slice(0, 20)
}
watch(() => recentWatchedQuery.data.value, rebuildUpNext, { immediate: true })

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
        const fileId = detail.files?.[0]?.id ?? null
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
            has_next: boolean; file_id?: number; episode_id?: number
            season_number?: number; episode_number?: number; episode_title?: string
          }
          if (up?.has_next && up.file_id) {
            const s = String(up.season_number ?? 0).padStart(2, '0')
            const e = String(up.episode_number ?? 0).padStart(2, '0')
            const base = `S${s}E${e}`
            const label = up.episode_title ? `${base} - ${up.episode_title}` : base
            heroPlayInfo.value[item.id] = { fileId: up.file_id, label, episodeId: up.episode_id }
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

function onPlayUpNext(entry: UpNextItem) {
  const s = String(entry.season_number).padStart(2, '0')
  const e = String(entry.episode_number).padStart(2, '0')
  const params = new URLSearchParams({
    media_item_id: String(entry.id),
    title: `${entry.title} - S${s}E${e}`,
  })
  if (entry.episode_id) {
    params.set('entity_type', 'episode')
    params.set('entity_id', String(entry.episode_id))
  }
  navigateTo(`/watch/${entry.play_file_id}?${params}`)
}

// Live refresh: media.added (file just matched) / media.updated (enrich
// landed — new seasons/episodes/albums included) map to each rail's query
// key by media_type. See useLiveRefresh for the coalescing rationale — a
// scan matching hundreds of files must not trigger hundreds of refetches.
useLiveRefresh([
  { events: ['media.added', 'media.updated'], filter: byMediaType('movie'), keys: [['media', 'recent', 'movie']] },
  { events: ['media.added', 'media.updated'], filter: byMediaType('tv'), keys: [['media', 'recent', 'tv']] },
  { events: ['media.added', 'media.updated'], filter: byMediaType('book'), keys: [['media', 'recent', 'book']] },
  { events: ['media.added', 'media.updated'], filter: byMediaType('music'), keys: [['home', 'recent-albums']] },
])
</script>

<style scoped>
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
