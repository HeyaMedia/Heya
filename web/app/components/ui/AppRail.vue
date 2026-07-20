<template>
  <div
    ref="scrollEl"
    class="app-rail"
    :class="{ 'rail-snap': snap }"
    :data-scroll-memory="memoryKey"
    @scroll.passive="onScroll"
  >
    <!-- Virtualized track: only the tiles inside the viewport (± overscan)
         exist in the DOM; each is absolutely positioned at its slot. The
         track's fixed width keeps the scrollbar honest for the full item
         count, so a 2000-deep rail scrubs like a plain overflow row.
         (Extracted from ContentRow — this is the one rail engine every
         horizontal list shares.) -->
    <div class="rail-track" :style="{ width: `${trackWidth}px`, height: `${tileHeight}px` }">
      <div
        v-for="v in visibleTiles"
        :key="keyFor(v.item, v.index)"
        class="rail-tile"
        :style="{ left: `${v.left}px`, width: `${tileW}px` }"
      >
        <slot :item="v.item" :index="v.index" :width="tileW" />
      </div>
      <div v-if="hasMore" class="rail-tail" :style="{ left: `${items.length * stride}px` }" aria-hidden="true">
        <span class="rail-tail-spin" :class="{ 'is-offscreen': tailOffscreen }" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts" generic="T">
const props = withDefaults(defineProps<{
  items: T[]
  /** Fixed tile width (desktop). All virtualization math is stride
   *  arithmetic on this — rails are uniform-width by design. */
  tileWidth?: number
  /** Phone tile width — rails collapse to one sensible size on phones. */
  phoneTileWidth?: number
  gap?: number
  phoneGap?: number
  /** Track height. Give either an aspect ("2/3", "1/1", "16/9") for
   *  image-box tiles whose captions bleed into the shadow padding (the
   *  ContentRow model), or an explicit tileHeight for odd tiles. */
  aspect?: string
  tileHeight?: number
  /** Stable history-restoration identity for scroll memory. */
  memoryKey?: string
  /** v-for key extractor; defaults to (item.key ?? item.id ?? index). */
  itemKey?: (item: T, index: number) => string | number
  /** More pages exist — show the tail spinner and emit `load-more` as the
   *  user nears the right edge. */
  hasMore?: boolean
  /** A page fetch is in flight; suppresses further load-more emits. */
  loadingMore?: boolean
  /** Opt into x-proximity scroll snap (the music-shelf feel). */
  snap?: boolean
}>(), {
  tileWidth: 168,
  phoneTileWidth: 140,
  gap: 18,
  phoneGap: 12,
})

const emit = defineEmits<{ 'load-more': [] }>()

const { isPhone } = useViewport()

// Tile geometry is the whole virtualization contract: fixed width + gap →
// slot i lives at i*stride, and the visible range is pure arithmetic on
// scrollLeft.
const tileW = computed(() => (isPhone.value ? props.phoneTileWidth : props.tileWidth))
const gapW = computed(() => (isPhone.value ? props.phoneGap : props.gap))
const stride = computed(() => tileW.value + gapW.value)
const tileHeight = computed(() => {
  if (props.tileHeight) return props.tileHeight
  const [w, h] = (props.aspect || '2/3').split('/').map(Number)
  return Math.round(tileW.value * ((h || 3) / (w || 2)))
})
const trackWidth = computed(() =>
  props.items.length * stride.value - (props.items.length ? gapW.value : 0)
  + (props.hasMore ? stride.value : 0))

const scrollEl = ref<HTMLElement>()
const scrollLeft = ref(0)
const viewportW = ref(0)

const OVERSCAN = 4
const visibleTiles = computed(() => {
  const s = stride.value
  const start = Math.max(0, Math.floor(scrollLeft.value / s) - OVERSCAN)
  const end = Math.min(props.items.length, Math.ceil((scrollLeft.value + viewportW.value) / s) + OVERSCAN)
  const out: { item: T; index: number; left: number }[] = []
  for (let i = start; i < end; i++) {
    out.push({ item: props.items[i]!, index: i, left: i * s })
  }
  return out
})

// The tail lives one slot past the last tile — same stride math as
// visibleTiles' window, just checked against a single index instead of
// collected into a range. Scrolled out of that window the spinner keeps
// compositing every frame for nothing; pausing it there is free.
const tailOffscreen = computed(() => {
  if (!props.hasMore) return false
  const s = stride.value
  const tailIndex = props.items.length
  const start = Math.max(0, Math.floor(scrollLeft.value / s) - OVERSCAN)
  const end = Math.ceil((scrollLeft.value + viewportW.value) / s) + OVERSCAN
  return tailIndex < start || tailIndex >= end
})

