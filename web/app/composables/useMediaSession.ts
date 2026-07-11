// Bridges the OS Media Session API to the music player: hardware media keys,
// lock-screen / notification-shade transport controls, now-playing metadata,
// artwork, and a live scrubber on the OS surface. Mounted once from the
// persistent Playbar. No-op on SSR and on browsers without the API.
//
// Heya specifics vs a stock implementation:
//   - the now-playing artwork comes from the track's `poster` URL
//   - usePlayerBindings().seek() takes a 0..1 fraction, so the per-second seek actions
//     convert through the current duration
//   - radio streams carry negative ids, so the metadata key is stringified
export function useMediaSession() {
  if (import.meta.server) return
  if (!('mediaSession' in navigator)) return

  const player = usePlayerBindings()
  const ms = navigator.mediaSession

  // Media Session artwork must be an absolute HTTP(S) URL the OS can fetch —
  // blob:/data: and relative paths don't work across browsers.
  function resolveArtworkUrl(src: string | null | undefined): string | null {
    if (!src) return null
    if (src.startsWith('/')) return window.location.origin + src
    if (src.startsWith('http://') || src.startsWith('https://')) return src
    return null
  }

  function seekToSeconds(seconds: number) {
    const dur = player.duration.value
    if (dur > 0) player.seek(Math.max(0, Math.min(dur, seconds)) / dur)
  }

  const actions: [MediaSessionAction, MediaSessionActionHandler][] = [
    ['play', () => { void player.play() }],
    ['pause', () => player.pause()],
    ['previoustrack', () => { void player.prevTrack() }],
    ['nexttrack', () => { void player.nextTrack() }],
    ['seekto', (d) => { if (d.seekTime != null) seekToSeconds(d.seekTime) }],
    ['seekbackward', (d) => seekToSeconds(player.position.value - (d.seekOffset ?? 10))],
    ['seekforward', (d) => seekToSeconds(player.position.value + (d.seekOffset ?? 10))],
  ]
  for (const [action, handler] of actions) {
    try { ms.setActionHandler(action, handler) } catch { /* action unsupported here */ }
  }

  // Metadata: set title/artist/album immediately, upgrade with artwork when present.
  let lastKey: string | null = null
  watch(() => player.currentTrack.value, (track) => {
    if (!track) { ms.metadata = null; lastKey = null; return }
    const key = String(track.id)
    if (key === lastKey) return
    lastKey = key

    const base = {
      title: track.title,
      artist: track.artist ?? undefined,
      album: track.album ?? undefined,
    }
    ms.metadata = new MediaMetadata(base)

    const artUrl = resolveArtworkUrl(track.poster)
    if (artUrl) {
      ms.metadata = new MediaMetadata({
        ...base,
        artwork: [
          { src: artUrl, sizes: '256x256' },
          { src: artUrl, sizes: '512x512' },
        ],
      })
    }
  }, { immediate: true })

  watch(() => player.playing.value, (playing) => {
    ms.playbackState = playing ? 'playing' : 'paused'
  }, { immediate: true })

  // Live position for the OS scrubber. Some browsers reject invalid states.
  watchEffect(() => {
    const dur = player.duration.value
    const pos = player.position.value
    if (dur > 0) {
      try {
        ms.setPositionState({ duration: dur, position: Math.min(pos, dur), playbackRate: 1 })
      } catch { /* invalid position state — ignore */ }
    }
  })
}
