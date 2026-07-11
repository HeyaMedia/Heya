<template>
  <!--
    TooltipProvider gives every AppTooltip below a shared hover-delay so
    they all "feel" the same. 400ms ≈ snappy without firing on every
    cursor-fly-through. delay-duration is the per-instance default that
    individual AppTooltips can still override via :delay.
  -->
  <TooltipProvider :delay-duration="400" :skip-delay-duration="200">
    <div class="app" :class="{ 'bg-reveal': bgCtl.reveal }">
      <!-- Ambient rotating library-artwork background. First child +
           z-index:-1 = paints above .app's own background, below all
           in-flow content; no sibling stacking changes needed. The corner
           cluster (reveal/shuffle/pause) steers it; both are excluded from
           the bg-reveal fade in heya.css. -->
      <AmbientBackdrop />
      <AmbientControls />
      <AppTopBar />
      <div class="app-main">
        <slot />
      </div>
      <BottomNav />
      <ConfirmDialog />

      <!--
        Phone player mount (MiniPlayer + sheets) — shared MobilePlayerHost,
        because usePlayerBindings() is a global singleton: music keeps playing
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

// Reveal mode (AmbientControls' eye): flips the .bg-reveal class that fades
// the whole app away so the ambient artwork shows clean.
const bgCtl = useBackgroundControls()
</script>
