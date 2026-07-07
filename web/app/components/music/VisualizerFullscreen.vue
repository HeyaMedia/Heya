<!--
  VisualizerFullscreen — immersive full-window visualizer.

  Hosts the active mode (Milkdrop / Spectrum / Scope / VU) full-bleed, with a
  hibiki-inspired bottom command bar: track + interactive seek, transport,
  Milkdrop preset controls (nav / random / favorite / liked-only / auto-cycle /
  browser), mode pills, and a native-fullscreen toggle. A slide-in preset
  browser handles search + favorites. Chrome auto-hides after idle; a persistent
  corner chip + a bottom progress line keep the essentials legible while hidden.

  Keys: v/Esc close · ←/→ or [ / ] preset · r/t random · o presets · f native
  fullscreen · 1–4 mode.
-->
<template>
  <Teleport to="body">
    <Transition name="viz-fade">
      <div
        v-if="vis.fullscreenOpen.value"
        ref="rootRef"
        class="viz-root"
        :class="{ 'controls-hidden': !controlsVisible }"
        @mousemove="poke"
      >
        <!-- Active mode fills the surface -->
        <VisualizerMilkdrop v-if="mode === 'milkdrop'" ref="mkRef" />
        <VisualizerStarfield v-else-if="mode === 'starfield'" />
        <VisualizerSpectrum v-else :variant="specVariant" :active="playing" />

        <!-- Floating close (top-right) -->
        <button class="viz-close" title="Close (Esc)" @click="close"><Icon name="close" :size="18" /></button>

        <!-- Preset browser (Milkdrop only) -->
        <VisualizerPresetBrowser
          v-if="vis.presetBrowserOpen.value && mode === 'milkdrop'"
          :preset-keys="presetKeys"
          @select="onSelectPreset"
        />

        <!-- Bottom command bar -->
        <div class="viz-bar">
          <!-- Track -->
          <div class="viz-track">
            <Poster
              v-if="currentTrack"
              :idx="currentTrack.id"
              :src="currentTrack.poster"
              aspect="1/1"
              style="width: 44px; height: 44px; border-radius: 6px; flex-shrink: 0"
            />
            <div v-if="currentTrack" class="viz-track-meta">
              <div class="viz-track-title">{{ currentTrack.title }}</div>
              <div class="viz-track-sub">{{ currentTrack.artist }}</div>
            </div>
          </div>

          <!-- Transport -->
          <div class="viz-transport">
            <button class="viz-ic" title="Previous" @click="prevTrack"><Icon name="prev" :size="18" /></button>
            <button class="viz-play" :title="playing ? 'Pause' : 'Play'" @click="togglePlay">
              <Icon :name="playing ? 'pause' : 'play'" :size="20" />
            </button>
            <button class="viz-ic" title="Next" @click="nextTrack"><Icon name="next" :size="18" /></button>
          </div>

          <!-- Seek -->
          <div class="viz-seek">
            <span class="viz-t">{{ formatTime(position) }}</span>
            <div class="viz-rail" @click="onSeek">
              <div class="viz-rail-fill" :style="{ width: progressPct + '%' }" />
              <div class="viz-rail-knob" :style="{ left: progressPct + '%' }" />
            </div>
            <span class="viz-t">{{ formatTime(duration) }}</span>
          </div>

          <!-- Milkdrop preset controls -->
          <div v-if="mode === 'milkdrop'" class="viz-presetctl">
            <span class="viz-sep" />
            <button class="viz-ic sm" title="Previous preset ([)" @click="mkRef?.prevPreset()"><Icon name="chevleft" :size="15" /></button>
            <button class="viz-preset-name" :title="vis.currentPresetName.value" @click="toggleBrowser">{{ prettyPreset }}</button>
            <button class="viz-ic sm" title="Next preset (])" @click="mkRef?.nextPreset()"><Icon name="chevright" :size="15" /></button>
            <button class="viz-ic sm" title="Random (r)" @click="mkRef?.randomPreset()"><Icon name="shuffle" :size="14" /></button>
            <button
              class="viz-ic sm"
              :class="{ liked: isFav }"
              :title="isFav ? 'Unfavorite' : 'Favorite'"
              @click="toggleFav"
            ><Icon :name="isFav ? 'heartfill' : 'heart'" :size="14" /></button>
            <button
              class="viz-ic sm"
              :class="{ active: vis.likedOnly.value }"
              title="Cycle liked only"
              @click="vis.setLikedOnly(!vis.likedOnly.value)"
            ><Icon name="star" :size="14" /></button>
            <button
              class="viz-ic sm"
              :class="{ active: vis.autoCycleEnabled.value }"
              title="Auto-cycle"
              @click="vis.setAutoCycleEnabled(!vis.autoCycleEnabled.value)"
            ><Icon name="timer" :size="14" /></button>
            <button
              class="viz-ic sm"
              :class="{ active: vis.presetBrowserOpen.value }"
              title="Browse presets (o)"
              @click="toggleBrowser"
            ><Icon name="list" :size="14" /></button>
          </div>

          <!-- Milkdrop preset controls, collapsed — shown only on the compact
               band (≤1200px). Same controls as the inline cluster above, behind
               a gear popover so the bar stays a single row on a fold. CSS toggles
               which of the two shows at each width. -->
          <div v-if="mode === 'milkdrop'" class="viz-presetctl-compact">
            <span class="viz-sep" />
            <PopoverRoot v-model:open="presetSettingsOpen">
              <PopoverTrigger as-child>
                <button class="viz-ic sm" :class="{ active: presetSettingsOpen }" title="Presets">
                  <Icon name="settings" :size="15" />
                </button>
              </PopoverTrigger>
              <PopoverPortal>
                <PopoverContent class="surface viz-presetpop" side="top" :side-offset="12" align="start" :collision-padding="12">
                  <div class="viz-presetpop-head">Presets</div>
                  <button class="viz-presetpop-name" :title="vis.currentPresetName.value" @click="openBrowserFromPopover">{{ prettyPreset }}</button>
                  <div class="viz-presetpop-nav">
                    <button title="Previous ([)" @click="mkRef?.prevPreset()"><Icon name="chevleft" :size="16" /></button>
                    <button title="Random (r)" @click="mkRef?.randomPreset()"><Icon name="shuffle" :size="15" /></button>
                    <button title="Next (])" @click="mkRef?.nextPreset()"><Icon name="chevright" :size="16" /></button>
                  </div>
                  <button class="viz-presetpop-item" :class="{ liked: isFav }" @click="toggleFav">
                    <Icon :name="isFav ? 'heartfill' : 'heart'" :size="14" />
                    <span>{{ isFav ? 'Favorited' : 'Favorite preset' }}</span>
                  </button>
                  <div class="viz-presetpop-switch">
                    <AppSwitch :model-value="vis.likedOnly.value" size="sm" label="Liked only" @update:model-value="vis.setLikedOnly" />
                  </div>
                  <div class="viz-presetpop-switch">
                    <AppSwitch :model-value="vis.autoCycleEnabled.value" size="sm" label="Auto-cycle" @update:model-value="vis.setAutoCycleEnabled" />
                  </div>
                  <button class="viz-presetpop-browse" @click="openBrowserFromPopover">
                    <Icon name="list" :size="14" /><span>Browse all presets</span>
                  </button>
                </PopoverContent>
              </PopoverPortal>
            </PopoverRoot>
          </div>

          <!-- Starfield settings -->
          <div v-if="mode === 'starfield'" class="viz-starctl">
            <span class="viz-sep" />
            <PopoverRoot v-model:open="starSettingsOpen">
              <PopoverTrigger as-child>
                <button class="viz-ic sm" :class="{ active: starSettingsOpen }" title="Starfield settings">
                  <Icon name="settings" :size="15" />
                </button>
              </PopoverTrigger>
              <PopoverPortal>
                <PopoverContent class="surface viz-starpop" side="top" :side-offset="12" align="start" :collision-padding="12">
                  <div class="viz-starpop-head">Starfield</div>
                  <div class="viz-starpop-row">
                    <label>Speed</label>
                    <AppSlider
                      :model-value="vis.starfieldSpeed.value"
                      :min="1" :max="10" :step="1"
                      aria-label="Star speed"
                      @update:model-value="vis.setStarfieldSpeed"
                    />
                    <span class="viz-starpop-val">{{ vis.starfieldSpeed.value }}</span>
                  </div>
                  <div class="viz-starpop-row">
                    <label>Reactivity</label>
                    <AppSlider
                      :model-value="vis.starfieldReactivity.value"
                      :min="0" :max="100" :step="5"
                      aria-label="Star reactivity"
                      @update:model-value="vis.setStarfieldReactivity"
                    />
                    <span class="viz-starpop-val">{{ vis.starfieldReactivity.value }}</span>
                  </div>
                </PopoverContent>
              </PopoverPortal>
            </PopoverRoot>
          </div>

          <!-- Mode pills -->
          <span class="viz-sep" />
          <div class="viz-modes">
            <button
              v-for="m in MODES"
              :key="m.id"
              class="viz-pill"
              :class="{ active: mode === m.id }"
              @click="vis.setMode(m.id)"
            >{{ m.label }}</button>
          </div>

          <!-- Native fullscreen -->
          <button class="viz-ic" :title="isNativeFullscreen ? 'Exit fullscreen (f)' : 'Fullscreen (f)'" @click="toggleNativeFullscreen">
            <Icon :name="isNativeFullscreen ? 'collapse' : 'expand'" :size="17" />
          </button>
        </div>

        <!-- Persistent corner chip — fades in as chrome fades out -->
        <div v-if="currentTrack" class="viz-corner">
          <Poster
            :idx="currentTrack.id"
            :src="currentTrack.poster"
            aspect="1/1"
            style="width: 40px; height: 40px; border-radius: 5px; flex-shrink: 0"
          />
          <div class="viz-corner-meta">
            <div class="viz-corner-title">{{ currentTrack.title }}</div>
            <div class="viz-corner-sub">
              <span>{{ currentTrack.artist }}</span>
              <span class="viz-corner-time">{{ formatTime(position) }} / {{ formatTime(duration) }}</span>
            </div>
          </div>
        </div>

        <!-- Always-on progress line -->
        <div class="viz-progress"><div class="viz-progress-fill" :style="{ width: progressPct + '%' }" /></div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { PopoverContent, PopoverPortal, PopoverRoot, PopoverTrigger } from 'reka-ui'
