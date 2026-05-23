<script setup lang="ts">
defineProps<{ mediaItemId: number }>()
</script>

<template>
  <div class="prefs-panel">
    <PlaybackPrefs :media-item-id="mediaItemId" always-open />
  </div>
</template>

<style scoped>
.prefs-panel {
  padding: 12px;
  background: rgba(0,0,0,0.45); backdrop-filter: blur(12px);
  border: 1px solid rgba(255,255,255,0.06);
  border-radius: var(--r-md);
}
.prefs-panel :deep(.pp.pp-inline) { margin-top: 0; }
.prefs-panel :deep(.pp-body-inline) { padding: 0; }
</style>
