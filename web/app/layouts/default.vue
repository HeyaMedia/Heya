<template>
  <!--
    TooltipProvider gives every AppTooltip below a shared hover-delay so
    they all "feel" the same. 400ms ≈ snappy without firing on every
    cursor-fly-through. delay-duration is the per-instance default that
    individual AppTooltips can still override via :delay.
  -->
  <TooltipProvider :delay-duration="400" :skip-delay-duration="200">
    <div class="app" :class="{ 'has-miniplayer': isPhone && currentTrack }">
      <AppTopBar />
      <div class="app-main">
        <slot />
      </div>
      <BottomNav />
      <ConfirmDialog />

      <!--
        Global mobile player mount (phone only). Mounted here — not in
        pages/music.vue — because usePlayer() is a global singleton: music
        keeps playing app-wide today, so the mini player needs to be visible
        app-wide too. pages/music.vue renders inside this layout's <slot>,
        so this single mount already covers /music as well; the music shell
        no longer renders its own mobile player (see docs/responsive-plan.md
        W1c).
      -->
      <template v-if="isPhone">
        <div class="global-miniplayer-dock">
          <MiniPlayer @expand="npOpen = true" />
        </div>
        <NowPlayingSheet v-model:open="npOpen" @open-queue="queueSheetOpen = true" />
        <QueueSheet v-model:open="queueSheetOpen" />
      </template>
    </div>
  </TooltipProvider>
</template>

<script setup lang="ts">
import { TooltipProvider } from 'reka-ui'

const { isPhone } = useViewport()
const { currentTrack } = usePlayer()

const npOpen = ref(false)
const queueSheetOpen = ref(false)
</script>

<style scoped>
/* MiniPlayer itself renders a plain, position-agnostic 64px row (see its
   own header comment) — this wrapper owns the fixed docking directly above
   BottomNav. z-index sits just below BottomNav's 45 since the two are
   visually adjacent, never overlapping. */
.global-miniplayer-dock {
  position: fixed;
  left: 0;
  right: 0;
  bottom: calc(var(--bottomnav-h) + var(--safe-bottom));
  z-index: 44;
}
</style>
