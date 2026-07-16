<template>
  <AppDialog
    :model-value="open"
    title="Audio"
    size="md"
    @update:model-value="(v) => v ? null : $emit('close')"
  >
    <div class="eq-tabs" role="tablist" aria-label="Audio settings">
      <button
        v-for="t in TABS"
        :key="t.id"
        :id="`eq-tab-${t.id}`"
        class="eq-tab"
        :class="{ active: tab === t.id }"
        role="tab"
        :aria-selected="tab === t.id"
        :aria-controls="`eq-panel-${t.id}`"
        @click="tab = t.id"
      >{{ t.label }}</button>
    </div>

    <!-- ── Equalizer ─────────────────────────────────────────────── -->
    <div v-show="tab === 'eq'" id="eq-panel-eq" role="tabpanel" aria-labelledby="eq-tab-eq">
      <!-- iOS runs the direct-element engine (no Web Audio graph — see
           engine/directEngine.ts) so there's no node to hang an equalizer
           off of. Show a notice instead of controls that would silently do
           nothing. -->
      <div v-if="bitPerfectRequested" class="eq-unavailable">
        <p class="eq-unavailable-title">Equalizer bypassed for bit-perfect output</p>
        <p class="eq-extra-hint">Your processed-mode EQ remains saved. Turn off Bit-perfect in Playback to apply EQ, gain, ReplayGain, crossfade, limiter, and visualizers again.</p>
      </div>
      <div v-else-if="!eqAvailable" class="eq-unavailable">
        <p class="eq-unavailable-title">Not available in compatibility playback mode (iOS)</p>
        <p class="eq-extra-hint">This device plays audio directly through the browser to keep it running in the background, which bypasses the equalizer stage entirely.</p>
      </div>
      <template v-else>
        <div class="eq-master-row">
          <span class="eq-master-label">Enable equalizer</span>
          <AppSwitch
            :model-value="eq.enabled"
            :aria-label="eq.enabled ? 'Disable EQ' : 'Enable EQ'"
            size="md"
            @update:model-value="settings.setEQEnabled"
          />
        </div>

        <div class="eq-presets">
          <button
            v-for="p in settings.presets"
            :key="p.name"
            class="eq-preset"
            :class="{ active: eq.presetName === p.name }"
            @click="settings.applyPreset(p.name)"
          >
            {{ p.name }}
          </button>
        </div>

        <div class="eq-bands">
          <div v-for="(value, i) in eq.bands" :key="i" class="eq-band">
            <div
              class="eq-bar-track"
              role="slider"
              aria-orientation="vertical"
              :aria-label="`${BAND_LABELS[i]} Hz band`"
              aria-valuemin="-12"
              aria-valuemax="12"
              :aria-valuenow="value"
              :aria-valuetext="`${value > 0 ? '+' : ''}${value} dB`"
              tabindex="0"
              @pointerdown="onBandPointerDown($event, i)"
              @keydown="onBandKeydown($event, i)"
            >
              <div class="eq-bar-baseline" />
              <div
                class="eq-bar-fill"
                :class="{ negative: value < 0 }"
                :style="bandStyle(value)"
              />
              <div class="eq-bar-knob" :style="knobStyle(value)" />
            </div>
            <span class="eq-val">{{ value > 0 ? `+${value}` : value }}</span>
            <span class="eq-freq">{{ BAND_LABELS[i] }}</span>
          </div>
        </div>

        <div class="eq-extras">
          <div class="eq-extra-row">
            <span class="eq-extra-label">Pre-amp</span>
            <AppSlider
              :model-value="eq.preamp"
              :min="-12"
              :max="12"
              :step="0.5"
              bipolar
              aria-label="Pre-amp"
              class="eq-slider-flex"
              @update:model-value="settings.setPreamp"
            />
            <span class="eq-extra-val">{{ eq.preamp > 0 ? `+${eq.preamp}` : eq.preamp }} dB</span>
          </div>
          <div class="eq-extra-row">
            <span class="eq-extra-label">Post-gain</span>
            <AppSlider
              :model-value="eq.postgain"
              :min="-12"
              :max="12"
              :step="0.5"
              bipolar
              aria-label="Post-gain"
              class="eq-slider-flex"
              @update:model-value="settings.setPostgain"
            />
            <span class="eq-extra-val">{{ eq.postgain > 0 ? `+${eq.postgain}` : eq.postgain }} dB</span>
          </div>
        </div>
      </template>
    </div>

    <!-- ── Playback ──────────────────────────────────────────────── -->
    <div v-show="tab === 'playback'" id="eq-panel-playback" role="tabpanel" aria-labelledby="eq-tab-playback" class="eq-pane">
      <div v-if="isTauriClient" class="eq-native-route">
        <div>
          <strong>{{ nativeRouteTitle }}</strong>
          <span>{{ nativeRouteDetail }}</span>
        </div>
        <label class="eq-native-toggle">
          <span>Bit-perfect</span>
          <AppSwitch
            :model-value="bitPerfectRequested"
            :disabled="nativeModeSaving || !bitPerfectSupported"
            size="sm"
            aria-label="Bit-perfect native audio"
            @update:model-value="setBitPerfectMode"
          />
        </label>
      </div>

      <div class="eq-playback-processing" :class="{ 'eq-bypassed': bitPerfectRequested }">
        <div class="eq-extra-row">
          <span class="eq-extra-label">Crossfade</span>
          <div class="eq-select-wrap">
            <AppSelect
              :model-value="crossfade.mode"
              :options="CROSSFADE_OPTIONS"
              aria-label="Crossfade mode"
              @change="v => settings.setCrossfadeMode(v as 'gapless' | 'crossfade' | 'smart')"
            />
          </div>
          <AppSlider
            v-if="crossfade.mode !== 'gapless'"
            :model-value="crossfade.durationSeconds"
            :min="1"
            :max="12"
            :step="1"
            aria-label="Crossfade duration"
            class="eq-slider-flex"
            @update:model-value="settings.setCrossfadeDuration"
          />
          <span v-if="crossfade.mode !== 'gapless'" class="eq-extra-val">{{ crossfade.durationSeconds }}s</span>
        </div>

        <div v-if="crossfade.mode === 'smart'" class="eq-extra-row">
          <span class="eq-extra-hint">Smart aligns the fade to each song's natural outro; the duration above is the fallback when a track hasn't been analyzed yet.</span>
        </div>

        <div v-if="crossfade.mode !== 'gapless'" class="eq-extra-row">
          <span class="eq-extra-label">Album segues</span>
          <span class="eq-extra-hint">Skip crossfade between same-album tracks</span>
          <AppSwitch
            :model-value="crossfade.albumAware"
            size="md"
            aria-label="Album-aware crossfade"
            @update:model-value="settings.setCrossfadeAlbumAware"
          />
        </div>

        <div class="eq-extra-row">
          <span class="eq-extra-label">Replay Gain</span>
          <div class="eq-select-wrap eq-select-wrap-wide">
            <AppSelect
              :model-value="replayGain.mode"
              :options="REPLAY_GAIN_OPTIONS"
              aria-label="Replay gain mode"
              @change="v => settings.setReplayGainMode(v as 'off' | 'track' | 'album' | 'auto')"
            />
          </div>
        </div>
      </div>
    </div>

    <!-- ── Effects ───────────────────────────────────────────────── -->
    <div v-show="tab === 'effects'" id="eq-panel-effects" role="tabpanel" aria-labelledby="eq-tab-effects" class="eq-pane" :class="{ 'eq-bypassed': bitPerfectRequested }">
      <div class="eq-extra-row">
        <span class="eq-extra-label">Crossfeed</span>
        <span class="eq-extra-hint">Eases hard L/R separation on headphones</span>
        <AppSwitch
          :model-value="crossfeed.enabled"
          size="md"
          aria-label="Enable crossfeed"
          @update:model-value="settings.setCrossfeedEnabled"
        />
      </div>
      <div v-if="crossfeed.enabled" class="eq-extra-row">
        <span class="eq-extra-label">Strength</span>
        <div class="eq-select-wrap eq-select-wrap-wide">
          <AppSelect
            :model-value="crossfeed.preset"
            :options="CROSSFEED_OPTIONS"
            aria-label="Crossfeed strength"
            @change="v => settings.setCrossfeedPreset(v as 'subtle' | 'natural' | 'strong')"
          />
        </div>
      </div>

      <div class="eq-divider" />

      <div class="eq-chain">
        <div class="eq-chain-label">Signal chain</div>
        <div class="chain-row chain-pinned">
          <span class="chain-name">Normalization</span>
          <span class="chain-note">{{ replayGain.mode === 'off' ? 'off' : `replay gain · ${replayGain.mode}` }}</span>
        </div>
        <div v-for="(id, i) in dspChain.order" :key="id" class="chain-row">
          <div class="chain-move">
            <button class="chain-arrow" :disabled="i === 0" aria-label="Move earlier" @click="settings.moveDspBlock(id, -1)">↑</button>
            <button class="chain-arrow" :disabled="i === dspChain.order.length - 1" aria-label="Move later" @click="settings.moveDspBlock(id, 1)">↓</button>
          </div>
          <span class="chain-name">{{ blockLabel(id) }}</span>
          <AppSwitch :model-value="blockEnabled(id)" size="sm" :aria-label="blockLabel(id)" @update:model-value="(v) => setBlockEnabled(id, v)" />
        </div>
        <div class="chain-row chain-pinned">
          <span class="chain-name">Limiter</span>
          <span class="chain-note">safety</span>
          <AppSwitch :model-value="dspChain.limiterEnabled" size="sm" aria-label="Limiter" @update:model-value="settings.setLimiterEnabled" />
        </div>
      </div>
    </div>

    <!-- ── Output ────────────────────────────────────────────────── -->
    <div v-show="tab === 'output'" id="eq-panel-output" role="tabpanel" aria-labelledby="eq-tab-output" class="eq-pane">
      <div v-if="isTauriClient && bitPerfectRequested" class="eq-unavailable">
        <p class="eq-unavailable-title">Output selection is locked during bit-perfect playback</p>
        <p class="eq-extra-hint">The current macOS exclusive-output path follows the system default device. Switch to Processed in Playback before selecting a specific output here.</p>
      </div>
      <div v-if="!supported && isTauriClient" class="eq-unavailable">
        <p class="eq-unavailable-title">Native outputs are unavailable</p>
        <p class="eq-extra-hint">HeyaClient could not enumerate audio outputs. Check that an output device is connected, then try again.</p>
        <button class="dev-reveal" :disabled="outputSaving" @click="refreshOutputs">Try again</button>
      </div>
      <p v-else-if="!supported" class="eq-extra-hint">
        This browser doesn't expose per-app audio-output routing (<code>AudioContext.setSinkId</code>), so playback follows the system default output. Chromium-based browsers support it today; other engines light this up automatically once they ship the API.
      </p>
      <template v-else>
        <div class="dev-head">
          <span class="eq-chain-label">Output device</span>
          <div class="dev-head-actions">
            <button v-if="isTauriClient" class="dev-reveal" :disabled="outputSaving" @click="refreshOutputs">Refresh</button>
            <button
              v-if="isTauriClient"
              class="dev-reveal"
              :class="{ active: followsSystemDefault }"
              :disabled="outputSaving || bitPerfectRequested"
              @click="selectSystemOutput"
            >Use system default</button>
            <button v-else-if="!labelsAvailable" class="dev-reveal" @click="devices.revealLabels()">Reveal names</button>
          </div>
        </div>

        <div class="dev-list">
          <button
            v-for="d in availableDevices"
            :key="d.deviceId"
            class="dev-row"
            :class="{ active: d.deviceId === activeDeviceId }"
            :disabled="outputSaving || bitPerfectRequested"
            @click="selectOutput(d.deviceId)"
          >
            <span class="dev-dot" />
            <span class="dev-name">
              {{ d.label }}
              <span v-if="d.isDefault" class="dev-tag">default</span>
              <span v-if="d.deviceId === activeDeviceId && followsSystemDefault" class="dev-tag">following</span>
            </span>
            <span v-if="devices.hasProfile(d)" class="dev-badge">EQ</span>
          </button>
          <p v-if="!availableDevices.length" class="eq-extra-hint">No output devices detected yet.</p>
        </div>

        <div class="eq-divider" />

        <div class="dev-actions">
          <button class="dev-save" :disabled="!availableDevices.length" @click="devices.saveActiveProfile()">Save current EQ to this device</button>
          <button v-if="devices.activeHasProfile()" class="dev-del" @click="devices.deleteProfile(devices.activeKey())">Remove profile</button>
        </div>
        <p class="eq-extra-hint">A saved profile stores the current EQ + crossfeed for this physical output. Switching to a device without a profile restores Flat EQ and disables crossfeed.</p>
      </template>
    </div>
  </AppDialog>
