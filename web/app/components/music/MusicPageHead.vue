<script setup lang="ts">
// MusicPageHead — the shared header for /music list pages. Every page under
// the music shell sits on the rotating ambient pool (pages/music.vue claims
// pool('music')), so a bare title + fg-3 count line washes out over bright
// art. This bakes in the design-system treatment once: halo'd title,
// fg-1 + halo subtitle, and a soft blurred bg-1 wash behind the block
// (same recipe as TagBrowse/RecsBrowse heads) so the text has a quiet
// landing zone on any artwork.
//
//   <MusicPageHead title="All Songs" subtitle="1,234 tracks">
//     <template #subtitle>…rich subtitle markup…</template>   (optional)
//     …right-aligned actions (buttons, filters)…              (default slot)
//   </MusicPageHead>
defineProps<{
  title: string
  /** Plain-text subtitle; use the #subtitle slot for markup instead. */
  subtitle?: string
}>()
</script>

<template>
  <header class="mhd">
    <div class="mhd-text">
      <h1 class="mhd-title">{{ title }}</h1>
      <div v-if="subtitle || $slots.subtitle" class="mhd-sub">
        <slot name="subtitle">{{ subtitle }}</slot>
      </div>
    </div>
    <div v-if="$slots.default" class="mhd-actions">
      <slot />
    </div>
  </header>
</template>

<style scoped>
.mhd {
  position: relative;
  isolation: isolate;
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 16px 24px;
  flex-wrap: wrap;
  margin-bottom: 22px;
}
/* Soft blended wash so the header block reads on busy ambient art — a
   blurred bg-1 ellipse biased toward the text, fading before it can read
   as a panel edge. */
.mhd::before {
  content: '';
  position: absolute;
  z-index: -1;
  inset: -26px -36px -20px -36px;
  background: radial-gradient(ellipse 80% 75% at 25% 50%,
    color-mix(in srgb, var(--bg-1) 55%, transparent),
    color-mix(in srgb, var(--bg-1) 38%, transparent) 45%,
    color-mix(in srgb, var(--bg-1) 16%, transparent) 70%,
    transparent 92%);
  filter: blur(26px);
  pointer-events: none;
}
/* Heya 2.0 head grammar: Archivo display title (wdth 115, weight 800), so
   every list page under the music shell inherits the same display look as the
   home greeting + the detail heroes. Props/slots API is unchanged. */
.mhd-title {
  font-family: var(--font-display);
  font-size: clamp(1.85rem, 2.8vw, 2.5rem);
  font-weight: 800;
  font-variation-settings: 'wdth' 115;
  letter-spacing: -0.02em;
  line-height: 1;
  color: var(--fg-0);
  text-shadow: 0 1px 2px var(--bg-1), 0 0 10px var(--bg-1), 0 0 24px var(--bg-1);
}
.mhd-sub {
  margin-top: 4px;
  font-size: 12px;
  font-family: var(--font-mono);
  letter-spacing: 0.04em;
  color: var(--fg-1);
  text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1);
  display: flex;
  align-items: center;
  gap: 8px;
}
.mhd-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}
</style>
