<script setup lang="ts">
import type { MediaDetail } from '~~/shared/types'
import { useQuery } from '@pinia/colada'
import { mediaDetailQuery } from '~/queries/media'

definePageMeta({ layout: 'default' })

const route = useRoute()
const slug = computed(() => route.params.slug as string)

const detailQuery = useQuery(() => mediaDetailQuery(slug.value))
await waitForQuery(detailQuery)

// Redirect on confirmed not-found rather than every transient error —
// the retry: 0 stops the infinite-spinner case but a real 404 still
// bubbles into errored state.
watch(detailQuery.error, (err) => {
  if (err) navigateTo('/music')
}, { immediate: true })

const mediaId = computed(() => detailQuery.data.value?.media_item.id ?? null)
const mediaType = computed(() => detailQuery.data.value?.media_item.media_type ?? null)
const loading = computed(() => detailQuery.isPending.value)
</script>

<template>
  <MusicArtistDetail v-if="mediaId && mediaType === 'music'" :media-id="mediaId" :slug="slug" />
  <MediaDetailView v-else-if="mediaId && detailQuery.data.value" :media-id="mediaId" :initial-detail="detailQuery.data.value" />
  <div v-else-if="loading" style="display: flex; align-items: center; justify-content: center; height: 100%; color: var(--fg-3)">
    Loading…
  </div>
</template>
