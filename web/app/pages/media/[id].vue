<template>
  <div v-if="loading" class="animate-pulse space-y-4">
    <div class="h-64 rounded-xl bg-surface-overlay" />
    <div class="h-8 w-1/3 rounded bg-surface-overlay" />
    <div class="h-4 w-2/3 rounded bg-surface-overlay" />
  </div>

  <div v-else-if="detail" class="space-y-6">
    <!-- Backdrop Hero -->
    <div class="relative -mx-6 -mt-6 h-72 overflow-hidden lg:h-96">
      <img
        v-if="backdropUrl"
        :src="backdropUrl"
        class="h-full w-full object-cover"
      />
      <div v-else class="h-full w-full bg-surface-overlay" />
      <div class="absolute inset-0 bg-gradient-to-t from-surface via-surface/60 to-transparent" />
    </div>

    <!-- Title row -->
    <div class="flex gap-6">
      <div class="hidden w-48 shrink-0 sm:block">
        <img
          v-if="posterUrl"
          :src="posterUrl"
          :alt="detail.media_item.title"
          class="-mt-32 relative z-10 w-full rounded-xl shadow-2xl"
        />
      </div>

      <div class="flex-1 space-y-3">
        <div class="flex items-center gap-3">
          <span class="rounded px-2 py-0.5 text-xs font-semibold uppercase" :class="mediaTypeBg(detail.media_item.media_type)">
            {{ mediaTypeLabel(detail.media_item.media_type) }}
          </span>
          <span v-if="detail.media_item.year" class="text-sm text-gray-500">{{ detail.media_item.year }}</span>
        </div>

        <h1 class="text-3xl font-bold">{{ detail.media_item.title }}</h1>

        <!-- Movie-specific -->
        <template v-if="detail.movie">
          <p v-if="detail.movie.tagline" class="italic text-gray-400">{{ detail.movie.tagline }}</p>
          <div class="flex flex-wrap gap-2">
            <span v-for="g in detail.movie.genres" :key="g" class="rounded-full bg-surface-overlay px-2.5 py-0.5 text-xs text-gray-300">{{ g }}</span>
          </div>
          <div class="flex gap-6 text-sm text-gray-400">
            <span v-if="detail.movie.runtime_minutes">{{ detail.movie.runtime_minutes }} min</span>
            <span v-if="detail.movie.rating">{{ parseFloat(detail.movie.rating).toFixed(1) }}/10</span>
            <span v-if="detail.movie.original_language" class="uppercase">{{ detail.movie.original_language }}</span>
          </div>
        </template>

        <!-- TV-specific -->
        <template v-if="detail.tv_series">
          <div class="flex flex-wrap gap-2">
            <span v-for="g in detail.tv_series.genres" :key="g" class="rounded-full bg-surface-overlay px-2.5 py-0.5 text-xs text-gray-300">{{ g }}</span>
          </div>
          <div class="flex gap-6 text-sm text-gray-400">
            <span v-if="detail.tv_series.status">{{ detail.tv_series.status }}</span>
            <span>{{ detail.tv_series.seasons_count }} season{{ detail.tv_series.seasons_count !== 1 ? 's' : '' }}</span>
            <span v-if="detail.tv_series.rating">{{ parseFloat(detail.tv_series.rating).toFixed(1) }}/10</span>
          </div>
        </template>

        <!-- Book-specific -->
        <template v-if="detail.book">
          <div class="flex gap-6 text-sm text-gray-400">
            <span v-if="detail.author">by {{ detail.author.name }}</span>
            <span v-if="detail.book.pages">{{ detail.book.pages }} pages</span>
            <span v-if="detail.book.publisher">{{ detail.book.publisher }}</span>
          </div>
          <div class="flex flex-wrap gap-2">
            <span v-for="g in detail.book.genres" :key="g" class="rounded-full bg-surface-overlay px-2.5 py-0.5 text-xs text-gray-300">{{ g }}</span>
          </div>
        </template>

        <p v-if="detail.media_item.description" class="max-w-prose text-sm leading-relaxed text-gray-400">
          {{ detail.media_item.description }}
        </p>

        <!-- TV Seasons -->
        <div v-if="detail.seasons?.length" class="space-y-2 pt-4">
          <h3 class="text-sm font-semibold uppercase tracking-wider text-gray-500">Seasons</h3>
          <div class="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
            <div
              v-for="s in detail.seasons"
              :key="s.id"
              class="card flex items-center gap-3 p-3"
            >
              <img
                v-if="usePosterUrl(s.poster_path, 'w185')"
                :src="usePosterUrl(s.poster_path, 'w185')!"
                class="h-16 w-11 rounded object-cover"
              />
              <div>
                <div class="text-sm font-medium">{{ s.name }}</div>
                <div class="text-xs text-gray-500">{{ s.episode_count }} episodes</div>
              </div>
            </div>
          </div>
        </div>

        <!-- Music Albums -->
        <div v-if="detail.albums?.length" class="space-y-2 pt-4">
          <h3 class="text-sm font-semibold uppercase tracking-wider text-gray-500">Albums</h3>
          <div class="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
            <div
              v-for="a in detail.albums"
              :key="a.id"
              class="card flex items-center gap-3 p-3"
            >
              <div class="flex h-12 w-12 items-center justify-center rounded bg-heya-music/20 text-lg text-heya-music">
                {{ a.title[0] }}
              </div>
              <div>
                <div class="text-sm font-medium">{{ a.title }}</div>
                <div class="text-xs text-gray-500">{{ a.track_count }} tracks</div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>

  <div v-else class="py-20 text-center text-gray-500">
    Media not found
  </div>
</template>

<script setup lang="ts">
import type { MediaDetail } from '~~/shared/types'

const { isAuthenticated } = useAuth()
watchEffect(() => {
  if (!isAuthenticated.value) navigateTo('/login')
})

const route = useRoute()
const detail = ref<MediaDetail | null>(null)
const loading = ref(true)

const posterUrl = computed(() => detail.value ? usePosterUrl(detail.value.media_item.poster_path, 'w500') : null)
const backdropUrl = computed(() => detail.value ? useBackdropUrl(detail.value.media_item.backdrop_path) : null)

onMounted(async () => {
  try {
    detail.value = await apiFetch<MediaDetail>(`/api/media/${route.params.id}`)
  } catch { /* empty */ }
  loading.value = false
})
</script>
