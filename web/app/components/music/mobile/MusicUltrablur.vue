<template>
  <div
    class="music-ultrablur"
    :class="`music-ultrablur--${variant}`"
    :data-ultrablur-source="target?.source ?? 'pending'"
    :data-ultrablur-key="target?.key ?? ''"
    aria-hidden="true"
  >
    <Transition name="music-ultrablur-fade">
      <div
        v-if="target"
        :key="target.key"
        class="music-ultrablur-layer"
        :style="{ background: target.background }"
      >
        <img
          v-if="target.imageUrl"
          class="music-ultrablur-image"
          :src="target.imageUrl"
          alt=""
          draggable="false"
        >
        <div class="music-ultrablur-scrim" />
      </div>
    </Transition>
  </div>
</template>

<script setup lang="ts">
import type { MusicUltrablurTarget } from '~/composables/useMusicUltrablur'

withDefaults(defineProps<{
  target: MusicUltrablurTarget | null
  variant?: 'bar' | 'sheet'
}>(), {
  variant: 'sheet',
})
</script>

<style scoped>
.music-ultrablur {
  position: absolute;
  inset: 0;
  overflow: hidden;
  background: var(--bg-2);
  pointer-events: none;
  user-select: none;
  contain: paint;
}

.music-ultrablur-layer {
  position: absolute;
  inset: -10%;
  transform: scale(1.04);
  overflow: hidden;
}

.music-ultrablur-image {
  position: absolute;
  inset: -5%;
  width: 110%;
  height: 110%;
  object-fit: cover;
  /* useMusicUltrablur resolves internal artwork through the Go image
     transform as a cached 960px WebP with blur=31. Keep this layer static:
     a second live CSS blur made Chrome continuously composite a large
     filtered surface even after the crossfade had settled. A little
     transparency lets the deterministic color bed retain the richer palette
     the former saturation filter supplied. */
  opacity: 0.9;
  transform: scale(1.08);
}

.music-ultrablur-scrim {
  position: absolute;
  inset: 0;
  background:
    radial-gradient(ellipse at 50% 12%, rgba(0, 0, 0, 0.08), rgba(0, 0, 0, 0.34) 68%),
    linear-gradient(180deg, rgba(0, 0, 0, 0.22), rgba(0, 0, 0, 0.54));
}

.music-ultrablur--bar .music-ultrablur-layer { inset: -35% -8%; }
.music-ultrablur--bar .music-ultrablur-image {
  inset: -30% -5%;
  width: 110%;
  height: 160%;
  opacity: 0.78;
}
.music-ultrablur--bar .music-ultrablur-scrim {
  background: linear-gradient(90deg, rgba(0, 0, 0, 0.58), rgba(0, 0, 0, 0.42) 55%, rgba(0, 0, 0, 0.62));
}

.music-ultrablur-fade-enter-active,
.music-ultrablur-fade-leave-active {
  transition: opacity 1.15s ease;
}
.music-ultrablur-fade-leave-active { position: absolute; }
.music-ultrablur-fade-enter-from,
.music-ultrablur-fade-leave-to { opacity: 0; }

@media (prefers-reduced-motion: reduce) {
  .music-ultrablur-fade-enter-active,
  .music-ultrablur-fade-leave-active { transition-duration: 0.01ms; }
}
</style>
