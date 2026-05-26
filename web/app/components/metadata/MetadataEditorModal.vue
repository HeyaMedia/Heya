<template>
  <DialogRoot :open="show" @update:open="(v) => v ? null : $emit('close')">
    <DialogPortal>
      <Transition name="modal">
        <DialogOverlay v-if="show" class="me-overlay" />
      </Transition>
      <Transition name="modal">
        <DialogContent v-if="show" class="me-dialog" :aria-describedby="undefined">
          <VisuallyHidden>
            <DialogTitle>Edit metadata</DialogTitle>
          </VisuallyHidden>
          <DialogClose class="me-close" aria-label="Close">
            <Icon name="close" :size="18" />
          </DialogClose>
          <MetadataManager
            :fixed-media-id="mediaId"
            :fixed-season-id="seasonId"
            :fixed-episode-id="episodeId"
            @close="$emit('close')"
          />
        </DialogContent>
      </Transition>
    </DialogPortal>
  </DialogRoot>
</template>

<script setup lang="ts">
import { DialogRoot, DialogPortal, DialogOverlay, DialogContent, DialogTitle, DialogClose, VisuallyHidden } from 'reka-ui'

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
}

.me-dialog {
  position: fixed;
  top: 50%; left: 50%;
  transform: translate(-50%, -50%);
  z-index: 1001;
  width: 96vw;
  max-width: 1100px;
  height: 90vh;
  max-height: 820px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  overflow: hidden;
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
  transition: opacity 0.2s ease;
}
.modal-enter-from,
.modal-leave-to {
  opacity: 0;
}
</style>
