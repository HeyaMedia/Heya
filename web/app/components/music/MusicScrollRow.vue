<template>
  <section class="msr">
    <header class="msr-head">
      <component
        :is="titleHref ? NuxtLink : 'span'"
        :to="titleHref"
        class="msr-title"
        :class="{ link: !!titleHref }"
      >
        {{ title }}
        <Icon v-if="titleHref" name="chevright" :size="14" class="msr-chev" />
      </component>
      <span v-if="aside" class="msr-aside">{{ aside }}</span>
      <button v-if="onPlayAll" class="msr-action" @click="onPlayAll" title="Play all" :aria-label="`Play all ${title}`">
        <Icon name="play" :size="13" />
      </button>
      <button v-if="onShuffleAll" class="msr-action" @click="onShuffleAll" title="Shuffle" :aria-label="`Shuffle ${title}`">
        <Icon name="shuffle" :size="13" />
      </button>
      <div class="msr-grow" />
      <template v-if="!expanded && overflows">
        <AppHoldButton class="msr-nav msr-nav-scroll" title="Hold to jump to start" aria-label="Scroll left" @click="rail?.scrollByDir(-1, 440)" @hold="rail?.scrollToStart()">
          <Icon name="chevleft" :size="16" />
        </AppHoldButton>
        <button class="msr-nav msr-nav-scroll" @click="rail?.scrollByDir(1, 440)" title="Scroll right" aria-label="Scroll right">
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

    <!-- Expanded: wrap into a grid (bounded — it renders every loaded item).
         Default: the shared AppRail virtualized endless sidescroller. -->
    <div
      v-if="expanded"
      class="msr-grid"
      :style="{ gridTemplateColumns: `repeat(auto-fill, minmax(${cardSize}px, 1fr))` }"
    >
      <div v-for="(item, i) in items" :key="keyFor(item, i)">
        <slot :item="item" :index="i" />
      </div>
    </div>
    <AppRail
      v-else
      ref="rail"
      class="msr-rail"
      :items="items"
      :tile-width="cardSize"
      :phone-tile-width="phoneCardSize ?? 140"
      :gap="16"
      :phone-gap="10"
      aspect="1/1"
      snap
      :memory-key="memoryKey || title"
      :item-key="itemKey"
      :has-more="hasMore"
      :loading-more="loadingMore"
      @load-more="$emit('load-more')"
    >
      <template #default="{ item, index }">
        <slot :item="item" :index="index" />
      </template>
    </AppRail>
  </section>
</template>

<script setup lang="ts" generic="T">
import { NuxtLink } from '#components'
const props = withDefaults(defineProps<{
  title: string
  /** Mono dim aside after the title (e.g. "refreshed daily", a count). */
  aside?: string
  memoryKey?: string
  titleHref?: string
  cardSize?: number
  /** Phone tile width — e.g. the Mixes rail keeps 208px on phones. */
  phoneCardSize?: number
  /** Shelf items — rendered one per tile through the scoped slot. */
  items: T[]
  /** v-for key extractor; defaults to (item.key ?? item.id ?? index). */
  itemKey?: (item: T, index: number) => string | number
  /** More pages exist — AppRail shows its tail spinner and emits load-more. */
  hasMore?: boolean
  loadingMore?: boolean
  onPlayAll?: () => void
  onShuffleAll?: () => void
}>(), {
  cardSize: 170,
})

defineEmits<{ 'load-more': [] }>()

const rail = ref<{ scrollByDir: (dir: number, step?: number) => void; scrollToStart: () => void; overflows: boolean } | null>(null)
const expanded = ref(false)
// AppRail knows whether its track exceeds the viewport; while expanded the
// rail is unmounted, so remember the last rail-mode answer for the collapse
// toggle.
const lastOverflow = ref(false)
watchEffect(() => {
  if (rail.value) lastOverflow.value = rail.value.overflows
})
const overflows = computed(() => (expanded.value ? lastOverflow.value : rail.value?.overflows ?? false))

function keyFor(item: T, index: number): string | number {
  if (props.itemKey) return props.itemKey(item, index)
  const anyItem = item as { key?: string; id?: string | number }
  return anyItem.key ?? anyItem.id ?? index
}

// Hint card-size to children that opt into it.
provide('msr:cardSize', props.cardSize)
</script>

<style scoped>
.msr { margin-bottom: 36px; }

.msr-head {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 18px;
  padding-bottom: 11px;
  border-bottom: 1px solid var(--hair);
}
.msr-title {
  display: inline-flex;
  align-items: baseline;
  gap: 6px;
  /* Heya 2.0 sec-head grammar — mono uppercase, matching SectionHeader. */
  font: 600 12.5px var(--font-mono);
  letter-spacing: 0.24em;
  text-transform: uppercase;
  color: var(--fg-0);
  text-decoration: none;
  /* Section-title halo — rails sit over the ambient art pool. */
  text-shadow: 0 1px 2px var(--bg-1), 0 0 10px var(--bg-1), 0 0 24px var(--bg-1);
}
.msr-aside {
  font: 600 12px var(--font-mono);
  letter-spacing: 0.06em;
  color: var(--tone, var(--gold));
  text-shadow: 0 1px 2px var(--bg-1), 0 0 10px var(--bg-1);
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

@media (max-width: 720px) {
  .msr-grid { grid-template-columns: repeat(auto-fill, minmax(110px, 1fr)) !important; gap: 14px 12px; }
}
</style>
