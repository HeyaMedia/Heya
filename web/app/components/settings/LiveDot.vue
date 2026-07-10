<script setup lang="ts">
defineProps<{ connected?: boolean, label?: string }>()
</script>

<template>
  <span class="sv2-live" :class="{ on: connected }">
    <span class="sv2-live-pulse" />
    <span v-if="label" class="sv2-live-label">{{ label }}</span>
    <span v-else class="sv2-live-label">{{ connected ? 'Live' : 'Offline' }}</span>
  </span>
</template>

<style scoped>
.sv2-live {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--fg-3);
}
.sv2-live.on { color: var(--good); }
.sv2-live-pulse {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--fg-4);
}
.sv2-live.on .sv2-live-pulse {
  background: var(--good);
  animation: sv2-live-pulse 1.5s ease-in-out infinite;
}
@keyframes sv2-live-pulse {
  0%   { box-shadow: 0 0 0 0   color-mix(in srgb, var(--good) 50%, transparent); }
  70%  { box-shadow: 0 0 0 6px transparent; }
  100% { box-shadow: 0 0 0 0   transparent; }
}
</style>
