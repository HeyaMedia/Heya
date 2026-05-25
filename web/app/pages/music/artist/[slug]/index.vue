<script setup lang="ts">
import type { MediaDetail } from '~~/shared/types'

definePageMeta({ layout: 'default' })

const route = useRoute()
const slug = computed(() => route.params.slug as string)

const mediaId = ref<number | null>(null)
const mediaType = ref<string | null>(null)
const loading = ref(true)

async function load() {
  loading.value = true
  try {
    const { $heya } = useNuxtApp()
    const detail = await $heya('/api/media/{id}', { path: { id: slug.value } }) as MediaDetail
    mediaId.value = detail.media_item.id
    mediaType.value = detail.media_item.media_type
  } catch {
    navigateTo('/music')
  } finally {
    loading.value = false
  }
}

watch(slug, load, { immediate: true })
</script>

<template>
  <MusicArtistDetail v-if="mediaId && mediaType === 'music'" :media-id="mediaId" />
  <MediaDetailView v-else-if="mediaId" :media-id="mediaId" />
  <div v-else-if="loading" style="display: flex; align-items: center; justify-content: center; height: 100%; color: var(--fg-3)">
    Loading…
  </div>
</template>
