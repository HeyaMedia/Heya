<template>
  <TagBrowse kind="genre" :raw-name="name" />
</template>

<script setup lang="ts">
const route = useRoute()
const name = computed(() => route.params.name as string)
</script>
