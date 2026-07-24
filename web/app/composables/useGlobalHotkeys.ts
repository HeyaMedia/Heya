import { useEventListener } from '@vueuse/core'

// Global transport hotkeys for the music player. Mounted once per non-auth
// layout (default.vue + settings.vue), so they reach every page the global
// playbar reaches — not just the music shell. Suppressed while typing (inputs /
// contenteditable) and never steals a modifier combo (so Cmd/Ctrl+K search,
// browser shortcuts, etc. still work).
//
//   Space      play / pause        ↑ / ↓        volume ±5
//   ← / →      seek ∓5s            ⇧← / ⇧→      previous / next track
//   M mute     S shuffle           R repeat     Q queue    L lyrics
//   V visualizer (when open: ←/→ preset, R random, Esc close)
//
// Off /music the keys stay dormant until a track is loaded — the same
// condition DesktopPlayerHost uses to mount the playbar, so a shortcut exists
// exactly when the UI it drives does. With nothing loaded, Space and the arrows
// keep their native page-scrolling behaviour.

// A mounted video player owns the whole keyboard: its transport keys (space,
// j/k/l, arrows) mean the same things for the thing the user is actually
// watching. VideoPlayer raises this claim for as long as it is on screen. It
// lives on a layout-less route today, so nothing overlaps in practice — the
// claim is what keeps that true if it is ever embedded beside the playbar.
export function useVideoKeyboardClaim() {
  return useState('video_keyboard_claim', () => false)
}

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

// An open dialog or menu owns the keyboard while it is up: the lightbox reads
// ←/→ as previous/next image, reka's menus read ↑/↓ as item navigation. Those
// all mark their content `data-state="open"`. The fullscreen visualizer is a
// bare `role="dialog"` div without that attribute — it wants the music keys to
// keep working over it, and coordinates per-key below instead.
const OVERLAY_SELECTOR = [
  '[role="dialog"][data-state="open"]',
  '[role="alertdialog"][data-state="open"]',
  '[role="menu"][data-state="open"]',
].join(',')

function overlayOwnsKeyboard(): boolean {
  return !!document.querySelector(OVERLAY_SELECTOR)
}

export function useGlobalHotkeys() {
  const player = usePlayerBindings()
  const vis = useVisualizer()
  const route = useRoute()
  const videoClaim = useVideoKeyboardClaim()
  // Shared with the HotkeyHelp modal mounted by the global player host.
  const helpOpen = useState('music_hotkey_help_open', () => false)

  // Mirrors DesktopPlayerHost's mount condition: the music shell always has a
  // player to drive (even idle), everywhere else waits for a loaded track.
  const playerPresent = computed(() =>
    !!player.currentTrack.value || route.path === '/music' || route.path.startsWith('/music/'))

  // seek() wants a 0..1 fraction; convert a per-second delta through duration.
  function seekBy(deltaSeconds: number) {
    const dur = player.duration.value
    if (dur > 0) player.seek(Math.max(0, Math.min(dur, player.position.value + deltaSeconds)) / dur)
  }

  useEventListener('keydown', (e: KeyboardEvent) => {
    if (isTypingTarget(e)) return
    if (e.metaKey || e.ctrlKey || e.altKey) return
    if (videoClaim.value) return

    // The shortcut sheet answers even with no track loaded, and stays closable
    // by the same key once it is itself the open overlay.
    if (e.key === '?') {
      if (!helpOpen.value && overlayOwnsKeyboard()) return
      e.preventDefault()
      helpOpen.value = !helpOpen.value
      return
    }

    if (!playerPresent.value) return
    if (overlayOwnsKeyboard()) return

    // While the immersive visualizer is open it owns ←/→/r (preset navigation)
    // and Escape (close) via its own listener — don't also seek/repeat below.
    if (vis.fullscreenOpen.value && ['ArrowLeft', 'ArrowRight', 'r', 'R', 'Escape'].includes(e.key)) return

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
      case 'v': case 'V':
        e.preventDefault(); vis.fullscreenOpen.value = !vis.fullscreenOpen.value; break
    }
  })

  return { helpOpen }
}
