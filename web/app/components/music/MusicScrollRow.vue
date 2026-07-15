<template>
  <section class="msr">
    <header class="msr-head">
      <component
        :is="titleHref ? 'NuxtLink' : 'span'"
        :to="titleHref"
        class="msr-title"
        :class="{ link: !!titleHref }"
      >
        {{ title }}
        <Icon v-if="titleHref" name="chevright" :size="14" class="msr-chev" />
      </component>
      <button v-if="onPlayAll" class="msr-action" @click="onPlayAll" title="Play all" :aria-label="`Play all ${title}`">
        <Icon name="play" :size="13" />
      </button>
      <button v-if="onShuffleAll" class="msr-action" @click="onShuffleAll" title="Shuffle" :aria-label="`Shuffle ${title}`">
        <Icon name="shuffle" :size="13" />
      </button>
      <div class="msr-grow" />
      <template v-if="!expanded && overflows">
        <button class="msr-nav msr-nav-scroll" @click="scroll('left')" title="Scroll left" aria-label="Scroll left">
          <Icon name="chevleft" :size="16" />
        </button>
        <button class="msr-nav msr-nav-scroll" @click="scroll('right')" title="Scroll right" aria-label="Scroll right">
          <Icon name="chevright" :size="16" />
        </button>
      </template>
      <button
        v-if="expanded || overflows"
        class="msr-nav msr-nav-expand"
        @click="expanded = !expanded"
        :title="expanded ? 'Collapse' : 'Expand all'"
        :aria-label="expanded ? `Collapse ${title}` : `Show all ${title}`"
        :aria-expanded="expanded"
      >
        <Icon name="chevdown" :size="16" :style="expanded ? { transform: 'rotate(180deg)' } : undefined" />
      </button>
    </header>

    <!-- Expanded: wrap into a grid. Default: horizontal scroller. -->
    <div
      v-if="expanded"
      class="msr-grid"
      :style="{ gridTemplateColumns: `repeat(auto-fill, minmax(${cardSize}px, 1fr))` }"
    >
      <slot />
    </div>
    <div
      v-else
      ref="scroller"
      class="msr-scroller"
      :data-scroll-memory="memoryKey || title"
    >
      <slot />
    </div>
  </section>
</template>

<script setup lang="ts">
const props = withDefaults(defineProps<{
  title: string
  memoryKey?: string
  titleHref?: string
  cardSize?: number
  onPlayAll?: () => void
  onShuffleAll?: () => void
}>(), {
  cardSize: 170,
})

const scroller = ref<HTMLElement | null>(null)
const expanded = ref(false)
const overflows = ref(false)

function scroll(dir: 'left' | 'right') {
  scroller.value?.scrollBy({ left: dir === 'left' ? -440 : 440, behavior: 'smooth' })
}

function checkOverflow() {
  const el = scroller.value
  if (!el) { overflows.value = false; return }
  overflows.value = el.scrollWidth > el.clientWidth + 1
}

useResizeObserver(scroller, checkOverflow)
useMutationObserver(scroller, checkOverflow, { childList: true, subtree: true })
watch(scroller, (el) => { if (el) checkOverflow() })

watch(expanded, () => {
  // When collapsing back, the scroller mounts fresh — observers latch on
  // via the scroller-ref watch above the next tick.
  nextTick(checkOverflow)
})

// Hint card-size to children that opt into it.
provide('msr:cardSize', props.cardSize)
</script>

<style scoped>
.msr { margin-bottom: 36px; }

.msr-head {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 14px;
}
.msr-title {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 22px;
  font-weight: 700;
  color: var(--fg-0);
  text-decoration: none;
  /* Section-title halo — rails sit over the ambient art pool. */
  text-shadow: 0 1px 2px var(--bg-1), 0 0 10px var(--bg-1), 0 0 24px var(--bg-1);
}
.msr-title.link:hover { color: var(--gold); }
.msr-title.link:hover .msr-chev { color: var(--gold); }
.msr-chev { color: var(--fg-3); transition: color 0.15s; }

