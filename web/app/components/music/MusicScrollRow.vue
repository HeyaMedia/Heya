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
      <button v-if="onPlayAll" class="msr-action" @click="onPlayAll" title="Play all">
        <Icon name="play" :size="13" />
      </button>
      <button v-if="onShuffleAll" class="msr-action" @click="onShuffleAll" title="Shuffle">
        <Icon name="shuffle" :size="13" />
      </button>
      <div class="msr-grow" />
      <template v-if="!expanded && overflows">
        <button class="msr-nav" @click="scroll('left')" title="Scroll left">
          <Icon name="chevleft" :size="16" />
        </button>
        <button class="msr-nav" @click="scroll('right')" title="Scroll right">
          <Icon name="chevright" :size="16" />
        </button>
      </template>
      <button
        v-if="expanded || overflows"
        class="msr-nav"
        @click="expanded = !expanded"
        :title="expanded ? 'Collapse' : 'Expand all'"
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
    >
      <slot />
    </div>
  </section>
</template>

<script setup lang="ts">
const props = withDefaults(defineProps<{
  title: string
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

.msr-nav {
  width: 30px;
  height: 30px;
  border-radius: 50%;
  border: 0;
  background: var(--gold);
  color: var(--bg-0);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: filter 0.15s, transform 0.1s;
}
.msr-nav:hover { filter: brightness(1.1); }
.msr-nav:active { transform: scale(0.95); }
.msr-nav :deep(svg) { transition: transform 0.2s; }

.msr-scroller {
  display: flex;
  gap: 16px;
  overflow-x: auto;
  scroll-snap-type: x proximity;
  padding-bottom: 6px;
  scrollbar-width: none;
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
</style>
