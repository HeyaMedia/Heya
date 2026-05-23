<template>
  <div class="mbc">
    <div class="mbc-head">
      <span class="mbc-title">{{ title }}</span>
      <span v-if="items.length" class="mbc-count">{{ filteredItems.length }}</span>
    </div>
    <div v-if="searchable" class="mbc-search">
      <input v-model="search" type="text" placeholder="Filter..." class="mbc-input" />
    </div>
    <div class="mbc-list scroll">
      <div v-if="loading" class="mbc-empty">Loading...</div>
      <div v-else-if="!filteredItems.length" class="mbc-empty">No items</div>
      <div
        v-for="item in filteredItems"
        :key="item.id"
        class="mbc-item"
        :class="{ active: item.id === selectedId }"
        @click="$emit('select', item.id)"
      >
        <img
          v-if="item.poster"
          :src="item.poster"
          class="mbc-poster"
          @error="(e: Event) => (e.target as HTMLImageElement).style.display = 'none'"
        />
        <div class="mbc-text">
          <div class="mbc-label">{{ item.label }}</div>
          <div v-if="item.sublabel" class="mbc-sub">{{ item.sublabel }}</div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
export interface BrowserColumnItem {
  id: number
  label: string
  sublabel?: string
  poster?: string
}

const props = defineProps<{
  title: string
  items: BrowserColumnItem[]
  selectedId: number | null
  loading?: boolean
  searchable?: boolean
}>()

defineEmits<{ select: [id: number] }>()

const search = ref('')

const filteredItems = computed(() => {
  if (!search.value) return props.items
  const q = search.value.toLowerCase()
  return props.items.filter(i => i.label.toLowerCase().includes(q))
})
</script>

<style scoped>
.mbc {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  border-right: 1px solid var(--border);
  background: var(--bg-2);
  height: 100%;
  overflow: hidden;
}
.mbc-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 12px 8px;
  border-bottom: 1px solid var(--border);
}
.mbc-title {
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--fg-2);
}
.mbc-count {
  font-size: 10px;
  color: var(--fg-3);
  font-family: var(--font-mono);
}
.mbc-search { padding: 6px 8px; }
.mbc-input {
  width: 100%;
  height: 28px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-3);
  color: var(--fg-1);
  font-size: 11px;
  padding: 0 8px;
  outline: none;
}
.mbc-input:focus { border-color: var(--gold); }
.mbc-list {
  flex: 1;
  overflow-y: auto;
  padding: 4px 0;
}
.mbc-empty {
  padding: 24px 12px;
  font-size: 11px;
  color: var(--fg-3);
  text-align: center;
}
.mbc-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 12px;
  cursor: pointer;
  transition: background 0.12s;
  position: relative;
}
.mbc-item:hover { background: rgba(255,255,255,0.03); }
.mbc-item.active {
  background: var(--gold-soft);
}
.mbc-item.active::before {
  content: '';
  position: absolute;
  left: 0;
  top: 4px;
  bottom: 4px;
  width: 3px;
  border-radius: 2px;
  background: var(--gold);
}
.mbc-poster {
  width: 28px;
  height: 42px;
  border-radius: 3px;
  object-fit: cover;
  flex-shrink: 0;
  background: var(--bg-3);
}
.mbc-text { flex: 1; min-width: 0; }
.mbc-label {
  font-size: 12px;
  font-weight: 500;
  color: var(--fg-1);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.mbc-item.active .mbc-label { color: var(--gold-bright); }
.mbc-sub {
  font-size: 10px;
  color: var(--fg-3);
  margin-top: 1px;
}
</style>
