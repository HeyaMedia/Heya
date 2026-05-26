<!--
  AppSurface — the floating-panel "box".

  Owns the visual identity (glass, border, shadow, radius, animation)
  via the .surface CSS layer. Renders as a plain <div> by default, but
  the `as` prop lets it morph into any component — used in tandem with
  reka's `as-child` on DropdownMenuContent / DialogContent so the
  surface element is also the positioned popper / dialog content.

  Usage:
    <DropdownMenuContent as-child>
      <AppSurface :width="320">
        ...content...
      </AppSurface>
    </DropdownMenuContent>

  Or as a plain panel anchored manually:
    <AppSurface class="my-panel">...</AppSurface>
-->
<template>
  <component
    :is="as"
    class="surface"
    :style="surfaceStyle"
  >
    <slot />
  </component>
</template>

<script setup lang="ts">
import type { Component } from 'vue'

const props = withDefaults(defineProps<{
  // Element / component to render as. Default <div>; pass a reka primitive
  // component when using as-child wiring.
  as?: string | Component
  // Optional fixed width — number is treated as px.
  width?: number | string
  // Optional max-height override (panels can scroll internally).
  maxHeight?: number | string
}>(), {
  as: 'div',
})

const surfaceStyle = computed(() => {
  const s: Record<string, string> = {}
  if (props.width != null) s.width = typeof props.width === 'number' ? `${props.width}px` : props.width
  if (props.maxHeight != null) s.maxHeight = typeof props.maxHeight === 'number' ? `${props.maxHeight}px` : props.maxHeight
  return s
})
</script>
