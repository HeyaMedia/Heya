<!--
  MobilePlayerHost — the phone-only player mount: MiniPlayer docked above
  BottomNav plus the NowPlayingSheet it opens (the queue lives inside that
  sheet now, as a second scroll-snap pane — there's no separate QueueSheet
  any more). Mounted once per layout (default.vue AND settings.vue — both
  render BottomNav, so both need the bar; /watch and /login use layout:false
  / auth and get neither).

  Renders nothing on desktop/tablet or when no local/remote media is loaded. The
  `.global-miniplayer-dock` element only exists while the bar is visible —
  heya.css keys `.app:has(.global-miniplayer-dock)` off that to pad
  .app-main, so layouts need no has-miniplayer class bookkeeping.
-->
<template>
  <template v-if="isPhone && (currentTrack || videoCastSession)">
    <div class="global-miniplayer-dock">
      <CastVideoMiniPlayer v-if="videoCastSession" @expand="cast.videoRemoteOpen = true" />
      <MiniPlayer v-else :ultrablur="ultrablur" @expand="npOpen = true" />
    </div>
    <CastVideoRemoteSheet v-if="videoCastSession" v-model:open="cast.videoRemoteOpen" />
    <NowPlayingSheet v-else v-model:open="npOpen" :ultrablur="ultrablur" />
  </template>
</template>

<script setup lang="ts">
const { isPhone } = useViewport()
const { currentTrack, muted, volume, toggleMute, setVolume } = usePlayerBindings()
const cast = useCastStore()
const videoCastSession = computed(() => cast.session?.media_kind === 'video' ? cast.session : null)
const ultrablurEnabled = computed(() => isPhone.value && !videoCastSession.value)
const ultrablur = useMusicUltrablur(currentTrack, ultrablurEnabled)

const npOpen = ref(false)

// Phones have their own hardware volume buttons / system output level
// already sitting between the engine and the speaker, so there's no phone
// volume UI for LOCAL playback (see NowPlayingSheet). Keep the Web Audio
// engine's own gain pinned at unity (unmuted, 100) here so nothing upstream
// can silently leave it attenuated with no on-screen control to fix it.
//
// While an output is engaged (casting / another device) the player's volume
// IS the remote device's stream volume, and NowPlayingSheet surfaces a
// slider for it — so bail out then, or this would slam the device back to
// 100 on every drag and there'd be no way to turn the room down.
watchEffect(() => {
  if (!isPhone.value || cast.engaged) return
  if (muted.value) toggleMute()
  if (volume.value !== 100) setVolume(100)
})
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
