// Reactive viewport-tier + pointer-coarseness classification.
//
// Shared singleton: every call site observes the same three `matchMedia`
// listeners instead of registering its own set. See docs/ui.md "Responsive
// conventions" for the breakpoint rationale — CSS custom properties can't
// appear in media queries, so the literal numbers here are duplicated (not
// derived from) the ones hardcoded into `@media` rules across the app.
//
// Boundaries match the CSS breakpoints exactly (docs/ui.md "Responsive
// conventions"): isPhone <=720px, isTablet 720.02-960px, isDesktop >960px —
// so a window sitting exactly on a breakpoint never gets phone CSS with
// tablet JS, or vice versa.
// isCoarse tracks `(pointer: coarse)` — touch capability, not width — so a
// touch laptop at desktop width still gets touch affordances (long-press,
// bigger tap targets) and a mouse-driven narrow window doesn't.
import type { Ref } from 'vue'
import { effectScope } from 'vue'

export interface ViewportInfo {
  isPhone: Ref<boolean>
  isTablet: Ref<boolean>
  isDesktop: Ref<boolean>
  /**
   * The adaptive tablet/narrow band: 720.02–1200px (iPad Mini both
   * orientations, foldable inner screens). In this band sidebars hide behind
   * the topbar burger, and the topbar/playbar run their collapsed layouts.
   * Above 1200px the app renders its full desktop chrome untouched.
   */
  isCompact: Ref<boolean>
  isCoarse: Ref<boolean>
}

let shared: ViewportInfo | null = null

export function useViewport(): ViewportInfo {
  // No `window` on the server, and no reactivity needed there — the client
  // mount re-evaluates via matchMedia immediately once it runs. Matches the
  // early-return-on-server convention used by useMediaSession/usePlayer.
  if (import.meta.server) {
    return {
      isPhone: ref(false),
      isTablet: ref(false),
      isDesktop: ref(true),
      isCompact: ref(false),
      isCoarse: ref(false),
    }
  }

  if (shared) return shared

  // Detached scope: useMediaQuery cleans up its matchMedia listener when the
  // *calling* effect scope disposes. Created bare inside the first caller's
  // component, the cached refs would freeze the moment that component
  // unmounts. The detached scope lives for the app's lifetime instead.
  const scope = effectScope(true)
  shared = scope.run(() => ({
    isPhone: useMediaQuery('(max-width: 720px)'),
    isTablet: useMediaQuery('(min-width: 720.02px) and (max-width: 960px)'),
    isDesktop: useMediaQuery('(min-width: 960.02px)'),
    isCompact: useMediaQuery('(min-width: 720.02px) and (max-width: 1200px)'),
    isCoarse: useMediaQuery('(pointer: coarse)'),
  }))!
  return shared
}
