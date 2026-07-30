<script setup lang="ts">
// Sonarr-style coverage pill: "have / total" printed over a proportional
// fill. Red = nothing, amber = partial, green = complete, grey = nothing to
// count. The fill makes coverage scannable down a column without reading
// the numbers.
const props = defineProps<{
  have: number
  total: number
  /** Override the derived tone (e.g. seasons count aired, not total). */
  tone?: 'good' | 'warn' | 'bad' | 'none'
}>()

const tone = computed(() => {
  if (props.tone) return props.tone
  if (props.total <= 0) return 'none'
  if (props.have <= 0) return 'bad'
  if (props.have < props.total) return 'warn'
  return 'good'
})
const pct = computed(() =>
  props.total > 0 ? Math.min(100, Math.round((props.have / props.total) * 100)) : 0)
</script>

<template>
  <span class="mcp" :class="`tone-${tone}`">
    <span class="mcp-fill" :style="{ width: `${pct}%` }" />
    <span class="mcp-label">{{ have }} / {{ total }}</span>
  </span>
</template>

<style scoped>
.mcp {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 62px;
  height: 18px;
  padding: 0 8px;
  border-radius: 4px;
  overflow: hidden;
  background: rgb(var(--ink) / 0.08);
  border: 1px solid var(--border);
  vertical-align: middle;
}
.mcp-fill {
  position: absolute;
  inset: 0 auto 0 0;
  transition: width 0.2s;
}
.mcp.tone-good { border-color: color-mix(in srgb, var(--good) 45%, transparent); }
.mcp.tone-good .mcp-fill { background: color-mix(in srgb, var(--good) 28%, transparent); }
.mcp.tone-warn { border-color: color-mix(in srgb, var(--gold) 45%, transparent); }
.mcp.tone-warn .mcp-fill { background: color-mix(in srgb, var(--gold) 24%, transparent); }
.mcp.tone-bad { border-color: color-mix(in srgb, var(--bad) 45%, transparent); }
.mcp.tone-bad .mcp-fill { background: color-mix(in srgb, var(--bad) 24%, transparent); }
.mcp-label {
  position: relative;
  font-family: var(--font-mono);
  font-size: 10.5px;
  font-weight: 600;
  line-height: 1;
  color: var(--fg-0);
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
}
</style>
