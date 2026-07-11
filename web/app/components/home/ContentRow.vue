<template>
  <section class="content-row">
    <SectionHeader :title="title" :subtitle="subtitle">
      <template #actions>
        <span v-if="more" class="more" @click="$emit('more')">{{ more }}</span>
        <button class="scroll-btn" @click="scrollBy(-1)"><Icon name="chevleft" :size="16" /></button>
        <button class="scroll-btn" @click="scrollBy(1)"><Icon name="chevright" :size="16" /></button>
      </template>
    </SectionHeader>
    <div class="row-scroll" ref="scrollEl">
      <AppContextMenu
        v-for="(item, i) in items"
        :key="item.key ?? item.id"
        :items="contextMenuItems(item)"
        :disabled="!contextItems || contextMenuItems(item).length === 0"
      >
        <div
          class="card-tile"
          :class="{ unavailable: item.available === false }"
          :style="{ width: `${tileWidth || 168}px`, flexShrink: 0 }"
          @click="item.available !== false && $emit('tile', item)"
          @keydown.enter.prevent="item.available !== false && $emit('tile', item)"
          @pointerenter="scheduleIntent(item)"
          @pointerleave="cancelIntent"
          @focus="signalIntent(item)"
          @pointerdown="signalIntent(item)"
          :tabindex="item.available === false ? -1 : 0"
          role="link"
        >
          <MediaCard
            :idx="i"
            :src="item.poster_src ?? usePosterUrl(item)"
            :title="item.title"
            :subtitle="item.year || item.sub"
            :aspect="aspect || '2/3'"
            :missing="item.available === false"
          />
        </div>
      </AppContextMenu>
    </div>
  </section>
</template>

<script setup lang="ts">
import type { ContextMenuItem, MediaItem } from '~~/shared/types'

type RowItem = MediaItem & { sub?: string; poster_src?: string; key?: string }

const props = defineProps<{
  title: string
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
}>()

const emit = defineEmits<{
  tile: [item: MediaItem]
  more: []
  intent: [item: MediaItem]
}>()

const scrollEl = ref<HTMLElement>()
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

function scrollBy(dir: number) {
  if (!scrollEl.value) return
  scrollEl.value.scrollBy({ left: dir * 600, behavior: 'smooth' })
}
</script>

<style scoped>
.content-row { margin-bottom: 40px; }


.row-scroll {
  display: flex;
  gap: 18px;
  overflow-x: auto;
  overflow-y: hidden;
  scroll-snap-type: x mandatory;
  scroll-padding-left: 48px; /* snap to the content edge, not the shadow-room padding */
  /* Padding/negative-margin pair: layout-neutral, but moves the clip edge
     outward so card drop shadows (--shadow-card) aren't cut off. */
  padding: 12px 48px 72px;
  margin: -12px -48px -68px;
  scrollbar-width: none;
}
.row-scroll::-webkit-scrollbar { display: none; }
.row-scroll > * { scroll-snap-align: start; }

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

/* Phone: 168px desktop tiles (both 2/3 posters and 1/1 covers) are too wide
   for a 390px screen — collapse to ~140px. tileWidth is a literal inline
   style so this needs !important to win. */
@media (max-width: 720px) {
  .row-scroll { gap: 12px; }
  .card-tile { width: 140px !important; }
}
</style>
