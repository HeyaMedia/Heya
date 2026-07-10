// Ambient-backdrop override channel.
//
// The AmbientBackdrop layer (layouts/default.vue) normally cycles a random
// per-route pool of library artwork. Pages that OWN a specific image — the
// movie/TV detail hero, the artist page, the home hero deck — push their
// current backdrop here instead, so the artwork you see in the hero is the
// artwork behind the whole page ("the hero extends to the entire page").
// The owner drives rotation (detail carousels already crossfade every 8s);
// the layer just follows the url. Owners auto-clear on unmount, at which
// point the layer falls back to its route pool.

export interface AmbientOverride {
  /** The image to show full-page right now. */
  url: string
}

export function useAmbientOverride() {
  return useState<AmbientOverride | null>('ambient_override', () => null)
}

/** Component-scoped owner handle: set() replaces the override, and it is
 *  cleared automatically on unmount — but only if this component still owns
 *  it (a newly-mounted page may already have claimed the channel). */
export function useAmbientArt() {
  const override = useAmbientOverride()
  let mine: AmbientOverride | null = null

  function set(url: string | null | undefined) {
    if (!url) return clear()
    if (mine?.url === url && override.value === mine) return
    mine = { url }
    override.value = mine
  }

  function clear() {
    if (mine && override.value === mine) override.value = null
    mine = null
  }

  onBeforeUnmount(clear)
  return { set, clear }
}
