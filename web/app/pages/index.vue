<template>
  <div class="scroll" style="height: 100%">
    <HeroA :items="heroItems" :movies="movieDetails" :play-info="heroPlayInfo" @play="onHeroPlay" />

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
        v-if="favoriteItems.length"
        title="Your Favorites"
        :items="favoriteItems"
        @tile="(item) => navigateTo(mediaUrl(item))"
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
        v-if="recentTV.length"
        title="Recently Added TV Shows"
        subtitle="Across all libraries"
        :items="recentTV"
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

// Music row shows recent ALBUMS (not artists / media items) — focuses the rail
// on something a user can actually press play on. Items are normalized to
// MediaItem-ish so ContentRow renders them, with poster_src set to the
// album-cover endpoint and the click handler routing to album detail.
type AlbumRowItem = MediaItem & { artist_slug: string; album_slug: string }

// One vue-query per rail. Each caches independently so cross-page navigation
// returns instantly. Event-bus listeners below invalidate by key to refresh.
const moviesQuery = useQuery({
  queryKey: ['media', 'recent', 'movie'],
  queryFn: async () => (await $heya('/api/media', { query: { type: 'movie', limit: 20 } })) as MediaItem[],
  staleTime: 1000 * 60,
})
const tvQuery = useQuery({
  queryKey: ['media', 'recent', 'tv'],
  queryFn: async () => (await $heya('/api/media', { query: { type: 'tv', limit: 20 } })) as MediaItem[],
  staleTime: 1000 * 60,
})
const booksQuery = useQuery({
  queryKey: ['media', 'recent', 'book'],
  queryFn: async () => (await $heya('/api/media', { query: { type: 'book', limit: 20 } })) as MediaItem[],
  staleTime: 1000 * 60,
})
const albumsQuery = useQuery({
  queryKey: ['home', 'recent-albums'],
  queryFn: async () => {
    const home = await $heya('/api/music/home', { query: { limit: 20 } }) as { recent_albums: Array<{
      id: number; title: string; year: string; artist_name: string; artist_slug: string; slug: string
    }> }
    return (home.recent_albums ?? []).map(albumToRowItem)
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
const recentTV = computed<MediaItem[]>(() => tvQuery.data.value ?? [])
const recentBooks = computed<MediaItem[]>(() => booksQuery.data.value ?? [])
const recentAlbums = computed<AlbumRowItem[]>(() => albumsQuery.data.value ?? [])

const continueWatching = computed<ContinueWatchingItem[]>(() => continueWatchingQuery.data.value ?? [])

// Hero/Up Next/Favorites/Recommendations are derived from the queries above.
// Up Next needs an extra per-show /up-next round-trip; keep that imperative
// since it depends on the recent-watched query landing first.
const movieDetails = ref<Record<number, Movie>>({})
const heroPlayInfo = ref<Record<number, HeroPlayInfo>>({})
const upNextItems = ref<UpNextItem[]>([])

const favoriteItems = computed<MediaItem[]>(() => {
  const favIDs = new Set(favoritesStateQuery.data.value?.favorited ?? [])
  if (favIDs.size === 0) return []
  return [...recentMovies.value, ...recentTV.value].filter(m => favIDs.has(m.id))
})

const recommendedItems = computed<MediaItem[]>(() => {
  const recs = recsQuery.data.value ?? []
  if (!recs.length) return []
  const mediaMap = new Map([...recentMovies.value, ...recentTV.value].map(m => [m.id, m]))
  const local = recs
    .filter(r => r.local_media_item_id !== null)
    .map(r => mediaMap.get(r.local_media_item_id as number))
    .filter((m): m is MediaItem => !!m)
  const existing = new Set([
    ...favoriteItems.value.map(m => m.id),
    ...upNextItems.value.map(m => m.id),
  ])
  return local.filter(m => !existing.has(m.id)).slice(0, 20)
})

const loading = computed(() =>
  moviesQuery.isPending.value || tvQuery.isPending.value || booksQuery.isPending.value || albumsQuery.isPending.value
)

const heroItems = computed(() => {
  const combined = [
    ...recentMovies.value.map(i => ({ ...i, _sort: new Date(i.created_at).getTime() })),
    ...recentTV.value.map(i => ({ ...i, _sort: new Date(i.created_at).getTime() })),
  ]
  combined.sort((a, b) => b._sort - a._sort)
  return combined.slice(0, 5)
})

const hasContent = computed(() =>
  recentMovies.value.length + recentTV.value.length + recentAlbums.value.length + recentBooks.value.length > 0
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
  id: number; title: string; year: string; artist_name: string; artist_slug: string; slug: string
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
    poster_src: useAlbumCoverUrl(al.artist_slug, al.slug) ?? undefined,
  } as unknown as AlbumRowItem
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

// Event-bus driven cache invalidation. media.added / media.updated map to
// the right query key by media_type. Debounced so a burst of scanner events
// doesn't trigger 50 refetches.
const { on } = useEventBus()
const invalidationTimers: Record<string, ReturnType<typeof setTimeout>> = {}
const queryKeyByType: Record<string, readonly unknown[]> = {
  movie: ['media', 'recent', 'movie'],
  tv: ['media', 'recent', 'tv'],
  book: ['media', 'recent', 'book'],
  music: ['home', 'recent-albums'],
}

function scheduleInvalidate(mt: string, delay: number) {
  const key = queryKeyByType[mt]
  if (!key) return
  const existing = invalidationTimers[mt]
  if (existing) clearTimeout(existing)
  invalidationTimers[mt] = setTimeout(() => {
    queryClient.invalidateQueries({ queryKey: key })
  }, delay)
}

onMounted(() => {
  const unsubs = [
    on('media.added', (event) => {
      const mt = (event.payload as { media_type?: string }).media_type
      if (mt) scheduleInvalidate(mt, 2000)
    }),
    on('media.updated', (event) => {
      const mt = (event.payload as { media_type?: string }).media_type
      if (mt) scheduleInvalidate(mt, 3000)
    }),
  ]

  onUnmounted(() => {
    unsubs.forEach(fn => fn())
    Object.values(invalidationTimers).forEach(t => clearTimeout(t))
  })
})
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
</style>
