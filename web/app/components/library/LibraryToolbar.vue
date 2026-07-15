<template>
  <div ref="barEl" class="lib-toolbar" :class="{ stuck }">
    <div class="lib-toolbar-left" :class="{ 'left-count-only': hideTitle }">
      <h1 v-if="!hideTitle" class="lib-toolbar-title">{{ title }}</h1>
      <span class="lib-toolbar-count">{{ count }} titles</span>
    </div>
    <div class="lib-toolbar-right">
      <!-- The section sidebar opens from AppTopBar's burger on phone + the
           compact band — no per-page button here. -->
      <AppMenu trigger-class="btn-ghost-sm" :width="220" align="end">
        <template #trigger>
          <Icon name="sort" :size="14" />
          {{ sortLabel }}
        </template>
        <DropdownMenuItem
          v-for="opt in sortOptions"
          :key="opt.value"
          class="surface-item lt-sort-item"
          :class="{ active: sort === opt.value }"
          @select="$emit('sort', opt.value)"
        >
          {{ opt.label }}
        </DropdownMenuItem>
      </AppMenu>
      <div class="view-toggle">
        <AppTooltip label="Grid view">
          <button class="btn-icon" :class="{ active: view === 'grid' }" aria-label="Grid view" @click="$emit('view', 'grid')">
            <Icon name="grid" :size="16" />
          </button>
        </AppTooltip>
        <AppTooltip label="List view">
          <button class="btn-icon" :class="{ active: view === 'list' }" aria-label="List view" @click="$emit('view', 'list')">
            <Icon name="list" :size="16" />
          </button>
        </AppTooltip>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { DropdownMenuItem } from 'reka-ui'

const props = defineProps<{
  title: string
  count: number
  sort: string
  view: string
  /** Hide the big title — a LibHead above already carries it. */
  hideTitle?: boolean
}>()

defineEmits<{
  sort: [value: string]
  view: [value: string]
}>()

const sortOptions = [
  { label: 'Recently Added', value: 'added' },
  { label: 'Title A→Z', value: 'title' },
  { label: 'Year (Newest)', value: 'year-desc' },
  { label: 'Year (Oldest)', value: 'year-asc' },
  { label: 'Rating', value: 'rating' },
]

const sortLabel = computed(() => sortOptions.find(o => o.value === props.sort)?.label || 'Sort')

// Stuck detection — mirrors FilterBar: transparent + hairline at rest, glass
// once pinned under the topbar. Compares the bar's top against the scroll
// container's top on scroll (the nested `.library-main.scroll` column, not the
// window). A nested sentinel can't work — it rides up with the pinned bar.
const stuck = ref(false)
const barEl = ref<HTMLElement | null>(null)
let scrollTarget: HTMLElement | null = null

function nearestScrollParent(el: HTMLElement): HTMLElement | null {
  let node = el.parentElement
  while (node) {
    const oy = getComputedStyle(node).overflowY
    if (oy === 'auto' || oy === 'scroll' || oy === 'overlay') return node
    node = node.parentElement
  }
  return null
}

function updateStuck() {
  const bar = barEl.value
  if (!bar) return
  const containerTop = scrollTarget ? scrollTarget.getBoundingClientRect().top : 0
  const next = bar.getBoundingClientRect().top <= containerTop + 1
  if (next !== stuck.value) stuck.value = next
}

onMounted(() => {
  if (!barEl.value) return
  scrollTarget = nearestScrollParent(barEl.value)
  scrollTarget?.addEventListener('scroll', updateStuck, { passive: true })
  window.addEventListener('resize', updateStuck, { passive: true })
  updateStuck()
})
onBeforeUnmount(() => {
  scrollTarget?.removeEventListener('scroll', updateStuck)
  window.removeEventListener('resize', updateStuck)
  scrollTarget = null
})
</script>

<style scoped>
/* Sticky control bar with the same rest/stuck paint as FilterBar: transparent
   over a bottom hairline at rest (breathes with the ambient), glass once it
   pins under the topbar so grid content ghosts through. Uses the shared
   --frame-glass-column / --frame-glass-blur tokens (heya.css) so it matches the
   topbar and sidebar exactly and honors the minimal-appearance knob for free;
   Firefox's solid-glass fallback rides the same token override. */
.lib-toolbar {
  position: sticky;
  top: 0;
  z-index: 20;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 32px 16px;
  border-bottom: 1px solid var(--hair);
  transition: background 0.22s ease, border-color 0.22s ease;
}
.lib-toolbar.stuck {
  background: var(--frame-glass-column);
  backdrop-filter: blur(var(--frame-glass-blur));
  -webkit-backdrop-filter: blur(var(--frame-glass-blur));
  border-bottom-color: var(--hair-strong);
}
.lib-toolbar-left { display: flex; align-items: baseline; gap: 12px; }
.lib-toolbar-title {
  font-family: var(--font-display);
  font-size: 30px; font-weight: 800; font-variation-settings: 'wdth' 112;
  letter-spacing: -0.02em; margin: 0;
}
/* Count reads as a ledger .k label. */
.lib-toolbar-count {
  font-family: var(--font-mono); font-size: 11px; font-weight: 600;
  letter-spacing: 0.12em; text-transform: uppercase;
  color: var(--fg-3);
}
.left-count-only .lib-toolbar-count { letter-spacing: 0.14em; color: var(--fg-2); }
.lib-toolbar-right { display: flex; align-items: center; gap: 8px; }
.lib-toolbar-right :deep(.btn-ghost-sm) { text-transform: uppercase; letter-spacing: 0.08em; }
.view-toggle { display: flex; gap: 2px; }

@media (max-width: 720px) {
  .lib-toolbar { flex-direction: column; align-items: stretch; gap: 12px; padding: 16px 16px 14px; }
  .lib-toolbar-left { justify-content: space-between; }
  .lib-toolbar-title { font-size: 22px; }
  .lib-toolbar-right { flex-wrap: wrap; }
  .btn-ghost-sm, .btn-icon { min-height: 44px; }
  .btn-icon { width: 44px; }
}
</style>

<style>
/* Sort menu rows live in AppMenu's portaled content. */
.lt-sort-item.active { color: var(--gold); }

@media (max-width: 720px) {
  /* AppMenu renders this trigger itself — same reachability gotcha as
     FilterBar's Sort button (docs/ui.md). */
  .app-menu-trigger.btn-ghost-sm { min-height: 44px; }
}
</style>
