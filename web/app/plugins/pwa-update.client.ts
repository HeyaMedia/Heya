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
//   • startup is gated on a no-cache server/client version comparison; a
//     mismatch downloads and activates the complete new worker before Nuxt
//     mounts, while the SPA loading screen reports the real update stage;
//   • we also re-check on every return to the foreground (covers reopening the
//     installed PWA from the background, where no fresh navigation fires);
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

function setBootStatus(label: string) {
  const status = document.getElementById('heya-boot-status')
  if (status) status.textContent = label
}

function waitForRegistration(pwa: PwaController, timeoutMs = 5000): Promise<ServiceWorkerRegistration | null> {
  const existing = pwa.getSWRegistration()
  if (existing) return Promise.resolve(existing)
  return new Promise((resolve) => {
    const started = performance.now()
    const timer = window.setInterval(() => {
      const registration = pwa.getSWRegistration()
      if (registration || performance.now() - started >= timeoutMs) {
        clearInterval(timer)
        resolve(registration ?? null)
      }
    }, 40)
  })
}

function waitForInstall(worker: ServiceWorker, timeoutMs = 60_000): Promise<void> {
  if (worker.state === 'installed' || worker.state === 'activated' || worker.state === 'redundant') return Promise.resolve()
  return new Promise((resolve) => {
    const timeout = window.setTimeout(done, timeoutMs)
    function done() {
      clearTimeout(timeout)
      worker.removeEventListener('statechange', changed)
      resolve()
    }
    function changed() {
      if (worker.state === 'installed' || worker.state === 'activated' || worker.state === 'redundant') done()
    }
    worker.addEventListener('statechange', changed)
  })
}

async function activateWaitingWorker(pwa: PwaController) {
  setBootStatus('Activating update…')
  const controlled = new Promise<void>((resolve) => {
    navigator.serviceWorker.addEventListener('controllerchange', () => resolve(), { once: true })
  })
  await pwa.updateServiceWorker(true)
  await Promise.race([controlled, new Promise<void>(resolve => window.setTimeout(resolve, 5000))])
  setBootStatus('Update ready · restarting…')
  window.location.reload()
  await new Promise<never>(() => {})
}

async function serverVersion(): Promise<string | null> {
  try {
    const response = await fetch(`/api/health?client-check=${Date.now()}`, {
      cache: 'no-store',
      headers: withClientSurfaceHeaders('/api/health', { 'cache-control': 'no-cache' }),
    })
    if (!response.ok) return null
    return ((await response.json()) as { version?: string }).version ?? null
  } catch {
    return null
  }
}

async function gateStartupUpdate(pwa: PwaController, clientVersion: string) {
  if (!('serviceWorker' in navigator) || !navigator.onLine) {
    setBootStatus(navigator.onLine ? 'Starting Heya…' : 'Offline · starting saved client…')
    return
  }
  setBootStatus('Checking for updates…')
  const currentServerVersion = await serverVersion()
  const registration = await waitForRegistration(pwa)
  if (!registration) {
    setBootStatus('Starting Heya…')
    return
  }

  try {
    if (registration.waiting) await activateWaitingWorker(pwa)
    // A matching release identity is stronger than a periodic SW byte check:
    // the Go binary and embedded Nuxt client were built from the same tag.
    if (currentServerVersion && currentServerVersion === clientVersion) {
      setBootStatus('Starting Heya…')
      return
    }

    // Workbox may have noticed the changed sw.js during registration before
    // our explicit update() call. Join that in-flight atomic install instead
    // of starting a competing check or letting the previous client mount.
    if (registration.installing) {
      setBootStatus('Downloading update…')
      await waitForInstall(registration.installing)
      if (registration.waiting) await activateWaitingWorker(pwa)
    }

    const updateFound = new Promise<ServiceWorker | null>((resolve) => {
      registration.addEventListener('updatefound', () => resolve(registration.installing), { once: true })
      window.setTimeout(() => resolve(null), 1500)
    })
    await registration.update()
    const installing = registration.installing ?? await updateFound
    if (installing) {
      // Browsers do not expose Workbox's byte count. The worker reaches
      // "installed" only after every precached asset has downloaded, so this
      // indeterminate stage is still a real, blocking download indicator.
      setBootStatus('Downloading update…')
      await waitForInstall(installing)
    }
    if (registration.waiting) await activateWaitingWorker(pwa)
    setBootStatus('Starting Heya…')
  } catch {
    // A flaky/offline connection must not lock the user out of an already
    // installed client. The active worker remains a complete atomic version.
    setBootStatus('Network unavailable · starting saved client…')
  }
}

export default defineNuxtPlugin({
  name: 'heya:pwa-update',
  dependsOn: ['vite-pwa:nuxt:client:plugin'],
  async setup(nuxtApp) {
    const injected = nuxtApp.$pwa as unknown as PwaController | undefined
    // Absent in dev (no SW) or if registration failed — nothing to drive then.
    if (!injected) return
    // Re-bind so the non-undefined narrowing carries into the closures below.
    const pwa = injected

    // Nuxt keeps spa-loading-template mounted until async plugins finish.
    // Gate mounting on the SW update/install so a server release is applied
    // atomically before the user sees or interacts with the previous client.
    await gateStartupUpdate(pwa, nuxtApp.$config.public.heyaVersion)

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

  },
})