import type { VisMode } from '~/composables/useVisualizer'

interface MilkdropApi {
  nextPreset: () => void
  prevPreset: () => void
  randomPreset: () => void
  loadPreset: (name: string) => void
  presetNames: () => string[]
}

const vis = useVisualizer()
const { playing, currentTrack, position, duration, togglePlay, nextTrack, prevTrack, seek, formatTime } = usePlayer()

const mode = vis.mode
const mkRef = ref<MilkdropApi | null>(null)
const rootRef = ref<HTMLElement | null>(null)
const presetKeys = ref<string[]>([])
const isNativeFullscreen = ref(false)
const starSettingsOpen = ref(false)
// Milkdrop preset controls collapse into this popover on the compact band.
const presetSettingsOpen = ref(false)
const { isCompact } = useViewport()

const MODES: { id: VisMode; label: string }[] = [
  { id: 'milkdrop', label: 'Milkdrop' },
  { id: 'bars', label: 'Spectrum' },
  { id: 'scope', label: 'Scope' },
  { id: 'vu', label: 'VU' },
  { id: 'starfield', label: 'Starfield' },
]

// Narrow VisMode (the spectrum's prop doesn't accept 'milkdrop'/'starfield',
// which render their own components) for the v-else branch.
const specVariant = computed<'bars' | 'scope' | 'vu'>(() =>
  (mode.value === 'milkdrop' || mode.value === 'starfield' ? 'bars' : mode.value),
)

