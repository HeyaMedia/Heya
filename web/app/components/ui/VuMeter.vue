<template>
  <span class="vu" :class="{ paused: !playing }">
    <span /><span /><span />
  </span>
</template>

<script setup lang="ts">
defineProps<{ playing?: boolean }>()
</script>