</template>

<script setup lang="ts">
import type { SelectOption } from '~/components/ui/AppSelect.vue'
import type { DspBlockId } from '~/stores/audio-settings'

const props = defineProps<{ open: boolean }>()
defineEmits<{ close: [] }>()

const settings = useAudioSettingsStore()
const player = usePlayerBindings()
const { isTauriClient } = useClientSurface()
// directMode is a plain boolean present on every useAudioEngine() branch
// (graph/direct/SSR stub) — see useAudioEngine.ts — so this needs no cast.
const engine = useAudioEngine()
const eqAvailable = computed(() => !engine.directMode)
const { eq, crossfade, replayGain, crossfeed, dspChain } = storeToRefs(settings)
const nativeModeSaving = ref(false)
const outputSaving = ref(false)
const bitPerfectSupported = computed(() => !!player.nativeAudioCapabilities.value?.bitPerfect.available)
const bitPerfectRequested = computed(() =>
  player.nativeAudioCapabilities.value?.preferredOutputMode === 'bit_perfect')
const nativeRouteTitle = computed(() => {
  if (!player.nativeAudioCapabilities.value) return 'Native audio · checking…'
  if (!player.nativeAudioCapabilities.value.available) return 'Native audio unavailable'
  return bitPerfectRequested.value ? 'Heya Rust Audio · bit-perfect' : 'Heya Rust Audio · processed'
})
const nativeRouteDetail = computed(() => {
  const capabilities = player.nativeAudioCapabilities.value
  if (!capabilities) return 'Negotiating the origin-scoped audio bridge.'
  if (!capabilities.bitPerfect.available) {
    return capabilities.bitPerfect.unavailableReason ?? 'Processed native playback is available.'
  }
  return bitPerfectRequested.value
    ? 'Exclusive source-rate output; all DSP is bypassed.'
    : 'Gapless playback, crossfade, ReplayGain, EQ, limiter, and native diagnostics.'
})

