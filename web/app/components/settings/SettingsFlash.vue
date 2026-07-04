<script setup lang="ts">
import type { FlashMessage } from '~/composables/useFlash'

defineProps<{ flash: FlashMessage | null }>()
</script>

<template>
  <div v-if="flash" class="sv2-flash" :class="flash.kind">
    <Icon :name="flash.kind === 'ok' ? 'check' : 'warning'" :size="13" />
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
.sv2-flash.ok   { background: rgba(111,191,124,0.10); border: 1px solid rgba(111,191,124,0.25); color: var(--good); }
.sv2-flash.warn { background: rgba(230,185,74,0.10); border: 1px solid rgba(230,185,74,0.30); color: var(--gold); }
.sv2-flash.err  { background: rgba(217,107,107,0.10); border: 1px solid rgba(217,107,107,0.30); color: var(--bad); }
</style>
