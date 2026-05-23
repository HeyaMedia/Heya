<template>
  <Teleport to="body">
    <Transition name="modal">
      <div v-if="show" class="me-overlay" @click.self="$emit('close')">
        <div class="me-dialog">
          <button class="me-close" @click="$emit('close')">
            <Icon name="close" :size="18" />
          </button>
          <MetadataManager
            :fixed-media-id="mediaId"
            :fixed-season-id="seasonId"
            :fixed-episode-id="episodeId"
            @close="$emit('close')"
          />
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
defineProps<{
  mediaId: number
  seasonId?: number | null
  episodeId?: number | null
  show: boolean
}>()

defineEmits<{ close: [] }>()
</script>

<style scoped>
.me-overlay {
  position: fixed;
  inset: 0;
  z-index: 1000;
  background: rgba(0, 0, 0, 0.75);
  backdrop-filter: blur(8px);
  display: flex;
  align-items: center;
  justify-content: center;
}

.me-dialog {
  width: 96vw;
  max-width: 1100px;
  height: 90vh;
  max-height: 820px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  overflow: hidden;
  position: relative;
  box-shadow: var(--shadow-3);
  display: flex;
  flex-direction: column;
}

.me-close {
  position: absolute;
  top: 14px;
  right: 14px;
  z-index: 10;
  width: 34px;
  height: 34px;
  border-radius: 50%;
  border: none;
  background: rgba(0, 0, 0, 0.5);
  color: var(--fg-2);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.15s;
}
.me-close:hover {
  background: rgba(0, 0, 0, 0.7);
  color: var(--fg-0);
}

.modal-enter-active,
.modal-leave-active {
  transition: all 0.2s ease;
}
.modal-enter-from,
.modal-leave-to {
  opacity: 0;
}
.modal-enter-from .me-dialog {
  transform: scale(0.96) translateY(8px);
}
.modal-leave-to .me-dialog {
  transform: scale(0.98);
}
</style>
