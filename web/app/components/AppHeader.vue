<template>
  <header class="flex h-14 items-center gap-4 border-b border-surface-border bg-surface-raised px-6">
    <div class="relative flex-1 max-w-md">
      <input
        v-model="query"
        type="text"
        placeholder="Search media..."
        class="input pl-9"
        @keydown.enter="search"
      />
      <svg class="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
      </svg>
    </div>

    <div v-if="results.length" class="absolute top-14 left-0 right-0 z-50 mx-auto max-w-md">
      <div class="card mt-1 max-h-80 overflow-y-auto p-2 shadow-2xl">
        <NuxtLink
          v-for="item in results"
          :key="item.id"
          :to="`/media/${item.id}`"
          class="flex items-center gap-3 rounded-lg px-3 py-2 hover:bg-surface-overlay"
          @click="results = []"
        >
          <img
            v-if="usePosterUrl(item.poster_path, 'w185')"
            :src="usePosterUrl(item.poster_path, 'w185')!"
            class="h-12 w-8 rounded object-cover"
          />
          <div v-else class="flex h-12 w-8 items-center justify-center rounded bg-surface-overlay text-xs text-gray-600">
            ?
          </div>
          <div>
            <div class="text-sm font-medium">{{ item.title }}</div>
            <div class="text-xs text-gray-500">{{ item.year }} &middot; {{ mediaTypeLabel(item.media_type) }}</div>
          </div>
        </NuxtLink>
        <div v-if="!results.length" class="px-3 py-4 text-center text-sm text-gray-500">
          No results
        </div>
      </div>
    </div>
  </header>
</template>

<script setup lang="ts">
import type { MediaItem } from '~~/shared/types'

const query = ref('')
const results = ref<MediaItem[]>([])

async function search() {
  if (!query.value.trim()) {
    results.value = []
    return
  }
  try {
    const data = await apiFetch<MediaItem[]>(`/api/search?q=${encodeURIComponent(query.value)}`)
    results.value = data
  } catch {
    results.value = []
  }
}

watch(query, (v) => {
  if (!v.trim()) results.value = []
})
</script>
