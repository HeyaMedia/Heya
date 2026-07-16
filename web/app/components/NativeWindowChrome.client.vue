<template>
  <div
    v-if="bridge && capabilities"
    class="native-window-chrome"
    :class="[
      `platform-${capabilities.platform}`,
      { 'video-active': videoActive, 'video-hidden': videoActive && !videoControlsVisible },
    ]"
    :aria-hidden="videoActive && !videoControlsVisible ? 'true' : undefined"
    @pointerenter="refreshState"
    @pointermove="requestVideoControls"
  >
    <div v-if="capabilities.customTitlebar" class="native-window-drag-strip" data-tauri-drag-region="deep" />
    <div v-if="capabilities.customTitlebar" class="native-window-controls">
      <button class="native-window-button close" type="button" aria-label="Close Heya" @click="closeWindow">
        <svg viewBox="0 0 12 12" aria-hidden="true"><path d="M3 3l6 6M9 3L3 9" /></svg>
      </button>
      <button class="native-window-button minimize" type="button" aria-label="Minimize Heya" @click="minimizeWindow">
        <svg viewBox="0 0 12 12" aria-hidden="true"><path d="M2.5 6h7" /></svg>
      </button>
      <button class="native-window-button maximize" type="button" :aria-label="maximized ? 'Restore Heya' : 'Maximize Heya'" @click="toggleMaximize">
        <svg viewBox="0 0 12 12" aria-hidden="true"><rect x="2.5" y="2.5" width="7" height="7" rx=".5" /></svg>
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { HeyaNativeWindowBridge, NativeWindowCapabilities } from '~/types/native-window'

const bridge = shallowRef<Readonly<HeyaNativeWindowBridge> | null>(null)
const capabilities = shallowRef<NativeWindowCapabilities | null>(null)
const maximized = ref(false)
let refreshStateTimer: ReturnType<typeof setTimeout> | null = null
let topbarDragRegion: Element | null = null
const {
  videoActive,
  videoControlsVisible,
  requestVideoControls,
} = useNativeWindowChrome()

function syncDocumentClass(platform: NativeWindowCapabilities['platform'] | null) {
  const root = document.documentElement
  root.classList.toggle('heya-native-window', platform !== null)
  root.classList.toggle('heya-native-window-macos', platform === 'macos')
  root.classList.toggle('heya-native-window-windows', platform === 'windows')
  root.classList.toggle('heya-native-window-linux', platform === 'linux')
}

function applyWindowState(state: { maximized: boolean, fullscreen: boolean } | null) {
  if (!state) return
  maximized.value = state.maximized
  document.documentElement.classList.toggle('heya-native-window-maximized', state.maximized)
  document.documentElement.classList.toggle('heya-native-window-fullscreen', state.fullscreen)
}

function onDocumentPointerMove(event: PointerEvent) {
  if (videoActive.value && event.clientY <= 46) requestVideoControls()
}

async function refreshState() {
  const state = await bridge.value?.getWindowState().catch(() => null)
  applyWindowState(state ?? null)
}

function scheduleRefreshState() {
  if (refreshStateTimer !== null) clearTimeout(refreshStateTimer)
  refreshStateTimer = setTimeout(() => {
    refreshStateTimer = null
    void refreshState()
  }, 100)
}

function minimizeWindow() {
  void bridge.value?.minimize().catch(() => {})
}

async function toggleMaximize() {
  const state = await bridge.value?.toggleMaximize().catch(() => null)
  applyWindowState(state ?? null)
}

function closeWindow() {
  void bridge.value?.close().catch(() => {})
}

watch(
  [bridge, capabilities, videoActive, videoControlsVisible],
  ([nativeBridge, nativeCapabilities, isVideoActive, areVideoControlsVisible]) => {
    if (!nativeBridge || !nativeCapabilities?.nativeControls) return
    void nativeBridge
      .setNativeControlsVisible(!isVideoActive || areVideoControlsVisible)
      .catch(() => {})
  },
  { immediate: true },
)

