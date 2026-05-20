<script setup lang="ts">
import type { MediaDetail } from '~~/shared/types'

const route = useRoute()
const slug = computed(() => route.params.slug as string)

const mediaId = ref<number | null>(null)
const loading = ref(true)

onMounted(async () => {
  try {
    const detail = await apiFetch<MediaDetail>(`/api/media/${slug.value}`)
    mediaId.value = detail.media_item.id
  } catch {
    navigateTo('/books')
  }
  loading.value = false
})
</script>

<template>
  <MediaDetailView v-if="mediaId" :media-id="mediaId" />
  <div v-else-if="loading" style="display: flex; align-items: center; justify-content: center; height: 100%; color: var(--fg-3)">
    Loading…
  </div>
</template>
