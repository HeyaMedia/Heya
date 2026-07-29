<script setup lang="ts">
import { MOCK_LIBRARY_OPTIONS, type MgrLibraryKey } from '~/utils/managerMock'

const props = defineProps<{ library: MgrLibraryKey }>()

const opt = computed(() =>
  MOCK_LIBRARY_OPTIONS.find(o => o.key === props.library)
  ?? { key: props.library, label: props.library, icon: 'folder' })
</script>

<template>
  <span class="mgr-lib-chip">
    <Icon :name="opt.icon" :size="12" />
    <span>{{ opt.label }}</span>
  </span>
</template>

<style scoped>
.mgr-lib-chip {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 2px 8px;
  border-radius: 999px;
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  color: var(--fg-2);
  font-family: var(--font-mono);
  font-size: 10.5px;
  font-weight: 600;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  white-space: nowrap;
}
</style>
