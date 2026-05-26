<template>
  <!--
    TooltipProvider gives every AppTooltip below a shared hover-delay so
    they all "feel" the same. 400ms ≈ snappy without firing on every
    cursor-fly-through. delay-duration is the per-instance default that
    individual AppTooltips can still override via :delay.
  -->
  <TooltipProvider :delay-duration="400" :skip-delay-duration="200">
    <div class="app">
      <AppTopBar />
      <div class="app-main">
        <slot />
      </div>
      <ConfirmDialog />
    </div>
  </TooltipProvider>
</template>

<script setup lang="ts">
import { TooltipProvider } from 'reka-ui'
</script>
