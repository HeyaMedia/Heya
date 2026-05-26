// Shared sizing for the virtualized responsive poster grid.
//
// Mimics the `.grid-posters` CSS rule (auto-fill, minmax(160px, 1fr)) but
// computed in JS so RecycleScroller knows the item dimensions ahead of time.
// Callers pass a container ref; we observe its width and derive column count
// + row height. `rows` chunks the items into virtual rows of `cols` items.

import type { Ref } from 'vue'

const MIN_CARD = 160
const COL_GAP = 18
const ROW_GAP = 22

interface Options {
  // Aspect ratio of the poster body (height / width). 2/3 posters → 1.5,
  // 1/1 covers → 1.0. Defaults to 1.5 (movie/TV poster).
  aspect?: number
  // Reserved px below the poster for title + sub + padding-top. ~43px for
  // the 13/11px font stack used everywhere.
  metaHeight?: number
}

export function usePosterGrid<T>(
  containerRef: Ref<HTMLElement | null | undefined>,
  items: Ref<readonly T[]>,
  opts: Options = {},
) {
  const aspect = opts.aspect ?? 1.5
  const metaHeight = opts.metaHeight ?? 43

  const { width } = useElementSize(containerRef)

  const cols = computed(() => {
    const w = width.value
    if (!w) return 6
    return Math.max(1, Math.floor((w + COL_GAP) / (MIN_CARD + COL_GAP)))
  })

  const cardWidth = computed(() => {
    const w = width.value
    if (!w) return MIN_CARD
    return (w - (cols.value - 1) * COL_GAP) / cols.value
  })

  const rowHeight = computed(() => Math.ceil(cardWidth.value * aspect) + metaHeight + ROW_GAP)

  const rows = computed(() => {
    const c = cols.value
    const src = items.value
    const out: { key: string; items: T[] }[] = []
    for (let i = 0; i < src.length; i += c) {
      out.push({ key: `r${i}`, items: src.slice(i, i + c) as T[] })
    }
    return out
  })

  return { cols, cardWidth, rowHeight, rows, colGap: COL_GAP }
}
