<template>
  <section v-if="items.length" class="content-row">
    <SectionHeader title="Continue Watching">
      <template #actions>
        <button class="scroll-btn" @click="scrollBy(-1)"><Icon name="chevleft" :size="16" /></button>
        <button class="scroll-btn" @click="scrollBy(1)"><Icon name="chevright" :size="16" /></button>
      </template>
    </SectionHeader>
    <div class="row-scroll" ref="scrollEl">
      <div
        v-for="(item, i) in items"
        :key="item.id"
        class="cw-tile"
        @click="$emit('play', item)"
      >
        <MediaCard
          :idx="i"
          :src="thumbUrl(item)"
          aspect="16/9"
          :title="item.title"
          :title-to="detailUrl(item)"
          :subtitle="bottomLine(item)"
          :badge-tl="episodeBadge(item)"
          :badge-tr="formatRemaining(item)"
          :badge-tr-gold="false"
          :progress-pct="progressPct(item)"
        >
          <template #badges>
            <div class="cw-play-overlay">
              <div class="cw-play-btn"><Icon name="play" :size="16" /></div>
            </div>
          </template>
        </MediaCard>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
export interface ContinueWatchingItem {
  id: number
  entity_type: string
  entity_id: number
  progress_seconds: number
  total_seconds: number
  media_item_id: number
  media_item_public_id?: string
  title: string
  poster_path: string
  slug: string
  media_type: string
  episode_number?: number
  episode_title?: string
  season_number?: number
  // file_id is enriched by the backend so the FE can navigate to /watch
  // without a second lookup. 0 when the file can't be resolved (deleted /
  // mismatched parse) — the FE should hide or disable the tile in that case.
  file_id: number
  file_public_id?: string
}

defineProps<{ items: ContinueWatchingItem[] }>()
defineEmits<{ play: [item: ContinueWatchingItem] }>()

const scrollEl = ref<HTMLElement>()

function scrollBy(dir: number) {
  if (!scrollEl.value) return
  scrollEl.value.scrollBy({ left: dir * 600, behavior: 'smooth' })
}

function thumbUrl(item: ContinueWatchingItem): string {
  if (item.entity_type === 'episode' && item.season_number && item.episode_number) {
    const label = `s${String(item.season_number).padStart(2, '0')}e${String(item.episode_number).padStart(2, '0')}`
    return `/api/media/${useMediaImageKey({ id: item.media_item_id, public_id: item.media_item_public_id })}/image/still?label=${label}`
  }
  return `/api/media/${useMediaImageKey({ id: item.media_item_id, public_id: item.media_item_public_id })}/image/backdrop`
}

function progressPct(item: ContinueWatchingItem): number {
  if (!item.total_seconds) return 0
  return Math.min(95, Math.round((item.progress_seconds / item.total_seconds) * 100))
}

function formatRemaining(item: ContinueWatchingItem): string {
  const remaining = item.total_seconds - item.progress_seconds
  if (remaining <= 0) return ''
  const m = Math.ceil(remaining / 60)
  if (m >= 60) return `${Math.floor(m / 60)}h ${m % 60}m left`
  return `${m}m left`
}

function episodeBadge(item: ContinueWatchingItem): string {
  if (item.entity_type === 'episode' && item.season_number && item.episode_number) {
    return `S${String(item.season_number).padStart(2, '0')}E${String(item.episode_number).padStart(2, '0')}`
  }
  return ''
}

function bottomLine(item: ContinueWatchingItem): string {
  if (item.entity_type === 'episode' && item.episode_title) return item.episode_title
  return ''
}

// The tile itself opens the player; the title deep-links to the entity
// (series for episodes, the movie otherwise) so you can reach the detail
// page without playing.
function detailUrl(item: ContinueWatchingItem): string {
  return mediaUrl({ id: item.media_item_id, title: item.title, slug: item.slug, media_type: item.media_type })
}
</script>

<style scoped>
.content-row { margin-bottom: 40px; }

.row-scroll {
  display: flex; gap: 16px;
  overflow-x: auto; overflow-y: hidden;
  scroll-snap-type: x mandatory;
  scroll-padding-left: 48px; /* snap to the content edge, not the shadow-room padding */
  /* Layout-neutral clip-edge expansion so card shadows aren't cut off. */
  padding: 12px 48px 72px;
  margin: -12px -48px -68px;
  scrollbar-width: none;
}
.row-scroll::-webkit-scrollbar { display: none; }
.row-scroll > * { scroll-snap-align: start; }

.scroll-btn {
  width: 32px; height: 32px; border-radius: 50%;
  display: flex; align-items: center; justify-content: center;
  background: rgb(var(--ink) / 0.06); border: 1px solid var(--border);
  color: var(--fg-2); transition: all 0.15s;
}
.scroll-btn:hover { background: rgb(var(--ink) / 0.12); color: var(--fg-0); }

.cw-tile {
  width: 280px; flex-shrink: 0; cursor: pointer;
}

/* Play overlay sits in MediaCard's badges slot — covers the full image,
   becomes visible only on hover. Above the gradient via z-index. */
.cw-play-overlay {
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center;
  background: rgba(0,0,0,0.3); /* on artwork — stays literal */
  opacity: 0; transition: opacity 0.15s;
  z-index: 3; pointer-events: none;
}
.cw-tile:hover .cw-play-overlay { opacity: 1; }
.cw-play-btn {
  width: 40px; height: 40px; border-radius: 50%;
  background: rgba(255,255,255,0.18); backdrop-filter: blur(8px); /* on artwork — stays literal */
  display: flex; align-items: center; justify-content: center; color: #fff;
}

/* Touch: swipe replaces the mouse-only scroll arrows. */
@media (pointer: coarse) {
  .scroll-btn { display: none; }
}

/* Phone: the 280px 16/9 backdrop card was rendering at ~85% of a 390px
   viewport (280 / (390 - page-pad) after the page's horizontal padding) —
   nearly the full screen for a "rail" tile. Cap wide episode cards at 70vw. */
@media (max-width: 720px) {
  .row-scroll { gap: 12px; }
  .cw-tile { width: min(70vw, 300px); }
}
</style>
