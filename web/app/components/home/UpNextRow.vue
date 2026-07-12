<template>
  <section v-if="items.length" class="content-row">
    <SectionHeader title="Up Next" subtitle="Pick up where you left off">
      <template #actions>
        <button class="scroll-btn" aria-label="Scroll left" @click="scrollBy(-1)"><Icon name="chevleft" :size="16" /></button>
        <button class="scroll-btn" aria-label="Scroll right" @click="scrollBy(1)"><Icon name="chevright" :size="16" /></button>
      </template>
    </SectionHeader>
    <div class="row-scroll" ref="scrollEl" data-scroll-memory="up-next">
      <!-- Mouse-click plays the next episode; the title deep-links to the
           series detail page (same interaction model as Continue Watching) so
           you can reach the show without starting playback. `title-to`
           re-enables pointer events on just the title inside MediaCard's
           overlay, and its @click.stop keeps that click from also firing play.
           The tile is a plain click target (no tabindex/role) on purpose:
           the only keyboard-focusable thing is the title link, so Enter on it
           navigates without a competing tile handler double-firing play. -->
      <div
        v-for="(item, i) in items"
        :key="item.id"
        class="un-tile"
        :style="{ width: '168px' }"
        @click="$emit('play', item)"
      >
        <MediaCard
          :idx="i"
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
    </div>
  </section>
</template>

<script setup lang="ts">
export interface UpNextItem {
  id: number
  title: string
  slug: string
  season_number: number
  episode_number: number
  episode_label: string
  play_file_id: number
  play_file_public_id?: string
  // Episode primary key — let the watch route surface "S01E03 · Episode
  // title" in the activity panel via entity_type=episode + entity_id.
  episode_id?: number
  // Episode runtime — the hero "Tonight" planner sums these for its
  // session-length estimate.
  runtime_minutes?: number
  public_id?: string
  media_item_public_id?: string
}

defineProps<{ items: UpNextItem[] }>()
defineEmits<{ play: [item: UpNextItem] }>()

const scrollEl = ref<HTMLElement>()

function scrollBy(dir: number) {
  if (!scrollEl.value) return
  scrollEl.value.scrollBy({ left: dir * 600, behavior: 'smooth' })
}

// Up Next is always the next TV episode, so the title links to the series
// detail page. The tile itself still plays; this is the escape hatch to the
// show without starting playback.
function detailUrl(item: UpNextItem): string {
  return mediaUrl({ id: item.id, title: item.title, slug: item.slug, media_type: 'tv' })
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

.un-tile { flex-shrink: 0; cursor: pointer; }

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

/* Phone: 168px poster tiles are too wide for a 390px screen — the
   width is a literal inline style (`:style="{ width: '168px' }"`) so this
   needs !important to win. */
@media (max-width: 720px) {
  .row-scroll { gap: 12px; }
  .un-tile { width: 140px !important; }
}
</style>
