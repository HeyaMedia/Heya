<template>
  <NuxtLink :to="`/media/${item.id}`" class="group">
    <div class="relative aspect-[2/3] overflow-hidden rounded-lg bg-surface-overlay">
      <img
        v-if="posterUrl"
        :src="posterUrl"
        :alt="item.title"
        class="h-full w-full object-cover transition-transform duration-300 group-hover:scale-105"
        loading="lazy"
      />
      <div v-else class="flex h-full w-full items-center justify-center text-3xl text-gray-700">
        {{ item.title[0] }}
      </div>
      <div class="absolute inset-0 bg-gradient-to-t from-black/60 via-transparent opacity-0 transition-opacity group-hover:opacity-100" />
      <span
        class="absolute left-2 top-2 rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase"
        :class="mediaTypeBg(item.media_type)"
      >
        {{ mediaTypeLabel(item.media_type) }}
      </span>
    </div>
    <div class="mt-2">
      <p class="truncate text-sm font-medium text-gray-200 group-hover:text-white">{{ item.title }}</p>
      <p class="text-xs text-gray-500">{{ item.year }}</p>
    </div>
  </NuxtLink>
</template>

<script setup lang="ts">
import type { MediaItem } from '~~/shared/types'

const props = defineProps<{ item: MediaItem }>()

const posterUrl = computed(() => usePosterUrl(props.item.poster_path))
</script>
