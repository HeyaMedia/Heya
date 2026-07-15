<!--
  AppDialog — generic modal dialog with surface chrome.

  Wraps reka-ui's Dialog primitives. Use this for any modal that's not
  destructive-confirm (that's ConfirmDialog / useConfirm) and not a
  pre-existing specialised one (CreatePlaylistModal etc.).

  Usage:
    <AppDialog v-model="show" title="Add to list" size="md">
      Pick a list from the options below.
      <template #footer>
        <button class="btn" @click="show = false">Cancel</button>
        <button class="btn btn-primary" @click="confirm">Save</button>
      </template>
    </AppDialog>

  Slots:
    default  — body
    header   — replaces the title/close header (rare; use title prop normally)
    footer   — actions row at the bottom (no default)

  defineModel handles the open state so consumers just bind v-model.
-->
<template>
  <DialogRoot v-model:open="open" :modal="modal">
    <DialogPortal>
      <Transition name="app-dialog">
        <DialogOverlay v-if="open" class="app-dialog-overlay" />
      </Transition>
      <Transition name="app-dialog">
        <DialogContent
          v-if="open"
          class="surface app-dialog-content"
          :class="[`app-dialog-${size}`, contentClass]"
          @open-auto-focus="onOpenAutoFocus"
        >
          <slot name="header">
            <header v-if="title || closable" class="app-dialog-header">
              <DialogTitle v-if="title" as="h3" class="app-dialog-title">{{ title }}</DialogTitle>
              <DialogDescription v-if="description" as="p" class="app-dialog-description">{{ description }}</DialogDescription>
              <DialogClose v-if="closable" class="app-dialog-close" aria-label="Close">
                <Icon name="close" :size="18" />
              </DialogClose>
            </header>
          </slot>
          <!-- reka needs exactly one DialogTitle inside DialogContent for the
               modal's accessible name (it warns and the dialog is unnamed
               otherwise). When no visible title is given, supply a hidden one
               — mirrors AppSheet's VisuallyHidden fallback. -->
          <DialogTitle v-if="!title" class="app-dialog-sr-only">
            {{ description || 'Dialog' }}
          </DialogTitle>
          <DialogDescription v-if="!description" as="p" class="app-dialog-sr-only">
            {{ title || 'Dialog' }}
          </DialogDescription>

          <!-- Shared dialog scroll body: portaled out (so `.scroll` auto-attach
               would still reach it, but it isn't tagged `.scroll`), so it opts
               into the overlay scrollbar explicitly. Covers every modal built
               on AppDialog. -->
          <div v-overlay-scrollbar class="app-dialog-body">
            <slot />
          </div>

          <footer v-if="$slots.footer" class="app-dialog-footer">
            <slot name="footer" :close="close" />
          </footer>
        </DialogContent>
      </Transition>
    </DialogPortal>
  </DialogRoot>
</template>

<script setup lang="ts">
import {
  DialogRoot, DialogPortal, DialogOverlay, DialogContent,
  DialogTitle, DialogDescription, DialogClose,
} from 'reka-ui'

const props = withDefaults(defineProps<{
  title?: string
  description?: string
  size?: 'sm' | 'md' | 'lg' | 'xl' | 'full'
  modal?: boolean
  closable?: boolean
  contentClass?: string | string[]
  // Prevent reka's default auto-focus on first focusable child — useful
  // for purely-display dialogs (video player) where focus on a close
  // button would be visually distracting.
  preventAutoFocus?: boolean
}>(), {
  size: 'md',
  modal: true,
  closable: true,
})

const open = defineModel<boolean>({ default: false })

function onOpenAutoFocus(e: Event) {
  if (props.preventAutoFocus) e.preventDefault()
}

function close() { open.value = false }
</script>

<!--
  Content is portaled out of this component, so anything that styles the
  portaled element has to live unscoped.
-->
<style>
.app-dialog-overlay {
  position: fixed;
  inset: 0;
  z-index: 5000;
  background: rgb(var(--shade) / 0.62);
  backdrop-filter: blur(6px) saturate(110%);
  -webkit-backdrop-filter: blur(6px) saturate(110%);
}

.app-dialog-content {
  position: fixed;
  z-index: 5001;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  width: calc(100vw - 32px);
  max-height: calc(100vh - 64px);
  display: flex;
  flex-direction: column;
  overflow: hidden;
  /* `.surface` already provides background/blur/border/shadow/animation —
     we only add the sizing here. */
}

.app-dialog-sm   { max-width: 380px; }
.app-dialog-md   { max-width: 560px; }
.app-dialog-lg   { max-width: 760px; }
.app-dialog-xl   { max-width: 1040px; }
.app-dialog-full { width: calc(100vw - 32px); max-width: none; }

/* ── Header ───────────────────────────────────── */
.app-dialog-header {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 18px 20px 14px;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}
.app-dialog-title {
  flex: 1;
  margin: 0;
  font-size: 16px;
  font-weight: 600;
  color: var(--fg-0);
}
.app-dialog-description {
  flex: 1;
  margin: 4px 0 0;
  font-size: 12px;
  color: var(--fg-2);
}
.app-dialog-sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}
.app-dialog-close {
  width: 30px;
  height: 30px;
  border-radius: var(--r-sm);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-2);
  background: transparent;
  border: 0;
  cursor: pointer;
  flex-shrink: 0;
  transition: color 0.12s, background 0.12s;
}
.app-dialog-close:hover {
  color: var(--fg-0);
  background: rgb(var(--ink) / 0.06);
}

/* ── Body ─────────────────────────────────────── */
.app-dialog-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 18px 20px;
}

/* ── Footer ───────────────────────────────────── */
.app-dialog-footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  padding: 14px 20px;
  border-top: 1px solid var(--border);
  background: rgb(var(--ink) / 0.02);
  flex-shrink: 0;
}

/* ── Animations ─────────────────────────────────────────── */
/* Override .surface[data-state="open"] from surface.css. The shared
   keyframe sets `transform: scale(0.94) translateY(-6px)` which clobbers
   the centering `translate(-50%, -50%)` and makes the dialog warp from
   the top-left back to centre. Our own keyframes preserve the centering
   transform throughout. */
.app-dialog-content[data-state="open"] {
  animation: app-dialog-in 0.18s cubic-bezier(0.16, 1, 0.3, 1);
}
.app-dialog-content[data-state="closed"] {
  animation: app-dialog-out 0.12s cubic-bezier(0.4, 0, 1, 1);
}
@keyframes app-dialog-in {
  from { opacity: 0; transform: translate(-50%, -50%) scale(0.96); }
  to   { opacity: 1; transform: translate(-50%, -50%) scale(1); }
}
@keyframes app-dialog-out {
  from { opacity: 1; transform: translate(-50%, -50%) scale(1); }
  to   { opacity: 0; transform: translate(-50%, -50%) scale(0.98); }
}

/* Overlay still fades via Vue <Transition>. */
.app-dialog-enter-active,
.app-dialog-leave-active { transition: opacity 0.18s ease; }
.app-dialog-overlay.app-dialog-enter-from,
.app-dialog-overlay.app-dialog-leave-to { opacity: 0; }
</style>