const progressPct = computed(() => (duration.value > 0 ? (position.value / duration.value) * 100 : 0))

const isFav = computed(() => vis.isFavorite(vis.currentPresetName.value))
function toggleFav() { if (vis.currentPresetName.value) vis.toggleFavorite(vis.currentPresetName.value) }

const prettyPreset = computed(() => {
  const raw = vis.currentPresetName.value
  if (!raw) return 'Preset'
  return raw.replace(/\.milk$/i, '').replace(/^[^-]+ - /, '').trim() || raw
})

function onSelectPreset(name: string) { mkRef.value?.loadPreset(name) }
function toggleBrowser() { vis.presetBrowserOpen.value = !vis.presetBrowserOpen.value }
// From the compact popover: close it, then open the full browser panel (which
// would otherwise render behind the higher-z popover).
function openBrowserFromPopover() { presetSettingsOpen.value = false; vis.presetBrowserOpen.value = true }
function close() {
  if (document.fullscreenElement) document.exitFullscreen().catch(() => {})
  vis.presetBrowserOpen.value = false
  vis.fullscreenOpen.value = false
}

function onSeek(e: MouseEvent) {
  if (duration.value <= 0) return
  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
  seek(Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width)))
}

// --- Native fullscreen -----------------------------------------------------
function toggleNativeFullscreen() {
  if (document.fullscreenElement) document.exitFullscreen().catch(() => {})
  else rootRef.value?.requestFullscreen().catch(() => {})
}
useEventListener(document, 'fullscreenchange', () => { isNativeFullscreen.value = !!document.fullscreenElement })

