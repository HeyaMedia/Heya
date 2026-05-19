<script setup lang="ts">
const route = useRoute()
const id = computed(() => parseSlugId(route.params.slug as string))
if (!id.value) navigateTo('/tv')
</script>

<template>
  <MediaDetailView v-if="id" :media-id="id" />
</template>
