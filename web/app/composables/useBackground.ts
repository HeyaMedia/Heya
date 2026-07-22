// App-wide background channel — THE single way to control what the
// AmbientBackdrop layer paints behind the page.
//
// Anywhere that wants to own the background calls useBackground() and makes
// a claim:
//
//   const background = useBackground()
//   background.set(url)          // a specific image — detail heroes, the
//                                // home hero deck, roulette's settled pick
//   background.pool('movie')     // a cycling pool of library artwork —
//                                // list pages (/movies, /tv, /music, /books)
//
// Claims live on a STACK, newest on top, and the top claim wins. That makes
// nesting just work: the /music shell claims pool('music') for every music
// page, an artist detail mounted inside it pushes its backdrop on top, and
// when the artist page unmounts its claim pops and the music pool resumes.
// Nuxt's Suspense mounts the incoming page before the outgoing one unmounts,
// which the stack absorbs for free — the new page's claim is already in
// place when the old one is removed.
//
// Claims are client-only (the layer itself only renders for an authenticated
// client); auto-cleared on unmount. When the stack is empty, AmbientBackdrop
// falls back to a route-derived pool (home = all libraries), so pages only
// need to claim when the default is wrong.

import type { ImageTone } from './useImageTone'
import { storeToRefs } from 'pinia'
import { useBackgroundStore } from '~/stores/background'

/** Sharp-hero geometry published with a v2 art claim so AmbientBackdrop can
 *  render the blurred underlay at EXACTLY the hero's scale and offset — the
 *  image continues past the hero seam instead of re-showing a differently
 *  cropped copy of itself. */
export interface ClaimAlign {
  /** Rendered height of the sharp hero box, px. */
  heroH: number
  /** The hero box's viewport Y at scroll 0, px — 0 on desktop (the topbar
   *  overlays the hero) but non-zero on layouts with an in-flow topbar. */
  heroTop: number
  /** Rendered width of the sharp hero box, px — the viewport on detail
   *  pages, the content column on pages with a side menu. The underlay
   *  derives its cover scale from THIS so image rows land exactly where the
   *  hero draws them. */
  heroW: number
  /** object-position Y of the sharp art as a 0..1 fraction (0.3 = `30%`). */
  posY: number
}

export type BackgroundClaim =
  // `grade` marks a Heya 2.0 art owner: AmbientBackdrop paints it with the
  // redesign's soft grade (blur 72 / brightness .4 / saturate 1.2, opacity 1)
  // instead of the legacy pool coat. Absent → unchanged legacy behaviour.
  | { kind: 'art'; url: string; grade?: 'v2'; align?: ClaimAlign }
  | { kind: 'pool'; types: string[] }

export function useBackgroundStack() {
  return storeToRefs(useBackgroundStore()).claims
}

/** The winning claim — what AmbientBackdrop actually shows. */
export function useBackgroundClaim() {
  const stack = useBackgroundStack()
  return computed(() => stack.value.at(-1) ?? null)
}

/** Dominant tone of whatever the background layer is CURRENTLY showing
 *  (pool image or an owner's art). Written by AmbientBackdrop as images
 *  crossfade; null while ambient is off. Sampled once app-wide — consumers
 *  just read. */
export function useBackgroundTone() {
  return storeToRefs(useBackgroundStore()).tone
}

/** Ready-made artwork-adaptive button style: the current background's
 *  dominant tone as the fill, luminance-picked ink on top. Bind with
 *  `:style` and give the element a slow background/color transition
 *  (~0.9s) so the color glides as the backdrop rotates. Undefined when
 *  ambient is off — the element's own CSS is the fallback coat. */
export function useBackgroundToneStyle() {
  const tone = useBackgroundTone()
  return computed(() =>
    tone.value ? { background: tone.value.main, color: tone.value.ink } : undefined)
}

/** Rotation cadence of the pool layer. */
export const BG_ROTATE_MS = 30_000

/** The one size the ambient layer renders at. Preloads MUST warm exactly
 *  this variant or the crossfade starts before the real bytes exist. */
