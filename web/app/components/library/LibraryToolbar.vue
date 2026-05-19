<template>
  <div class="lib-toolbar">
    <div class="lib-toolbar-left">
      <h1 class="lib-toolbar-title">{{ title }}</h1>
      <span class="lib-toolbar-count">{{ count }} titles</span>
    </div>
    <div class="lib-toolbar-right">
      <div class="sort-wrap" ref="sortWrap">
        <button class="btn-ghost-sm" @click="sortOpen = !sortOpen">
          <Icon name="sort" :size="14" />
          {{ sortLabel }}
        </button>
        <div v-if="sortOpen" class="sort-menu">
          <div
            v-for="opt in sortOptions"
            :key="opt.value"
            class="sort-option"
            :class="{ active: sort === opt.value }"
            @click="$emit('sort', opt.value); sortOpen = false"
          >
            {{ opt.label }}
          </div>
        </div>
      </div>
      <div class="view-toggle">
        <button class="btn-icon" :class="{ active: view === 'grid' }" @click="$emit('view', 'grid')">
          <Icon name="grid" :size="16" />
        </button>
        <button class="btn-icon" :class="{ active: view === 'list' }" @click="$emit('view', 'list')">
          <Icon name="list" :size="16" />
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
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

const sortOpen = ref(false)
const sortWrap = ref<HTMLElement>()

const sortOptions = [
  { label: 'Recently Added', value: 'added' },
  { label: 'Title A→Z', value: 'title' },
  { label: 'Year (Newest)', value: 'year-desc' },
  { label: 'Year (Oldest)', value: 'year-asc' },
  { label: 'Rating', value: 'rating' },
]

const sortLabel = computed(() => sortOptions.find(o => o.value === props.sort)?.label || 'Sort')

onMounted(() => {
  document.addEventListener('click', (e) => {
    if (sortWrap.value && !sortWrap.value.contains(e.target as Node)) {
      sortOpen.value = false
    }
  })
})
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
.sort-wrap { position: relative; }
.sort-menu {
  position: absolute;
  top: calc(100% + 6px);
  right: 0;
  min-width: 200px;
  background: var(--bg-3);
  border: 1px solid var(--border-strong);
  border-radius: var(--r-md);
  padding: 4px;
  z-index: 20;
  box-shadow: var(--shadow-2);
}
.sort-option {
  padding: 8px 12px;
  font-size: 13px;
  border-radius: var(--r-sm);
  cursor: pointer;
  color: var(--fg-1);
}
.sort-option:hover { background: rgba(255,255,255,0.06); }
.sort-option.active { color: var(--gold); }
.view-toggle { display: flex; gap: 2px; }
</style>
