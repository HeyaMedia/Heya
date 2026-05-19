<template>
  <div class="space-y-6">
    <h1 class="text-2xl font-semibold">Dashboard</h1>

    <div class="grid grid-cols-2 gap-4 lg:grid-cols-4">
      <StatCard label="Movies" :value="stats.movies" color="movie" />
      <StatCard label="TV Shows" :value="stats.tv" color="tv" />
      <StatCard label="Music" :value="stats.music" color="music" />
      <StatCard label="Books" :value="stats.books" color="book" />
    </div>

    <section v-if="recentItems.length">
      <h2 class="mb-3 text-lg font-medium text-gray-300">Recently Added</h2>
      <div class="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6 xl:grid-cols-8">
        <MediaCard v-for="item in recentItems" :key="item.id" :item="item" />
      </div>
    </section>

    <section>
      <h2 class="mb-3 text-lg font-medium text-gray-300">Libraries</h2>
      <div v-if="libraries.length" class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <NuxtLink
          v-for="lib in libraries"
          :key="lib.id"
          :to="`/libraries?id=${lib.id}`"
          class="card flex items-center gap-4 p-4 transition-colors hover:border-heya-primary/30"
        >
          <div
            class="flex h-10 w-10 items-center justify-center rounded-lg text-lg"
            :class="mediaTypeBg(lib.media_type)"
          >
            {{ mediaTypeLabel(lib.media_type)[0] }}
          </div>
          <div>
            <div class="font-medium">{{ lib.name }}</div>
            <div class="text-xs text-gray-500">{{ mediaTypeLabel(lib.media_type) }} &middot; {{ lib.paths.length }} path{{ lib.paths.length !== 1 ? 's' : '' }}</div>
          </div>
        </NuxtLink>
      </div>
      <div v-else class="card p-8 text-center text-gray-500">
        <p>No libraries yet</p>
        <NuxtLink to="/libraries" class="btn-primary mt-3 inline-flex">Add Library</NuxtLink>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import type { MediaItem, Library } from '~~/shared/types'

const { isAuthenticated } = useAuth()
watchEffect(() => {
  if (!isAuthenticated.value) navigateTo('/login')
})

const stats = ref({ movies: 0, tv: 0, music: 0, books: 0 })
const recentItems = ref<MediaItem[]>([])
const libraries = ref<Library[]>([])

onMounted(async () => {
  const [movieData, tvData, musicData, bookData, libData] = await Promise.allSettled([
    apiFetch<MediaItem[]>('/api/media?type=movie&limit=1'),
    apiFetch<MediaItem[]>('/api/media?type=tv&limit=1'),
    apiFetch<MediaItem[]>('/api/media?type=music&limit=1'),
    apiFetch<MediaItem[]>('/api/media?type=book&limit=1'),
    apiFetch<Library[]>('/api/libraries'),
  ])

  if (libData.status === 'fulfilled') libraries.value = libData.value

  const types = ['movie', 'tv', 'music', 'book'] as const
  const counts = [movieData, tvData, musicData, bookData]
  for (let i = 0; i < types.length; i++) {
    const result = counts[i]
    if (result.status === 'fulfilled') {
      stats.value[types[i] === 'tv' ? 'tv' : types[i] === 'movie' ? 'movies' : types[i] === 'book' ? 'books' : types[i]] = result.value.length
    }
  }

  const all = await Promise.allSettled(
    (['movie', 'tv', 'music', 'book'] as const).map(t =>
      apiFetch<MediaItem[]>(`/api/media?type=${t}&limit=8`)
    )
  )
  const items: MediaItem[] = []
  for (const r of all) {
    if (r.status === 'fulfilled') items.push(...r.value)
  }
  items.sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
  recentItems.value = items.slice(0, 16)
})
</script>
