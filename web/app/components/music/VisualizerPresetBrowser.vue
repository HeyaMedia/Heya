<!--
  VisualizerPresetBrowser — slide-in Milkdrop preset panel.

  Search + All/Favorites/History tabs, per-row heart toggle, favorites floated
  to the top of the "All" list, and the auto-cycle controls (interval, order,
  liked-only). Selecting a preset emits `select`; the host loads it on the
  Milkdrop instance so we avoid a store→watch→load feedback loop.
-->
<template>
  <div class="pb-backdrop" @click="close" />

  <div class="pb-panel" @click.stop>
    <div class="pb-head">
      <span class="pb-head-title">Presets</span>
      <button class="pb-x" aria-label="Close presets" @click="close"><Icon name="close" :size="15" /></button>
    </div>

    <!-- Auto-cycle -->
    <div class="pb-cycle">
      <div class="pb-cycle-row">
        <AppSwitch
          :model-value="vis.autoCycleEnabled.value"
          size="sm"
          label="Auto-cycle"
          @update:model-value="vis.setAutoCycleEnabled"
        />
        <div class="pb-cycle-selects" :class="{ dim: !vis.autoCycleEnabled.value }">
          <AppSelect
            :model-value="String(vis.autoCycleIntervalSec.value)"
            :options="INTERVALS"
            aria-label="Auto-cycle interval"
            @change="v => vis.setAutoCycleIntervalSec(Number(v))"
          />
          <AppSelect
            :model-value="vis.autoCycleMode.value"
            :options="ORDERS"
            aria-label="Auto-cycle order"
            @change="v => vis.setAutoCycleMode(v as 'random' | 'sequential')"
          />
        </div>
      </div>
      <div class="pb-cycle-row">
        <AppSwitch
          :model-value="vis.likedOnly.value"
          size="sm"
          label="Liked only"
          @update:model-value="vis.setLikedOnly"
        />
        <span class="pb-cycle-hint">Navigate + cycle only your favorites</span>
      </div>
    </div>

    <!-- Search -->
    <div class="pb-search">
      <Icon name="search" :size="14" class="pb-search-icon" />
      <input
        ref="searchRef"
        v-model="query"
        type="text"
        placeholder="Search presets…"
        class="pb-search-input"
        spellcheck="false"
      >
    </div>

    <!-- Tabs -->
    <div class="pb-tabs">
      <button
        v-for="t in tabs"
        :key="t.id"
        class="pb-tab"
        :class="{ active: tab === t.id }"
        @click="tab = t.id"
      >{{ t.label }}</button>
    </div>

    <!-- List -->
    <div ref="listRef" class="pb-list">
      <button
        v-for="name in filtered"
        :key="name"
        class="pb-item"
        :class="{ active: name === vis.currentPresetName.value }"
        :data-active="name === vis.currentPresetName.value"
        @click="select(name)"
      >
        <span
          class="pb-item-heart"
          :class="{ liked: vis.isFavorite(name) }"
          @click.stop="vis.toggleFavorite(name)"
        >
          <Icon :name="vis.isFavorite(name) ? 'heartfill' : 'heart'" :size="12" />
        </span>
        <span class="pb-item-name">{{ prettyName(name) }}</span>
      </button>
      <div v-if="!filtered.length" class="pb-empty">
        {{ query ? 'No presets match.' : 'No presets here yet.' }}
      </div>
    </div>

    <div class="pb-foot">
      {{ filtered.length }} shown · {{ vis.favoritePresets.value.length }} liked
    </div>
  </div>
</template>

<script setup lang="ts">
import type { SelectOption } from '~/components/ui/AppSelect.vue'

const props = defineProps<{ presetKeys: string[] }>()
const emit = defineEmits<{ select: [name: string] }>()

const vis = useVisualizer()

type Tab = 'all' | 'favorites' | 'history'
const tab = ref<Tab>('all')
const query = ref('')
const searchRef = ref<HTMLInputElement | null>(null)
const listRef = ref<HTMLElement | null>(null)

const INTERVALS: SelectOption[] = [15, 30, 45, 60, 90, 120].map((s) => ({ value: String(s), label: `${s}s` }))
const ORDERS: SelectOption[] = [
  { value: 'random', label: 'Random' },
  { value: 'sequential', label: 'In order' },
]

const tabs = computed(() => [
  { id: 'all' as Tab, label: `All ${props.presetKeys.length}` },
  { id: 'favorites' as Tab, label: `Liked ${vis.favoritePresets.value.length}` },
  { id: 'history' as Tab, label: `Recent ${vis.presetHistory.value.length}` },
])

const source = computed(() => {
  const keys = props.presetKeys
  if (tab.value === 'favorites') return vis.favoritePresets.value.filter((n) => keys.includes(n))
  if (tab.value === 'history') return vis.presetHistory.value.filter((n) => keys.includes(n))
  // All: floats liked presets to the top, preserving each group's order.
  const liked = new Set(vis.favoritePresets.value)
  const favs = keys.filter((k) => liked.has(k))
  const rest = keys.filter((k) => !liked.has(k))
  return [...favs, ...rest]
})

