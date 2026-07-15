<template>
  <div class="section-row-head sec-head">
    <div class="sh-text">
      <h2 class="sh-title"><slot name="title">{{ title }}</slot></h2>
      <span v-if="subtitle || $slots.subtitle" class="sh-count">
        <slot name="subtitle">{{ subtitle }}</slot>
      </span>
    </div>
    <div v-if="$slots.actions" class="sh-actions">
      <slot name="actions" />
    </div>
  </div>
</template>

<script setup lang="ts">
// Shared header for horizontal rows (and any artwork-backed section). Heya 2.0
// "sec-head" look: a hairline-ruled row with an uppercase letterspaced mono
// title, a tone-colored count/subtitle riding its baseline, and a right-
// aligned actions area (See-all link, scroll buttons). Root keeps the
// `.section-row-head` class so the global layout/actions chrome in heya.css
// applies unchanged. API preserved: title/subtitle props + title/subtitle/
// actions slots.
defineProps<{
  title?: string
  subtitle?: string
}>()
</script>

<style scoped>
/* Hairline-ruled mono header (heya2.css .sec-head). Scoped attribute lifts
   specificity over the global .section-row-head base, so the margin/border
   here win while legacy .section-row-head markup elsewhere is untouched. */
.sec-head {
  display: flex;
  align-items: baseline;
  gap: 14px;
  margin-bottom: 22px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--hair);
}

.sh-text {
  display: flex;
  align-items: baseline;
  gap: 14px;
  min-width: 0;
}

/* Uppercase letterspaced mono title. A faint --bg-1 halo keeps it legible
   where a section still rides bright pool artwork (home/browse), without the
   heavy blur washes the old art-proof header used. */
.sh-title {
  font: 600 12.5px var(--font-mono);
  letter-spacing: 0.24em;
  text-transform: uppercase;
  color: rgb(var(--ink) / 0.88);
  text-shadow: 0 0 10px var(--bg-1), 0 1px 2px var(--bg-1);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.sh-count {
  font: 600 12.5px var(--font-mono);
  color: var(--tone);
  text-shadow: 0 0 10px var(--bg-1), 0 1px 2px var(--bg-1);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.sh-actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 10px;
  flex-shrink: 0;
}
</style>
