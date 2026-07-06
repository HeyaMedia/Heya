<template>
  <DialogRoot :open="isOpen" @update:open="(v) => v ? null : close()">
    <DialogPortal>
      <Transition name="lb">
        <DialogOverlay v-if="isOpen" class="lb-overlay" />
      </Transition>
      <Transition name="lb">
        <DialogContent
          v-if="isOpen"
          class="lb-content"
          :aria-describedby="undefined"
          @click.self="close"
        >
          <VisuallyHidden>
            <DialogTitle>Image viewer</DialogTitle>
          </VisuallyHidden>

          <NuxtImg :key="currentSrc" :src="currentSrc" class="lb-img" @click.stop />

          <DialogClose class="lb-close" aria-label="Close"><Icon name="close" :size="20" /></DialogClose>

          <button v-if="hasPrev" class="lb-nav lb-prev" @click.stop="prev"><Icon name="chevleft" :size="28" /></button>
          <button v-if="hasNext" class="lb-nav lb-next" @click.stop="next"><Icon name="chevright" :size="28" /></button>

          <div v-if="total > 1" class="lb-counter">{{ index + 1 }} / {{ total }}</div>
        </DialogContent>
      </Transition>
    </DialogPortal>
  </DialogRoot>
</template>

<script setup lang="ts">
import { DialogRoot, DialogPortal, DialogOverlay, DialogContent, DialogTitle, DialogClose, VisuallyHidden } from 'reka-ui'

const { isOpen, currentSrc, index, total, hasNext, hasPrev, close, next, prev } = useLightbox()

// reka's DialogContent handles Escape natively. Arrow keys aren't part of
// the dialog primitive, so we still wire those manually.
useEventListener(window, 'keydown', (e: KeyboardEvent) => {
  if (!isOpen.value) return
  if (e.key === 'ArrowRight' && hasNext.value) next()
  else if (e.key === 'ArrowLeft' && hasPrev.value) prev()
})
</script>

<style scoped>
.lb-overlay {
  position: fixed;
  inset: 0;
  z-index: 9999;
  background: rgba(0, 0, 0, 0.92);
}
.lb-content {
  position: fixed;
  inset: 0;
  z-index: 10000;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: zoom-out;
  /* Lightbox covers the full viewport — reset any default Dialog focus ring. */
  outline: none;
}
.lb-img {
  max-width: 92vw;
  max-height: 92vh;
  object-fit: contain;
  border-radius: 4px;
  cursor: default;
  user-select: none;
}
.lb-close {
  position: absolute;
  top: 20px;
  right: 20px;
  width: 40px;
  height: 40px;
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.08);
  border: none;
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: background 0.15s;
}
.lb-close:hover { background: rgba(255, 255, 255, 0.18); }
.lb-nav {
  position: absolute;
  top: 50%;
  transform: translateY(-50%);
  width: 48px;
  height: 48px;
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.06);
  border: none;
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: background 0.15s;
}
.lb-nav:hover { background: rgba(255, 255, 255, 0.16); }
.lb-prev { left: 20px; }
.lb-next { right: 20px; }
.lb-counter {
  position: absolute;
  bottom: 24px;
  left: 50%;
  transform: translateX(-50%);
  font-family: var(--font-mono);
  font-size: 12px;
  color: rgba(255, 255, 255, 0.5);
  letter-spacing: 0.08em;
}

.lb-enter-active, .lb-leave-active { transition: opacity 0.2s ease; }
.lb-enter-from, .lb-leave-to { opacity: 0; }
</style>
