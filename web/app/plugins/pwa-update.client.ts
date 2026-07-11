// Silent, playback-safe PWA updates.
//
// The service worker precaches the app shell, so a freshly deployed version is
// invisible until the SW is refreshed — and on an installed standalone PWA
// (phone/tablet) a real navigation that would trigger that refresh almost never
// happens (the app resumes an SPA session; client-side routing never re-checks
// the SW). Left alone, phones/tablets sit on a stale build for days. This
// plugin closes the gap:
//   • pwa.client.periodicSyncForUpdates (nuxt.config) polls for a new SW hourly
//     while the app is open;
//   • we also re-check on every return to the foreground (covers reopening the
//     installed PWA from the background, where no fresh navigation fires) and
//     once shortly after boot;
//   • when a new version is found we apply it SILENTLY — no prompt — but only
//     while nothing is playing, so a running song or video is never cut off.
//     A pending update is held and applied the moment playback stops (or on the
//     next foreground while idle).
//
// Requires registerType: 'prompt' (see nuxt.config): the module then exposes
// `needRefresh` and leaves the reload to us, instead of 'autoUpdate' reloading
// the instant an update lands. `dependsOn` guarantees the module's client
// plugin has provided `$pwa` before this runs.

// Minimal slice of @vite-pwa/nuxt's injected `$pwa` reactive helper (its type
// augmentation isn't reliably in scope during type-check, so pin the runtime
// shape we actually use — verified against the module's pwa.client plugin). The
// object is a Vue reactive() proxy at runtime, so reads inside a watch getter
// are tracked even though this interface is a plain type.
interface PwaController {
  needRefresh: boolean
  updateServiceWorker: (reloadPage?: boolean) => Promise<void>
  getSWRegistration: () => ServiceWorkerRegistration | undefined
}

export default defineNuxtPlugin({
  name: 'heya:pwa-update',
  dependsOn: ['vite-pwa:nuxt:client:plugin'],
  setup(nuxtApp) {
    const injected = nuxtApp.$pwa as unknown as PwaController | undefined
    // Absent in dev (no SW) or if registration failed — nothing to drive then.
    if (!injected) return
    // Re-bind so the non-undefined narrowing carries into the closures below.
    const pwa = injected

    const { playing } = usePlayerBindings()

    // "Is real content playing?" Music decks are detached `new Audio()` elements
    // (never in the DOM), so a `<video>` scan can't see them — read the player
    // state for audio. For video, the muted autoplay hero background must NOT
    // count; genuine content playback is either audible or fullscreen.
    function contentVideoPlaying(): boolean {
      const fs = document.fullscreenElement
      return Array.from(document.querySelectorAll('video')).some((v) => {
        if (v.paused || v.ended || v.readyState < 2) return false
        return !v.muted || v === fs || !!fs?.contains(v)
      })
    }
    const isBusy = () => playing.value || contentVideoPlaying()

    let pending = false
    // While an update is held during playback we poll for the idle transition:
    // music exposes a reactive `playing` (watched below) but VIDEO has no global
    // signal (useVideoPlayer state is per-component) and closing the player by
    // unmounting its <video> doesn't reliably fire pause/ended — so a watch
    // alone would leave the update stuck after a video stops. The interval only
    // exists while pending+busy and clears itself the moment it applies.
    let idlePoll: ReturnType<typeof setInterval> | null = null
    function stopIdlePoll() {
      if (idlePoll) { clearInterval(idlePoll); idlePoll = null }
    }
    function applyIfIdle() {
      if (!pending) { stopIdlePoll(); return }
      if (isBusy()) {
        if (!idlePoll) idlePoll = setInterval(applyIfIdle, 5000)
        return
      }
      pending = false
      stopIdlePoll()
      // reloadPage=true → skip-waiting on the waiting SW, then reload once it
      // takes control. This is the only place the app reloads for an update.
      void pwa.updateServiceWorker(true)
    }

    // A new version has installed and is waiting (prompt mode flips this without
    // reloading). immediate:true also catches a worker left waiting from a prior
    // session. Apply now if idle, otherwise hold it.
    watch(() => pwa.needRefresh, (need) => {
      if (!need) return
      pending = true
      applyIfIdle()
    }, { immediate: true })

    // The moment music stops, flush any held update.
    watch(playing, (isPlaying) => { if (!isPlaying) applyIfIdle() })

    function checkForUpdate() {
      pwa.getSWRegistration()?.update().catch(() => { /* offline / transient */ })
    }

    // Reopening the installed PWA from the background fires visibilitychange but
    // usually not a fresh navigation — so re-check here, and flush a pending
    // update if we're now idle.
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState !== 'visible') return
      checkForUpdate()
      applyIfIdle()
    })

    // Belt-and-suspenders over the browser's own launch-time SW check (notably
    // unreliable for iOS standalone PWAs).
    nuxtApp.hook('app:mounted', () => { window.setTimeout(checkForUpdate, 2500) })
  },
})
