<template>
  <section class="content-row">
    <SectionHeader :title="title" :subtitle="subtitle">
      <template #actions>
        <button v-if="more" class="more" @click="$emit('more')">{{ more }}</button>
        <button class="scroll-btn" aria-label="Scroll left" @click="rail?.scrollByDir(-1)"><Icon name="chevleft" :size="16" /></button>
        <button class="scroll-btn" aria-label="Scroll right" @click="rail?.scrollByDir(1)"><Icon name="chevright" :size="16" /></button>
      </template>
    </SectionHeader>
    <!-- AppRail owns the virtualization: fixed-stride tiles, honest scrollbar,
         tail spinner, load-ahead. This component is just the media-card skin. -->
    <AppRail
      ref="rail"
      :items="items"
      :tile-width="tileWidth || 168"
      :aspect="aspect || '2/3'"
      :memory-key="memoryKey || title"
      :has-more="hasMore"
      :loading-more="loadingMore"
      @load-more="$emit('load-more')"
    >
      <template #default="{ item, index }">
        <AppContextMenu
          :items="contextMenuItems(item)"
          :disabled="!contextItems || contextMenuItems(item).length === 0"
        >
          <div
            class="card-tile"
            :class="{ unavailable: item.available === false }"
            :tabindex="item.available === false ? -1 : 0"
            role="link"
            @click="item.available !== false && $emit('tile', item)"
            @keydown.enter.prevent="item.available !== false && $emit('tile', item)"
            @pointerenter="scheduleIntent(item)"
            @pointerleave="cancelIntent"
            @focus="signalIntent(item)"
            @pointerdown="signalIntent(item)"
          >
            <MediaCard
              :idx="index"
              :src="item.poster_src ?? usePosterUrl(item)"
              :title="item.title"
              :subtitle="item.year || item.sub"
              :aspect="aspect || '2/3'"
              :missing="item.available === false"
              :badge-br="showAdded ? timeAgoShort(item.added_at ?? item.created_at) : ''"
            />
          </div>
        </AppContextMenu>
      </template>
    </AppRail>
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

// AppRail is generic, so InstanceType<> can't name it — type the exposed
// surface directly.
const rail = ref<{ scrollByDir: (dir: number) => void } | null>(null)

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
</script>

<style scoped>
.content-row { margin-bottom: 40px; }

.card-tile { width: 100%; }
.unavailable { opacity: 0.4; cursor: default !important; }

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

/* Touch: swipe replaces the mouse-only scroll arrows. */
@media (pointer: coarse) {
  .scroll-btn { display: none; }
}
</style>
