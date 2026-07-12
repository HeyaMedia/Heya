<!--
  Cast output picker — the topbar entry point for server-side casting
  (docs/cast-plan.md Phase 2). Replaces the placeholder button that sat
  here since the topbar was built. Lists the receivers the server
  discovered over mDNS; picking one hands current playback off to it
  mid-track (usePlayer.startCastTo), picking the connected one — or
  Disconnect — releases it back to local output. Renders nothing until
  discovery finds a device (or a session already exists).
-->
<template>
  <AppMenu
    v-if="visible"
    v-model="menuOpen"
    align="end"
    :width="280"
    :trigger-class="{ 'btn-icon': true, 'topbar-cast-btn': true, active: cast.engaged }"
    :trigger-title="cast.engaged ? `Casting to ${cast.deviceName}` : 'Cast'"
    trigger-aria-label="Cast to a device"
  >
    <template #trigger>
      <Icon :name="connecting ? 'loading' : 'cast'" :size="18" :class="{ 'cast-btn-spin': connecting }" />
    </template>

    <div class="surface-section-label cast-menu-label">
      {{ cast.engaged ? `Casting to ${cast.deviceName}` : 'Play on' }}
    </div>
    <template v-if="cast.devices.length">
      <DropdownMenuItem
        v-for="d in cast.devices"
        :key="d.id"
        class="surface-item app-context-item cast-device-item"
        @select="pick(d.id)"
      >
        <Icon name="speakerhigh" :size="15" class="surface-item-icon" />
        <span class="cast-device-text">
          <span class="cast-device-name">{{ d.name }}</span>
          <span v-if="deviceSub(d)" class="cast-device-sub">{{ deviceSub(d) }}</span>
        </span>
        <Icon v-if="cast.engagedDeviceId === d.id" name="check" :size="13" class="cast-device-check" />
      </DropdownMenuItem>
    </template>
    <div v-else class="cast-menu-empty">
      No cast devices found on the network
    </div>

    <template v-if="cast.engaged">
      <DropdownMenuSeparator class="surface-divider" />
      <DropdownMenuItem class="surface-item app-context-item cast-disconnect" @select="disconnect">
        <Icon name="close" :size="14" class="surface-item-icon" />
        <span>Disconnect</span>
      </DropdownMenuItem>
    </template>
  </AppMenu>
</template>

<script setup lang="ts">
import { DropdownMenuItem, DropdownMenuSeparator } from 'reka-ui'
import type { CastDevice } from '~/composables/useCast'

const cast = useCastStore()
const { startCastTo, stopCasting } = usePlayerBindings()

const visible = computed(() => cast.devices.length > 0 || cast.engaged)
const menuOpen = ref(false)
// Re-browse on every open — receivers come and go (power state, sleep).
watch(menuOpen, (open) => {
  if (open) void cast.refreshDevices()
})

const connecting = computed(() => cast.connecting || cast.session?.state === 'starting')

function pick(deviceId: string) {
  // Picking the connected device again is the disconnect gesture.
  if (cast.engagedDeviceId === deviceId) { void stopCasting(); return }
  void startCastTo(deviceId)
}
function disconnect() { void stopCasting() }

function deviceSub(d: CastDevice) {
  return [d.manufacturer, d.model].filter(Boolean).join(' ')
}
</script>

<!-- Menu content is portaled by AppMenu — these rules must be unscoped
     (docs/ui.md: scoped CSS doesn't reach portaled children). The phone
     rule lives here too: AppTopBar's scoped styles can't reliably reach a
     child component's trigger, and the phone set is brand/search/avatar
     only (same reasoning as the old placeholder's hide rule). -->
<style>
.cast-menu-label {
  padding: 8px 14px 4px;
}
.cast-menu-empty {
  padding: 10px 14px 12px;
  font-size: 12px;
  color: var(--fg-3);
}
.cast-device-item {
  min-height: 44px;
}
.cast-device-text {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}
.cast-device-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.cast-device-sub {
  font-size: 10px;
  color: var(--fg-3);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.cast-device-check {
  color: var(--gold-bright, var(--gold));
  flex-shrink: 0;
}
.cast-disconnect { color: var(--bad); }
.cast-disconnect[data-highlighted],
.cast-disconnect:hover { background: color-mix(in srgb, var(--bad) 8%, transparent); color: var(--bad); }

.cast-btn-spin {
  animation: cast-btn-spin 0.9s linear infinite;
}
@keyframes cast-btn-spin {
  to { transform: rotate(360deg); }
}

@media (max-width: 720px) {
  .topbar-cast-btn { display: none; }
}
</style>
