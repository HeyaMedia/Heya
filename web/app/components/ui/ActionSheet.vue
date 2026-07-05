<!--
  ActionSheet — generic bottom sheet that renders an AppContextMenu-compatible
  `items` array as tappable rows. Built on AppSheet; meant as the touch
  equivalent of AppContextMenu for anywhere a right-click/long-press menu
  exists today (music rows now, poster cards in a later wave).

  v1 deliberately has no nested navigation: a `submenu` item (e.g. "Add to
  Playlist", "Rate") is flattened into an inert section-header row (its own
  label + icon, non-interactive) followed by its children rendered indented
  directly beneath — tap any child to run it. This trades the desktop
  fly-out for "scroll a bit further," which is the right call for a sheet
  that's already full-width and scrollable.

  Usage:
    <ActionSheet v-model:open="open" :items="actions.forTrack(entity)" :title="track.title" />
-->
<template>
  <AppSheet v-model:open="open" :title="title">
    <div class="action-sheet-list">
      <template v-for="(item, i) in items" :key="i">
        <div v-if="item.separator" class="action-sheet-divider" />

        <template v-else-if="item.submenu && item.submenu.length">
          <div class="action-sheet-section">
            <Icon v-if="item.icon" :name="item.icon" :size="14" class="action-sheet-section-icon" />
            <span>{{ item.label }}</span>
          </div>
          <button
            v-for="(sub, j) in item.submenu"
            :key="j"
            type="button"
            class="action-sheet-row action-sheet-row-indent"
            :class="{ 'action-sheet-row-disabled': sub.disabled }"
            :disabled="sub.disabled"
            @click="run(sub)"
          >
            <Icon v-if="sub.icon" :name="sub.icon" :size="15" class="action-sheet-row-icon" />
            <span class="action-sheet-row-label">{{ sub.label }}</span>
          </button>
        </template>

        <button
          v-else
          type="button"
          class="action-sheet-row"
          :class="{ 'action-sheet-row-disabled': item.disabled }"
          :disabled="item.disabled"
          @click="run(item)"
        >
          <Icon v-if="item.icon" :name="item.icon" :size="16" class="action-sheet-row-icon" />
          <span class="action-sheet-row-label">{{ item.label }}</span>
        </button>
      </template>
    </div>
  </AppSheet>
</template>

<script setup lang="ts">
import type { ContextMenuItem } from '~~/shared/types'

defineProps<{
  /** Same shape AppContextMenu's `items` prop takes. */
  items: ContextMenuItem[]
  title?: string
}>()

const open = defineModel<boolean>('open', { default: false })

function run(item: ContextMenuItem) {
  if (item.disabled) return
  item.action?.()
  open.value = false
}
</script>

<!--
  AppSheet portals its content to <body>, so styling for anything rendered
  inside it must be unscoped (docs/ui.md gotcha #2).
-->
<style>
.action-sheet-list {
  display: flex;
  flex-direction: column;
}

.action-sheet-row {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  min-height: 48px;
  padding: 10px 6px;
  background: transparent;
  border: 0;
  border-radius: var(--r-sm);
  color: var(--fg-1);
  font-family: inherit;
  font-size: 14px;
  font-weight: 500;
  text-align: left;
  cursor: pointer;
}
.action-sheet-row:active { background: rgba(255, 255, 255, 0.06); }
.action-sheet-row-icon { flex-shrink: 0; opacity: 0.8; }
.action-sheet-row-label { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.action-sheet-row-indent { padding-left: 36px; font-size: 13.5px; color: var(--fg-2); font-weight: 500; }

.action-sheet-row-disabled { opacity: 0.4; pointer-events: none; }

.action-sheet-section {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 14px 6px 4px;
  font-size: 11px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--fg-3);
}
.action-sheet-section-icon { opacity: 0.7; }

.action-sheet-divider {
  height: 1px;
  background: var(--border);
  margin: 6px 0;
}
</style>