async function setBitPerfectMode(enabled: boolean) {
  if (nativeModeSaving.value) return
  nativeModeSaving.value = true
  const changed = await player.setBitPerfectAudio(enabled)
  nativeModeSaving.value = false
  if (!changed) useToast().toast.err('Could not change the native audio output mode')
}

const TABS = [
  { id: 'eq', label: 'Equalizer' },
  { id: 'playback', label: 'Playback' },
  { id: 'effects', label: 'Effects' },
  { id: 'output', label: 'Output' },
] as const
const tab = ref<typeof TABS[number]['id']>('eq')

const devices = useAudioDevices()
const { availableDevices, activeDeviceId, labelsAvailable, followsSystemDefault, supported } = devices

async function selectOutput(deviceId: string) {
  if (outputSaving.value) return
  outputSaving.value = true
  const changed = await devices.selectDevice(deviceId)
  outputSaving.value = false
  if (!changed) useToast().toast.err('Could not change the audio output device')
}

async function selectSystemOutput() {
  if (outputSaving.value) return
  outputSaving.value = true
  const changed = await devices.useSystemDefault()
  outputSaving.value = false
  if (!changed) useToast().toast.err('Could not follow the system audio output')
}

async function refreshOutputs() {
  if (outputSaving.value) return
  outputSaving.value = true
  await devices.refresh()
  outputSaving.value = false
}

