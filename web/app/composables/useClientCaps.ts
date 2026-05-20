export interface ClientCapabilities {
  supports_hevc: boolean
  supports_av1: boolean
  supports_flac: boolean
  supports_opus: boolean
  supports_mp4: boolean
  supports_mkv: boolean
  supports_webm: boolean
}

let cachedCaps: ClientCapabilities | null = null

export function useClientCaps(): ClientCapabilities {
  if (cachedCaps) return cachedCaps

  const video = document.createElement('video')

  const supports_hevc = !!(
    video.canPlayType('video/mp4; codecs="hvc1.1.6.L93.B0"') ||
    video.canPlayType('video/mp4; codecs="hev1.1.6.L93.B0"')
  )

  const supports_av1 = !!video.canPlayType('video/mp4; codecs="av01.0.08M.08"')

  const supports_flac = !!(
    video.canPlayType('audio/flac') ||
    video.canPlayType('audio/x-flac')
  )

  const supports_opus = !!(
    video.canPlayType('audio/ogg; codecs="opus"') ||
    video.canPlayType('audio/webm; codecs="opus"')
  )

  const supports_mp4 = !!video.canPlayType('video/mp4; codecs="avc1.42E01E"')

  const supports_mkv = !!video.canPlayType('video/x-matroska; codecs="avc1.42E01E"')

  const supports_webm = !!video.canPlayType('video/webm; codecs="vp9"')

  cachedCaps = {
    supports_hevc,
    supports_av1,
    supports_flac,
    supports_opus,
    supports_mp4,
    supports_mkv,
    supports_webm,
  }

  return cachedCaps
}

export function capsToQueryString(caps: ClientCapabilities): string {
  const params = new URLSearchParams()
  for (const [key, val] of Object.entries(caps)) {
    if (val) params.set(key, '1')
  }
  return params.toString()
}
