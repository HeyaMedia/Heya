// Server play-queue live mirror (docs/queue-plan.md Phase B). The server
// owns the queue and emits per-user `queue.changed` events on every
// mutation; this plugin folds them into the useQueue window and keeps the
// player store in step so every open client renders the same queue —
// add a track on the phone, watch it appear on the desktop.
//
// It also handles the single-active-output dance: when another tab (or
// the CLI, or a cast binding) claims playback, this tab's engine pauses
// and mirrors the renderer's heartbeats; pressing play here claims back.
import type { QueueChangedEvent } from '~/composables/useQueue'

export default defineNuxtPlugin((nuxtApp) => {
  const { on, connected } = useEventBus()
  const { token } = useAuth()
  const qs = useQueueStore()
  const player = usePlayerStore()

  // Deferred to app:mounted — plugins load alphabetically and a $heya
  // call during setup would fire before heyaApi registers the bearer hook
  // (the cast-live lesson: unauthenticated 401 → forced logout).
  nuxtApp.hook('app:mounted', () => {
    watch(token, (t) => {
      if (!t) return
      void restoreQueue()
    }, { immediate: true })

    // WS reconnect: events were lost — re-pull the snapshot.
    watch(connected, (up, wasUp) => {
      if (up && wasUp === false && token.value) void qs.refetch().catch(() => {})
    })
  })

  // Boot restore: the "open the phone 45 minutes later" moment. Rehydrate
  // the queue window and put the current track on the playbar (paused —
  // autoplay policy forbids sound anyway; mirroring covers the case where
  // another output is actively rendering).
  async function restoreQueue() {
    try {
      await qs.refetch()
    } catch {
      return
    }
    if (player.currentTrack || player.localMode) return
    const idx = qs.currentWindowIndex
    if (idx < 0) return
    const track = player.queue[idx]
    if (!track) return
    player.currentTrack = track
    player.position = qs.positionSeconds
    if (track.duration) player.duration = track.duration
  }

  on('queue.changed', (ev) => {
    const p = ev.payload as QueueChangedEvent
    const outcome = qs.applyEvent(p)
    if (outcome === 'refetch') {
      void qs.refetch().then(() => syncPointerMirror()).catch(() => {})
      return
    }
    syncPointerMirror()
  })

  // Keep the player's current track following the server pointer when
  // something OTHER than this tab moved it (another tab's next button,
  // the CLI, a future cast binding). The active renderer follows by
  // actually playing; a mirror tab follows by display only.
  function syncPointerMirror() {
    if (player.localMode) return
    const idx = qs.currentWindowIndex
    if (idx < 0) return
    const track = player.queue[idx]
    if (!track || player.currentTrack?.id === track.id) {
      // Same track — still mirror transport when we're not rendering.
      if (!qs.isActiveOutput) {
        player.position = qs.positionSeconds
        player.playing = qs.playing
      }
      return
    }
    if (qs.isActiveOutput && player.playing) {
      // We render, someone else drove the pointer — remote control: switch.
      void player.play(track, { skipQueueSync: true })
    } else {
      player.currentTrack = track
      player.position = qs.positionSeconds
      if (track.duration) player.duration = track.duration
      player.playing = !qs.isActiveOutput && qs.playing
    }
  }

  // Output takeover: another renderer claimed while this tab was playing —
  // stop the local engine (the claim event's playing/position mirror in).
  watch(() => qs.activeOutput, (out, prev) => {
    if (player.localMode) return
    if (out && out !== qs.outputID && prev !== out && player.playing) {
      player.pause()
    }
  })

  // Renderer heartbeat: coarse position every 15s while this tab is the
  // active output and actually playing, plus one on pause edges so the
  // server's "where is it" answer stays honest.
  let beat: ReturnType<typeof setInterval> | null = null
  watch(() => player.playing && !player.localMode && qs.isActiveOutput && !!player.currentTrack, (active) => {
    if (active && beat === null) {
      qs.heartbeat(player.position, true)
      beat = setInterval(() => qs.heartbeat(player.position, player.playing), 15_000)
    } else if (!active && beat !== null) {
      clearInterval(beat)
      beat = null
      if (!player.localMode && qs.isActiveOutput) qs.heartbeat(player.position, false)
    }
  })
})
