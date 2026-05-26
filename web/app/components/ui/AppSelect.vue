<!--
  AppSelect — value-picking dropdown.

  Wraps reka-ui's Select primitives with the shared .surface chrome from
  surface.css so it sits in the same visual system as AppMenu (user/activity
  popovers). Replaces the hand-rolled Dropdown.vue — reka covers all the
  positioning / keyboard nav / type-ahead / focus management we had been
  writing by hand, and the surface utility makes the look consistent with
  everything else that floats above the page.

  Usage:
    <AppSelect v-model="value" :options="opts" @change="save" />

  Each option is `{ value, label, meta? }`. Use a non-empty sentinel for the
  "default" entry — reka treats an empty string as "no value" and falls back
  to the placeholder rather than showing your zero-state row.
-->
<template>
  <SelectRoot :model-value="modelValue" @update:model-value="onSelect">
    <SelectTrigger
      class="app-select-trigger"
      :class="{ 'is-custom': isCustom }"
      :aria-label="ariaLabel"
    >
      <SelectValue class="app-select-label" :placeholder="placeholder ?? 'Select…'" />
      <Icon name="chevdown" :size="12" class="app-select-chev" />
    </SelectTrigger>

    <SelectPortal>
      <SelectContent
        class="surface app-select-content"
        position="popper"
        :side-offset="4"
        :align="align"
      >
        <SelectViewport class="app-select-viewport">
          <SelectItem
            v-for="opt in options"
            :key="opt.value"
            :value="opt.value"
            class="surface-item app-select-item"
          >
            <SelectItemText>{{ opt.label }}</SelectItemText>
            <span v-if="opt.meta" class="app-select-item-meta">{{ opt.meta }}</span>
            <SelectItemIndicator class="app-select-item-check">
              <Icon name="check" :size="13" />
            </SelectItemIndicator>
          </SelectItem>
        </SelectViewport>
      </SelectContent>
    </SelectPortal>
  </SelectRoot>
</template>

<script setup lang="ts">
import {
  SelectRoot, SelectTrigger, SelectValue, SelectPortal,
  SelectContent, SelectViewport, SelectItem, SelectItemText, SelectItemIndicator,
} from 'reka-ui'

export interface SelectOption {
  value: string
  label: string
  meta?: string
}

const props = withDefaults(defineProps<{
  modelValue: string
  options: SelectOption[]
  placeholder?: string
  ariaLabel?: string
  align?: 'start' | 'center' | 'end'
  // Adds a gold tint when the current value is anything other than the first
  // option — used by PlaybackPrefs to signal an explicit non-default choice.
  // Defaults to the first option's value as the "neutral" baseline.
  customBaseline?: string
}>(), {
  align: 'start',
})

const emit = defineEmits<{
  'update:modelValue': [string]
  change: [string]
}>()

function onSelect(v: string | number | object | string[] | number[] | object[] | null | undefined) {
  const next = typeof v === 'string' ? v : (v == null ? '' : String(v))
  emit('update:modelValue', next)
  emit('change', next)
}

const isCustom = computed(() => {
  const baseline = props.customBaseline ?? props.options[0]?.value ?? ''
  return props.modelValue !== baseline && props.modelValue != null && props.modelValue !== ''
})
</script>

<style scoped>
.app-select-trigger {
  display: flex; align-items: center; gap: 8px;
  width: 100%;
  padding: 7px 10px 7px 12px;
  font-size: 12px; font-weight: 500;
  font-family: inherit;
  color: rgba(255, 255, 255, 0.85);
  background: rgba(255, 255, 255, 0.06);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: var(--r-sm);
  cursor: pointer;
  outline: none;
  text-align: left;
  transition: background 0.12s, border-color 0.12s, color 0.12s;
}
.app-select-trigger:hover {
  background: rgba(255, 255, 255, 0.1);
  border-color: rgba(255, 255, 255, 0.22);
  color: #fff;
}
.app-select-trigger[data-state="open"] {
  border-color: var(--gold);
  background: rgba(255, 255, 255, 0.08);
}
.app-select-trigger.is-custom {
  color: var(--gold);
  border-color: rgba(251, 191, 36, 0.35);
  background: rgba(251, 191, 36, 0.08);
}
.app-select-trigger.is-custom[data-state="open"] {
  border-color: var(--gold);
  background: rgba(251, 191, 36, 0.12);
}
.app-select-trigger:focus-visible {
  outline: 2px solid rgba(251, 191, 36, 0.4);
  outline-offset: 2px;
}

.app-select-label {
  flex: 1; min-width: 0;
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.app-select-chev {
  flex-shrink: 0;
  opacity: 0.7;
  transition: opacity 0.12s, transform 0.18s ease;
}
.app-select-trigger:hover .app-select-chev,
.app-select-trigger[data-state="open"] .app-select-chev { opacity: 1; }
.app-select-trigger[data-state="open"] .app-select-chev { transform: rotate(180deg); }
</style>

<!--
  Content is portaled out of the trigger's component instance, so its
  styling has to live unscoped or the scope-id selectors won't reach it.
-->
<style>
.app-select-content {
  min-width: var(--reka-select-trigger-width);
  max-height: min(280px, var(--reka-select-content-available-height));
  padding: 4px;
}
.app-select-viewport {
  scrollbar-width: thin;
  scrollbar-color: rgba(255, 255, 255, 0.2) transparent;
}
.app-select-viewport::-webkit-scrollbar { width: 8px; }
.app-select-viewport::-webkit-scrollbar-thumb {
  background: rgba(255, 255, 255, 0.15);
  border-radius: 4px;
}

.app-select-item {
  /* Inherits .surface-item base; tighten the padding/font for select rows
     vs full menu rows, and reserve a right-edge slot for the check mark. */
  padding: 7px 10px;
  font-size: 12px;
  border-radius: var(--r-xs);
  position: relative;
}
.app-select-item[data-state="checked"] {
  color: var(--gold);
  font-weight: 600;
}
.app-select-item[data-state="checked"][data-highlighted] {
  background: var(--gold-soft);
  color: var(--gold);
}

.app-select-item-meta {
  font-size: 10px;
  font-family: var(--font-mono);
  color: var(--fg-4);
  flex-shrink: 0;
  margin-left: auto;
}
.app-select-item[data-state="checked"] .app-select-item-meta {
  color: var(--gold);
  opacity: 0.65;
}
.app-select-item-check {
  margin-left: auto;
  color: var(--gold);
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
}
/* When both meta and check are present, push the check to the very right
   and let meta sit just before it. */
.app-select-item:has(.app-select-item-meta) .app-select-item-check {
  margin-left: 6px;
}
</style>
