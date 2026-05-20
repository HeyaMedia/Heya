<template>
  <div class="scroll" style="height: 100%">
    <HeroA :items="heroItems" :movies="movieDetails" />

    <div class="page-pad">
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

const recentMovies = ref<MediaItem[]>([])
const recentTV = ref<MediaItem[]>([])
const recentMusic = ref<MediaItem[]>([])
const recentBooks = ref<MediaItem[]>([])
const movieDetails = ref<Record<number, Movie>>({})
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

async function loadMedia() {
  const types = ['movie', 'tv', 'music', 'book'] as const
  const refs = [recentMovies, recentTV, recentMusic, recentBooks]

  await Promise.all(
    types.map(async (t, i) => {
      try {
        refs[i].value = await apiFetch<MediaItem[]>(`/api/media?type=${t}&limit=20`)
      } catch (e) {
        console.warn(`Failed to load ${t}:`, e)
      }
    })
  )

  for (const item of heroItems.value) {
    try {
      const detail = await apiFetch<MediaDetail>(`/api/media/${item.id}`)
      if (detail.movie) {
        movieDetails.value[item.id] = detail.movie
      }
      if (detail.tv_series) {
        movieDetails.value[item.id] = {
          id: 0, media_item_id: item.id, tmdb_id: null, imdb_id: '',
          runtime_minutes: 0, tagline: '', genres: detail.tv_series.genres || [],
          rating: detail.tv_series.rating, release_date: detail.tv_series.first_air_date,
          original_title: '', original_language: '', budget: 0, revenue: 0,
        } as Movie
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
      if (!mt || !typeRefs[mt]) return
      if (mediaRefreshTimers[mt]) clearTimeout(mediaRefreshTimers[mt])
      mediaRefreshTimers[mt] = setTimeout(() => {
        apiFetch<MediaItem[]>(`/api/media?type=${mt}&limit=20`)
          .then(items => { typeRefs[mt].value = items })
          .catch(() => {})
      }, 2000)
    }),
    on('media.updated', (event) => {
      const mt = (event.payload as { media_type?: string }).media_type
      if (!mt || !typeRefs[mt]) return
      if (mediaRefreshTimers[mt]) clearTimeout(mediaRefreshTimers[mt])
      mediaRefreshTimers[mt] = setTimeout(() => {
        apiFetch<MediaItem[]>(`/api/media?type=${mt}&limit=20`)
          .then(items => { typeRefs[mt].value = items })
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
