<script setup lang="ts">
// Circular trashcan badge overlaid on media artwork whose files were removed
// from disk. Render it inside a positioned ancestor — the <Poster> slot, or a
// `position: relative` tile — and pair it with the `.poster--missing` class on
// the Poster to grey + dim the art (see heya.css).
</script>

<template>
  <div class="missing-badge" title="Missing on disk" aria-label="Missing on disk">
    <Icon name="trash" :size="12" />
  </div>
</template>

<style scoped>
.missing-badge {
  position: absolute; top: 8px; right: 8px; z-index: 4;
  display: inline-flex; align-items: center; justify-content: center;
  width: 24px; height: 24px; border-radius: 50%;
  background: rgba(217, 107, 107, 0.92); color: #fff;
  box-shadow: 0 2px 6px rgba(0, 0, 0, 0.35);
  pointer-events: none;
}
</style>
