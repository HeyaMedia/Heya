<template>
  <section class="content-row">
    <SectionHeader :title="title" :subtitle="subtitle">
      <template #actions>
        <button v-if="more" class="more" @click="$emit('more')">{{ more }}</button>
        <button class="scroll-btn" aria-label="Scroll left" @click="scrollByDir(-1)"><Icon name="chevleft" :size="16" /></button>
        <button class="scroll-btn" aria-label="Scroll right" @click="scrollByDir(1)"><Icon name="chevright" :size="16" /></button>
      </template>
    </SectionHeader>
    <div ref="scrollEl" class="row-scroll" :data-scroll-memory="memoryKey || title" @scroll.passive="onScroll">
      <!-- Virtualized track: only the tiles inside the viewport (± overscan)
           exist in the DOM; each is absolutely positioned at its slot. The
           track's fixed width keeps the scrollbar honest for the full item
           count, so a 2000-deep rail scrubs like a plain overflow row. -->
      <div class="row-track" :style="{ width: `${trackWidth}px`, height: `${tileHeight}px` }">
        <AppContextMenu
          v-for="v in visibleTiles"
          :key="v.item.key ?? v.item.id"
          :items="contextMenuItems(v.item)"
          :disabled="!contextItems || contextMenuItems(v.item).length === 0"
        >
          <div
            class="card-tile"
            :class="{ unavailable: v.item.available === false }"
            :style="{ left: `${v.left}px`, width: `${tileW}px` }"
            :tabindex="v.item.available === false ? -1 : 0"
            role="link"
            @click="v.item.available !== false && $emit('tile', v.item)"
            @keydown.enter.prevent="v.item.available !== false && $emit('tile', v.item)"
            @pointerenter="scheduleIntent(v.item)"
            @pointerleave="cancelIntent"
            @focus="signalIntent(v.item)"
            @pointerdown="signalIntent(v.item)"
          >
            <MediaCard
              :idx="v.index"
              :src="v.item.poster_src ?? usePosterUrl(v.item)"
              :title="v.item.title"
              :subtitle="v.item.year || v.item.sub"
              :aspect="aspect || '2/3'"
              :missing="v.item.available === false"
              :badge-br="showAdded ? timeAgoShort(v.item.added_at ?? v.item.created_at) : ''"
            />
          </div>
        </AppContextMenu>
        <div v-if="hasMore" class="rail-tail" :style="{ left: `${items.length * stride}px` }" aria-hidden="true">
          <span class="rail-tail-spin" />
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import type { ContextMenuItem, MediaItem } from '~~/shared/types'

type RowItem = MediaItem & {
  sub?: string
  poster_src?: string
  key?: string
  // ISO string (service-formatted) or pgtype.Timestamptz object (raw sqlc rows)
  added_at?: string | { Time?: string; Valid?: boolean }
}

const props = defineProps<{
  title: string
  /** Stable history-restoration identity. Defaults to the visible title. */
  memoryKey?: string
  subtitle?: string
  // `poster_src` overrides the default `/api/media/{id}/image/poster` lookup —
  // needed for album rows whose covers live under a different endpoint.
  // `key` overrides the v-for key — needed for rows where the same media
  // item can appear more than once (e.g. two episode drops of one show).
  items: RowItem[]
  tileWidth?: number
  aspect?: string
  more?: string
  contextItems?: (item: RowItem) => ContextMenuItem[]
  /** More pages exist — show the tail spinner and emit `load-more` as the
   *  user nears the right edge. */
  hasMore?: boolean
  /** A page fetch is in flight; suppresses further load-more emits. */
  loadingMore?: boolean
  /** Paint a "3d ago" chip (added_at ?? created_at) on each poster. */
  showAdded?: boolean
}>()

const emit = defineEmits<{
  tile: [item: MediaItem]
  more: []
  intent: [item: MediaItem]
  'load-more': []
}>()

const { isPhone } = useViewport()

