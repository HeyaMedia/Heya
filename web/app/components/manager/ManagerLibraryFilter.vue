<script setup lang="ts">
// Library filter chips for the manager's cross-cutting pages (calendar,
// queue, wanted, history). Empty selection = all libraries. Multi-select
// toggles; "All" clears.
const props = defineProps<{
  options: { key: string, label: string, icon: string }[]
}>()

const selected = defineModel<string[]>({ default: () => [] })

function toggle(key: string) {
  if (selected.value.includes(key)) {
    selected.value = selected.value.filter(k => k !== key)
  } else {
    selected.value = [...selected.value, key]
  }
}

const allActive = computed(() =>
  selected.value.length === 0 || selected.value.length === props.options.length)
</script>

<template>
  <div class="mgr-filter" role="group" aria-label="Filter by library">
    <button
      type="button"
      class="mgr-filter-chip"
      :class="{ active: allActive }"
      @click="selected = []"
    >All</button>
    <button
      v-for="opt in options"
      :key="opt.key"
      type="button"
      class="mgr-filter-chip"
      :class="{ active: !allActive && selected.includes(opt.key) }"
      @click="toggle(opt.key)"
    >
      <Icon :name="opt.icon" :size="13" />
      <span>{{ opt.label }}</span>
    </button>
  </div>
</template>

<style scoped>
.mgr-filter {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.mgr-filter-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 30px;
  padding: 0 12px;
  border-radius: 999px;
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  color: var(--fg-2);
  font-family: var(--font-mono);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  cursor: pointer;
  transition: background 0.12s, color 0.12s, border-color 0.12s;
}
.mgr-filter-chip:hover { background: rgb(var(--ink) / 0.09); color: var(--fg-0); }
.mgr-filter-chip.active {
  color: var(--gold-bright);
  background: var(--gold-soft);
  border-color: color-mix(in srgb, var(--gold) 45%, transparent);
}
</style>
