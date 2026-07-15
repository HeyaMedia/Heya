<script setup lang="ts">
defineProps<{
  label: string
  description?: string
  lockedBy?: string         // env-var name if managed externally
  hint?: string
}>()

// A stable per-field id so the visible <label> is programmatically tied to
// its control. Exposed through the slot as `fieldId` (bind to the input's
// :id) and `hintId` (bind to :aria-describedby) — without that binding the
// label is visual-only and screen readers announce the input nameless.
const fieldId = useId()
const hintId = `${fieldId}-hint`
</script>

<template>
  <div class="sv2-field" :class="{ locked: !!lockedBy }">
    <label class="sv2-field-label" :for="fieldId">
      {{ label }}
      <span v-if="lockedBy" class="sv2-field-lock" :title="`Managed by ${lockedBy}`">
        <Icon name="key" :size="10" />
        env
      </span>
    </label>
    <p v-if="description" class="sv2-field-desc">{{ description }}</p>
    <div class="sv2-field-control">
      <slot :locked="!!lockedBy" :field-id="fieldId" :hint-id="hint ? hintId : undefined" />
    </div>
    <p v-if="hint" :id="hintId" class="sv2-field-hint">{{ hint }}</p>
  </div>
</template>

<style scoped>
.sv2-field {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 15px 0;
  border-bottom: 1px solid var(--border);
}
.sv2-field:last-child { border-bottom: 0; }
.sv2-field.locked .sv2-field-control { opacity: 0.6; pointer-events: none; }

.sv2-field-label {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12.5px;
  font-weight: 580;
  color: var(--fg-0);
}
/* Crisp mono ENV chip — the config-provenance affordance: env-locked fields
   grey their control (below) and carry this bordered mono tag so the source
   is unmistakable. */
.sv2-field-lock {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 7px;
  border-radius: var(--r-xs);
  border: 1px solid color-mix(in srgb, var(--gold) 40%, transparent);
  background: var(--gold-soft);
  color: var(--gold-bright);
  font-family: var(--font-mono);
  font-size: 9px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.12em;
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
