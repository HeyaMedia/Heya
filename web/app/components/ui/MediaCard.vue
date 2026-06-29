<script setup lang="ts">
// MediaCard — unified poster tile with the gradient + overlaid-text treatment
// shared by MusicCard / EpisodeCard. Wraps the <Poster> primitive so we keep
// the same idx-keyed palette fallback and NuxtImg densities handling.
//
// Slot `badges` is rendered above the gradient (z-index 3) for custom
// chips/overlays — watched check, resolution tag, etc. Each consumer
// positions them absolutely with the existing badge classes.

withDefaults(defineProps<{
  src?: string | null
  idx?: number
  title: string
  subtitle?: string
  /** Top-left chip painted on the art (e.g. "S2", "2024", "MIX"). */
  badgeTl?: string
  /** Top-right chip painted on the art (e.g. "★ 8.7", "42 plays"). */
  badgeTr?: string
  /** Highlight badgeTr in gold (default true). Pass `false` for neutral. */
  badgeTrGold?: boolean
  aspect?: string
  /** Width hint forwarded to the NuxtImg densities resolver. */
  width?: number
  /** 0..100 — bottom progress bar. */
  progressPct?: number
  /** Renders a trashcan badge top-right and greys + dims the tile. */
  missing?: boolean
}>(), {
  src: '',
  idx: 0,
  subtitle: '',
  badgeTl: '',
  badgeTr: '',
  badgeTrGold: true,
  aspect: '2/3',
  width: 200,
  progressPct: 0,
  missing: false,
})
</script>

<template>
  <div class="mediac" :aria-label="title">
    <!-- Don't forward title to Poster — Poster paints it in the centre of
         the fallback gradient when there's no src, which collides with
         our own bottom overlay. We always show the title via .mediac-title
         (visible regardless of image state). -->
    <Poster :idx="idx" :src="src" :aspect="aspect" :width="width" :class="{ 'poster--missing': missing }">
      <div class="mediac-gradient" />

      <div v-if="badgeTl" class="mediac-badge mediac-badge-tl">{{ badgeTl }}</div>
      <div v-if="badgeTr" class="mediac-badge mediac-badge-tr" :class="{ 'mediac-badge-gold': badgeTrGold }">{{ badgeTr }}</div>
      <MediaMissingBadge v-if="missing" />

      <slot name="badges" />

      <div class="mediac-info">
        <div class="mediac-title">{{ title }}</div>
        <div v-if="subtitle" class="mediac-sub">{{ subtitle }}</div>
      </div>

      <div v-if="progressPct > 0" class="mediac-progress">
        <div class="mediac-progress-fill" :style="{ width: Math.min(100, progressPct) + '%' }" />
      </div>
    </Poster>
  </div>
</template>

<style scoped>
.mediac { display: block; height: 100%; }

/* Painted from inside the Poster slot, so the gradient sits above the image
   but below any badges/info. z-index inherits Poster's `isolation: isolate`. */
.mediac-gradient {
  position: absolute; inset: 0;
  background: linear-gradient(0deg, rgba(0,0,0,0.88) 0%, rgba(0,0,0,0.25) 45%, transparent 72%);
  pointer-events: none;
  z-index: 2;
}

.mediac-badge {
  position: absolute; z-index: 3;
  display: inline-flex; align-items: center; gap: 3px;
  padding: 3px 9px;
  border-radius: 999px;
  background: rgba(0, 0, 0, 0.6);
  backdrop-filter: blur(6px);
  font-size: 10px; font-weight: 700; font-family: var(--font-mono);
  color: rgba(255, 255, 255, 0.85);
  text-transform: uppercase; letter-spacing: 0.06em;
  max-width: calc(100% - 16px);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  pointer-events: none;
}
.mediac-badge-tl { top: 8px; left: 8px; }
.mediac-badge-tr { top: 8px; right: 8px; }
.mediac-badge-gold { color: var(--gold); }

.mediac-info {
  position: absolute; bottom: 0; left: 0; right: 0; z-index: 3;
  padding: 10px 12px 12px;
  pointer-events: none;
}
.mediac-title {
  font-size: 14px; font-weight: 700; line-height: 1.25;
  color: #fff;
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  text-shadow: 0 1px 4px rgba(0, 0, 0, 0.5);
}
.mediac-sub {
  font-size: 11px;
  color: rgba(255, 255, 255, 0.7);
  margin-top: 2px;
  font-family: var(--font-mono);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}

.mediac-progress {
  position: absolute; bottom: 0; left: 0; right: 0;
  height: 3px; z-index: 4;
  background: rgba(255, 255, 255, 0.12);
  pointer-events: none;
}
.mediac-progress-fill {
  height: 100%;
  background: var(--gold);
  border-radius: 0 2px 2px 0;
  transition: width 0.3s ease;
}
</style>