// Enumerate outputs the first time the modal opens. Deferred (not on mount) so
// the enumerate/permission surface only touches the API when the user actually
// looks at audio settings.
watch(() => props.open, (isOpen) => {
  if (isOpen) {
    void devices.init()
    if (isTauriClient.value) void player.probeNativeAudio()
  }
}, { immediate: true })

// Signal-chain block helpers. 'equalizer' covers preamp+EQ+postgain as a unit.
function blockLabel(id: DspBlockId) {
  return id === 'equalizer' ? 'Equalizer' : 'Crossfeed'
}
function blockEnabled(id: DspBlockId) {
  return id === 'equalizer' ? eq.value.enabled : crossfeed.value.enabled
}
function setBlockEnabled(id: DspBlockId, v: boolean) {
  if (id === 'equalizer') settings.setEQEnabled(v)
  else settings.setCrossfeedEnabled(v)
}

const CROSSFADE_OPTIONS: SelectOption[] = [
  { value: 'gapless', label: 'Gapless' },
  { value: 'crossfade', label: 'Crossfade' },
  { value: 'smart', label: 'Smart' },
]

const REPLAY_GAIN_OPTIONS: SelectOption[] = [
  { value: 'off',   label: 'Off — native level' },
  { value: 'track', label: 'Track — each song normalized' },
  { value: 'album', label: 'Album — preserve album dynamics' },
  { value: 'auto',  label: 'Auto — track on shuffle, album otherwise' },
]

