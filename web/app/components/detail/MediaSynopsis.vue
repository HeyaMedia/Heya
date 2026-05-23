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
}
</style>
