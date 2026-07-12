<script setup lang="ts">
import { computed } from 'vue'

type Row = {
  key: string
  value: string | number | null | undefined
  mono?: boolean
  copy?: boolean
}

const props = defineProps<{ rows: Row[] }>()

const visibleRows = computed(() => props.rows.filter(r => r.value !== undefined && r.value !== null && r.value !== ''))

// Copy feedback: a transient per-row "copied" state (icon swap) plus a
// polite live-region announcement, so the action isn't silent to screen
// readers and low-vision users (the button had no success signal before).
const copiedKey = ref<string | null>(null)
const announce = ref('')
async function copy(row: Row) {
  try {
    await navigator.clipboard.writeText(String(row.value))
    copiedKey.value = row.key
    announce.value = `Copied ${row.key}`
    setTimeout(() => { if (copiedKey.value === row.key) copiedKey.value = null }, 1500)
  } catch {
    announce.value = `Couldn't copy ${row.key}`
  }
}
</script>

<template>
  <div class="sv2-kv">
    <div v-for="r in visibleRows" :key="r.key" class="sv2-kv-row">
      <span class="sv2-kv-k">{{ r.key }}</span>
      <span class="sv2-kv-v" :class="{ mono: r.mono }">
        {{ r.value }}
        <button
          v-if="r.copy && r.value != null"
          class="sv2-kv-copy"
          :class="{ copied: copiedKey === r.key }"
          :aria-label="copiedKey === r.key ? `Copied ${r.key}` : `Copy ${r.key}`"
          :title="`Copy ${r.key}`"
          @click="copy(r)"
        ><Icon :name="copiedKey === r.key ? 'check' : 'clipboard'" :size="11" aria-hidden="true" /></button>
      </span>
    </div>
    <span class="sr-only" role="status" aria-live="polite">{{ announce }}</span>
  </div>
</template>

<style scoped>
.sv2-kv {
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  background: var(--bg-2);
  overflow: hidden;
}
.sv2-kv-row {
  display: grid;
  grid-template-columns: 200px 1fr;
  padding: 10px 16px;
  gap: 16px;
  border-bottom: 1px solid var(--border);
  font-size: 12px;
}
.sv2-kv-row:last-child { border-bottom: 0; }
.sv2-kv-k { color: var(--fg-3); }
.sv2-kv-v {
  color: var(--fg-1);
  display: flex;
  align-items: center;
  gap: 6px;
  word-break: break-word;
}
.sv2-kv-v.mono { font-family: var(--font-mono); font-size: 11.5px; }
.sv2-kv-copy {
  opacity: 0;
  color: var(--fg-3);
  padding: 2px;
  border-radius: var(--r-xs);
  transition: opacity 0.12s, background 0.12s;
}
.sv2-kv-row:hover .sv2-kv-copy { opacity: 1; }
.sv2-kv-copy:hover { background: rgb(var(--ink) / 0.05); color: var(--fg-1); }
.sv2-kv-copy.copied { opacity: 1; color: var(--good); }

/* Phone: the 200px key column leaves almost nothing for the value (paths,
   URLs, version strings) at 390px. Stack key above value instead. */
@media (max-width: 720px) {
  .sv2-kv-row {
    grid-template-columns: 1fr;
    gap: 3px;
    padding: 10px 14px;
  }
  .sv2-kv-k {
    font-size: 10px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--fg-4);
  }
  .sv2-kv-copy { opacity: 1; }
}
</style>
