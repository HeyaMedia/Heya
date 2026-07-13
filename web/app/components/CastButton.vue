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
    :width="videoCastSession ? 336 : 304"
    :trigger-class="{ 'btn-icon': true, 'topbar-cast-btn': true, active: cast.engaged }"
    :trigger-title="cast.engaged ? `Casting to ${cast.deviceName}` : 'Cast'"
    trigger-aria-label="Cast to a device"
    content-class="cast-menu-surface"
  >
    <template #trigger>
      <Icon :name="connecting ? 'loading' : 'cast'" :size="18" :class="{ 'cast-btn-spin': connecting }" />
    </template>

    <CastVideoRemote v-if="videoCastSession" compact />
    <div v-else-if="cast.engaged" class="cast-menu-current">
      <span class="cast-device-icon is-active">
        <Icon name="cast" :size="16" />
      </span>
      <span class="cast-device-text">
        <span class="cast-current-label">Now casting</span>
        <span class="cast-device-name">{{ cast.deviceName }}</span>
      </span>
    </div>
    <DropdownMenuSeparator v-if="cast.engaged" class="surface-divider cast-menu-divider" />
    <template v-if="audioOnlyDevices.length">
      <div class="surface-section-label cast-menu-label">
        <span>Audio only</span>
        <span class="cast-menu-count">{{ audioOnlyDevices.length }}</span>
      </div>
      <DropdownMenuItem
        v-for="d in audioOnlyDevices"
        :key="d.id"
        class="surface-item app-context-item cast-device-item"
        :class="{ 'is-active': cast.engagedDeviceId === d.id }"
        :disabled="!!videoCastSession"
        @select="pick(d.id)"
      >
        <span class="cast-device-icon">
          <Icon name="speakerhigh" :size="15" />
        </span>
        <span class="cast-device-text">
          <span class="cast-device-name">{{ d.name }}</span>
          <span v-if="deviceSub(d)" class="cast-device-sub">{{ deviceSub(d) }}</span>
        </span>
        <Icon v-if="cast.engagedDeviceId === d.id" name="check" :size="13" class="cast-device-check" />
      </DropdownMenuItem>
    </template>
    <DropdownMenuSeparator v-if="audioOnlyDevices.length && videoCapableDevices.length" class="surface-divider cast-menu-divider" />
    <template v-if="videoCapableDevices.length">
      <div class="surface-section-label cast-menu-label">
        <span>Video capable</span>
        <span class="cast-menu-count">{{ videoCapableDevices.length }}</span>
      </div>
      <DropdownMenuItem
        v-for="d in videoCapableDevices"
        :key="d.id"
        class="surface-item app-context-item cast-device-item"
        :class="{ 'is-active': cast.engagedDeviceId === d.id }"
        @select="pick(d.id)"
      >
        <span class="cast-device-icon is-video">
          <Icon name="television-simple" :size="15" />
        </span>
        <span class="cast-device-text">
          <span class="cast-device-name">{{ d.name }}</span>
          <span v-if="deviceSub(d)" class="cast-device-sub">{{ deviceSub(d) }}</span>
        </span>
        <Icon v-if="cast.engagedDeviceId === d.id" name="check" :size="13" class="cast-device-check" />
      </DropdownMenuItem>
    </template>
    <div v-if="!audioOnlyDevices.length && !videoCapableDevices.length" class="cast-menu-empty">
      No cast devices found on the network
    </div>

    <template v-if="cast.engaged">
      <DropdownMenuSeparator class="surface-divider cast-menu-divider" />
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
const { toast } = useToast()

const visible = computed(() => cast.devices.length > 0 || cast.engaged)
const videoCastSession = computed(() => cast.session?.media_kind === 'video' ? cast.session : null)
const supportsVideo = (d: CastDevice) => d.capabilities?.includes('video') ?? false
const audioOnlyDevices = computed(() => cast.devices.filter(d => !supportsVideo(d)))
const videoCapableDevices = computed(() => cast.devices.filter(supportsVideo))
const menuOpen = ref(false)
// Re-browse on every open — receivers come and go (power state, sleep).
watch(menuOpen, (open) => {
  if (open) void cast.refreshDevices()
})

const connecting = computed(() => cast.connecting || cast.session?.state === 'starting')

async function pick(deviceId: string) {
  // Picking the connected device again is the disconnect gesture.
  if (cast.engagedDeviceId === deviceId) { disconnect(); return }
  const video = videoCastSession.value
  if (video?.file_id && video.entity_type && video.entity_id) {
    const position = cast.livePositionSec()
    try {
      await cast.stopSession()
      cast.engagedDeviceId = deviceId
      await cast.playVideo({
        fileId: video.file_id,
        entityType: video.entity_type,
        entityId: video.entity_id,
        title: video.title,
        audioTrack: video.audio_track,
        subtitleTrack: video.subtitle_track,
        quality: video.quality,
        fallbackVolume: video.volume,
        startSeconds: position,
        startPaused: video.state === 'paused',
      })
    } catch (error) {
      cast.engagedDeviceId = null
      toast.err(error instanceof Error ? error.message : 'Could not move Chromecast playback')
    }
    return
  }
  void startCastTo(deviceId)
}
function disconnect() {
  if (videoCastSession.value) void cast.disconnect()
  else void stopCasting()
}

