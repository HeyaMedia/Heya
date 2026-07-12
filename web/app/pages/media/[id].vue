<script setup lang="ts">
import { useQuery } from '@pinia/colada'
import { mediaDetailQuery } from '~/queries/media'

const route = useRoute()
const id = parseInt(route.params.id as string, 10)

if (isNaN(id)) await navigateTo('/')
else {
  const detailQuery = useQuery(mediaDetailQuery(id))
  try {
    await waitForQuery(detailQuery)
    if (detailQuery.data.value) await navigateTo(mediaUrl(detailQuery.data.value.media_item), { replace: true })
    else await navigateTo('/')
  } catch {
    await navigateTo('/')
  }
}
</script>

<template>
  <div style="display: flex; align-items: center; justify-content: center; height: 100%; color: var(--fg-3)">
    Redirecting…
  </div>
</template>
