<!--
  MobilePlayerHost — the phone-only player mount: MiniPlayer docked above
  BottomNav plus the NowPlayingSheet/QueueSheet it opens. Mounted once per
  layout (default.vue AND settings.vue — both render BottomNav, so both need
  the bar; /watch and /login use layout:false / auth and get neither).

  Renders nothing on desktop/tablet or when no track is loaded. The
  `.global-miniplayer-dock` element only exists while the bar is visible —
  heya.css keys `.app:has(.global-miniplayer-dock)` off that to pad
  .app-main, so layouts need no has-miniplayer class bookkeeping.
-->
<template>
  <template v-if="isPhone && currentTrack">
    <div class="global-miniplayer-dock">
      <MiniPlayer @expand="npOpen = true" />
    </div>
    <NowPlayingSheet v-model:open="npOpen" @open-queue="queueSheetOpen = true" />
    <QueueSheet v-model:open="queueSheetOpen" />
  </template>
</template>

<script setup lang="ts">
const { isPhone } = useViewport()
const { currentTrack } = usePlayer()

const npOpen = ref(false)
const queueSheetOpen = ref(false)
</script>

<style scoped>
/* MiniPlayer renders a plain, position-agnostic 64px row — this wrapper
   owns the fixed docking directly above BottomNav. z-index just below
   BottomNav's 45; the two are adjacent, never overlapping. */
.global-miniplayer-dock {
  position: fixed;
  left: 0;
  right: 0;
  bottom: calc(var(--bottomnav-h) + var(--safe-bottom));
  z-index: 44;
}
</style>
