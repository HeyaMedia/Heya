<!--
  VirtualPosterGrid — full-length virtualized poster/cover grid over a
  random-access catalog (useVirtualCatalog). The grid is sized to `total`
  from the first page, so the page scrollbar spans the entire dataset;
  RecycleScroller's @update reports the rendered row range and the parent
  fetches those pages (emit: range). Unloaded cells render as pulsing
  skeleton tiles that keep the exact card footprint.

  Column math mirrors `.grid-posters` (auto-fill minmax) via the same
  constants usePosterGrid uses, so a converted page keeps its exact tile
  sizing and gaps — only the plumbing changes. Tiles render through the
  default scoped slot: `<template #default="{ item, index }">`.
-->
<template>
  <div ref="wrapEl" class="vpg">
    <!-- Wait for a real container width before mounting the scroller: with
         width 0 the column fallback (6) pairs with a wrong fixed item-size
         and rows overlap for a beat — the blown-up-posters race the old
         genre page hit with JS-measured grids. -->
    <RecycleScroller
      v-if="width > 0 && gridRows.length"
      :items="gridRows"
      :item-size="rowHeight"
      key-field="key"
      page-mode
      :buffer="600"
      @update="onUpdate"
      v-slot="{ item: row }"
    >
      <div
        class="vpg-row"
        :style="{
          gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))`,
          columnGap: `${colGap}px`,
          paddingBottom: `${rowGap}px`,
        }"
      >
        <template v-for="cell in row.cells" :key="cell.index">
          <slot v-if="cell.item !== undefined" :item="cell.item" :index="cell.index" />
          <div v-else class="vpg-skel" :style="{ aspectRatio: aspectCss }" />
        </template>
      </div>
    </RecycleScroller>
  </div>
</template>

<script setup lang="ts" generic="T">
const props = withDefaults(defineProps<{
  /** Full dataset size — sizes the scroll track. */
  total: number
  /** Random-access lookup into the catalog (undefined = not loaded yet). */
  itemAt: (index: number) => T | undefined
  /** Poster height/width ratio: 1.5 for 2/3 posters, 1 for square covers. */
  aspect?: number
  /** Reserved px under the poster — 0 for overlay-caption cards. */
  metaHeight?: number
  /** Desktop minimum card width (matches --tile-min / .grid-posters). */
  minCard?: number
}>(), {
  aspect: 1.5,
  metaHeight: 0,
  minCard: 160,
})

const emit = defineEmits<{ range: [start: number, end: number] }>()

const wrapEl = ref<HTMLElement | null>(null)

// Same numbers as usePosterGrid / .grid-posters so tile sizing is identical
// to the CSS auto-fill grid this replaces.
const { width } = useElementSize(wrapEl)
const { isPhone } = useViewport()
const minCard = computed(() => (isPhone.value ? 105 : props.minCard))
const colGap = computed(() => (isPhone.value ? 12 : 18))
const rowGap = computed(() => (isPhone.value ? 14 : 22))

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
const rowHeight = computed(() =>
  Math.ceil(cardWidth.value * props.aspect) + props.metaHeight + rowGap.value)

const aspectCss = computed(() => `1 / ${props.aspect}`)

interface Cell { index: number; item: T | undefined }
const gridRows = computed(() => {
  const c = cols.value
  const rowCount = Math.ceil(props.total / c)
  const out: { key: string; cells: Cell[] }[] = []
  for (let r = 0; r < rowCount; r++) {
    const cells: Cell[] = []
    for (let i = r * c; i < Math.min((r + 1) * c, props.total); i++) {
      cells.push({ index: i, item: props.itemAt(i) })
    }
    out.push({ key: `r${r}`, cells })
  }
  return out
})

// RecycleScroller reports the RENDERED row range (buffer included) — convert
// to item space and let the parent fetch whatever pages that touches.
function onUpdate(startRow: number, endRow: number) {
  const c = cols.value
  emit('range', startRow * c, Math.min(props.total - 1, endRow * c + c - 1))
}
</script>

<style scoped>
.vpg-row { display: grid; }

.vpg-skel {
  background: var(--bg-3);
  border-radius: var(--r-md);
  animation: vpg-pulse 1.5s ease-in-out infinite;
}
@keyframes vpg-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.55; }
}
</style>
