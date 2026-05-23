export interface ClientCapabilities {
  supports_hevc: boolean
  supports_hevc_hev1: boolean
  supports_av1: boolean
  supports_flac: boolean
  supports_opus: boolean
  supports_ac3: boolean
  supports_eac3: boolean
  supports_mp4: boolean
  supports_mkv: boolean
  supports_webm: boolean
  supports_hdr: boolean
  supports_hdr10: boolean
  supports_hlg: boolean
  supports_dovi: boolean
}

let cachedCaps: ClientCapabilities | null = null

// mseSupports probes MediaSource for a specific MIME+codec combo. This is more
// accurate than HTMLVideoElement.canPlayType for HLS playback, because hls.js
// feeds segments through SourceBuffer — which uses the same gate.
function mseSupports(mimeWithCodec: string): boolean {
  if (typeof MediaSource === 'undefined') return false
  try {
    return MediaSource.isTypeSupported(mimeWithCodec)
  } catch {
    return false
  }
}

export function useClientCaps(): ClientCapabilities {
  if (cachedCaps) return cachedCaps

  const video = document.createElement('video')

  // Video codecs: probe both bare canPlayType and MSE for safety.
  const supports_hevc_hvc1 = mseSupports('video/mp4; codecs="hvc1.1.6.L93.B0"')
    || !!video.canPlayType('video/mp4; codecs="hvc1.1.6.L93.B0"')
  const supports_hevc_hev1 = mseSupports('video/mp4; codecs="hev1.1.6.L93.B0"')
    || !!video.canPlayType('video/mp4; codecs="hev1.1.6.L93.B0"')
  const supports_hevc = supports_hevc_hvc1 || supports_hevc_hev1

  const supports_av1 = mseSupports('video/mp4; codecs="av01.0.08M.08"')
    || !!video.canPlayType('video/mp4; codecs="av01.0.08M.08"')

  // HDR variants. PQ (HDR10) and HLG have distinct transfer functions; a
  // client may support one and not the other. DoVi (Dolby Vision) uses its
  // own codec strings (dvh1.05 / dvh1.08).
  const supports_hdr10 = mseSupports('video/mp4; codecs="hvc1.2.4.L153.B0"')
    || !!video.canPlayType('video/mp4; codecs="hvc1.2.4.L153.B0"')
  const supports_hlg = mseSupports('video/mp4; codecs="hvc1.1.6.L153.B0"')
    || !!video.canPlayType('video/mp4; codecs="hvc1.1.6.L153.B0"')
  const supports_dovi = mseSupports('video/mp4; codecs="dvh1.05.06"')
    || mseSupports('video/mp4; codecs="dvh1.08.06"')
    || !!video.canPlayType('video/mp4; codecs="dvh1.05.06"')
    || !!video.canPlayType('video/mp4; codecs="dvh1.08.06"')

  // Audio codecs: must work in MP4 container via MSE (that's what fMP4 HLS uses).
  // Probe both lowercase and the official RFC 6381 capitalisations.
  const supports_flac = mseSupports('audio/mp4; codecs="flac"')
    || mseSupports('audio/mp4; codecs="fLaC"')
    || !!video.canPlayType('audio/flac')
  const supports_opus = mseSupports('audio/mp4; codecs="opus"')
    || mseSupports('audio/mp4; codecs="Opus"')
    || !!video.canPlayType('audio/ogg; codecs="opus"')
  const supports_ac3 = mseSupports('audio/mp4; codecs="ac-3"')
    || mseSupports('audio/mp4; codecs="ac3"')
  const supports_eac3 = mseSupports('audio/mp4; codecs="ec-3"')
    || mseSupports('audio/mp4; codecs="eac3"')

  const supports_mp4 = !!video.canPlayType('video/mp4; codecs="avc1.42E01E"')

  const supports_mkv = false

  const supports_webm = mseSupports('video/webm; codecs="vp9"')
    || !!video.canPlayType('video/webm; codecs="vp9"')

  // Generic HDR catch-all: the display can actually render HDR. Kept distinct
  // from the codec-level capability probes — we only set this true when the
  // window media query also says HDR is on.
  const supports_hdr = !!(
    (supports_hdr10 || supports_hlg || supports_dovi) &&
    window.matchMedia?.('(dynamic-range: high)')?.matches
  )

  cachedCaps = {
    supports_hevc,
    supports_hevc_hev1,
    supports_av1,
    supports_flac,
    supports_opus,
    supports_ac3,
    supports_eac3,
    supports_mp4,
    supports_mkv,
    supports_webm,
    supports_hdr,
    supports_hdr10,
    supports_hlg,
    supports_dovi,
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
