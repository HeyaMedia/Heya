<!--
  DesktopPlayerHost — the desktop/tablet counterpart to MobilePlayerHost.

  /music keeps its traditional idle playbar, while every other app page gets
  the same bar as soon as a track is loaded. The host also owns the overlays
  launched by Playbar so those controls keep working after route navigation.
-->
<template>
  <div v-if="!isPhone && (currentTrack || isMusic)" class="global-playbar-host">
    <!-- /music owns a docked QueuePanel inside its three-column body. On all
         other routes this fixed twin + the .app-main padding-right rule in
         heya.css reproduce the same docked reading: content reflows beside
         the panel on desktop, while the compact band (≤1200px) keeps
         QueuePanel's own floating-overlay presentation. -->
    <QueuePanel v-if="!isMusic" class="global-queue-panel" />
    <Playbar />
    <EQPanel :open="eqOpen" @close="eqOpen = false" />
    <VisualizerFullscreen />
    <HotkeyHelp />
  </div>
</template>

<script setup lang="ts">
const route = useRoute()
const { isPhone } = useViewport()
const { currentTrack } = usePlayerBindings()

const isMusic = computed(() => route.path === '/music' || route.path.startsWith('/music/'))
const eqOpen = useState('music_eq_open', () => false)
</script>

<style scoped>
.global-playbar-host {
  min-height: 0;
  position: relative;
  z-index: 40;
}

/* Away from /music the queue's native in-flow form has no column to sit in,
   so pin it to the right edge between topbar and playbar. On desktop the
   .app-main padding-right rule (heya.css) reflows page content beside it —
   no border/shadow, so it reads as the same frameless dock as /music (any
   divider re-splits the frame; glass-vs-content contrast defines the edge).
   In the compact band QueuePanel's own media block layers overlay chrome
   (border + shadow) on top of this same geometry. */
.global-queue-panel {
  position: fixed;
  top: var(--topbar-h);
  right: 0;
  bottom: var(--playbar-h);
  height: auto;
  z-index: 60;
}
</style>
