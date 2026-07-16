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
    >
      <!-- Mouse-click plays the next episode; the title deep-links to the
           series detail page (same interaction model as Continue Watching) so
           you can reach the show without starting playback. `title-to`
           re-enables pointer events on just the title inside MediaCard's
           overlay, and its @click.stop keeps that click from also firing play.
           The tile is a plain click target (no tabindex/role) on purpose:
           the only keyboard-focusable thing is the title link, so Enter on it
           navigates without a competing tile handler double-firing play. -->
      <template #default="{ item, index }">
        <div class="un-tile card-tile" @click="$emit('play', item)">
          <MediaCard
            :idx="index"
            :src="usePosterUrl(item)"
            :title="item.title"
            :title-to="detailUrl(item)"
            :subtitle="item.episode_label"
            aspect="2/3"
          >
            <template #badges>
              <div class="un-play-overlay">
                <div class="un-play-btn"><Icon name="play" :size="18" /></div>
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

defineProps<{ items: UpNextItem[] }>()
defineEmits<{ play: [item: UpNextItem] }>()

// AppRail is generic, so InstanceType<> can't name it — type the exposed
// surface directly (same pattern as ContentRow).
const rail = ref<{ scrollByDir: (dir: number, step?: number) => void; scrollToStart: () => void; overflows: boolean } | null>(null)

// Up Next is always the next TV episode, so the title links to the series
// detail page. The tile itself still plays; this is the escape hatch to the
// show without starting playback.
function detailUrl(item: UpNextItem): string {
  return mediaUrl({ id: item.id, title: item.title, slug: item.slug, media_type: 'tv' })
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

/* Play overlay lives in MediaCard's badges slot — covers the whole art and
   fades in on tile hover/focus. Above the gradient, below the title text via
   z-index (MediaCard's .mediac-info is z-index 3 and paints later). */
.un-play-overlay {
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center;
  background: rgba(0,0,0,0.35); /* on artwork — stays literal */
  opacity: 0;
  transition: opacity 0.15s;
  pointer-events: none;
  z-index: 3;
}
.un-tile:hover .un-play-overlay { opacity: 1; }
.un-play-btn {
  width: 44px; height: 44px; border-radius: 50%;
  background: rgba(255,255,255,0.18); /* on artwork — stays literal */
  backdrop-filter: blur(4px);
  display: flex; align-items: center; justify-content: center;
  color: #fff;
}

/* Touch: swipe replaces the mouse-only scroll arrows. */
@media (pointer: coarse) {
  .scroll-btn { display: none; }
}
</style>