const CROSSFEED_OPTIONS: SelectOption[] = [
  { value: 'subtle',  label: 'Subtle' },
  { value: 'natural', label: 'Natural' },
  { value: 'strong',  label: 'Strong' },
]

const BAND_LABELS = ['32', '64', '125', '250', '500', '1K', '2K', '4K', '8K', '16K'] as const

// Bands map -12..+12 dB onto the bar track. 0 dB sits at the vertical
// center; positive fills up from the middle, negative fills down. The
// knob shows the absolute position for precision.
function bandStyle(value: number) {
  const pct = Math.abs(value) / 12 * 50
  if (value >= 0) {
    return { height: `${pct}%`, bottom: '50%' }
  }
  return { height: `${pct}%`, top: '50%' }
}
function knobStyle(value: number) {
  // 50% = 0dB, 0% = top (+12dB), 100% = bottom (-12dB).
  const top = 50 - (value / 12) * 50
  return { top: `${top}%` }
}

let dragIndex = -1
let dragRect: DOMRect | null = null

// Pointer events (not mouse-only) so the same handlers cover touch —
// mirrors the pointerdown/pointermove/pointerup pattern in MusicWaveform.vue.
function onBandPointerDown(e: PointerEvent, index: number) {
  dragIndex = index
  dragRect = (e.currentTarget as HTMLElement).getBoundingClientRect()
  try { (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId) } catch { /* not capturable */ }
  applyDrag(e.clientY)
}
function applyDrag(clientY: number) {
  if (dragIndex < 0 || !dragRect) return
  const y = (clientY - dragRect.top) / dragRect.height
  // y=0 (top) → +12; y=1 (bottom) → -12; linear in between.
  const value = Math.round(((1 - Math.max(0, Math.min(1, y))) * 24 - 12) * 2) / 2
  settings.setEQBand(dragIndex, value)
}
useEventListener(window, 'pointermove', (e: PointerEvent) => { if (dragIndex >= 0) applyDrag(e.clientY) })
useEventListener(window, 'pointerup', () => { dragIndex = -1; dragRect = null })

