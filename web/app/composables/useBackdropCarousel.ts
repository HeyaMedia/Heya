import type { Ref } from 'vue'
import type { MediaDetail } from '~~/shared/types'

export interface BackdropCarouselOptions {
  /**
   * Exclude backdrop assets with sort_order >= this value. The movie/TV
   * detail pages pass 1000 (locally-added extras live above that);
   * MediaDetailView historically imposed no cap.
   */
  maxSortOrder?: number
  /**
   * Seed backdropB with the *second* backdrop (preload for an instant first
   * crossfade) instead of mirroring the first. MediaDetailView behaviour.
   */
  preloadSecond?: boolean
}

/** One rotation window — CycleControls' ring animates at exactly this.
 *  Matches the home hero's 30s cadence: backdrops are scenery, not a
 *  slideshow demanding attention. */
export const BACKDROP_INTERVAL = 30_000

/**
 * Crossfade backdrop carousel for detail-page heroes.
 *
 * Owns the A/B image pair, indicator state, and the backdrop lightbox. The
 * CLOCK is not here: the CycleControls ring drives rotation — its
 * animationend calls advanceBackdrop(), and every advance/retreat/jump bumps
 * `cycleKey`, which re-keys the ring for a fresh window. `carouselPaused` is
 * the sticky user pause (bind it to the cluster's v-model:paused); pausing
 * freezes the ring, so no animationend ever fires while paused.
 *
 * Call `seedCarousel()` whenever the detail payload arrives/changes — the
 * caller decides when (typically inside its `watch(detail)` / `onMounted`).
 */
export function useBackdropCarousel(detail: Ref<MediaDetail | null>, opts: BackdropCarouselOptions = {}) {
  const lightbox = useLightbox()

  const showA = ref(true)
  const backdropA = ref<string | null>(null)
  const backdropB = ref<string | null>(null)
  const backdropIdx = ref(0)
  const carouselPaused = ref(false)
  const cycleKey = ref(0)

  const backdropAssets = computed(() => {
    if (!detail.value?.assets) return []
    const seen = new Set<number>()
    return detail.value.assets
      .filter(a => a.asset_type === 'backdrop' && (opts.maxSortOrder == null || a.sort_order < opts.maxSortOrder))
      .sort((a, b) => a.sort_order - b.sort_order)
      .filter(a => { if (seen.has(a.sort_order)) return false; seen.add(a.sort_order); return true })
  })

  function getBackdropUrl(idx: number) {
    if (backdropAssets.value.length > 0) {
      const asset = backdropAssets.value[idx % backdropAssets.value.length]
      if (!asset) return null
      return `/api/media/${useMediaImageKey(detail.value?.media_item)}/image/backdrop?sort=${asset.sort_order}`
    }
    return detail.value ? useBackdropUrl(detail.value.media_item) : null
  }

  function showIdx(idx: number) {
    backdropIdx.value = idx
    const url = getBackdropUrl(idx)
    if (showA.value) { backdropB.value = url } else { backdropA.value = url }
    showA.value = !showA.value
    cycleKey.value++
  }

  function advanceBackdrop() {
    const n = backdropAssets.value.length
    if (n <= 1) return
    showIdx((backdropIdx.value + 1) % n)
  }

  function retreatBackdrop() {
    const n = backdropAssets.value.length
    if (n <= 1) return
    showIdx((backdropIdx.value - 1 + n) % n)
  }

  function jumpToBackdrop(idx: number) {
    if (idx === backdropIdx.value) return
    showIdx(idx)
  }

  /** (Re)seed the A/B pair from the current detail and start a fresh window. */
  function seedCarousel() {
    backdropA.value = getBackdropUrl(0)
    if (opts.preloadSecond && backdropAssets.value.length > 1) {
      backdropB.value = getBackdropUrl(1)
    } else if (!opts.preloadSecond) {
      backdropB.value = getBackdropUrl(0)
    }
    cycleKey.value++
  }

  function openBackdropLightbox() {
    const urls = backdropAssets.value.map((_, i) => getBackdropUrl(i)!)
    if (urls.length) lightbox.open(urls, backdropIdx.value)
    else {
      const src = useBackdropUrl(detail.value!.media_item)
      if (src) lightbox.open(src)
    }
  }

  return {
    showA,
    backdropA,
    backdropB,
    backdropIdx,
    carouselPaused,
    cycleKey,
    backdropAssets,
    getBackdropUrl,
    advanceBackdrop,
    retreatBackdrop,
    jumpToBackdrop,
    seedCarousel,
    openBackdropLightbox,
  }
}
