<script setup lang="ts">
definePageMeta({ layout: false })

const route = useRoute()
const router = useRouter()

const fileId = computed(() => Number(route.params.fileId))
const mediaItemId = computed(() => {
  const id = route.query.media_item_id
  return id ? Number(id) : null
})
const title = computed(() => (route.query.title as string) || '')

function handleClose() {
  if (window.history.length > 1) {
    router.back()
  } else {
    navigateTo('/')
  }
}
</script>

<template>
  <VideoPlayer
    :file-id="fileId"
    :media-item-id="mediaItemId"
    :title="title"
    @close="handleClose"
  />
</template>
