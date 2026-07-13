<!--
  DesktopPlayerHost — the desktop/tablet counterpart to MobilePlayerHost.

  /music keeps its traditional idle playbar, while every other app page gets
  the same bar as soon as a track is loaded. The host also owns the overlays
  launched by Playbar so those controls keep working after route navigation.
-->
<template>
  <div v-if="!isPhone && (currentTrack || isMusic)" class="global-playbar-host">
    <!-- /music owns a docked QueuePanel inside its three-column body. On all
         other routes it becomes an overlay so opening Queue/Lyrics doesn't
         reflow an unrelated page. -->
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

/* Away from /music the queue is a floating companion to the persistent
   playbar. Its native wide-screen form is an in-flow flex sibling, so pin it
   to the right edge here just like its existing compact-band presentation. */
.global-queue-panel {
  position: fixed;
  top: var(--topbar-h);
  right: 0;
  bottom: var(--playbar-h);
  height: auto;
  z-index: 60;
  border-left: 1px solid var(--border-strong);
  box-shadow: var(--shadow-3);
}
</style>
