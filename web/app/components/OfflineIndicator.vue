<template>
  <Transition name="offline-slide">
    <div v-if="!online && isAuthenticated" class="offline-indicator" role="status">
      <Icon name="cloud" :size="13" />
      Offline · showing saved data
    </div>
  </Transition>
</template>

<script setup lang="ts">
import { useOnline } from '@vueuse/core'

const online = useOnline()
const { isAuthenticated } = useAuth()
</script>

<style scoped>
.offline-indicator {
  position: fixed; top: max(9px, env(safe-area-inset-top)); left: 50%; z-index: 10020;
  display: flex; align-items: center; gap: 7px;
  padding: 7px 12px; border-radius: 999px;
  color: #f4e7c4; background: rgba(42, 34, 20, 0.94);
  border: 1px solid rgba(214, 181, 109, 0.38);
  box-shadow: 0 5px 20px rgba(0, 0, 0, 0.35);
  font: 600 11px/1 var(--font-mono, monospace);
  transform: translateX(-50%);
  backdrop-filter: blur(12px);
}
.offline-slide-enter-active, .offline-slide-leave-active { transition: opacity 180ms ease, transform 180ms ease; }
.offline-slide-enter-from, .offline-slide-leave-to { opacity: 0; transform: translate(-50%, -8px); }
</style>
