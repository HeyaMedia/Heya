<script setup lang="ts">
defineProps<{
  state: 'ok' | 'warn' | 'error' | 'idle'
}>()
</script>

<template>
  <span class="sv2-badge" :class="`sv2-badge-${state}`">
    <span class="sv2-badge-dot" />
    <slot />
  </span>
</template>

<style scoped>
.sv2-badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 3px 8px;
  border-radius: 999px;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.02em;
  border: 1px solid transparent;
}

.sv2-badge-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex-shrink: 0;
}

.sv2-badge-ok {
  color: var(--good);
  background: rgba(111, 191, 124, 0.10);
  border-color: rgba(111, 191, 124, 0.25);
}
.sv2-badge-ok .sv2-badge-dot { background: var(--good); }

.sv2-badge-warn {
  color: var(--gold);
  background: var(--gold-soft);
  border-color: rgba(230, 185, 74, 0.30);
}
.sv2-badge-warn .sv2-badge-dot { background: var(--gold); }

.sv2-badge-error {
  color: var(--bad);
  background: rgba(217, 107, 107, 0.10);
  border-color: rgba(217, 107, 107, 0.30);
}
.sv2-badge-error .sv2-badge-dot { background: var(--bad); }

.sv2-badge-idle {
  color: var(--fg-3);
  background: rgba(255, 255, 255, 0.04);
  border-color: var(--border);
}
.sv2-badge-idle .sv2-badge-dot { background: var(--fg-4); }
</style>