// Keyboard: arrow keys nudge by 0.5dB (the same step the drag rounds to),
// Page Up/Down by 2dB, Home/End to the extremes. stopPropagation mirrors
// MusicWaveform's reasoning — EQPanel opens as a dialog over the player, so
// there's no global-hotkey conflict risk here, but it keeps arrow keys from
// also scrolling the dialog body.
function onBandKeydown(e: KeyboardEvent, index: number) {
  const cur = eq.value.bands[index] ?? 0
  let next: number
  switch (e.key) {
    case 'ArrowUp':
    case 'ArrowRight': next = cur + 0.5; break
    case 'ArrowDown':
    case 'ArrowLeft': next = cur - 0.5; break
    case 'PageUp': next = cur + 2; break
    case 'PageDown': next = cur - 2; break
    case 'Home': next = -12; break
    case 'End': next = 12; break
    default: return
  }
  e.preventDefault()
  e.stopPropagation()
  settings.setEQBand(index, Math.max(-12, Math.min(12, Math.round(next * 2) / 2)))
}
</script>

<style scoped>
/* AppDialog supplies the modal chrome (overlay/panel/header/close). The
   rules below are layout-only for the EQ-specific body content. */

/* Tabs */
.eq-tabs {
  display: flex;
  gap: 4px;
  padding: 3px;
  margin-bottom: 18px;
  background: rgb(var(--ink) / 0.04);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.eq-tab {
  flex: 1;
  padding: 7px 10px;
  font-size: 12px;
  font-weight: 500;
  color: var(--fg-2);
  background: transparent;
  border: none;
  border-radius: var(--r-sm);
  cursor: pointer;
  transition: background 0.12s, color 0.12s;
}
.eq-tab:hover { color: var(--fg-0); }
.eq-tab.active {
  color: var(--gold-bright, var(--gold));
  background: var(--gold-soft, rgba(230, 185, 74, 0.1));
}
.eq-native-route {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 10px 12px;
  border: 1px solid color-mix(in srgb, var(--gold) 28%, var(--border));
  border-radius: var(--r-md);
  background: var(--gold-soft, rgba(230, 185, 74, 0.08));
}
.eq-native-route strong,
.eq-native-route span { display: block; }
.eq-native-route strong { color: var(--fg-1); font-size: 12px; font-weight: 600; }
.eq-native-route > div > span { margin-top: 3px; color: var(--fg-3); font-size: 10px; }
.eq-native-toggle { display: flex; align-items: center; gap: 9px; color: var(--fg-2); font-size: 11px; white-space: nowrap; }
.eq-bypassed { opacity: 0.5; pointer-events: none; }
.eq-playback-processing { display: flex; flex-direction: column; gap: 12px; }
/* Standalone tab panes (Playback / Effects) — no top border, the tab bar
   already separates them from the header. */
.eq-pane { display: flex; flex-direction: column; gap: 12px; }

.eq-unavailable {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 16px;
  background: rgb(var(--ink) / 0.03);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.eq-unavailable-title {
  margin: 0;
  font-size: 13px;
  font-weight: 600;
  color: var(--fg-1);
}

.eq-master-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 14px;
  margin-bottom: 18px;
  background: rgb(var(--ink) / 0.03);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.eq-master-label {
  font-size: 13px;
  font-weight: 500;
  color: var(--fg-1);
}

.eq-presets { display: flex; gap: 6px; flex-wrap: wrap; margin-bottom: 20px; }
.eq-preset {
  padding: 5px 12px;
  border-radius: 999px;
  font-size: 11px;
  font-family: var(--font-mono);
  color: var(--fg-1);
  background: rgb(var(--ink) / 0.04);
  border: 1px solid var(--border);
  cursor: pointer;
  transition: all 0.15s;
}
.eq-preset:hover { background: rgb(var(--ink) / 0.08); }
.eq-preset.active { background: var(--gold-soft); border-color: color-mix(in srgb, var(--gold) 40%, transparent); color: var(--gold-bright); }

.eq-bands {
  display: flex;
  gap: 12px;
  justify-content: space-between;
  margin-bottom: 22px;
  padding: 0 4px;
}
.eq-band { display: flex; flex-direction: column; align-items: center; gap: 6px; flex: 1; min-width: 0; }
.eq-bar-track {
  width: 12px;
  height: 160px;
  background: rgb(var(--ink) / 0.06);
  border-radius: 6px;
  position: relative;
  cursor: ns-resize;
  user-select: none;
  touch-action: none;
}
.eq-bar-baseline {
  position: absolute;
  left: 0; right: 0;
  top: 50%;
  height: 1px;
  background: rgb(var(--ink) / 0.18);
}
.eq-bar-fill {
  position: absolute;
  left: 0; right: 0;
  background: var(--gold);
  border-radius: 6px;
}
.eq-bar-fill.negative { background: var(--fg-3); }
.eq-bar-knob {
  position: absolute;
  left: 50%;
  width: 18px; height: 18px;
  border-radius: 50%;
  background: var(--fg-0);
  transform: translate(-50%, -50%);
  box-shadow: 0 2px 6px rgba(0,0,0,0.5);
  pointer-events: none;
}
.eq-val {
  font-size: 9px;
  font-family: var(--font-mono);
  color: var(--fg-2);
  min-width: 24px;
  text-align: center;
}
.eq-freq { font-size: 10px; font-family: var(--font-mono); color: var(--fg-3); }

.eq-extras { display: flex; flex-direction: column; gap: 12px; padding-top: 18px; border-top: 1px solid var(--border); }
.eq-extra-row { display: flex; align-items: center; gap: 12px; font-size: 12px; color: var(--fg-1); }
.eq-extra-label { width: 80px; flex-shrink: 0; color: var(--fg-2); }
.eq-extra-hint { flex: 1; min-width: 0; color: var(--fg-3); font-size: 11px; }
.eq-extra-val {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--fg-3);
  min-width: 56px;
  text-align: right;
}
/* The AppSlider has its own visual identity; we only need the row layout
   constraint that it takes the remaining horizontal space. Pre-amp,
   Post-gain, and Crossfade-duration all share this. */
.eq-slider-flex { flex: 1; min-width: 0; }
.eq-select-wrap {
  /* AppSelect's trigger is width:100% — we constrain it via the wrapper.
     Crossfade gets a compact slot so the slider sits next to it; Replay
     Gain takes the full remaining width since its option labels are long. */
  flex: 0 0 160px;
  min-width: 0;
}
.eq-select-wrap-wide { flex: 1; }
.eq-divider { height: 1px; background: var(--border); margin: 6px 0; }

/* Signal chain */
.eq-chain { display: flex; flex-direction: column; gap: 4px; }
.eq-chain-label {
  font-size: 11px;
  font-weight: 600;
  color: var(--fg-2);
  margin-bottom: 4px;
}
.chain-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 7px 10px;
  border-radius: var(--r-sm);
  background: rgb(var(--ink) / 0.03);
  border: 1px solid var(--border);
}
.chain-row.chain-pinned { background: transparent; border-style: dashed; opacity: 0.8; }
.chain-name { font-size: 12px; color: var(--fg-1); flex: 1; }
.chain-note {
  font-size: 10px;
  font-family: var(--font-mono);
  color: var(--fg-3);
  text-transform: lowercase;
}
.chain-move { display: flex; flex-direction: column; gap: 1px; }
.chain-arrow {
  width: 18px; height: 13px;
  display: flex; align-items: center; justify-content: center;
  font-size: 10px; line-height: 1;
  color: var(--fg-2);
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  border-radius: 3px;
  cursor: pointer;
  transition: background 0.12s, color 0.12s;
}
.chain-arrow:hover:not(:disabled) { background: rgb(var(--ink) / 0.12); color: var(--fg-0); }
.chain-arrow:disabled { opacity: 0.3; cursor: default; }

