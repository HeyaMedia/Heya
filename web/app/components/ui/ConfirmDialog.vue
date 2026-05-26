<template>
  <AlertDialogRoot :open="state.open" @update:open="onOpenChange">
    <AlertDialogPortal>
      <AlertDialogOverlay class="cd-overlay" />
      <AlertDialogContent class="cd-content">
        <AlertDialogTitle class="cd-title">{{ state.title }}</AlertDialogTitle>
        <AlertDialogDescription v-if="state.message" class="cd-message">
          {{ state.message }}
        </AlertDialogDescription>
        <div class="cd-actions">
          <AlertDialogCancel as="button" class="btn" @click="onCancel">{{ state.cancelLabel }}</AlertDialogCancel>
          <AlertDialogAction
            as="button"
            class="btn"
            :class="state.destructive ? 'btn-danger' : 'btn-primary'"
            @click="onConfirm"
          >
            {{ state.confirmLabel }}
          </AlertDialogAction>
        </div>
      </AlertDialogContent>
    </AlertDialogPortal>
  </AlertDialogRoot>
</template>

<script setup lang="ts">
import {
  AlertDialogRoot,
  AlertDialogPortal,
  AlertDialogOverlay,
  AlertDialogContent,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogCancel,
  AlertDialogAction,
} from 'reka-ui'

const { state, _resolve } = useConfirm()

// reka emits update:open(false) on overlay click, ESC, and both action
// buttons. We bind explicit handlers on the Cancel/Action buttons so we
// can distinguish confirm from cancel; the open-change handler is just
// the fallback for ESC/overlay dismissals.
function onOpenChange(v: boolean) {
  if (!v && state.value.open) _resolve(false)
}
function onConfirm() { _resolve(true) }
function onCancel() { _resolve(false) }
</script>

<style scoped>
.cd-overlay {
  position: fixed; inset: 0; z-index: 10000;
  background: rgba(0, 0, 0, 0.6);
  backdrop-filter: blur(6px);
}
.cd-content {
  position: fixed;
  top: 50%; left: 50%;
  transform: translate(-50%, -50%);
  z-index: 10001;
  width: 420px;
  max-width: 92vw;
  background: var(--bg-2);
  border: 1px solid var(--border-strong);
  border-radius: var(--r-lg);
  padding: 22px 24px 18px;
  box-shadow: var(--shadow-3);
}
.cd-title {
  font-size: 15px;
  font-weight: 700;
  color: var(--fg-0);
  margin: 0 0 8px;
}
.cd-message {
  font-size: 13px;
  color: var(--fg-2);
  line-height: 1.5;
  margin: 0 0 18px;
}
.cd-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
.btn-danger {
  background: var(--bad);
  color: #fff;
  border: 1px solid var(--bad);
}
.btn-danger:hover { filter: brightness(1.08); }
</style>
