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
.info-grid {
  display: grid; grid-template-columns: auto 1fr; gap: 6px 20px;
  font-size: 12px; margin-top: 16px; max-width: 560px;
  padding: 12px 16px;
  /* Theme glass — the old 3% ink wash vanished over ambient artwork and
     left the labels unreadable in light mode. */
  background: color-mix(in oklab, var(--bg-2) 78%, transparent);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  border-radius: var(--r-md);
  border: 1px solid var(--border);
  box-shadow: var(--shadow-el);
}
.info-grid :deep(.info-label) {
  color: var(--fg-3); font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.06em;
  font-size: 10px; padding-top: 3px;
}
.info-grid :deep(.info-value) {
  font-size: 13px; color: var(--fg-1); line-height: 1.5;
}
.info-label {
  color: var(--fg-3); font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.06em;
  font-size: 10px; padding-top: 3px;
}
.info-value { font-size: 13px; color: var(--fg-1); line-height: 1.5; }
</style>
