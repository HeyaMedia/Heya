<script setup lang="ts">
definePageMeta({ layout: false })

const route = useRoute()
const router = useRouter()

const fileId = computed(() => String(route.params.fileId || ''))
const mediaItemId = computed(() => {
  const id = route.query.media_item_id
  return id ? Number(id) : null
})
const title = computed(() => (route.query.title as string) || '')
// `?t=<seconds>` lets callers (e.g. continue-watching tiles) request playback start
// at a specific offset. Captured once at mount so a downstream router push
// doesn't move the seek target after the user starts scrubbing.
const startTime = computed(() => {
  const t = route.query.t
  if (!t) return 0
  const n = Number(t)
  return Number.isFinite(n) && n > 0 ? n : 0
})

// entity_type/entity_id let VideoPlayer report the right session shape
// for the activity panel — "movie" defaults at the server when missing,
// but for an episode we want entity_id=episode_id so the title resolves
// as "Series · S01E03 · Episode title".
const entityType = computed(() => (route.query.entity_type as string | undefined) ?? '')
const entityId = computed(() => {
  const v = route.query.entity_id
  if (!v) return 0
  const n = Number(v)
  return Number.isFinite(n) && n > 0 ? n : 0
})

// `?shuffle=1` — episode-shuffle session: the player picks a RANDOM held
// episode as "up next" (and keeps forwarding the flag) instead of the
// sequential next-unwatched.
const shuffle = computed(() => route.query.shuffle === '1')

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
    :key="fileId"
    :file-id="fileId"
    :media-item-id="mediaItemId"
    :title="title"
    :start-time="startTime"
    :entity-type="entityType"
    :entity-id="entityId"
    :shuffle="shuffle"
    @close="handleClose"
  />
</template>