// Tile geometry is the whole virtualization contract: fixed width + gap →
// slot i lives at i*stride, and the visible range is pure arithmetic on
// scrollLeft. Phone tiles collapse to 140px (used to be a CSS !important
// override — the JS math has to know the real width, so it lives here now).
const tileW = computed(() => (isPhone.value ? 140 : props.tileWidth || 168))
const gap = computed(() => (isPhone.value ? 12 : 18))
const stride = computed(() => tileW.value + gap.value)
const tileHeight = computed(() => {
  const [w, h] = (props.aspect || '2/3').split('/').map(Number)
  return Math.round(tileW.value * ((h || 3) / (w || 2)))
})
const trackWidth = computed(() =>
  props.items.length * stride.value - (props.items.length ? gap.value : 0)
  + (props.hasMore ? stride.value : 0))

const scrollEl = ref<HTMLElement>()
const scrollLeft = ref(0)
const viewportW = ref(0)

const OVERSCAN = 4
const visibleTiles = computed(() => {
  const s = stride.value
  const start = Math.max(0, Math.floor(scrollLeft.value / s) - OVERSCAN)
  const end = Math.min(props.items.length, Math.ceil((scrollLeft.value + viewportW.value) / s) + OVERSCAN)
  const out: { item: RowItem; index: number; left: number }[] = []
  for (let i = start; i < end; i++) {
    out.push({ item: props.items[i]!, index: i, left: i * s })
  }
  return out
})

let ro: ResizeObserver | null = null
onMounted(() => {
  if (!scrollEl.value) return
  viewportW.value = scrollEl.value.clientWidth
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

let intentTimer: ReturnType<typeof setTimeout> | null = null

function cancelIntent() {
  if (!intentTimer) return
  clearTimeout(intentTimer)
  intentTimer = null
}

function signalIntent(item: MediaItem) {
  cancelIntent()
  if (item.available !== false) emit('intent', item)
}

function scheduleIntent(item: MediaItem) {
  cancelIntent()
  intentTimer = setTimeout(() => signalIntent(item), 100)
}

onScopeDispose(cancelIntent)

function contextMenuItems(item: RowItem): ContextMenuItem[] {
  return props.contextItems?.(item) ?? []
}

function scrollByDir(dir: number) {
  if (!scrollEl.value) return
  scrollEl.value.scrollBy({ left: dir * 600, behavior: 'smooth' })
}
</script>

<style scoped>
.content-row { margin-bottom: 40px; }

.row-scroll {
  overflow-x: auto;
  overflow-y: hidden;
  /* Shadow room (Heya 2.0): a layout-neutral padding/negative-margin pair pushes
     the clip box out so the enlarged directional shadows + the -4px hover lift
     aren't sliced by overflow. Horizontal bleed tracks the page gutter
     (--page-pad-x) so the rail runs edge-to-edge WITHOUT ever overflowing the
     page sideways. The virtualizer's tile geometry is pure stride arithmetic
     (padding-agnostic — see visibleTiles), so the bigger padding only widens
     the clip box; overscan absorbs the few px of clientWidth the padding adds. */
  --rail-bleed: var(--page-pad-x, 40px);
  padding: 44px var(--rail-bleed) 130px;
  margin: -44px calc(-1 * var(--rail-bleed)) -130px;
  scrollbar-width: none;
}
@media (max-width: 1100px) { .row-scroll { --rail-bleed: 24px; } }
@media (max-width: 720px) {
  .row-scroll {
    --rail-bleed: 12px;
    padding-top: 30px; padding-bottom: 100px;
    margin-top: -30px; margin-bottom: -100px;
  }
}
.row-scroll::-webkit-scrollbar { display: none; }

.row-track { position: relative; }
.row-track > .card-tile,
.row-track :deep(.card-tile) { position: absolute; top: 0; }

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
@keyframes rail-spin {
  to { transform: rotate(360deg); }
}

.scroll-btn {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgb(var(--ink) / 0.06);
  border: 1px solid var(--border);
  color: var(--fg-2);
  transition: all 0.15s;
}
.scroll-btn:hover {
  background: rgb(var(--ink) / 0.12);
  color: var(--fg-0);
}
.unavailable { opacity: 0.4; cursor: default !important; }

/* Touch: swipe replaces the mouse-only scroll arrows. */
@media (pointer: coarse) {
  .scroll-btn { display: none; }
}
</style>
