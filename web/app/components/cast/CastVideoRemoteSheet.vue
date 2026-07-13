<template>
  <AppSheet v-model:open="open" size="full" title="Video remote">
    <template #header>
      <header class="app-sheet-header cvrs-header">
        <span>
          <DrawerTitle as="h3" class="app-sheet-title">Video remote</DrawerTitle>
          <small v-if="session">Playing on {{ session.device_name }}</small>
        </span>
        <button type="button" aria-label="Close" @click="open = false"><Icon name="close" :size="18" /></button>
      </header>
    </template>
    <div class="cvrs-body">
      <CastVideoRemote @disconnected="open = false" />
    </div>
  </AppSheet>
</template>

<script setup lang="ts">
import { DrawerTitle } from 'reka-ui'

const open = defineModel<boolean>('open', { default: false })
const cast = useCastStore()
const session = computed(() => cast.session?.media_kind === 'video' ? cast.session : null)
watch(session, (value) => { if (!value) open.value = false })
</script>

<style>
.cvrs-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.cvrs-header > span { display: flex; min-width: 0; flex-direction: column; gap: 2px; }
.cvrs-header .app-sheet-title { margin: 0; }
.cvrs-header small { overflow: hidden; color: var(--fg-3); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.cvrs-header button {
  display: inline-grid;
  place-items: center;
  width: 34px;
  height: 34px;
  flex-shrink: 0;
  border: 0;
  border-radius: 50%;
  background: transparent;
  color: var(--fg-2);
  cursor: pointer;
}
.cvrs-header button:active { background: rgb(var(--ink) / 0.08); }
.cvrs-body { height: 100%; overflow-y: auto; overscroll-behavior: contain; }
</style>
