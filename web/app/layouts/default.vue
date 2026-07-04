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
      <BottomNav />
      <ConfirmDialog />

      <!--
        Phone player mount (MiniPlayer + sheets) — shared MobilePlayerHost,
        because usePlayer() is a global singleton: music keeps playing
        app-wide, so the bar must be visible app-wide too. settings.vue
        mounts the same host; pages/music.vue renders inside this layout's
        <slot>, so /music is covered by this one mount (docs/responsive-plan
        W1c). Content padding comes from heya.css keying on
        .app:has(.global-miniplayer-dock) — no class bookkeeping here.
      -->
      <MobilePlayerHost />
    </div>
  </TooltipProvider>
</template>

<script setup lang="ts">
import { TooltipProvider } from 'reka-ui'
</script>
