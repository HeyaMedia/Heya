<script setup lang="ts">
import type { MediaDetail } from '~~/shared/types'
import { useQuery } from '@pinia/colada'
import { mediaDetailQuery } from '~/queries/media'

const route = useRoute()
const slug = computed(() => route.params.slug as string)

const detailQuery = useQuery(() => mediaDetailQuery(slug.value))
await waitForQuery(detailQuery)
watch(detailQuery.error, (error) => {
  if (error) navigateTo('/books')
}, { immediate: true })

const detail = computed(() => detailQuery.data.value ?? null)
const mediaId = computed(() => detail.value?.media_item.id ?? null)
const loading = computed(() => detailQuery.isPending.value)
</script>

<template>
  <MediaDetailView v-if="mediaId && detail" :media-id="mediaId" :initial-detail="detail" />
  <div v-else-if="loading" style="display: flex; align-items: center; justify-content: center; height: 100%; color: var(--fg-3)">
    Loading…
  </div>
</template>
