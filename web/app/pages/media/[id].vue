<script setup lang="ts">
import type { MediaDetail } from '~~/shared/types'

const route = useRoute()
const id = parseInt(route.params.id as string, 10)

onMounted(async () => {
  if (isNaN(id)) { navigateTo('/'); return }
  try {
    const detail = await apiFetch<MediaDetail>(`/api/media/${id}`)
    const url = mediaUrl(detail.media_item)
    navigateTo(url, { replace: true })
  } catch {
    navigateTo('/')
  }
})
</script>

<template>
  <div style="display: flex; align-items: center; justify-content: center; height: 100%; color: var(--fg-3)">
    Redirecting…
  </div>
</template>
