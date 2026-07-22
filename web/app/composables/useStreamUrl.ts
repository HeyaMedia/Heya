// Single source of truth for the URL an <audio> element hits to play a track,
// and for the cache key that URL maps to in the prefetch manager's Cache API
// bucket (see ~/engine/prefetch.ts). These two concerns MUST live together:
// the prefetch manager and usePlayer's decks have to build byte-identical
// URLs, or a track fetched ahead of time under one URL will never be found
// under the (slightly different) URL the deck actually requests.
//
// Extracted out of usePlayer.ts (previously a private `resolveStreamUrl`
// closure) so engine/prefetch.ts can share the exact same logic without
// importing from usePlayer (which would create a composables <-> engine
// import cycle).

export interface StreamableTrack {
  id: number
  stream_url?: string | null
}

// Browser media elements authenticate with the same-origin HttpOnly cookie.
//
// For /stream URLs (the smart endpoint that picks the best playable file and
// transcodes if needed) we also append the audio caps so the server can match
// what this browser will actually decode, and — when the device settings ask
// for a lower tier — a `quality` hint. /file/{id} URLs (original direct
// file playback) and non-/stream `stream_url`s (e.g. internet radio) skip
// both: caps/quality only make sense where the server is picking an encode.
//
// Returns '' (never undefined) when there's nothing playable to build a URL
// from — callers already treat a falsy return as "can't play this", and ''
// keeps the return type a plain `string` per the prefetch manager's contract.
export function buildStreamUrl(t: StreamableTrack): string {
  const base = t.stream_url ?? (t.id > 0 ? `/api/music/tracks/${t.id}/stream` : undefined)
  if (!base) return ''

  const params = new URLSearchParams()
  const isStreamEndpoint = base.endsWith('/stream')
  if (import.meta.client && isStreamEndpoint) {
    const caps = useClientCaps()
    for (const [key, val] of Object.entries(caps)) {
      if (key.startsWith('supports_') && val) params.set(key, '1')
    }
    const { settings } = useDeviceSettings()
    if (settings.value.streamQuality !== 'original') {
      params.set('quality', settings.value.streamQuality)
    }
  }

  const sep = base.includes('?') ? '&' : '?'
  return params.toString() ? `${base}${sep}${params.toString()}` : base
}

// Cache key for the Cache API. Strip the obsolete pre-cookie token parameter
// defensively so old cached URLs migrate cleanly, but retain caps + quality:
// they select which encode the server returns and must not collide.
export function streamCacheKey(url: string): string {
  const origin = typeof location !== 'undefined' ? location.origin : 'http://localhost'
  const parsed = new URL(url, origin)
  parsed.searchParams.delete('token')
  return `${parsed.pathname}${parsed.search}`
}
