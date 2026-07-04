<!--
  AppSheet — bottom sheet for phone/tablet, built on reka-ui's Drawer family.

  Reka component names used (reka-ui 2.10.1, verified against
  node_modules/reka-ui/src/Drawer): DrawerRoot, DrawerPortal, DrawerOverlay,
  DrawerContent, DrawerHandle, DrawerTitle. `DrawerRoot`'s default
  `swipeDirection` is 'down' and its swipe-dismiss gesture is wired into
  `DrawerContent` itself (attaches to the content element, scroll-edge aware)
  — no extra wiring needed here for "swipe down to dismiss".

  Usage:
    <AppSheet v-model:open="show" title="Queue" size="full">
      …rows…
    </AppSheet>

  Slots:
    default  — body, wrapped in an internal `.scroll` overflow region
    header   — replaces the default title row entirely (include your own
               `<DrawerTitle>` if you do — reka warns without one)

  Visuals reuse the shared `.surface` glass chrome (background/blur/border)
  but override radius (top corners only), position (fixed, docked to the
  viewport bottom), and animation (slide, not the popover scale-in) — see the
  unscoped <style> block below. Content is portaled to `body`, same as
  AppDialog/AppMenu/AppContextMenu, so ancestor `backdrop-filter` never
  poisons this panel (docs/ui.md gotcha #4) and any styling here must stay
  unscoped (gotcha #2).
-->
<template>
  <DrawerRoot v-model:open="open">
    <DrawerPortal>
      <DrawerOverlay class="app-sheet-overlay" />
      <!-- open-auto-focus is prevented unconditionally: sheets are touch-first
           surfaces, and reka's default focus-on-first-focusable paints a
           browser focus ring on whatever link/button happens to be first
           (and would summon the soft keyboard if it were an input). -->
      <DrawerContent
        class="surface app-sheet-content"
        :class="`app-sheet-${size}`"
        @open-auto-focus.prevent
      >
        <DrawerHandle v-if="handle" class="app-sheet-handle" />

        <slot name="header">
          <header v-if="title" class="app-sheet-header">
            <DrawerTitle as="h3" class="app-sheet-title">{{ title }}</DrawerTitle>
          </header>
          <VisuallyHidden v-else>
            <DrawerTitle>Sheet</DrawerTitle>
          </VisuallyHidden>
        </slot>

        <div class="app-sheet-body scroll">
          <slot />
        </div>
      </DrawerContent>
    </DrawerPortal>
  </DrawerRoot>
</template>

<script setup lang="ts">
import { DrawerRoot, DrawerPortal, DrawerOverlay, DrawerContent, DrawerHandle, DrawerTitle, VisuallyHidden } from 'reka-ui'

withDefaults(defineProps<{
  title?: string
  /** 'auto' (default) = content height, capped at 92dvh. 'full' = 92dvh. */
  size?: 'auto' | 'full'
  handle?: boolean
}>(), {
  size: 'auto',
  handle: true,
})

const open = defineModel<boolean>('open')
</script>

<!--
  Content is portaled out of this component (to <body>), so its styling has
  to be unscoped — the same constraint AppDialog/AppMenu/AppContextMenu live
  with.
-->
<style>
.app-sheet-overlay {
  position: fixed;
  inset: 0;
  z-index: 399;
  background: rgba(0, 0, 0, 0.62);
  backdrop-filter: blur(6px) saturate(110%);
  -webkit-backdrop-filter: blur(6px) saturate(110%);
}
.app-sheet-overlay[data-state="open"] {
  animation: app-sheet-overlay-in 0.18s ease both;
}
.app-sheet-overlay[data-state="closed"] {
  animation: app-sheet-overlay-out 0.18s ease both;
}
@keyframes app-sheet-overlay-in {
  from { opacity: 0; }
  to   { opacity: 1; }
}
@keyframes app-sheet-overlay-out {
  from { opacity: 1; }
  to   { opacity: 0; }
}

/* `.surface` supplies background/blur/border/shadow — everything below is
   sizing, position, and the slide animation that replaces its scale-in. */
.app-sheet-content {
  position: fixed;
  left: 0;
  right: 0;
  bottom: 0;
  width: 100vw;
  max-width: 100vw;
  z-index: 400;
  display: flex;
  flex-direction: column;
  border-radius: var(--r-lg) var(--r-lg) 0 0;
  padding-bottom: var(--safe-bottom);
  transform: translateY(var(--drawer-swipe-movement-y, 0px));
}
.app-sheet-auto { max-height: 92dvh; }
.app-sheet-full { height: 92dvh; }

.app-sheet-content[data-state="open"] {
  animation: app-sheet-in 0.22s cubic-bezier(0.16, 1, 0.3, 1) both;
}
.app-sheet-content[data-state="closed"] {
  animation: app-sheet-out 0.16s cubic-bezier(0.4, 0, 1, 1) both;
}
@keyframes app-sheet-in {
  from { transform: translateY(100%); }
  to   { transform: translateY(var(--drawer-swipe-movement-y, 0px)); }
}
@keyframes app-sheet-out {
  from { transform: translateY(var(--drawer-swipe-movement-y, 0px)); }
  to   { transform: translateY(100%); }
}

/* ── Drag handle ──────────────────────────────────── */
.app-sheet-handle {
  width: 36px;
  height: 4px;
  flex-shrink: 0;
  margin: 10px auto 2px;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.22);
}

/* ── Header ───────────────────────────────────────── */
.app-sheet-header {
  flex-shrink: 0;
  padding: 6px 20px 14px;
}
.app-sheet-title {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
  color: var(--fg-0);
}

/* ── Body ─────────────────────────────────────────── */
.app-sheet-body {
  flex: 1;
  min-height: 0;
  padding: 0 20px 20px;
}
</style>