// --- Poll Milkdrop preset keys (they arrive after the async chunk loads) ----
let keyPoll: ReturnType<typeof setInterval> | null = null
function stopKeyPoll() { if (keyPoll) { clearInterval(keyPoll); keyPoll = null } }
watch([mkRef, mode], ([ref, m]) => {
  stopKeyPoll()
  if (m !== 'milkdrop' || !ref) { presetKeys.value = []; return }
  keyPoll = setInterval(() => {
    const keys = ref.presetNames?.() ?? []
    if (keys.length) { presetKeys.value = keys; stopKeyPoll() }
  }, 200)
}, { immediate: true })

// Leaving a mode closes its mode-specific chrome.
watch(mode, (m) => {
  if (m !== 'milkdrop') { vis.presetBrowserOpen.value = false; presetSettingsOpen.value = false }
  if (m !== 'starfield') starSettingsOpen.value = false
})
// The compact preset popover is compact-band only — dropping to desktop hides
// its trigger, so close it rather than leave a portaled panel with no anchor.
watch(isCompact, (c) => { if (!c) presetSettingsOpen.value = false })

// --- Auto-hide chrome ------------------------------------------------------
const controlsVisible = ref(true)
let hideTimer: ReturnType<typeof setTimeout> | null = null
function poke() {
  controlsVisible.value = true
  if (hideTimer) clearTimeout(hideTimer)
  // Don't hide while any pinned chrome is open (preset browser / the mode popovers).
  if (vis.presetBrowserOpen.value || starSettingsOpen.value || presetSettingsOpen.value) return
  hideTimer = setTimeout(() => { controlsVisible.value = false }, 3000)
}
watch(vis.fullscreenOpen, (open) => {
  if (open) poke()
  else if (hideTimer) { clearTimeout(hideTimer); controlsVisible.value = true }
})
// Opening the browser pins the chrome; closing re-arms the idle timer.
watch(vis.presetBrowserOpen, (o) => { if (o) { controlsVisible.value = true; if (hideTimer) clearTimeout(hideTimer) } else poke() })
// Same pin/re-arm for the mode popovers (portaled outside the viz-root, so
// mousemove-poke never reaches them — pin explicitly while open).
watch(starSettingsOpen, (o) => { if (o) { controlsVisible.value = true; if (hideTimer) clearTimeout(hideTimer) } else poke() })
watch(presetSettingsOpen, (o) => { if (o) { controlsVisible.value = true; if (hideTimer) clearTimeout(hideTimer) } else poke() })

