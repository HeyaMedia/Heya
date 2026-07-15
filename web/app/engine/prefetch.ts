// PrefetchManager — warms the in-page Cache API ahead of playback so the
// upcoming N tracks (device setting `prefetchCount`) are already downloaded
// by the time the player needs them, and hands the deck a blob: URL instead
// of a cold network request. Replaces the old dead `PrefetchQueue` (a bounded
// pool of <audio> elements nothing ever called — see
// docs/music-audio-engine-plan.md's "remaining backlog" note).
//
// Module singleton, matching ~/engine/context.ts's style: plain exported
// functions closing over module-scope state, bundled into `prefetchManager`
// for its two callers — usePlayer's transition orchestration, and the device
// settings page's cache-usage panel.
//
// Cache bucket: 'heya-audio-v1'. This is deliberately NOT the PWA service
// worker's cache — the SW's fetch handler never intercepts /api/* (see
// nuxt.config.ts's pwa.workbox comment) and this manager must not change
// that. Every fetch/cache.put/cache.match here runs in-page, from application
// code — the SW is never involved.
//
// engine/ is outside Nuxt's auto-import dirs (composables/ + utils/ only),
// so — unlike a composable — everything this file needs is imported
// explicitly below.
import { useDeviceSettings } from '~/composables/useDeviceSettings'
import { buildStreamUrl, streamCacheKey } from '~/composables/useStreamUrl'
import { alog } from '~/engine/debug'

// Minimal shape this module needs from a track — deliberately NOT importing
// usePlayer's `Track` (that would create a composables <-> engine import
// cycle; usePlayer already imports from here). Any object with an id/
// stream_url structurally satisfies this, so usePlayer's Track flows straight
// through with no adapter.
export interface PrefetchTrack {
  id: number
  stream_url?: string | null
  title?: string
}

export interface PrefetchUsage {
  entries: number
  bytes: number
}

const CACHE_NAME = 'heya-audio-v1'

function cachesAvailable(): boolean {
  return typeof caches !== 'undefined'
}

// Memoized so repeated calls share one Cache handle instead of re-opening it.
// Reset to null on failure/clearAll so a later call retries cleanly.
let cachePromise: Promise<Cache> | null = null
async function openCache(): Promise<Cache | null> {
  if (!cachesAvailable()) return null
  try {
    if (!cachePromise) cachePromise = caches.open(CACHE_NAME)
    return await cachePromise
  } catch {
    // Quota / private-mode / storage disabled — treat as absent.
    cachePromise = null
    return null
  }
}

// Object URLs handed out by resolvePlayable, keyed by the same
// token-stripped cache key as the underlying Cache entry. Tracked so
// eviction/clearAll/replacement can revoke them — an un-revoked blob: URL
// leaks its backing blob for the life of the tab.
const objectUrls = new Map<string, string>()

function revoke(key: string) {
  const url = objectUrls.get(key)
  if (url) {
    URL.revokeObjectURL(url)
    objectUrls.delete(key)
  }
}

// --- Network gate ----------------------------------------------------------
// navigator.connection is Chromium-only (Android/desktop Chrome); everywhere
// else (all of iOS, Firefox) it's undefined and — per useDeviceSettings'
// wifiOnlyPrefetch doc comment — absence means "always allowed".
interface NetworkInformationLike {
  type?: string
  effectiveType?: string
  saveData?: boolean
}
function getConnection(): NetworkInformationLike | undefined {
  const nav = navigator as Navigator & {
    connection?: NetworkInformationLike
    mozConnection?: NetworkInformationLike
    webkitConnection?: NetworkInformationLike
  }
  return nav.connection ?? nav.mozConnection ?? nav.webkitConnection
}
function isMeteredConnection(): boolean {
  const conn = getConnection()
  if (!conn) return false
  if (conn.saveData) return true
  if (conn.type) return conn.type === 'cellular'
  // No `type` (common on desktop Chrome) — fall back to effectiveType as a
  // loose proxy: anything worse than 4g skips speculative prefetch. Imperfect
  // (slow wifi reads the same as cellular) but this gate is opt-in and
  // documented as best-effort.
  if (conn.effectiveType) return conn.effectiveType !== '4g'
  return false
}
function prefetchAllowedByNetwork(): boolean {
  const { settings } = useDeviceSettings()
  if (!settings.value.wifiOnlyPrefetch) return true
  return !isMeteredConnection()
}

// --- sync() ------------------------------------------------------------
// Serializes sync() calls so overlapping triggers (armed-next-track call +
// the debounced queue-change watcher) never run their fetch loops
// concurrently — fetches within (and across) sync() calls always happen one
// at a time.
//
// `current` is the actively-playing track, passed through so trim() can
// retain it even though it's never part of `upcoming` (upcomingTracks
// excludes the current track by definition). It's an explicit parameter
// rather than something resolvePlayable infers as a side effect, because
// resolvePlayable is ALSO called for the pending/next track (armSync) — if
// resolving the pending track silently overwrote "what's current", the
// actually-playing track could fall out of the retention set and have its
// still-in-use blob: URL revoked out from under the deck.
let syncSeq = 0
let syncChain: Promise<void> = Promise.resolve()