/* Output devices */
.dev-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.dev-head-actions { display: flex; align-items: center; justify-content: flex-end; gap: 6px; flex-wrap: wrap; }
.dev-reveal {
  font-size: 11px;
  color: var(--fg-2);
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  border-radius: 999px;
  padding: 3px 10px;
  cursor: pointer;
  transition: background 0.12s, color 0.12s;
}
.dev-reveal:hover { background: rgb(var(--ink) / 0.1); color: var(--fg-0); }
.dev-reveal.active { color: var(--gold-bright, var(--gold)); border-color: color-mix(in srgb, var(--gold) 35%, var(--border)); background: var(--gold-soft); }
.dev-reveal:disabled { opacity: 0.45; cursor: default; }

.dev-list { display: flex; flex-direction: column; gap: 4px; }
.dev-row {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 8px 10px;
  border-radius: var(--r-sm);
  background: rgb(var(--ink) / 0.03);
  border: 1px solid var(--border);
  cursor: pointer;
  text-align: left;
  transition: background 0.12s, border-color 0.12s;
}
.dev-row:hover { background: rgb(var(--ink) / 0.06); }
.dev-row.active { border-color: color-mix(in srgb, var(--gold) 40%, transparent); background: var(--gold-soft); }
.dev-row:disabled { cursor: default; opacity: 0.55; }
.dev-row:disabled:hover { background: rgb(var(--ink) / 0.03); }
.dev-row.active:disabled:hover { background: var(--gold-soft); }
.dev-dot {
  width: 8px; height: 8px;
  flex-shrink: 0;
  border-radius: 50%;
  background: var(--fg-3);
  transition: background 0.12s, box-shadow 0.12s;
}
.dev-row.active .dev-dot { background: var(--gold); box-shadow: 0 0 6px var(--gold); }
.dev-name { flex: 1; min-width: 0; font-size: 12px; color: var(--fg-1); display: flex; align-items: center; gap: 8px; }
.dev-tag {
  font-size: 9px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--fg-3);
  border: 1px solid var(--border);
  border-radius: 4px;
  padding: 1px 5px;
}
.dev-badge {
  font-size: 9px;
  font-family: var(--font-mono);
  font-weight: 600;
  color: var(--gold-bright, var(--gold));
  background: var(--gold-soft);
  border-radius: 4px;
  padding: 2px 6px;
}

.dev-actions { display: flex; gap: 8px; align-items: center; }
.dev-save {
  flex: 1;
  padding: 8px 12px;
  font-size: 12px;
  font-weight: 500;
  color: var(--fg-0);
  background: rgb(var(--ink) / 0.06);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  cursor: pointer;
  transition: background 0.12s;
}
.dev-save:hover { background: rgb(var(--ink) / 0.12); }
.dev-save:disabled { opacity: 0.45; cursor: default; }
.dev-del {
  padding: 8px 12px;
  font-size: 12px;
  color: var(--fg-2);
  background: transparent;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  cursor: pointer;
  transition: background 0.12s, color 0.12s;
}
/* Matches the .btn-danger/.sv2-btn.danger convention (heya.css) instead of a
   hand-tuned red that duplicated --bad and wouldn't track its theme cut. */
.dev-del:hover {
  background: color-mix(in srgb, var(--bad) 14%, transparent);
  color: var(--bad);
  border-color: color-mix(in srgb, var(--bad) 40%, transparent);
}
</style>