function keyFor(item: T, index: number): string | number {
  if (props.itemKey) return props.itemKey(item, index)
  const anyItem = item as { key?: string; id?: string | number }
  return anyItem.key ?? anyItem.id ?? index
}

let ro: ResizeObserver | null = null
onMounted(() => {
  if (!scrollEl.value) return
  viewportW.value = scrollEl.value.clientWidth
  // Scroll memory may have restored a position before we mounted.
  scrollLeft.value = scrollEl.value.scrollLeft
  ro = new ResizeObserver(() => {
    if (scrollEl.value) viewportW.value = scrollEl.value.clientWidth
  })
  ro.observe(scrollEl.value)
})
onBeforeUnmount(() => ro?.disconnect())

// Ask for the next page while the user still has ~8 tiles of runway, so the
// rail keeps flowing instead of hitting a wall. The watchEffect also covers
// the "first page doesn't even fill the viewport" case with no scroll at all.
const LOAD_AHEAD_TILES = 8
function maybeLoadMore() {
  if (!props.hasMore || props.loadingMore) return
  const remaining = trackWidth.value - (scrollLeft.value + viewportW.value)
  if (remaining < stride.value * LOAD_AHEAD_TILES) emit('load-more')
}
function onScroll() {
  if (scrollEl.value) scrollLeft.value = scrollEl.value.scrollLeft
  maybeLoadMore()
}
watchEffect(() => {
  // touch the reactive deps so a new page / resize / prop change re-checks
  void props.items.length
  void viewportW.value
  maybeLoadMore()
})

function scrollByDir(dir: number, step = 600) {
  if (!scrollEl.value) return
  scrollEl.value.scrollBy({ left: dir * step, behavior: 'smooth' })
}

/** Center tile i in the viewport (used by strips that highlight a "current"
 *  item — the tile may not be in the DOM yet, so scrollIntoView can't work;
 *  position math can). */
function scrollToIndex(i: number, behavior: ScrollBehavior = 'auto') {
  if (!scrollEl.value) return
  const left = Math.max(0, i * stride.value - (viewportW.value - tileW.value) / 2)
  scrollEl.value.scrollTo({ left, behavior })
}

/** Jump back to the rail's start (the hold-to-rewind affordance on left
 *  arrows). */
function scrollToStart() {
  scrollEl.value?.scrollTo({ left: 0, behavior: 'smooth' })
}

// Whether the rail actually overflows its viewport — consumers gate their
// scroll-arrow / expand chrome on this.
const overflows = computed(() => trackWidth.value > viewportW.value + 1)

defineExpose({ scrollByDir, scrollToIndex, scrollToStart, overflows })
</script>

<style scoped>
.app-rail {
  overflow-x: auto;
  overflow-y: hidden;
  /* Shadow room (Heya 2.0): a layout-neutral padding/negative-margin pair
     pushes the clip box out so the enlarged directional shadows + the -4px
     hover lift aren't sliced by overflow. Horizontal bleed tracks the page
     gutter (--page-pad-x) so the rail runs edge-to-edge WITHOUT overflowing
     the page sideways. The virtualizer's tile geometry is pure stride
     arithmetic (padding-agnostic), so the bigger padding only widens the
     clip box; overscan absorbs the few px of clientWidth the padding adds. */
  --rail-bleed: var(--page-pad-x, 40px);
  padding: 44px var(--rail-bleed) 130px;
  margin: -44px calc(-1 * var(--rail-bleed)) -130px;
  scroll-padding-left: var(--rail-bleed);
  scrollbar-width: none;
}
@media (max-width: 1100px) { .app-rail { --rail-bleed: 24px; } }
@media (max-width: 720px) {
  .app-rail {
    --rail-bleed: 12px;
    padding-top: 30px; padding-bottom: 100px;
    margin-top: -30px; margin-bottom: -100px;
  }
}
.app-rail::-webkit-scrollbar { display: none; }
.rail-snap { scroll-snap-type: x proximity; }

.rail-track { position: relative; }
.rail-tile { position: absolute; top: 0; }
.rail-snap .rail-tile { scroll-snap-align: start; }

.rail-tail {
  position: absolute;
  top: 0;
  bottom: 0;
  width: 80px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.rail-tail-spin {
  width: 22px;
  height: 22px;
  border-radius: 50%;
  border: 2px solid rgb(var(--ink) / 0.15);
  border-top-color: var(--gold);
  animation: rail-spin 0.8s linear infinite;
}
/* Scrolled outside the rail's virtualization window — freeze rather than
   spin somewhere nobody's looking. */
.rail-tail-spin.is-offscreen { animation-play-state: paused; }
@keyframes rail-spin {
  to { transform: rotate(360deg); }
}
</style>