const filtered = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return source.value
  return source.value.filter((k) => k.toLowerCase().includes(q))
})

// Butterchurn keys look like "$author - Title.milk" — strip to the title.
function prettyName(raw: string) {
  return raw.replace(/\.milk$/i, '').replace(/^[^-]+ - /, '').trim() || raw
}

function select(name: string) {
  emit('select', name)
}
function close() {
  vis.presetBrowserOpen.value = false
}

onMounted(() => {
  searchRef.value?.focus()
  nextTick(() => {
    listRef.value?.querySelector('[data-active="true"]')?.scrollIntoView({ block: 'center' })
  })
})
</script>

<style scoped>
.pb-backdrop { position: absolute; inset: 0; z-index: 4; }
.pb-panel {
  position: absolute;
  top: 0; right: 0; bottom: 0;
  z-index: 5;
  width: 340px;
  max-width: 86vw;
  display: flex;
  flex-direction: column;
  background: rgba(10, 10, 14, 0.82);
  backdrop-filter: blur(16px);
  border-left: 1px solid rgba(255, 255, 255, 0.1);
  box-shadow: -20px 0 60px rgba(0, 0, 0, 0.5);
  animation: pb-slide 0.22s cubic-bezier(0.16, 1, 0.3, 1);
}
@keyframes pb-slide { from { transform: translateX(20px); opacity: 0; } to { transform: translateX(0); opacity: 1; } }

.pb-head {
  display: flex; align-items: center; justify-content: space-between;
  padding: 14px 16px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
}
.pb-head-title { font-size: 14px; font-weight: 600; color: #fff; }
.pb-x {
  width: 28px; height: 28px; border-radius: 50%;
  display: inline-flex; align-items: center; justify-content: center;
  color: rgba(255,255,255,0.5); background: transparent; border: 0; cursor: pointer;
  transition: background 0.12s, color 0.12s;
}
.pb-x:hover { background: rgba(255,255,255,0.1); color: #fff; }

.pb-cycle {
  display: flex; flex-direction: column; gap: 10px;
  padding: 12px 16px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
}
.pb-cycle-row { display: flex; align-items: center; gap: 12px; justify-content: space-between; }
.pb-cycle-selects { display: flex; gap: 6px; transition: opacity 0.15s; }
.pb-cycle-selects.dim { opacity: 0.4; pointer-events: none; }
.pb-cycle-selects :deep(.app-select-trigger) { min-width: 84px; }
.pb-cycle-hint { font-size: 11px; color: rgba(255,255,255,0.4); flex: 1; text-align: right; }

.pb-search { position: relative; padding: 10px 16px; border-bottom: 1px solid rgba(255,255,255,0.08); }
.pb-search-icon { position: absolute; left: 27px; top: 50%; transform: translateY(-50%); color: rgba(255,255,255,0.3); }
.pb-search-input {
  width: 100%;
  padding: 8px 12px 8px 34px;
  font-size: 12px;
  color: #fff;
  background: rgba(255, 255, 255, 0.06);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 8px;
  outline: none;
}
.pb-search-input::placeholder { color: rgba(255,255,255,0.3); }
.pb-search-input:focus { border-color: rgba(230, 185, 74, 0.5); }

.pb-tabs { display: flex; border-bottom: 1px solid rgba(255,255,255,0.08); }
.pb-tab {
  flex: 1; padding: 9px 4px;
  font-size: 11px; font-weight: 500;
  color: rgba(255,255,255,0.5);
  background: transparent; border: 0;
  border-bottom: 2px solid transparent;
  cursor: pointer; transition: color 0.12s, border-color 0.12s;
}
.pb-tab:hover { color: rgba(255,255,255,0.8); }
.pb-tab.active { color: var(--gold-bright, var(--gold)); border-bottom-color: var(--gold); }

.pb-list { flex: 1; overflow-y: auto; padding: 4px 0; }
.pb-item {
  display: flex; align-items: center; gap: 8px;
  width: 100%; padding: 7px 14px;
  background: transparent; border: 0; cursor: pointer; text-align: left;
  color: rgba(255,255,255,0.72);
  transition: background 0.1s, color 0.1s;
}
.pb-item:hover { background: rgba(255,255,255,0.05); }
.pb-item.active { background: var(--gold-soft); color: var(--gold-bright, var(--gold)); }
.pb-item-heart {
  flex-shrink: 0; display: inline-flex;
  color: rgba(255,255,255,0.2);
  opacity: 0; transition: opacity 0.12s, color 0.12s;
}
.pb-item:hover .pb-item-heart { opacity: 1; }
.pb-item-heart.liked { color: #ff5b7a; opacity: 1; }
.pb-item-name { min-width: 0; flex: 1; font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.pb-empty { padding: 32px 16px; text-align: center; font-size: 12px; color: rgba(255,255,255,0.3); }

.pb-foot {
  padding: 9px 16px;
  border-top: 1px solid rgba(255,255,255,0.08);
  text-align: center;
  font-size: 10px; font-family: var(--font-mono);
  color: rgba(255,255,255,0.35);
}
</style>
