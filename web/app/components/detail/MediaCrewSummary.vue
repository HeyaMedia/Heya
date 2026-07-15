<script setup lang="ts">
import type { CrewMember } from '~~/shared/types'

const props = defineProps<{ crew?: CrewMember[] | null }>()

const JOBS = ['Director', 'Screenplay', 'Writer', 'Producer', 'Original Music Composer', 'Director of Photography'] as const
const LABELS: Record<string, string> = {
  Director: 'Director',
  Screenplay: 'Writer',
  Writer: 'Writer',
  Producer: 'Producer',
  'Original Music Composer': 'Music',
  'Director of Photography': 'Cinematography',
}
const ORDER = ['Director', 'Screenplay', 'Producer', 'Original Music Composer', 'Director of Photography']

const rows = computed(() => {
  if (!props.crew?.length) return []
  const byJob: Record<string, string[]> = {}
  for (const c of props.crew) {
    if ((JOBS as readonly string[]).includes(c.job)) {
      const list = byJob[c.job] ?? (byJob[c.job] = [])
      if (!list.includes(c.name)) list.push(c.name)
    }
  }
  return ORDER
    .filter(j => byJob[j])
    .map(j => ({ label: LABELS[j] || j, value: byJob[j]!.slice(0, 3).join(', ') }))
})

const hasExtra = useSlots().extra != null
</script>

<template>
  <div v-if="rows.length || hasExtra" class="info-grid">
    <template v-for="r in rows" :key="r.label">
      <div class="info-label">{{ r.label }}</div>
      <div class="info-value">{{ r.value }}</div>
    </template>
    <slot name="extra" />
  </div>
</template>

<style scoped>
/* heya2.css `.credits`: hairline-ruled key/value rows (no glass panel). The
   own rows and the #extra slot's rows are both flat `.info-label`/`.info-value`
   pairs, so a single 130px/1fr grid with per-cell bottom borders renders them
   identically — the top border closes the first row, each cell's bottom border
   rules the rest. `:deep()` reaches the slotted cells (they carry the consumer's
   scope, not this component's). */
.info-grid {
  display: grid;
  grid-template-columns: 130px 1fr;
  gap: 0 18px;
  margin-top: 16px;
  border-top: 1px solid var(--hair);
}
.info-grid :deep(.info-label),
.info-label {
  font: 600 10.5px var(--font-mono);
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: rgb(var(--ink) / 0.45);
  padding: 11px 0;
  border-bottom: 1px solid var(--hair);
}
.info-grid :deep(.info-value),
.info-value {
  font-size: 14px;
  color: rgb(var(--ink) / 0.88);
  line-height: 1.5;
  padding: 11px 0;
  border-bottom: 1px solid var(--hair);
}
.info-grid :deep(.info-value) a:hover,
.info-value a:hover { color: var(--tone); }
</style>