onMounted(async () => {
  const handshake = await waitForNativeWindowBridge()
  if (!handshake) return
  bridge.value = handshake.bridge
  capabilities.value = handshake.capabilities
  syncDocumentClass(handshake.capabilities.platform)
  if (handshake.capabilities.customTitlebar) {
    topbarDragRegion = document.querySelector('.topbar')
    topbarDragRegion?.setAttribute('data-tauri-drag-region', 'deep')
  }
  await refreshState()
  document.addEventListener('pointermove', onDocumentPointerMove, true)
  window.addEventListener('resize', scheduleRefreshState)
})

onUnmounted(() => {
  if (capabilities.value?.nativeControls) {
    void bridge.value?.setNativeControlsVisible(true).catch(() => {})
  }
  document.removeEventListener('pointermove', onDocumentPointerMove, true)
  window.removeEventListener('resize', scheduleRefreshState)
  if (refreshStateTimer !== null) clearTimeout(refreshStateTimer)
  topbarDragRegion?.removeAttribute('data-tauri-drag-region')
  topbarDragRegion = null
  document.documentElement.classList.remove('heya-native-window-maximized', 'heya-native-window-fullscreen')
  syncDocumentClass(null)
})
</script>

<style>
.native-window-chrome {
  position: fixed;
  inset: 0 0 auto;
  height: var(--topbar-h, 60px);
  z-index: 10050;
  pointer-events: none;
  user-select: none;
  -webkit-user-select: none;
}

.native-window-controls {
  position: absolute;
  top: 0;
  height: var(--topbar-h, 60px);
  display: flex;
  align-items: center;
  gap: 8px;
  pointer-events: auto;
  opacity: 1;
  transition: opacity 220ms ease;
}

.native-window-drag-strip {
  position: absolute;
  inset: 0 0 auto;
  height: 12px;
  pointer-events: auto;
}

.native-window-chrome.video-hidden .native-window-controls {
  opacity: 0;
  pointer-events: none;
}

.native-window-chrome.platform-macos .native-window-controls {
  left: 13px;
  transform: translateY(-15px);
}
.native-window-chrome.platform-windows .native-window-controls,
.native-window-chrome.platform-linux .native-window-controls {
  right: 0;
  height: var(--topbar-h, 60px);
  gap: 0;
  flex-direction: row-reverse;
}

.native-window-button {
  display: grid;
  place-items: center;
  width: 13px;
  height: 13px;
  border: 0;
  border-radius: 50%;
  padding: 0;
  cursor: default;
}

.native-window-button svg {
  width: 8px;
  height: 8px;
  fill: none;
  stroke: rgb(24 24 28 / 78%);
  stroke-linecap: round;
  stroke-width: 1.35;
  opacity: 0;
}

.platform-macos .native-window-controls:hover svg,
.platform-macos .native-window-button:focus-visible svg { opacity: 1; }
.platform-macos .native-window-button.close { background: #ff5f57; }
.platform-macos .native-window-button.minimize { background: #febc2e; }
.platform-macos .native-window-button.maximize { background: #28c840; }

.platform-windows .native-window-button,
.platform-linux .native-window-button {
  width: 46px;
  height: 38px;
  border-radius: 0;
  background: transparent;
  color: var(--fg-1);
  transition: background 120ms ease;
}

.platform-windows .native-window-button svg,
.platform-linux .native-window-button svg {
  width: 10px;
  height: 10px;
  stroke: currentColor;
  opacity: 1;
}

.platform-windows .native-window-button:hover,
.platform-linux .native-window-button:hover { background: rgb(var(--ink) / 0.1); }
.platform-windows .native-window-button.close:hover,
.platform-linux .native-window-button.close:hover { background: #c42b1c; color: white; }

/* Share one title row with the platform controls. */
html.heya-native-window-macos .topbar { padding-left: 112px; }
html.heya-native-window-windows .topbar,
html.heya-native-window-linux .topbar { padding-right: 154px; }

/* The player close/info row occupies the same upper edge as the custom
   titlebar, so reserve the control cluster's horizontal footprint there. */
html.heya-native-window-macos .p .ctrl-top { padding-left: 104px; }
html.heya-native-window-windows .p .ctrl-top,
html.heya-native-window-linux .p .ctrl-top { padding-right: 154px; }

@media (prefers-reduced-motion: reduce) {
  .native-window-controls { transition-duration: 0.01ms; }
}
</style>
