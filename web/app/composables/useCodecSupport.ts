import type { CodecSupport } from '~~/shared/types/audio'

const MIME_MAP: Record<keyof CodecSupport, string> = {
  flac: 'audio/flac',
  alac: 'audio/mp4; codecs="alac"',
  aac: 'audio/mp4; codecs="mp4a.40.2"',
  mp3: 'audio/mpeg',
  opus: 'audio/ogg; codecs="opus"',
  vorbis: 'audio/ogg; codecs="vorbis"',
  wav: 'audio/wav',
  pcm: 'audio/wav; codecs="1"',
  wma: 'audio/x-ms-wma',
  aiff: 'audio/aiff',
  webm: 'audio/webm',
  ac3: 'audio/ac3',
  eac3: 'audio/eac3',
  dsd: 'audio/dsd',
  dsf: 'audio/dsf',
  m4a: 'audio/mp4',
}

let cached: CodecSupport | null = null

function detect(): CodecSupport {
  if (cached) return cached

  if (import.meta.server) {
    cached = Object.fromEntries(Object.keys(MIME_MAP).map((k) => [k, false])) as unknown as CodecSupport
    return cached
  }

  const audio = document.createElement('audio')
  const support = {} as CodecSupport
  for (const [codec, mime] of Object.entries(MIME_MAP)) {
    const result = audio.canPlayType(mime)
    support[codec as keyof CodecSupport] = result === 'probably' || result === 'maybe'
  }
  cached = support
  return cached
}

export function useCodecSupport() {
  const codecSupport = ref<CodecSupport>(detect())
  if (import.meta.client && !cached) {
    onMounted(() => { codecSupport.value = detect() })
  }
  return { codecSupport }
}
