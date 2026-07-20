export type ClientSurface = 'browser' | 'tauri'

export const CLIENT_SURFACE_MARKER = 'heya_client'
export const CLIENT_SURFACE_STORAGE_KEY = 'heya.client.surface'
export const CLIENT_SURFACE_HEADER = 'X-Heya-Client-Surface'
export const CLIENT_SURFACE_WS_PARAM = 'client_surface'
export const TAURI_SWITCH_SERVER_URI = 'heya-client://switch-server'

const clientSurface = shallowRef<ClientSurface>('browser')
let initialized = false

function storedClientSurface(): ClientSurface {
  if (!import.meta.client) return 'browser'
  try {
    return sessionStorage.getItem(CLIENT_SURFACE_STORAGE_KEY) === 'tauri' ? 'tauri' : 'browser'
  } catch {
    // Storage can be unavailable in hardened browser contexts. The URL marker
    // still activates Tauri mode for the current SPA lifetime in that case.
    return 'browser'
  }
}

function persistTauriSurface() {
  try {
    sessionStorage.setItem(CLIENT_SURFACE_STORAGE_KEY, 'tauri')
  } catch {
    // See storedClientSurface(): retaining the reactive value still keeps the
    // current page in Tauri mode even when storage is unavailable.
  }
}

function hydrateClientSurface() {
  if (!import.meta.client || initialized) return
  clientSurface.value = storedClientSurface()
  initialized = true
}

/**
 * Captures the native-client launch marker before auth redirects can discard
 * it, then removes only that marker from the visible URL. This marker and all
 * derived metadata are intentionally spoofable and must never establish trust.
 */
export function captureClientSurfaceMarker() {
  if (!import.meta.client) return

  const url = new URL(window.location.href)
  if (url.searchParams.get(CLIENT_SURFACE_MARKER) === '1') {
    clientSurface.value = 'tauri'
    persistTauriSurface()
  } else {
    clientSurface.value = storedClientSurface()
  }
  initialized = true

  if (url.searchParams.has(CLIENT_SURFACE_MARKER)) {
    url.searchParams.delete(CLIENT_SURFACE_MARKER)
    window.history.replaceState(
      window.history.state,
      '',
      `${url.pathname}${url.search}${url.hash}`,
    )
  }
}

export function getClientSurface(): ClientSurface {
  hydrateClientSurface()
  return clientSurface.value
}

export function isSameOriginHeyaApiRequest(target: RequestInfo | URL): boolean {
  if (!import.meta.client) return false
  try {
    const raw = typeof target === 'string'
      ? target
      : target instanceof URL
        ? target.href
        : target.url
    const url = new URL(raw, window.location.href)
    return url.origin === window.location.origin && (url.pathname === '/api' || url.pathname.startsWith('/api/'))
  } catch {
    return false
  }
}

/** Merge caller headers and add untrusted client-surface telemetry when valid. */
export function withClientSurfaceHeaders(target: RequestInfo | URL, headers?: HeadersInit): Headers {
  const merged = new Headers(headers)
  // Ensure this helper itself never leaks a caller-provided surface value onto
  // browser or cross-origin requests. The value remains trivially spoofable by
  // arbitrary clients and is metadata only on the server.
  merged.delete(CLIENT_SURFACE_HEADER)
  if (isSameOriginHeyaApiRequest(target)) {
    merged.set(CLIENT_SURFACE_HEADER, getClientSurface())
  }
  return merged
}

export function useClientSurface() {
  hydrateClientSurface()
  const isTauriClient = computed(() => clientSurface.value === 'tauri')

  return {
    surface: readonly(clientSurface),
    isTauriClient: readonly(isTauriClient),
  }
}
