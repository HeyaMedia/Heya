<script setup lang="ts">
defineProps<{
  label: string
  description?: string
  lockedBy?: string         // env-var name if managed externally
  hint?: string
}>()
</script>

<template>
  <div class="sv2-field" :class="{ locked: !!lockedBy }">
    <label class="sv2-field-label">
      {{ label }}
      <span v-if="lockedBy" class="sv2-field-lock" :title="`Managed by ${lockedBy}`">
        <Icon name="key" :size="10" />
        env
      </span>
    </label>
    <p v-if="description" class="sv2-field-desc">{{ description }}</p>
    <div class="sv2-field-control">
      <slot :locked="!!lockedBy" />
    </div>
    <p v-if="hint" class="sv2-field-hint">{{ hint }}</p>
  </div>
</template>

<style scoped>
.sv2-field {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 14px 0;
  border-bottom: 1px solid var(--border);
}
.sv2-field:last-child { border-bottom: 0; }
.sv2-field.locked .sv2-field-control { opacity: 0.6; pointer-events: none; }

.sv2-field-label {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  font-weight: 500;
  color: var(--fg-1);
}
.sv2-field-lock {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  padding: 2px 6px;
  border-radius: var(--r-xs);
  background: var(--gold-soft);
  color: var(--gold);
  font-family: var(--font-mono);
  font-size: 9px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
}
.sv2-field-desc {
  margin: 0;
  font-size: 11.5px;
  color: var(--fg-3);
  line-height: 1.5;
}
.sv2-field-control { margin-top: 2px; }
.sv2-field-hint {
  margin: 4px 0 0;
  font-size: 11px;
  color: var(--fg-4);
  font-style: italic;
}
</style>
