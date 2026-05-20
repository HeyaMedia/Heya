<template>
  <Teleport to="body">
    <Transition name="lb">
      <div v-if="isOpen" class="lb-overlay" @click.self="close">
        <img :key="currentSrc" :src="currentSrc" class="lb-img" @click.stop />

        <button class="lb-close" @click="close"><Icon name="close" :size="20" /></button>

        <button v-if="hasPrev" class="lb-nav lb-prev" @click.stop="prev"><Icon name="chevleft" :size="28" /></button>
        <button v-if="hasNext" class="lb-nav lb-next" @click.stop="next"><Icon name="chevright" :size="28" /></button>

        <div v-if="total > 1" class="lb-counter">{{ index + 1 }} / {{ total }}</div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
const { isOpen, currentSrc, index, total, hasNext, hasPrev, close, next, prev } = useLightbox()

function onKeydown(e: KeyboardEvent) {
  if (!isOpen.value) return
  if (e.key === 'Escape') close()
  else if (e.key === 'ArrowRight' && hasNext.value) next()
  else if (e.key === 'ArrowLeft' && hasPrev.value) prev()
}

onMounted(() => window.addEventListener('keydown', onKeydown))
onUnmounted(() => window.removeEventListener('keydown', onKeydown))
</script>

<style scoped>
.lb-overlay {
  position: fixed;
  inset: 0;
  z-index: 9999;
  background: rgba(0, 0, 0, 0.92);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: zoom-out;
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