function deviceSub(d: CastDevice) {
  if (d.provider === 'client') return d.kind ? `HeyaConnect · ${titleCase(d.kind)}` : 'HeyaConnect'

  const provider = d.provider === 'airplay'
    ? 'AirPlay'
    : d.provider === 'chromecast'
      ? 'Chromecast'
      : titleCase(d.provider)
  const manufacturer = d.manufacturer
    ?.replace(/\s+(corporation|corp\.?|incorporated|inc\.?|limited|ltd\.?)$/i, '')
    .trim()
  const model = [manufacturer, d.model].filter(Boolean).join(' ')
  return model ? `${provider} · ${model}` : provider
}

function titleCase(value: string) {
  return value ? value.charAt(0).toUpperCase() + value.slice(1) : value
}
</script>

<!-- Menu content is portaled by AppMenu — these rules must be unscoped
     (docs/ui.md: scoped CSS doesn't reach portaled children). The phone
     rule lives here too: AppTopBar's scoped styles can't reliably reach a
     child component's trigger, and the phone set is brand/search/avatar
     only (same reasoning as the old placeholder's hide rule). -->
<style>
.cast-menu-surface {
  max-height: min(70vh, 520px);
  padding: 5px;
  overflow-x: hidden;
  overflow-y: auto;
  overscroll-behavior: contain;
  scrollbar-width: thin;
}
.cast-menu-current {
  display: flex;
  align-items: center;
  gap: 10px;
  min-height: 48px;
  padding: 6px 9px;
}
.cast-current-label {
  color: var(--gold-bright, var(--gold));
  font-family: var(--font-mono);
  font-size: 8px;
  font-weight: 700;
  letter-spacing: 0.1em;
  line-height: 1.2;
  text-transform: uppercase;
}
.cast-menu-label {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin: 0;
  padding: 10px 9px 5px;
}
.cast-menu-count {
  display: inline-grid;
  place-items: center;
  min-width: 18px;
  height: 18px;
  padding: 0 5px;
  border-radius: var(--r-pill);
  background: rgb(var(--ink) / 0.06);
  color: var(--fg-2);
  font-size: 8px;
  letter-spacing: 0;
}
.cast-menu-divider {
  margin: 5px 6px;
}
.cast-menu-empty {
  padding: 12px 10px 14px;
  font-size: 12px;
  color: var(--fg-3);
}
.cast-device-item {
  min-height: 48px;
  gap: 10px;
  padding: 6px 8px;
  border-radius: var(--r-md);
}
.cast-device-item.is-active {
  background: color-mix(in srgb, var(--gold) 8%, transparent);
}
.cast-device-icon {
  display: inline-grid;
  place-items: center;
  width: 30px;
  height: 30px;
  flex: 0 0 30px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: rgb(var(--ink) / 0.035);
  color: var(--fg-2);
}
.cast-device-icon.is-video {
  color: var(--fg-1);
}
.cast-device-icon.is-active {
  border-color: color-mix(in srgb, var(--gold) 28%, var(--border));
  background: color-mix(in srgb, var(--gold) 10%, transparent);
  color: var(--gold-bright, var(--gold));
}
.cast-device-item.is-active .cast-device-icon {
  border-color: color-mix(in srgb, var(--gold) 28%, var(--border));
  background: color-mix(in srgb, var(--gold) 10%, transparent);
  color: var(--gold-bright, var(--gold));
}
.cast-device-text {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}
.cast-device-name {
  color: var(--fg-0);
  font-size: 13px;
  font-weight: 550;
  line-height: 1.25;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.cast-device-sub {
  color: var(--fg-2);
  font-size: 10px;
  line-height: 1.25;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.cast-device-check {
  color: var(--gold-bright, var(--gold));
  flex-shrink: 0;
  margin-right: 3px;
}
.cast-disconnect {
  margin-top: 1px;
  border-radius: var(--r-md);
  color: var(--bad);
}
.cast-disconnect[data-highlighted],
.cast-disconnect:hover { background: color-mix(in srgb, var(--bad) 8%, transparent); color: var(--bad); }

.cast-btn-spin {
  animation: cast-btn-spin 0.9s linear infinite;
}
@keyframes cast-btn-spin {
  to { transform: rotate(360deg); }
}

</style>
