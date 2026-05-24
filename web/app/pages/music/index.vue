<template>
  <MusicHome @see-artists="$router.push('/music/artists')" @see-albums="$router.push('/music/albums')" />
</template>

<script setup lang="ts">
// /music renders the music home view; sidebar + playbar + queue live in the
// parent /music shell so they survive navigation.
definePageMeta({ layout: 'default' })
</script>
