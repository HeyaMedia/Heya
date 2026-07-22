<template>
  <section v-if="items.length" class="content-row">
    <SectionHeader title="Up Next" subtitle="Pick up where you left off">
      <template #actions>
        <AppHoldButton class="scroll-btn" aria-label="Scroll left" title="Hold to jump to start" @click="rail?.scrollByDir(-1)" @hold="rail?.scrollToStart()"><Icon name="chevleft" :size="16" /></AppHoldButton>
        <button class="scroll-btn" aria-label="Scroll right" @click="rail?.scrollByDir(1)"><Icon name="chevright" :size="16" /></button>
      </template>
    </SectionHeader>
    <!-- AppRail owns the virtualization + scroller chrome; this component is
         just the Up Next tile skin. -->
    <AppRail
      ref="rail"
      :items="items"
      :tile-width="168"
      :phone-tile-width="140"
      :gap="18"
      :phone-gap="12"
      aspect="2/3"
      memory-key="up-next"
      snap
      :has-more="hasMore"
      :loading-more="loadingMore"
      @load-more="$emit('load-more')"
    >
      <!-- The card is the episode-page navigation surface. Playback is a
           separate, explicit action layered over the artwork: mouse users see
           it on hover/focus, while touch users keep it visible because they
           have no hover state. The full-card link and play button are siblings
           in the artwork slot, avoiding nested interactive controls. -->
      <template #default="{ item, index }">
        <div class="un-tile card-tile">
          <MediaCard
            :idx="index"
            :src="usePosterUrl(item)"
            :title="item.title"
            :subtitle="item.episode_label"
            aspect="2/3"
          >
            <template #badges>
              <NuxtLink
                :to="detailUrl(item)"
                class="un-detail-link"
                :aria-label="episodeLinkLabel(item)"
              />
              <div class="un-play-overlay">
                <button
                  type="button"
                  class="un-play-btn"
                  :aria-label="`Play ${item.episode_label} of ${item.title}`"
                  @click.stop="$emit('play', item)"
                >
                  <Icon name="play" :size="18" />
                </button>
              </div>
            </template>
          </MediaCard>
        </div>
      </template>
    </AppRail>
  </section>
</template>

<script setup lang="ts">
import type { UpNextItem } from '~/types/home'

defineProps<{ items: UpNextItem[]; hasMore?: boolean; loadingMore?: boolean }>()
defineEmits<{ play: [item: UpNextItem]; 'load-more': [] }>()

// AppRail is generic, so InstanceType<> can't name it — type the exposed
// surface directly (same pattern as ContentRow).
const rail = ref<{ scrollByDir: (dir: number, step?: number) => void; scrollToStart: () => void; overflows: boolean } | null>(null)

// Up Next rows are episode-specific. Route the card to the episode rather
// than dropping the user at the parent series; season zero uses the route's
// human-readable `specials` segment, matching the season/episode pages.
function detailUrl(item: UpNextItem): string {
  return episodeUrl(item)
}

function episodeLinkLabel(item: UpNextItem): string {
  return `Open ${item.title} ${item.episode_label}`
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

.un-tile { width: 100%; cursor: pointer; }
.un-detail-link {
  position: absolute;
  inset: 0;
  z-index: 3;
  border-radius: inherit;
}
.un-detail-link:focus-visible {
  outline: 2px solid var(--gold);
  outline-offset: -2px;
}

/* Play overlay lives in MediaCard's badges slot and fades in over the art.
   Only the button accepts pointer events; every other pixel falls through to
   the episode-page card action. */
.un-play-overlay {
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center;
  background: rgba(0,0,0,0.35); /* on artwork — stays literal */
  opacity: 0;
  transition: opacity 0.15s;
  pointer-events: none;
  z-index: 4;
}
.un-tile:hover .un-play-overlay,
.un-tile:focus-within .un-play-overlay { opacity: 1; }
.un-play-btn {
  width: 44px; height: 44px; border-radius: 50%;
  background: rgba(255,255,255,0.18); /* on artwork — stays literal */
  border: 1px solid rgba(255,255,255,0.25);
  backdrop-filter: blur(4px);
  display: flex; align-items: center; justify-content: center;
  color: #fff;
  pointer-events: auto;
  cursor: pointer;
  transition: transform 0.15s, background 0.15s;
}
.un-play-btn:hover { transform: scale(1.08); background: rgba(255,255,255,0.28); }
.un-play-btn:focus-visible { outline: 2px solid #fff; outline-offset: 3px; }

/* Touch: swipe replaces the mouse-only scroll arrows. */
@media (pointer: coarse) {
  .scroll-btn { display: none; }
  .un-play-overlay { opacity: 1; background: transparent; }
}
</style>
