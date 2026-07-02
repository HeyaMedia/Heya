<script setup lang="ts">
import type { MediaDetail } from '~~/shared/types'
import { useQuery } from '@tanstack/vue-query'

definePageMeta({ layout: 'default' })

const route = useRoute()
const slug = computed(() => route.params.slug as string)

const { $heya } = useNuxtApp()
const detailQuery = useQuery({
  queryKey: ['media', 'detail', slug],
  queryFn: async () => (await $heya('/api/media/{id}', { path: { id: slug.value } })) as MediaDetail,
  staleTime: 1000 * 60 * 5,
  retry: false,
})

// Redirect on confirmed not-found rather than every transient error —
// the retry: false stops the infinite-spinner case but a real 404 still
// bubbles into errored state.
watch(detailQuery.error, (err) => {
  if (err) navigateTo('/music')
})

const mediaId = computed(() => detailQuery.data.value?.media_item.id ?? null)
const mediaType = computed(() => detailQuery.data.value?.media_item.media_type ?? null)
const loading = computed(() => detailQuery.isPending.value)
</script>

<template>
  <MusicArtistDetail v-if="mediaId && mediaType === 'music'" :media-id="mediaId" :slug="slug" />
  <MediaDetailView v-else-if="mediaId" :media-id="mediaId" />
  <div v-else-if="loading" style="display: flex; align-items: center; justify-content: center; height: 100%; color: var(--fg-3)">
    Loading…
  </div>
</template>
