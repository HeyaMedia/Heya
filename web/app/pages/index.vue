<template>
  <div class="scroll" style="height: 100%">
    <HeroA :items="heroItems" :movies="movieDetails" />

    <div class="page-pad">
      <ContinueWatchingRow
        v-if="continueWatching.length"
        :items="continueWatching"
        @play="onPlayContinue"
      />

      <ContentRow
        v-if="recentlyWatched.length"
        title="Recently Watched"
        :items="recentlyWatched"
        @tile="(item) => navigateTo(mediaUrl(item))"
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
        title="TV Shows"
        subtitle="Across all libraries"
        :items="recentTV"
        more="See all"
        @tile="(item) => navigateTo(mediaUrl(item))"
        @more="navigateTo('/tv')"
      />

      <ContentRow
        v-if="recentMusic.length"
        title="Music"
        :items="recentMusic"
        :aspect="'1/1'"
        :tile-width="168"
        more="See all"
        @tile="(item) => navigateTo(mediaUrl(item))"
        @more="navigateTo('/music')"
      />

      <ContentRow
        v-if="recentBooks.length"
        title="Books"
        :items="recentBooks"
        more="See all"
        @tile="(item) => navigateTo(mediaUrl(item))"
        @more="navigateTo('/books')"
      />

      <ActivityFeed />

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
import type { MediaItem, MediaDetail, MediaType, Movie } from '~~/shared/types'
import type { ContinueWatchingItem } from '~/components/home/ContinueWatchingRow.vue'

const recentMovies = ref<MediaItem[]>([])
const recentTV = ref<MediaItem[]>([])
const recentMusic = ref<MediaItem[]>([])
const recentBooks = ref<MediaItem[]>([])
const movieDetails = ref<Record<number, Movie>>({})
const continueWatching = ref<ContinueWatchingItem[]>([])
const recentlyWatched = ref<MediaItem[]>([])
const favoriteItems = ref<MediaItem[]>([])
const recommendedItems = ref<MediaItem[]>([])
const loading = ref(true)

const heroItems = computed(() => {
  const combined = [
    ...recentMovies.value.map(i => ({ ...i, _sort: new Date(i.created_at).getTime() })),
    ...recentTV.value.map(i => ({ ...i, _sort: new Date(i.created_at).getTime() })),
  ]
  combined.sort((a, b) => b._sort - a._sort)
  return combined.slice(0, 5)
})

const hasContent = computed(() =>
  recentMovies.value.length + recentTV.value.length + recentMusic.value.length + recentBooks.value.length > 0
)

function onPlayContinue(item: ContinueWatchingItem) {
  if (item.entity_type === 'episode') {
    navigateTo(mediaUrl({ id: item.media_item_id, title: item.title, media_type: item.media_type as any } as MediaItem))
  } else {
    navigateTo(mediaUrl({ id: item.media_item_id, title: item.title, media_type: item.media_type as any } as MediaItem))
  }
}

