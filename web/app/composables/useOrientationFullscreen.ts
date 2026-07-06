// Rotate-to-fullscreen for the video player on touch devices.
//
// The player overlay is `position: fixed; inset: 0`, so it already reflows to
// whichever way the device is held. This adds the immersive half: rotating to
// LANDSCAPE requests browser fullscreen (hiding the address bar / status chrome)
// and rotating back to PORTRAIT drops it again — the YouTube-style "turn the
// phone sideways and it fills the screen" behaviour.
//
// Platform reality, handled by degrading gracefully rather than pretending:
//   • Android Chrome / installed PWA: works. (In a standalone PWA there's no
//     browser chrome to hide, but requestFullscreen is still a harmless no-op
//     escalation to the OS status bar.)
//   • Browser fullscreen needs transient user activation; some browsers reject
//     a request driven purely by rotation. We try, and swallow the rejection —
//     the player still reflows to landscape, just without hiding chrome.
//   • iOS Safari has no element requestFullscreen (only native <video>
//     fullscreen), so `requestFullscreen` is undefined there → no-op; iOS keeps
//     its own native rotation handling.
//
// Only fullscreen WE auto-entered is auto-exited on portrait, so a user who
// tapped the fullscreen button and then rotated keeps control.
export function useOrientationFullscreen() {
  if (import.meta.server) return
  const { isCoarse } = useViewport()

  let autoEntered = false
  let mq: MediaQueryList | null = null

  // Single-flight guard + trailing re-run flag. requestFullscreen /
  // exitFullscreen are async, and the device can flip again mid-await — without
  // serialization a pending enter could resolve *after* we're back in portrait
  // and strand the player in fullscreen. reconcile() therefore drives actual →
  // desired in a loop, re-reading the live orientation after every await, and
  // any orientation event that arrives while it's busy just flags a re-run.
  let busy = false
  let rerun = false
  // Set on teardown. A requestFullscreen() can still be in flight when the
  // player unmounts (navigate away mid-rotation); it resolves *after* the
  // listeners are gone and would otherwise leave the app fullscreen with no
  // player behind it. reconcile() re-checks this right after the await and
  // unwinds a fullscreen that outlived us.
  let disposed = false

  const inFullscreen = () => !!document.fullscreenElement

  async function reconcile() {
    if (busy) { rerun = true; return }
    busy = true
    try {
      do {
        rerun = false
        if (disposed || !isCoarse.value || !mq) break
        const wantFs = mq.matches
        if (wantFs && !inFullscreen()) {
          // Landscape → immersive fullscreen (best-effort). Break (don't spin)
          // when the API is absent (iOS) or the request is rejected (some
          // browsers gate it behind a fresh user gesture) — the overlay still
          // reflows to landscape either way.
          if (!document.documentElement.requestFullscreen) break
          try {
            await document.documentElement.requestFullscreen()
            autoEntered = true
          } catch { autoEntered = false; break }
          // Torn down while the request was in flight → the player is gone;
          // don't let the fullscreen it just granted outlive it.
          if (disposed) {
            try { await document.exitFullscreen() } catch { /* already gone */ }
            autoEntered = false
            break
          }
        } else if (!wantFs && inFullscreen()) {
          // Portrait → drop only the fullscreen we entered ourselves; leave a
          // user-initiated fullscreen alone.
          if (!autoEntered) break
          try { await document.exitFullscreen() } catch { /* already gone */ }
          autoEntered = false
        }
        // else already consistent — loop again only if a flip queued mid-await.
      } while (rerun)
    } finally {
      busy = false
    }
  }

  // If the user exits fullscreen by any other means (the in-player button, the
  // system gesture), forget that we owned it so we don't fight them on rotate.
  function onFsChange() { if (!inFullscreen()) autoEntered = false }

  onMounted(() => {
    mq = window.matchMedia('(orientation: landscape)')
    // addEventListener('change') is the reliable cross-browser orientation
    // signal (fires on the flip; supported on modern iOS + Android).
    mq.addEventListener('change', reconcile)
    document.addEventListener('fullscreenchange', onFsChange)
  })

  onUnmounted(() => {
    disposed = true
    mq?.removeEventListener('change', reconcile)
    document.removeEventListener('fullscreenchange', onFsChange)
    // Leaving the player: never strand the browser in the fullscreen we forced.
    // (An in-flight request is handled by reconcile()'s post-await disposed check.)
    if (autoEntered && inFullscreen()) document.exitFullscreen().catch(() => {})
  })
}
