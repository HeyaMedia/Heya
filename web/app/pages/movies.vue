<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between">
      <h1 class="text-2xl font-semibold">Movies</h1>
      <span class="text-sm text-gray-500">{{ items.length }} titles</span>
    </div>
    <MediaGrid :items="items" :loading="loading" />
  </div>
</template>

<script setup lang="ts">
import type { MediaItem } from '~~/shared/types'

const { isAuthenticated } = useAuth()
watchEffect(() => {
  if (!isAuthenticated.value) navigateTo('/login')
})

const items = ref<MediaItem[]>([])
const loading = ref(true)

onMounted(async () => {
  try {
    items.value = await apiFetch<MediaItem[]>('/api/media?type=movie&limit=200')
  } catch { /* empty */ }
  loading.value = false
})
</script>
