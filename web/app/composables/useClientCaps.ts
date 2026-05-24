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
  // Audio caps — used by the music stream decision (direct/remux/transcode).
  // Probed against a bare <audio> element, distinct from the MSE-in-MP4
  // codecs above which are for HLS video playback.
  supports_flac_native: boolean
  supports_alac: boolean
  supports_mp3: boolean
  supports_aac_audio: boolean
  supports_ogg_vorbis: boolean
  supports_opus_audio: boolean
  supports_wav_pcm: boolean
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

  // Audio: probed via a bare <audio> element (separate from MSE/MP4 video
  // probes above). `canPlayType` returns "" / "maybe" / "probably" — we treat
  // anything non-empty as supported, matching the way browsers expose it.
  const audio = document.createElement('audio')
  const canAudio = (type: string) => !!audio.canPlayType(type)
  const supports_flac_native = canAudio('audio/flac') || canAudio('audio/x-flac')
  const supports_alac = canAudio('audio/mp4; codecs="alac"')
  const supports_mp3 = canAudio('audio/mpeg') || canAudio('audio/mp3')
  const supports_aac_audio = canAudio('audio/mp4; codecs="mp4a.40.2"') || canAudio('audio/aac')
  const supports_ogg_vorbis = canAudio('audio/ogg; codecs="vorbis"')
  const supports_opus_audio = canAudio('audio/ogg; codecs="opus"')
  const supports_wav_pcm = canAudio('audio/wav') || canAudio('audio/wave') || canAudio('audio/x-wav')

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
    supports_flac_native,
    supports_alac,
    supports_mp3,
    supports_aac_audio,
    supports_ogg_vorbis,
    supports_opus_audio,
    supports_wav_pcm,
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
