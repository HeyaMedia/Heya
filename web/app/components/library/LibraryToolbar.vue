<template>
  <div class="lib-toolbar">
    <div class="lib-toolbar-left">
      <h1 class="lib-toolbar-title">{{ title }}</h1>
      <span class="lib-toolbar-count">{{ count }} titles</span>
    </div>
    <div class="lib-toolbar-right">
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
</script>

<style scoped>
.lib-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 24px 32px 20px;
}
.lib-toolbar-left { display: flex; align-items: baseline; gap: 12px; }
.lib-toolbar-title { font-size: 30px; font-weight: 600; letter-spacing: -0.02em; margin: 0; }
.lib-toolbar-count { font-family: var(--font-mono); font-size: 12px; color: var(--fg-3); }
.lib-toolbar-right { display: flex; align-items: center; gap: 8px; }
.view-toggle { display: flex; gap: 2px; }
</style>

<style>
/* Sort menu rows live in AppMenu's portaled content. */
.lt-sort-item.active { color: var(--gold); }
</style>
