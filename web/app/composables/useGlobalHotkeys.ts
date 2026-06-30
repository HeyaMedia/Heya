import { useEventListener } from '@vueuse/core'

// Global transport hotkeys for the music player. Mounted once from the music
// shell. Suppressed while typing (inputs / contenteditable) and never steals a
// modifier combo (so Cmd/Ctrl+K search, browser shortcuts, etc. still work).
//
//   Space      play / pause        ↑ / ↓        volume ±5
//   ← / →      seek ∓5s            ⇧← / ⇧→      previous / next track
//   M mute     S shuffle           R repeat     Q queue    L lyrics

function isTypingTarget(e: KeyboardEvent): boolean {
  const t = e.target as HTMLElement | null
  if (!t) return false
  const tag = t.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || t.isContentEditable
}

function isActivatable(el: Element | null): boolean {
  if (!el) return false
  return el.tagName === 'BUTTON' || el.tagName === 'A' || el.getAttribute('role') === 'button'
}

export function useGlobalHotkeys() {
  const player = usePlayer()
  // Shared with the HotkeyHelp modal mounted in the music shell.
  const helpOpen = useState('music_hotkey_help_open', () => false)

  // seek() wants a 0..1 fraction; convert a per-second delta through duration.
  function seekBy(deltaSeconds: number) {
    const dur = player.duration.value
    if (dur > 0) player.seek(Math.max(0, Math.min(dur, player.position.value + deltaSeconds)) / dur)
  }

  useEventListener('keydown', (e: KeyboardEvent) => {
    if (isTypingTarget(e)) return
    if (e.metaKey || e.ctrlKey || e.altKey) return

    switch (e.key) {
      case ' ':
        // Let a focused button/link handle its own activation.
        if (isActivatable(document.activeElement)) return
        e.preventDefault()
        void player.togglePlay()
        break
      case 'ArrowLeft':
        e.preventDefault()
        if (e.shiftKey) void player.prevTrack()
        else seekBy(-5)
        break
      case 'ArrowRight':
        e.preventDefault()
        if (e.shiftKey) void player.nextTrack()
        else seekBy(5)
        break
      case 'ArrowUp':
        e.preventDefault()
        player.setVolume(player.volume.value + 5)
        break
      case 'ArrowDown':
        e.preventDefault()
        player.setVolume(player.volume.value - 5)
        break
      case 'm': case 'M':
        e.preventDefault(); player.toggleMute(); break
      case 's': case 'S':
        e.preventDefault(); player.toggleShuffle(); break
      case 'r': case 'R':
        e.preventDefault(); player.cycleRepeat(); break
      case 'q': case 'Q':
        e.preventDefault(); player.toggleQueue(); break
      case 'l': case 'L':
        e.preventDefault(); player.toggleLyrics(); break
      case '?':
        e.preventDefault(); helpOpen.value = !helpOpen.value; break
    }
  })

  return { helpOpen }
}
