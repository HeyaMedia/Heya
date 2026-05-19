<template>
  <div>
    <div v-if="loading" class="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
      <div v-for="i in 12" :key="i" class="animate-pulse">
        <div class="aspect-[2/3] rounded-lg bg-surface-overlay" />
        <div class="mt-2 h-4 w-3/4 rounded bg-surface-overlay" />
        <div class="mt-1 h-3 w-1/4 rounded bg-surface-overlay" />
      </div>
    </div>
    <div v-else-if="items.length" class="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6">
      <MediaCard v-for="item in items" :key="item.id" :item="item" />
    </div>
    <div v-else class="flex flex-col items-center justify-center py-20 text-gray-500">
      <p class="text-lg">No media found</p>
      <p class="mt-1 text-sm">Scan a library to discover content</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { MediaItem } from '~~/shared/types'

defineProps<{
  items: MediaItem[]
  loading?: boolean
}>()
</script>
