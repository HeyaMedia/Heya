<!--
  AppMenu — trigger-anchored dropdown menu.

  Composes reka-ui's DropdownMenu primitives with AppSurface for visual
  identity. The trigger renders as a real <button> (no slot-forwarded
  as-child), which is what makes the click handler actually attach.

  Slot API:
    #trigger        — content of the trigger button (text/icon/avatar)
    default         — content of the menu; receives `{ close }` slot prop

  Place reka's <DropdownMenuItem> elements in the default slot for items
  that should auto-close on select. Plain buttons work too — call
  `close()` from the slot prop to dismiss the menu programmatically.
-->
<template>
  <DropdownMenuRoot v-model:open="open">
    <DropdownMenuTrigger
      class="app-menu-trigger"
      :class="triggerClass"
      :title="triggerTitle"
      :aria-label="triggerAriaLabel ?? triggerTitle"
      :style="triggerStyle"
    >
      <slot name="trigger" :open="open" />
    </DropdownMenuTrigger>
    <DropdownMenuPortal>
      <DropdownMenuContent
        :side-offset="sideOffset"
        :align="align"
        as-child
      >
        <AppSurface :width="width" :class="contentClass">
          <slot :close="closeMenu" />
        </AppSurface>
      </DropdownMenuContent>
    </DropdownMenuPortal>
  </DropdownMenuRoot>
</template>

<script setup lang="ts">
import { DropdownMenuRoot, DropdownMenuTrigger, DropdownMenuPortal, DropdownMenuContent } from 'reka-ui'

withDefaults(defineProps<{
  sideOffset?: number
  align?: 'start' | 'center' | 'end'
  width?: number | string
  triggerClass?: string | string[] | Record<string, boolean>
  triggerTitle?: string
  /** Explicit accessible name for icon-only triggers; falls back to
   *  triggerTitle (title-as-accname works but is the fragile path). */
  triggerAriaLabel?: string
  /** Inline style for the trigger — artwork-adaptive tints etc. */
  triggerStyle?: Record<string, string>
  contentClass?: string | string[] | Record<string, boolean>
}>(), {
  sideOffset: 8,
  align: 'end',
})

// `defineModel` gives us a real two-way binding that works whether or not the
// caller passes v-model. The earlier hand-rolled computed+emit pattern broke
// when modelValue was undefined: the v-model:open round-trip into
// DropdownMenuRoot didn't re-trigger the computed getter, so reka's
// onOpenToggle would update internal state but the prop bound back into
// DropdownMenuRoot kept reading false. Reka v2 reacts to its `open` prop,
// not just our setter, so we need a ref it can observe.
const open = defineModel<boolean>({ default: false })

function closeMenu() { open.value = false }
</script>

<style>
/* Reset the UA button chrome on the rendered trigger — consumers style
   via `trigger-class`. Kept unscoped because it's a global utility for
   any AppMenu instance. */
.app-menu-trigger {
  background: transparent;
  border: 0;
  padding: 0;
  font: inherit;
  color: inherit;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
}
</style>