// --- Hotkeys ---------------------------------------------------------------
function isTyping(e: KeyboardEvent) {
  const t = e.target as HTMLElement | null
  return !!t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)
}
useEventListener(window, 'keydown', (e: KeyboardEvent) => {
  if (!vis.fullscreenOpen.value) return
  poke()

  if (e.key === 'Escape') {
    if (starSettingsOpen.value) starSettingsOpen.value = false
    else if (presetSettingsOpen.value) presetSettingsOpen.value = false
    else if (vis.presetBrowserOpen.value) vis.presetBrowserOpen.value = false
    else if (isNativeFullscreen.value) document.exitFullscreen().catch(() => {})
    else close()
    return
  }
  // While typing in the preset search, let the input own everything else.
  if (isTyping(e)) return

  const k = e.key.toLowerCase()
  if (k === 'f') { toggleNativeFullscreen(); return }
  if (k >= '1' && k <= '5') { vis.setMode(MODES[Number(k) - 1]!.id); return }

  if (mode.value !== 'milkdrop') return
  if (e.key === 'ArrowRight' || e.key === ']') mkRef.value?.nextPreset()
  else if (e.key === 'ArrowLeft' || e.key === '[') mkRef.value?.prevPreset()
  else if (k === 'r' || k === 't') mkRef.value?.randomPreset()
  else if (k === 'o') toggleBrowser()
})

onUnmounted(() => { stopKeyPoll(); if (hideTimer) clearTimeout(hideTimer) })
</script>

<style scoped>
.viz-root {
  position: fixed;
  inset: 0;
  z-index: 400;
  background: #000;
  overflow: hidden;
  cursor: default;
}
.viz-root.controls-hidden { cursor: none; }
.viz-root.controls-hidden .viz-bar,
.viz-root.controls-hidden .viz-close { opacity: 0; pointer-events: none; }

