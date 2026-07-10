<template>
  <div class="section-row-head">
    <div class="sh-text">
      <h2 class="section-title-lg sh-title"><slot name="title">{{ title }}</slot></h2>
      <div v-if="subtitle || $slots.subtitle" class="sh-sub">
        <slot name="subtitle">{{ subtitle }}</slot>
      </div>
    </div>
    <div v-if="$slots.actions" class="sh-actions">
      <slot name="actions" />
    </div>
  </div>
</template>

<script setup lang="ts">
// Shared header for horizontal rows (and any artwork-backed section):
// title + optional subtitle with an art-proof treatment that holds up in
// both themes over any backdrop, plus an actions slot (See-all link,
// scroll buttons). Root keeps the `.section-row-head` class so the global
// layout/actions chrome in heya.css applies unchanged.
defineProps<{
  title?: string
  subtitle?: string
}>()
</script>

<style scoped>
/* Blended readability washes — same recipe as the hero: --bg-1-derived
   (paper in light, dark in dark), long falloff, heavy blur, no locatable
   edge. BOTH washes live on the header root at z:-1 inside one isolated
   stacking context: as child pseudos they'd be sibling contexts, and the
   later (actions) wash would paint OVER the title text wherever the two
   overlap on narrow screens. Scoped, so legacy .section-row-head markup
   elsewhere is untouched. */
.section-row-head {
  position: relative;
  isolation: isolate;
}
.section-row-head::before {
  content: '';
  position: absolute;
  top: -48px;
  bottom: -52px;
  left: -70px;
  width: min(56%, 520px);
  z-index: -1;
  pointer-events: none;
  background: radial-gradient(ellipse 90% 75% at 32% 50%,
    color-mix(in srgb, var(--bg-1) 55%, transparent) 0%,
    color-mix(in srgb, var(--bg-1) 38%, transparent) 40%,
    color-mix(in srgb, var(--bg-1) 16%, transparent) 68%,
    transparent 92%);
  filter: blur(24px);
}
.section-row-head::after {
  content: '';
  position: absolute;
  top: -44px;
  bottom: -48px;
  right: -70px;
  width: min(40%, 380px);
  z-index: -1;
  pointer-events: none;
  background: radial-gradient(ellipse 85% 75% at 66% 50%,
    color-mix(in srgb, var(--bg-1) 55%, transparent) 0%,
    color-mix(in srgb, var(--bg-1) 38%, transparent) 40%,
    color-mix(in srgb, var(--bg-1) 16%, transparent) 68%,
    transparent 92%);
  filter: blur(24px);
}

.sh-text { min-width: 0; }
/* Triple-layer --bg-1 halo: a tight contact shadow plus two glow radii.
   Adapts per theme (paper glow in light, dark glow in dark) and keeps
   text readable over near-white or near-black artwork alike. */
.sh-title {
  text-shadow:
    0 1px 2px var(--bg-1),
    0 0 10px var(--bg-1),
    0 0 24px var(--bg-1);
}
.sh-sub {
  font-size: 12px;
  font-family: var(--font-mono);
  letter-spacing: 0.04em;
  margin-top: 2px;
  /* One tier below the title — NOT the muted fg-2/3 tiers, which wash out
     over bright art no matter the halo. */
  color: var(--fg-1);
  text-shadow:
    0 1px 2px var(--bg-1),
    0 0 10px var(--bg-1),
    0 0 24px var(--bg-1);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sh-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-shrink: 0;
}
</style>
