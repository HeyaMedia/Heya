<script setup lang="ts">
defineProps<{ text?: string | null }>()
</script>

<template>
  <p v-if="text" class="synopsis">{{ text }}</p>
</template>

<style scoped>
.synopsis {
  font-size: 14px; line-height: 1.65; color: var(--fg-1);
  max-width: 640px; margin: 12px 0 0;
  /* Art-proof halo — the hero sits on raw artwork in ambient mode. */
  text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1);
}
</style>
