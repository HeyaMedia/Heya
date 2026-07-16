<template>
  <section v-if="items.length" class="content-row">
    <SectionHeader title="Continue Watching">
      <template #actions>
        <button class="scroll-btn" aria-label="Scroll left" @click="rail?.scrollByDir(-1)"><Icon name="chevleft" :size="16" /></button>
        <button class="scroll-btn" aria-label="Scroll right" @click="rail?.scrollByDir(1)"><Icon name="chevright" :size="16" /></button>
      </template>
    </SectionHeader>
    <!-- AppRail owns the virtualization + scroller chrome; this component is
         just the Continue Watching tile skin (progress bar, play overlay). -->
    <AppRail
      ref="rail"
      :items="items"
      :tile-width="280"
      :phone-tile-width="260"
      :gap="16"
      :phone-gap="12"
      aspect="16/9"
      memory-key="continue-watching"
      snap
    >
      <template #default="{ item, index }">
        <div class="cw-tile card-tile" @click="$emit('play', item)">
          <MediaCard
            :idx="index"
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
      </template>
    </AppRail>
  </section>
</template>

<script setup lang="ts">
import type { ContinueWatchingItem } from '~/types/home'

defineProps<{ items: ContinueWatchingItem[] }>()
defineEmits<{ play: [item: ContinueWatchingItem] }>()

// AppRail is generic, so InstanceType<> can't name it — type the exposed
// surface directly (same pattern as ContentRow).
const rail = ref<{ scrollByDir: (dir: number, step?: number) => void; overflows: boolean } | null>(null)

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

.scroll-btn {
  width: 32px; height: 32px; border-radius: 50%;
  display: flex; align-items: center; justify-content: center;
  background: rgb(var(--ink) / 0.06); border: 1px solid var(--border);
  color: var(--fg-2); transition: all 0.15s;
}
.scroll-btn:hover { background: rgb(var(--ink) / 0.12); color: var(--fg-0); }

.cw-tile {
  width: 100%; cursor: pointer;
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
</style>
