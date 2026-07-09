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

/**
 * Crossfade backdrop carousel for detail-page heroes.
 *
 * Owns the A/B image pair, the 8s advance timer with pause/resume (used by
 * the `.bd-indicators` hover), indicator jumps, and the backdrop lightbox.
 * Call `seedCarousel()` whenever the detail payload arrives/changes — the
 * caller decides when (typically inside its `watch(detail)` / `onMounted`).
 * Timer cleanup is registered on the calling component's `onUnmounted`.
 */
export function useBackdropCarousel(detail: Ref<MediaDetail | null>, opts: BackdropCarouselOptions = {}) {
  const lightbox = useLightbox()

  const showA = ref(true)
  const backdropA = ref<string | null>(null)
  const backdropB = ref<string | null>(null)
  const backdropIdx = ref(0)
  const carouselPaused = ref(false)

  const BACKDROP_INTERVAL = 8000
  let bdTimeout: ReturnType<typeof setTimeout> | null = null
  let bdStart = 0
  let bdRemaining = BACKDROP_INTERVAL

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

  async function advanceBackdrop() {
    if (backdropAssets.value.length <= 1) return
    backdropIdx.value = (backdropIdx.value + 1) % backdropAssets.value.length
    const url = getBackdropUrl(backdropIdx.value)
    if (showA.value) { backdropB.value = url } else { backdropA.value = url }
    await nextTick()
    showA.value = !showA.value
  }

  function startCarouselTimer() {
    bdStart = Date.now()
    bdRemaining = BACKDROP_INTERVAL
    bdTimeout = setTimeout(() => {
      advanceBackdrop()
      startCarouselTimer()
    }, BACKDROP_INTERVAL)
  }

  function pauseCarousel() {
    carouselPaused.value = true
    if (bdTimeout) clearTimeout(bdTimeout)
    bdRemaining -= Date.now() - bdStart
  }

  function resumeCarousel() {
    carouselPaused.value = false
    bdStart = Date.now()
    bdTimeout = setTimeout(() => {
      advanceBackdrop()
      startCarouselTimer()
    }, bdRemaining)
  }

  function jumpToBackdrop(idx: number) {
    if (idx === backdropIdx.value) return
    if (bdTimeout) clearTimeout(bdTimeout)
    backdropIdx.value = idx
    const url = getBackdropUrl(idx)
    if (showA.value) { backdropB.value = url } else { backdropA.value = url }
    showA.value = !showA.value
    if (!carouselPaused.value) startCarouselTimer()
  }

  /** (Re)seed the A/B pair from the current detail and (re)start the timer. */
  function seedCarousel() {
    if (bdTimeout) clearTimeout(bdTimeout)
    backdropA.value = getBackdropUrl(0)
    if (opts.preloadSecond) {
      if (backdropAssets.value.length > 1) {
        backdropB.value = getBackdropUrl(1)
        startCarouselTimer()
      }
    } else {
      backdropB.value = getBackdropUrl(0)
      if (backdropAssets.value.length > 1) startCarouselTimer()
    }
  }

  function openBackdropLightbox() {
    const urls = backdropAssets.value.map((_, i) => getBackdropUrl(i)!)
    if (urls.length) lightbox.open(urls, backdropIdx.value)
    else {
      const src = useBackdropUrl(detail.value!.media_item)
      if (src) lightbox.open(src)
    }
  }

  onUnmounted(() => { if (bdTimeout) clearTimeout(bdTimeout) })

  return {
    showA,
    backdropA,
    backdropB,
    backdropIdx,
    carouselPaused,
    backdropAssets,
    getBackdropUrl,
    pauseCarousel,
    resumeCarousel,
    jumpToBackdrop,
    seedCarousel,
    openBackdropLightbox,
  }
}
