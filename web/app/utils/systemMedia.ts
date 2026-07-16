interface MediaIdentity {
  id: number
  title: string
  artist?: string | null
  album?: string | null
}

/**
 * Compact, non-secret identity for one now-playing item. Including the visible
 * metadata lets an ICY radio stream (whose synthetic negative id stays fixed)
 * publish each real song while keeping the native bridge key comfortably
 * below its validation limit.
 */
export function systemMediaItemKey(track: MediaIdentity): string {
  const input = [track.title, track.artist ?? '', track.album ?? ''].join('\u0000')
  return `track:${track.id}:${hashSystemMediaString(input)}`
}

export function systemMediaArtworkKey(source: string): string {
  return `art:${hashSystemMediaString(source)}`
}

function hashSystemMediaString(input: string): string {
  let hash = 0x811c9dc5
  for (let i = 0; i < input.length; i++) {
    hash ^= input.charCodeAt(i)
    hash = Math.imul(hash, 0x01000193)
  }
  return (hash >>> 0).toString(16).padStart(8, '0')
}

export function systemMediaNotificationBody(track: MediaIdentity): string {
  const artist = track.artist?.trim()
  const album = track.album?.trim()
  if (artist && album) return `${artist} · ${album}`
  return artist || album || 'Now playing in Heya'
}

export function clampSystemMediaPosition(position: number, duration: number): number {
  if (!Number.isFinite(position) || position <= 0) return 0
  if (!Number.isFinite(duration) || duration <= 0) return 0
  return Math.min(position, duration)
}
