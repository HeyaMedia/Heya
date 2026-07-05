// Platform detection for playback-engine selection.
//
// iOS Safari suspends the Web Audio `AudioContext` when the app backgrounds
// or the screen locks. Any `<audio>` element that has been routed through
// `AudioContext.createMediaElementSource` goes silent when that happens —
// the *context* is suspended, not just the element, and the graph connection
// is irreversible per element. A bare `HTMLAudioElement` (no
// `createMediaElementSource`) keeps playing straight through backgrounding.
// `useAudioEngine.ts` uses `isIOS()` to pick `engine/directEngine.ts` (no
// Web Audio graph at all) over the normal graph engine on iOS by default;
// `useDeviceSettings().settings.value.forceDirectEngine` lets a user override
// that in either direction.
//
// iPadOS 13+ identifies as `MacIntel` in `navigator.platform` when Safari
// requests the desktop-class site (the default since iPadOS 13) — the
// classic iPhone/iPad/iPod UA regex alone misses those, so a touch-capable
// "Mac" is also treated as iOS. Desktop Macs report `maxTouchPoints === 0`,
// so this doesn't misfire on real desktop Safari/Chrome.
export function isIOS(): boolean {
  if (import.meta.server) return false
  if (typeof navigator === 'undefined') return false
  return /iPad|iPhone|iPod/.test(navigator.userAgent)
    || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1)
}