/* Floating close */
.viz-close {
  position: absolute;
  top: 18px; right: 20px;
  z-index: 3;
  width: 40px; height: 40px;
  border-radius: 50%;
  display: inline-flex; align-items: center; justify-content: center;
  color: rgba(255,255,255,0.55);
  background: rgba(0,0,0,0.35);
  backdrop-filter: blur(6px);
  border: 0; cursor: pointer;
  transition: background 0.15s, color 0.15s, opacity 0.3s ease;
}
.viz-close:hover { background: rgba(255,255,255,0.16); color: #fff; }

/* Bottom command bar */
.viz-bar {
  position: absolute;
  left: 0; right: 0; bottom: 0;
  z-index: 2;
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 16px 20px 22px;
  overflow-x: auto;
  background: linear-gradient(0deg, rgba(0,0,0,0.92) 0%, rgba(0,0,0,0.6) 45%, transparent 100%);
  transition: opacity 0.3s ease;
  scrollbar-width: none;
}
.viz-bar::-webkit-scrollbar { display: none; }

.viz-track { display: flex; align-items: center; gap: 11px; flex-shrink: 0; min-width: 0; }
.viz-track-meta { min-width: 0; max-width: 190px; }
.viz-track-title { font-size: 13px; font-weight: 600; color: #fff; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.viz-track-sub { font-size: 11px; color: rgba(255,255,255,0.55); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.viz-transport { display: flex; align-items: center; gap: 12px; flex-shrink: 0; }
.viz-play {
  width: 42px; height: 42px; border-radius: 50%;
  display: inline-flex; align-items: center; justify-content: center;
  color: var(--bg-0); background: var(--gold); border: 0; cursor: pointer;
  box-shadow: 0 6px 18px var(--gold-glow);
  transition: transform 0.12s ease, background 0.15s;
}
.viz-play:hover { transform: scale(1.06); background: var(--gold-bright); }

.viz-seek { display: flex; align-items: center; gap: 10px; flex: 1 1 220px; min-width: 160px; }
.viz-t { font-size: 10px; font-family: var(--font-mono); color: rgba(255,255,255,0.5); min-width: 34px; text-align: center; flex-shrink: 0; }
.viz-rail { position: relative; flex: 1; min-width: 0; height: 4px; border-radius: 999px; background: rgba(255,255,255,0.18); cursor: pointer; }
.viz-rail:hover { height: 6px; }
.viz-rail-fill { position: absolute; left: 0; top: 0; bottom: 0; border-radius: 999px; background: var(--gold); }
.viz-rail-knob {
  position: absolute; top: 50%; width: 12px; height: 12px;
  border-radius: 50%; background: #fff; transform: translate(-50%, -50%);
  opacity: 0; transition: opacity 0.12s; box-shadow: 0 2px 6px rgba(0,0,0,0.5);
}
.viz-rail:hover .viz-rail-knob { opacity: 1; }

.viz-presetctl { display: flex; align-items: center; gap: 5px; flex-shrink: 0; }
/* Inline preset controls are desktop-only; the compact gear popover takes over
   ≤1200px (see the media query below). */
.viz-presetctl-compact { display: none; flex-shrink: 0; }
.viz-starctl { display: flex; align-items: center; gap: 5px; flex-shrink: 0; }
.viz-preset-name {
  max-width: 150px;
  padding: 5px 10px;
  font-size: 11px;
  color: rgba(255,255,255,0.65);
  background: rgba(255,255,255,0.06);
  border: 1px solid rgba(255,255,255,0.08);
  border-radius: 6px;
  cursor: pointer;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  transition: background 0.12s, color 0.12s;
}
.viz-preset-name:hover { background: rgba(255,255,255,0.12); color: #fff; }

.viz-modes { display: flex; gap: 6px; flex-shrink: 0; }
.viz-pill {
  padding: 6px 13px; font-size: 12px; font-weight: 500;
  color: rgba(255,255,255,0.7);
  background: rgba(255,255,255,0.08);
  border: 1px solid rgba(255,255,255,0.12);
  border-radius: 999px; cursor: pointer;
  transition: background 0.15s, color 0.15s;
}
.viz-pill:hover { color: #fff; background: rgba(255,255,255,0.14); }
.viz-pill.active { color: var(--bg-0); background: var(--gold); border-color: transparent; }

.viz-sep { width: 1px; height: 24px; background: rgba(255,255,255,0.12); flex-shrink: 0; }

.viz-ic {
  width: 36px; height: 36px; border-radius: 50%;
  display: inline-flex; align-items: center; justify-content: center;
  color: rgba(255,255,255,0.75); background: transparent; border: 0; cursor: pointer;
  flex-shrink: 0;
  transition: background 0.15s, color 0.15s;
}
.viz-ic:hover { background: rgba(255,255,255,0.12); color: #fff; }
.viz-ic.active { color: var(--gold); }
.viz-ic.liked { color: #ff5b7a; }
.viz-ic.sm { width: 30px; height: 30px; }

/* Persistent corner now-playing */
.viz-corner {
  position: absolute;
  left: 22px; bottom: 22px;
  z-index: 2;
  display: flex; align-items: center; gap: 11px;
  max-width: 40vw;
  padding: 8px 14px 8px 8px;
  border-radius: 10px;
  background: rgba(0, 0, 0, 0.45);
  backdrop-filter: blur(8px);
  opacity: 0; pointer-events: none;
  transition: opacity 0.3s ease;
}
.viz-root.controls-hidden .viz-corner { opacity: 1; }
.viz-corner-meta { min-width: 0; }
.viz-corner-title { font-size: 13px; font-weight: 600; color: #fff; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.viz-corner-sub { display: flex; align-items: center; gap: 10px; font-size: 11px; color: rgba(255,255,255,0.6); overflow: hidden; }
.viz-corner-sub > span:first-child { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
.viz-corner-time { flex-shrink: 0; font-family: var(--font-mono); color: rgba(255,255,255,0.5); }

/* Always-on progress line */
.viz-progress { position: absolute; left: 0; right: 0; bottom: 0; z-index: 3; height: 3px; background: rgba(255,255,255,0.14); }
.viz-progress-fill { height: 100%; background: var(--gold); box-shadow: 0 0 8px var(--gold); transition: width 0.25s linear; }

.viz-fade-enter-active, .viz-fade-leave-active { transition: opacity 0.25s ease; }
.viz-fade-enter-from, .viz-fade-leave-to { opacity: 0; }

/* Tablet / foldable (compact band, ≤1200px — this component never mounts on
   phone, so in practice that's the 720-1200 fold band): the command bar packs a
   lot — track, transport, seek, 8 Milkdrop controls, 5 mode pills, fullscreen —
   which crowds the narrower width. Slim every section (tighter gaps/paddings,
   smaller controls, narrower seek + track meta), and give the mode pills their
   OWN dedicated second row (below) so the transport/seek line stays uncramped
   at every fold width instead of the pills fighting it for horizontal space. */
@media (max-width: 1200px) {
  .viz-bar { gap: 10px; row-gap: 14px; padding: 12px 14px 16px; flex-wrap: wrap; }
  .viz-track { gap: 9px; }
  .viz-track-meta { max-width: 120px; }
  .viz-transport { gap: 8px; }
  .viz-play { width: 38px; height: 38px; }
  .viz-seek { flex-basis: 150px; min-width: 110px; gap: 8px; }
  /* Collapse the 8-button Milkdrop cluster into the gear popover so line 1
     holds a single row — same move the starfield settings use. */
  .viz-presetctl { display: none; }
  .viz-presetctl-compact { display: flex; align-items: center; gap: 5px; }
  /* Mode pills to their own full-width second row, always. `order: 99` makes
     them the last flex item so the fullscreen button (DOM-after them) stays up
     on line 1; `flex-basis: 100%` forces the line break deterministically
     rather than relying on opportunistic wrap; they wrap + centre within their
     row on the very narrowest folds. */
  .viz-modes {
    order: 99;
    flex-basis: 100%;
    gap: 6px;
    flex-wrap: wrap;
    justify-content: center;
  }
  /* The separator that preceded the pills on the single-row layout is pointless
     now they sit on their own line — drop just that one (the gear-cluster seps
     are nested deeper and keep their height rule below). */
  .viz-bar > .viz-sep { display: none; }
  .viz-pill { padding: 5px 12px; font-size: 11px; }
  .viz-ic { width: 32px; height: 32px; }
  .viz-ic.sm { width: 27px; height: 27px; }
  .viz-sep { height: 20px; }
}
</style>

<!-- Unscoped: the starfield settings popover is portaled to <body>, out of this
     component's scope, so its chrome + z-index live in a global block. -->
<style>
.viz-starpop {
  z-index: 500; /* above the fullscreen viz-root (z-index 400) */
  min-width: 240px;
  padding: 12px 14px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.viz-starpop-head {
  font-size: 9px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--fg-3);
}
.viz-starpop-row {
  display: grid;
  grid-template-columns: 62px 1fr 26px;
  align-items: center;
  gap: 10px;
}
.viz-starpop-row label { font-size: 12px; color: var(--fg-1); }
.viz-starpop-val {
  font-size: 11px;
  font-family: var(--font-mono);
  color: var(--fg-2);
  text-align: right;
}

/* Compact Milkdrop preset popover (fold/tablet). */
.viz-presetpop {
  z-index: 500; /* above the fullscreen viz-root (z-index 400) */
  min-width: 232px;
  padding: 10px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.viz-presetpop-head {
  font-size: 9px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--fg-3);
  padding: 2px 4px 4px;
}
.viz-presetpop-name {
  text-align: left;
  font-size: 13px;
  font-weight: 600;
  color: #fff;
  padding: 7px 9px;
  border-radius: var(--r-xs);
  background: rgba(255, 255, 255, 0.06);
  border: 1px solid rgba(255, 255, 255, 0.08);
  cursor: pointer;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  transition: background 0.12s;
}
.viz-presetpop-name:hover { background: rgba(255, 255, 255, 0.12); }
.viz-presetpop-nav { display: grid; grid-template-columns: repeat(3, 1fr); gap: 6px; }
.viz-presetpop-nav button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  height: 34px;
  border-radius: var(--r-xs);
  color: rgba(255, 255, 255, 0.8);
  background: rgba(255, 255, 255, 0.06);
  border: 1px solid rgba(255, 255, 255, 0.08);
  cursor: pointer;
  transition: background 0.12s, color 0.12s;
}
.viz-presetpop-nav button:hover { background: rgba(255, 255, 255, 0.14); color: #fff; }
.viz-presetpop-item,
.viz-presetpop-browse {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 8px;
  border-radius: var(--r-xs);
  font-size: 12px;
  color: var(--fg-1);
  background: transparent;
  border: 0;
  cursor: pointer;
  text-align: left;
  transition: background 0.12s, color 0.12s;
}
.viz-presetpop-item:hover,
.viz-presetpop-browse:hover { background: rgba(255, 255, 255, 0.06); color: #fff; }
.viz-presetpop-item.liked { color: #ff5b7a; }
.viz-presetpop-switch { padding: 4px 8px; }
</style>