async function loadMedia() {
  // Type-specific feeds first so the cross-cutting endpoints below can dedupe
  // against them.
  const typeRefsTuple: [MediaType, Ref<MediaItem[]>][] = [
    ['movie', recentMovies], ['tv', recentTV], ['music', recentMusic], ['book', recentBooks],
  ]
  await Promise.allSettled(typeRefsTuple.map(async ([t, target]) => {
    try {
      target.value = await apiFetch<MediaItem[]>(`/api/media?type=${t}&limit=20`)
    } catch (e) {
      console.warn(`Failed to load ${t}:`, e)
    }
  }))

  // Cross-cutting personal data — keep as a typed 4-tuple so each result keeps
  // its own value shape after destructuring.
  const [cwRes, rwRes, favRes, recRes] = await Promise.allSettled([
    apiFetch<ContinueWatchingItem[]>('/api/watch/continue'),
    apiFetch<{ media_item_id: number }[]>('/api/watch/recent'),
    apiFetch<{ favorited: number[] }>('/api/user/state', {
      method: 'POST',
      body: JSON.stringify({ scope: 'movies' }),
    }),
    apiFetch<{ local_media_item_id: number | null }[]>('/api/recommendations?limit=20'),
  ])

  if (cwRes.status === 'fulfilled') {
    continueWatching.value = cwRes.value || []
  }

  if (rwRes.status === 'fulfilled' && rwRes.value?.length) {
    const rwItems = rwRes.value
    const allMedia = [...recentMovies.value, ...recentTV.value]
    const mediaMap = new Map(allMedia.map(m => [m.id, m]))
    recentlyWatched.value = rwItems
      .map(rw => mediaMap.get(rw.media_item_id))
      .filter((m): m is MediaItem => !!m)
      .slice(0, 20)
  }

  if (favRes.status === 'fulfilled') {
    const favIDs = new Set(favRes.value?.favorited || [])
    if (favIDs.size > 0) {
      const allMedia = [...recentMovies.value, ...recentTV.value]
      favoriteItems.value = allMedia.filter(m => favIDs.has(m.id))
    }
  }

  if (recRes.status === 'fulfilled' && recRes.value?.length) {
    const allMedia = [...recentMovies.value, ...recentTV.value]
    const mediaMap = new Map(allMedia.map(m => [m.id, m]))
    const localRecs = recRes.value
      .filter(r => r.local_media_item_id !== null)
      .map(r => mediaMap.get(r.local_media_item_id as number))
      .filter((m): m is MediaItem => !!m)
    const existingIds = new Set([
      ...favoriteItems.value.map(m => m.id),
      ...recentlyWatched.value.map(m => m.id),
    ])
    recommendedItems.value = localRecs.filter(m => !existingIds.has(m.id)).slice(0, 20)
  }

  for (const item of heroItems.value) {
    try {
      const detail = await apiFetch<MediaDetail>(`/api/media/${item.id}`)
      if (detail.movie) {
        movieDetails.value[item.id] = detail.movie
      } else if (detail.tv_series) {
        // Hero only reads a small subset (genres, rating, release_date) so a
        // minimal Movie-shaped projection is enough.
        movieDetails.value[item.id] = {
          id: 0, media_item_id: item.id,
          runtime_minutes: 0, tagline: '', genres: detail.tv_series.genres || [],
          rating: detail.tv_series.rating, release_date: detail.tv_series.first_air_date,
          original_title: '', original_language: '', budget: 0, revenue: 0,
        }
      }
    } catch { /* empty */ }
  }

  loading.value = false
}

const { on } = useEventBus()
const mediaRefreshTimers: Record<string, ReturnType<typeof setTimeout>> = {}
const typeRefs: Record<string, Ref<MediaItem[]>> = {
  movie: recentMovies, tv: recentTV, music: recentMusic, book: recentBooks,
}

onMounted(() => {
  loadMedia()

  const unsubs = [
    on('media.added', (event) => {
      const mt = (event.payload as { media_type?: string }).media_type
      const target = mt ? typeRefs[mt] : undefined
      if (!mt || !target) return
      const existing = mediaRefreshTimers[mt]
      if (existing) clearTimeout(existing)
      mediaRefreshTimers[mt] = setTimeout(() => {
        apiFetch<MediaItem[]>(`/api/media?type=${mt}&limit=20`)
          .then(items => { target.value = items })
          .catch(() => {})
      }, 2000)
    }),
    on('media.updated', (event) => {
      const mt = (event.payload as { media_type?: string }).media_type
      const target = mt ? typeRefs[mt] : undefined
      if (!mt || !target) return
      const existing = mediaRefreshTimers[mt]
      if (existing) clearTimeout(existing)
      mediaRefreshTimers[mt] = setTimeout(() => {
        apiFetch<MediaItem[]>(`/api/media?type=${mt}&limit=20`)
          .then(items => { target.value = items })
          .catch(() => {})
      }, 3000)
    }),
  ]

  onUnmounted(() => {
    unsubs.forEach(fn => fn())
    Object.values(mediaRefreshTimers).forEach(t => clearTimeout(t))
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