export const BG_IMG = { width: 1920, quality: 70 } as const

/** URL helpers bound to the nuxt-image provider. Call in setup and keep the
 *  returned object — the factory touches useImage()/useNuxtApp(), which
 *  silently hangs when first called inside a computed or async body
 *  (docs/ui.md gotcha #1). The methods themselves are safe anywhere. */
export function useBackgroundImageTools() {
  const $img = useImage()
  return {
    /** The exact URL the ambient layer renders — preload THIS, not the raw url. */
    variant: (url: string) => $img(url, { ...BG_IMG }),
    /** Tiny thumb for tone sampling (a 24×24 canvas needs nothing more). */
    thumb: (url: string) => $img(url, { width: 64 }),
    /** Fire-and-forget cache warmer for the rendered variant, so the next
     *  rotation/advance crossfades from a hot cache instead of stuttering. */
    warm(url: string) {
      if (!import.meta.client) return
      const img = new Image()
      img.decoding = 'async'
      img.fetchPriority = 'low' // never compete with page content
      img.src = this.variant(url)
    },
  }
}

/** Two-way channel between the AmbientBackdrop layer and the bottom-left
 *  AmbientControls cluster (and anything else that wants to steer the
 *  background). The layer WRITES mode/rotating/cycle; the controls WRITE
 *  paused/shuffleReq/reveal. useState, so it persists across navigation;
 *  `paused` additionally survives reloads via localStorage (the layer owns
 *  that mirror). */
export interface BackgroundControls {
  /** What the layer is showing: an owner's art, a rotating pool, or nothing. */
  mode: 'off' | 'art' | 'pool'
  /** True while an auto-rotation window is armed. */
  rotating: boolean
  /** User wish: stop auto-rotation. */
  paused: boolean
  /** Bump to request an immediate switch to a random pool image. */
  shuffleReq: number
  /** Reveal: fade the app away and show the artwork clean. */
  reveal: boolean
  /** The library item behind the current pool image — feeds the corner
   *  poster button. Null in art mode (the owning page IS that item). */
  current: { title: string; slug: string; mediaType: string; poster: string } | null
}

export function useBackgroundControls() {
  return storeToRefs(useBackgroundStore()).controls
}

/** Component-scoped owner handle. Repeated set()/pool() calls replace this
 *  owner's claim IN PLACE (a rotating hero keeps its stack position instead
 *  of leapfrogging claims made after it); clear() removes it, and unmount
 *  clears automatically. */
export function useBackground() {
  const stack = useBackgroundStack()
  let mine: BackgroundClaim | null = null

  function place(next: BackgroundClaim) {
    // Server claims would fossilize in the SSR payload with no owner to pop
    // them — and the layer never paints during SSR anyway.
    if (import.meta.server) return
    const cur = stack.value
    const i = mine ? cur.indexOf(mine) : -1
    mine = next
    stack.value = i >= 0
      ? [...cur.slice(0, i), next, ...cur.slice(i + 1)]
      : [...cur, next]
  }

  function set(url: string | null | undefined, opts?: { grade?: 'v2'; align?: ClaimAlign }) {
    if (!url) return clear()
    const grade = opts?.grade
    const align = opts?.align
    if (
      mine?.kind === 'art' && mine.url === url && mine.grade === grade
      && mine.align?.heroH === align?.heroH && mine.align?.heroTop === align?.heroTop
      && mine.align?.heroW === align?.heroW && mine.align?.posY === align?.posY
    ) return
    place({ kind: 'art', url, grade, align })
  }

  function pool(...types: string[]) {
    if (mine?.kind === 'pool' && mine.types.join(',') === types.join(',')) return
    place({ kind: 'pool', types })
  }

  function clear() {
    if (!mine) return
    const cur = stack.value
    const i = cur.indexOf(mine)
    if (i >= 0) stack.value = [...cur.slice(0, i), ...cur.slice(i + 1)]
    mine = null
  }

  onBeforeUnmount(clear)
  return { set, pool, clear }
}