function sync(upcoming: PrefetchTrack[], current?: PrefetchTrack | null): Promise<void> {
  const mySeq = ++syncSeq
  syncChain = syncChain.then(() => runSync(upcoming, current ?? null, mySeq)).catch(() => {})
  return syncChain
}

async function runSync(upcoming: PrefetchTrack[], current: PrefetchTrack | null, mySeq: number): Promise<void> {
  try {
    const { settings } = useDeviceSettings()
    const n = settings.value.prefetchCount
    if (n <= 0) {
      // Disabled — make sure nothing lingers from a previously higher setting.
      await clearAll()
      return
    }
    if (!cachesAvailable()) return // Older WebView — pure pass-through, nothing to warm.
    const cache = await openCache()
    if (!cache) return

    const retainWindow = upcoming.slice(0, n)
    for (const track of retainWindow) {
      if (mySeq !== syncSeq) return // Superseded by a newer sync — stop issuing fetches.
      const url = buildStreamUrl(track)
      if (!url) continue
      const key = streamCacheKey(url)
      const hit = await cache.match(key)
      if (hit) continue // Already warm.
      if (!prefetchAllowedByNetwork()) continue // wifi-only gate — retried on the next sync().

      try {
        const res = await fetch(url, { headers: withClientSurfaceHeaders(url) })
        if (mySeq !== syncSeq) return
        if (res.ok) {
          await cache.put(key, res)
          alog('prefetch', `warmed "${track.title ?? track.id}"`)
        }
        // Non-2xx (a cold NAS/transcode path can 404/500) — skip silently,
        // the next sync() call retries.
      } catch (err) {
        // Network error / stream unreachable — never throw into the player.
        alog('prefetch', `fetch failed for "${track.title ?? track.id}" — will retry later`, err)
      }
    }

    if (mySeq === syncSeq) await trim(cache, retainWindow, current)
  } catch (err) {
    // Should be unreachable (every awaited step above already guards its own
    // failure), but sync() must never reject into a caller that fire-and-forgets it.
    alog('prefetch', 'sync() aborted unexpectedly', err)
  }
}

// Evict everything not in the current window + the actively-playing track.
// Bounds the cache at prefetchCount+1 entries by construction — the keep set
// can never hold more than that.
async function trim(cache: Cache, retainWindow: PrefetchTrack[], current: PrefetchTrack | null): Promise<void> {
  const keep = new Set<string>()
  for (const track of retainWindow) {
    const url = buildStreamUrl(track)
    if (url) keep.add(streamCacheKey(url))
  }
  if (current) {
    const url = buildStreamUrl(current)
    if (url) keep.add(streamCacheKey(url))
  }

  const requests = await cache.keys()
  for (const req of requests) {
    const key = streamCacheKey(req.url)
    if (keep.has(key)) continue
    await cache.delete(req)
    revoke(key)
  }
}

// --- resolvePlayable ---------------------------------------------------
// The URL a deck should actually load for `track`: a cached blob: URL when
// warm, otherwise the plain tokened network URL — unchanged behavior. Never
// triggers a network fetch itself (sync() owns all fetching), so this stays
// cheap and safe to call from the hot playback path.
async function resolvePlayable(track: PrefetchTrack): Promise<string> {
  const networkUrl = buildStreamUrl(track)
  if (!networkUrl) return ''

  const { settings } = useDeviceSettings()
  if (settings.value.prefetchCount <= 0) return networkUrl // Disabled — pure pass-through.
  if (!cachesAvailable()) return networkUrl // Older WebView — pure pass-through.

  const key = streamCacheKey(networkUrl)
  const existing = objectUrls.get(key)
  if (existing) return existing

  const cache = await openCache()
  if (!cache) return networkUrl
  const hit = await cache.match(key)
  if (!hit) return networkUrl // Not warmed (yet) — network URL plays fine.

  try {
    const blob = await hit.blob()
    const objUrl = URL.createObjectURL(blob)
    objectUrls.set(key, objUrl)
    return objUrl
  } catch {
    return networkUrl // Corrupt/unreadable cache entry — fall back rather than break playback.
  }
}

// --- clearAll / usage ----------------------------------------------------
async function clearAll(): Promise<void> {
  for (const url of objectUrls.values()) URL.revokeObjectURL(url)
  objectUrls.clear()
  if (!cachesAvailable()) return
  try {
    await caches.delete(CACHE_NAME)
  } catch { /* best-effort */ }
  cachePromise = null
}

async function usage(): Promise<PrefetchUsage> {
  if (!cachesAvailable()) return { entries: 0, bytes: 0 }
  const cache = await openCache()
  if (!cache) return { entries: 0, bytes: 0 }
  const requests = await cache.keys()
  let bytes = 0
  for (const req of requests) {
    try {
      const res = await cache.match(req)
      if (!res) continue
      const len = res.headers.get('content-length')
      const declared = len ? Number.parseInt(len, 10) : Number.NaN
      if (Number.isFinite(declared)) {
        bytes += declared
        continue
      }
      // No usable content-length (opaque/compressed response) — fall back to
      // materializing the blob. Wasteful, but best-effort and only runs for
      // entries missing the header.
      const blob = await res.clone().blob()
      bytes += blob.size
    } catch { /* Unreadable entry — skip it. */ }
  }
  return { entries: requests.length, bytes }
}

export const prefetchManager = {
  sync,
  resolvePlayable,
  clearAll,
  usage,
}
