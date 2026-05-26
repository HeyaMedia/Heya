<!--
  AppSwitch — boolean toggle with knob.

  Reka's SwitchRoot does the a11y, keyboard (Space toggles), focus management.
  We provide a knob + track styled with the surface design tokens so it slots
  cleanly into the existing visual system. Defaults to a small variant for use
  alongside btn-icons; pass `size="md"` for settings-row scale.

  Usage:
    <AppSwitch v-model="enabled" />
    <AppSwitch v-model="enabled" size="md" :label="'Pre-amp'" />

  Pair with a sibling <label> via `for` or wrap in <label> for click-to-toggle;
  reka's SwitchRoot also handles label-association internally when a label is
  in the same form-control context.
-->
<template>
  <SwitchRoot
    v-model:checked="checked"
    class="app-switch"
    :class="[`app-switch-${size}`, { 'has-label': !!label }]"
    :aria-label="ariaLabel ?? label"
    :disabled="disabled"
  >
    <SwitchThumb class="app-switch-thumb" />
    <span v-if="label" class="app-switch-label">{{ label }}</span>
  </SwitchRoot>
</template>

<script setup lang="ts">
import { SwitchRoot, SwitchThumb } from 'reka-ui'

withDefaults(defineProps<{
  label?: string
  ariaLabel?: string
  size?: 'sm' | 'md'
  disabled?: boolean
}>(), {
  size: 'sm',
})

const checked = defineModel<boolean>({ default: false })
</script>

<style scoped>
.app-switch {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  background: rgba(255, 255, 255, 0.08);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 999px;
  padding: 2px;
  cursor: pointer;
  position: relative;
  transition: background 0.15s, border-color 0.15s;
}
.app-switch:hover { border-color: rgba(255, 255, 255, 0.22); }
.app-switch:focus-visible {
  outline: 2px solid rgba(251, 191, 36, 0.4);
  outline-offset: 2px;
}
.app-switch[data-disabled] { opacity: 0.4; cursor: not-allowed; }

.app-switch-sm { width: 36px; height: 20px; }
.app-switch-md { width: 44px; height: 24px; }

.app-switch[data-state="checked"] {
  background: var(--gold-soft);
  border-color: rgba(230, 185, 74, 0.4);
}

.app-switch-thumb {
  display: block;
  border-radius: 50%;
  background: var(--fg-3);
  transition: transform 0.18s cubic-bezier(0.16, 1, 0.3, 1), background 0.15s, box-shadow 0.15s;
  pointer-events: none;
}
.app-switch-sm .app-switch-thumb { width: 14px; height: 14px; }
.app-switch-md .app-switch-thumb { width: 18px; height: 18px; }

.app-switch[data-state="checked"] .app-switch-thumb {
  background: var(--gold);
  box-shadow: 0 0 6px var(--gold);
}
.app-switch-sm[data-state="checked"] .app-switch-thumb { transform: translateX(16px); }
.app-switch-md[data-state="checked"] .app-switch-thumb { transform: translateX(20px); }

/* Inline label (rare for sm; common for md in settings rows). The switch
   shape is still the leftmost interactive element; the label sits beside
   it inside the same hit target. */
.app-switch.has-label {
  width: auto;
  padding-right: 12px;
  gap: 10px;
}
.app-switch-label {
  font-size: 13px;
  font-weight: 500;
  color: var(--fg-1);
  user-select: none;
}
</style>
