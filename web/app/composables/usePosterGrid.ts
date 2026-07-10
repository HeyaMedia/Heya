// Shared sizing for the virtualized responsive poster grid.
//
// Mimics the `.grid-posters` CSS rule (auto-fill, minmax(160px, 1fr)) but
// computed in JS so RecycleScroller knows the item dimensions ahead of time.
// Callers pass a container ref; we observe its width and derive column count
// + row height. `rows` chunks the items into virtual rows of `cols` items.
//
// Phone (<=720px, via useViewport().isPhone): mirrors the `.grid-posters`
// phone override in heya.css (minmax(110px,1fr), tighter gaps) — see
// docs/responsive-plan.md W3b. Callers whose CSS grid-row rule sets a fixed
// `column-gap`/`padding-bottom` (movies/tv/books index.vue) must add a
// matching `@media (max-width: 720px)` override using these same phone
// values, or the JS column math and the actual rendered gap will disagree.

import type { MaybeRefOrGetter, Ref } from 'vue'

const MIN_CARD = 160
const COL_GAP = 18
const ROW_GAP = 22

const MIN_CARD_PHONE = 105
const COL_GAP_PHONE = 10
const ROW_GAP_PHONE = 14

interface Options {
  // Aspect ratio of the poster body (height / width). 2/3 posters → 1.5,
  // 1/1 covers → 1.0. Defaults to 1.5 (movie/TV poster).
  aspect?: number
  // Reserved px below the poster for title + sub + padding-top. ~43px for
  // the 13/11px font stack used everywhere.
  metaHeight?: number
  // Desktop minimum card width — the FilterBar poster-size slider feeds a
  // ref here (default 160, the historical constant). Phones keep their own
  // fixed minimum: the slider's range would leave 1 giant or 4 unreadable
  // columns there.
  minCard?: MaybeRefOrGetter<number>
}

export function usePosterGrid<T>(
  containerRef: Ref<HTMLElement | null | undefined>,
  items: Ref<readonly T[]>,
  opts: Options = {},
) {
  const aspect = opts.aspect ?? 1.5
  const metaHeight = opts.metaHeight ?? 43

  const { width } = useElementSize(containerRef)
  const { isPhone } = useViewport()

  const minCard = computed(() => {
    if (isPhone.value) return MIN_CARD_PHONE
    return toValue(opts.minCard) || MIN_CARD
  })
  const colGap = computed(() => isPhone.value ? COL_GAP_PHONE : COL_GAP)
  const rowGap = computed(() => isPhone.value ? ROW_GAP_PHONE : ROW_GAP)

  const cols = computed(() => {
    const w = width.value
    if (!w) return 6
    return Math.max(1, Math.floor((w + colGap.value) / (minCard.value + colGap.value)))
  })

  const cardWidth = computed(() => {
    const w = width.value
    if (!w) return minCard.value
    return (w - (cols.value - 1) * colGap.value) / cols.value
  })

  const rowHeight = computed(() => Math.ceil(cardWidth.value * aspect) + metaHeight + rowGap.value)

  const rows = computed(() => {
    const c = cols.value
    const src = items.value
    const out: { key: string; items: T[] }[] = []
    for (let i = 0; i < src.length; i += c) {
      out.push({ key: `r${i}`, items: src.slice(i, i + c) as T[] })
    }
    return out
  })

  return { cols, cardWidth, rowHeight, rows, colGap }
}
