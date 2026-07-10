<!--
  AppTooltip — hover tooltip with surface chrome.

  Wraps reka-ui's Tooltip primitives. Uses the .surface class for visual
  identity so it sits in the same design system as menus / dropdowns.

  Usage:
    <AppTooltip label="Equalizer">
      <button class="btn-icon"><Icon name="eq" :size="16" /></button>
    </AppTooltip>

  Default slot is the trigger (anything that can receive focus / hover);
  `label` is the tooltip text. For richer content (kbd shortcuts,
  multi-line), use the `#content` slot instead.

  Wire <TooltipProvider :delay-duration="…"> at the app root (default
  layout) to control delay globally — without one, reka defaults to 700ms.
-->
<template>
  <TooltipRoot :delay-duration="delay" :disable-hoverable-content="true">
    <TooltipTrigger as-child>
      <slot />
    </TooltipTrigger>
    <TooltipPortal>
      <TooltipContent
        class="surface app-tooltip"
        :side="side"
        :side-offset="sideOffset"
        :align="align"
      >
        <slot name="content">{{ label }}</slot>
        <TooltipArrow v-if="arrow" class="app-tooltip-arrow" :width="10" :height="5" />
      </TooltipContent>
    </TooltipPortal>
  </TooltipRoot>
</template>

<script setup lang="ts">
import {
  TooltipRoot, TooltipTrigger, TooltipPortal,
  TooltipContent, TooltipArrow,
} from 'reka-ui'

withDefaults(defineProps<{
  label?: string
  side?: 'top' | 'right' | 'bottom' | 'left'
  sideOffset?: number
  align?: 'start' | 'center' | 'end'
  delay?: number
  arrow?: boolean
}>(), {
  side: 'top',
  sideOffset: 6,
  align: 'center',
  delay: 400,
  arrow: false,
})
</script>

<!--
  Content is portaled, so it has to live outside `scoped` to reach the
  rendered element.
-->
<style>
.app-tooltip {
  /* Tighter padding than full menus — tooltips are short labels. */
  padding: 5px 10px;
  font-size: 11px;
  font-weight: 500;
  font-family: var(--font-mono);
  letter-spacing: 0.02em;
  color: var(--fg-0);
  text-transform: none;
  /* Tooltips render fast; skip the larger surface shadow stack and use a
     lighter one so they don't feel like a popover. */
  box-shadow: 0 6px 18px rgb(var(--shade) / 0.45);
  border-radius: var(--r-sm);
  max-width: 240px;
  z-index: 300;
}

.app-tooltip-arrow {
  fill: color-mix(in oklab, var(--bg-2) 92%, transparent);
}
</style>