.msr-action {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  border: 0;
  background: var(--gold-soft);
  color: var(--gold);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: background 0.15s, transform 0.1s;
}
.msr-action:hover { background: var(--gold); color: var(--bg-0); }
.msr-action:active { transform: scale(0.95); }

.msr-grow { flex: 1; }

/* Prev/next/expand — small glass circles (the .steer-glass recipe), not
   solid accent buttons: they're steering chrome, not content actions. */
.msr-nav {
  width: 30px;
  height: 30px;
  border-radius: 50%;
  background: color-mix(in oklab, var(--bg-2) 82%, transparent);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  border: 1px solid var(--border);
  box-shadow: var(--shadow-el);
  color: var(--fg-1);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: background 0.15s, color 0.15s, transform 0.1s;
}
.msr-nav:hover { background: var(--bg-3); color: var(--fg-0); }
.msr-nav:active { transform: scale(0.95); }
.msr-nav :deep(svg) { transition: transform 0.2s; }

.msr-scroller {
  display: flex;
  gap: 16px;
  overflow-x: auto;
  scroll-snap-type: x proximity;
  /* Shadow escape (ContentRow's pattern): pad the clip box out to the page
     gutter and pull it back with matching negative margins, so the cards'
     --shadow-card isn't cut off by overflow-x — and the rail runs edge to
     edge under the page-pad gutter. --msr-bleed MUST track .page-pad's
     per-breakpoint gutter (40/24/12): every consumer is a page-pad page,
     and a bleed wider than the gutter would overflow .music-main sideways. */
  --msr-bleed: var(--page-pad-x, 40px);
  /* Heya 2.0 shadow room: big symmetric vertical padding/negative-margin so the
     enlarged directional shadows + -4px hover lift aren't sliced. Horizontal
     bleed STAYS at the page gutter (--msr-bleed) — a wider bleed would overflow
     .music-main sideways (it doesn't clip overflow-x like the home .scroll). */
  padding: 44px var(--msr-bleed) 130px;
  margin: -44px calc(-1 * var(--msr-bleed)) -130px;
  scroll-padding-left: var(--msr-bleed);
  scrollbar-width: none;
}
@media (max-width: 1100px) { .msr-scroller { --msr-bleed: 24px; } }
@media (max-width: 720px) {
  .msr-scroller {
    --msr-bleed: 12px;
    padding-top: 30px; padding-bottom: 100px;
    margin-top: -30px; margin-bottom: -100px;
  }
}
.msr-scroller::-webkit-scrollbar { display: none; }
/* Use :deep(*) instead of :slotted(*) so the sizing rule survives reka-ui's
   slot cloning — AppContextMenu's <Slot>-with-as-child wraps swap the slot
   child for a cloned VNode that loses Vue's :slotted() marker, so tiles
   inside an AppContextMenu wrapper would otherwise lose their width. */
.msr-scroller > :deep(*) {
  flex-shrink: 0;
  scroll-snap-align: start;
  width: v-bind('`${cardSize}px`');
}

.msr-grid {
  display: grid;
  gap: 16px;
}

/* Touch: swipe replaces the mouse-only scroll arrows. The expand-to-grid
   toggle (.msr-nav-expand) stays — it's a tap target, not a mouse affordance. */
@media (pointer: coarse) {
  .msr-nav-scroll { display: none; }
  /* Keep the compact glass visuals but grow the touch target to the app's
     44px coarse-pointer minimum via an invisible expanded hit-area — the
     play-all/shuffle/expand controls stay tappable on phones without
     changing the header's visual density. */
  .msr-action, .msr-nav-expand { position: relative; }
  .msr-action::before, .msr-nav-expand::before {
    content: '';
    position: absolute;
    inset: -8px;
  }
}

/* Phone: rail cards (mixes at 220px, everything else at 170px on desktop)
   collapse to one sensible size — these are all square covers or circular
   artist portraits, not text-heavy, so a uniform ~140px reads fine. */
@media (max-width: 720px) {
  .msr-scroller { gap: 10px; }
  .msr-scroller > :deep(*) { width: 140px; }
  .msr-grid { grid-template-columns: repeat(auto-fill, minmax(110px, 1fr)) !important; gap: 14px 12px; }
}
</style>
