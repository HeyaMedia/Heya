<template>
  <div class="progress">
    <div :style="{ width: `${pct}%` }" />
  </div>
</template>

<script setup lang="ts">
defineProps<{ pct: number }>()
</script>
