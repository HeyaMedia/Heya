<template>
  <span class="chip" :class="{ gold }">
    <slot />
  </span>
</template>

<script setup lang="ts">
defineProps<{ gold?: boolean }>()
</script>
