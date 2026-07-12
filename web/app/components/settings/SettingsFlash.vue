<script setup lang="ts">
import type { FlashMessage } from '~/composables/useFlash'

defineProps<{ flash: FlashMessage | null }>()
</script>

<template>
  <div
    v-if="flash"
    class="sv2-flash"
    :class="flash.kind"
    :role="flash.kind === 'err' ? 'alert' : 'status'"
    :aria-live="flash.kind === 'err' ? 'assertive' : 'polite'"
  >
    <Icon :name="flash.kind === 'ok' ? 'check' : 'warning'" :size="13" aria-hidden="true" />
    {{ flash.text }}
  </div>
</template>

<style scoped>
.sv2-flash {
  margin-top: 16px;
  padding: 10px 14px;
  border-radius: var(--r-sm);
  font-size: 12px;
  display: flex; align-items: center; gap: 8px;
}
.sv2-flash.ok   { background: color-mix(in srgb, var(--good) 10%, transparent); border: 1px solid color-mix(in srgb, var(--good) 25%, transparent); color: var(--good); }
.sv2-flash.warn { background: color-mix(in srgb, var(--gold) 10%, transparent); border: 1px solid color-mix(in srgb, var(--gold) 30%, transparent); color: var(--gold); }
.sv2-flash.err  { background: color-mix(in srgb, var(--bad) 10%, transparent); border: 1px solid color-mix(in srgb, var(--bad) 30%, transparent); color: var(--bad); }
</style>
