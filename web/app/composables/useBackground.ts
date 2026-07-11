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

export type BackgroundClaim =
  | { kind: 'art'; url: string }
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

/** Rotation cadence of the pool layer. The corner ring animates at exactly
 *  this duration, so indicator and timer can't drift apart. */
export const BG_ROTATE_MS = 30_000

/** Two-way channel between the AmbientBackdrop layer and the bottom-left
 *  AmbientControls cluster (and anything else that wants to steer the
 *  background). The layer WRITES mode/rotating/cycle; the controls WRITE
 *  paused/shuffleReq/reveal. useState, so it persists across navigation;
 *  `paused` additionally survives reloads via localStorage (the layer owns
 *  that mirror). */
export interface BackgroundControls {
  /** What the layer is showing: an owner's art, a rotating pool, or nothing. */
  mode: 'off' | 'art' | 'pool'
  /** True while an auto-rotation window is armed (the ring is running). */
  rotating: boolean
  /** Increments when a rotation window starts — re-keys the progress ring. */
  cycle: number
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

  function set(url: string | null | undefined) {
    if (!url) return clear()
    if (mine?.kind === 'art' && mine.url === url) return
    place({ kind: 'art', url })
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
