<template>
  <AppDialog
    :model-value="open"
    title="Audio"
    size="md"
    @update:model-value="(v) => v ? null : $emit('close')"
  >
    <div class="eq-tabs">
      <button
        v-for="t in TABS"
        :key="t.id"
        class="eq-tab"
        :class="{ active: tab === t.id }"
        @click="tab = t.id"
      >{{ t.label }}</button>
    </div>

    <!-- ── Equalizer ─────────────────────────────────────────────── -->
    <div v-show="tab === 'eq'">
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
            @mousedown="startDrag($event, i)"
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
    </div>

    <!-- ── Playback ──────────────────────────────────────────────── -->
    <div v-show="tab === 'playback'" class="eq-pane">
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

    <!-- ── Effects ───────────────────────────────────────────────── -->
    <div v-show="tab === 'effects'" class="eq-pane">
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
    <div v-show="tab === 'output'" class="eq-pane">
      <p v-if="!supported" class="eq-extra-hint">
        This browser doesn't expose per-app audio-output routing (<code>AudioContext.setSinkId</code>), so playback follows the system default output. Chromium-based browsers support it today; other engines light this up automatically once they ship the API.
      </p>
      <template v-else>
        <div class="dev-head">
          <span class="eq-chain-label">Output device</span>
          <button v-if="!labelsAvailable" class="dev-reveal" @click="devices.revealLabels()">Reveal names</button>
        </div>

        <div class="dev-list">
          <button
            v-for="d in availableDevices"
            :key="d.deviceId"
            class="dev-row"
            :class="{ active: d.deviceId === activeDeviceId }"
            @click="devices.selectDevice(d.deviceId)"
          >
            <span class="dev-dot" />
            <span class="dev-name">
              {{ d.label }}
              <span v-if="d.isDefault" class="dev-tag">default</span>
            </span>
            <span v-if="devices.hasProfile(d)" class="dev-badge">EQ</span>
          </button>
          <p v-if="!availableDevices.length" class="eq-extra-hint">No output devices detected yet.</p>
        </div>

        <div class="eq-divider" />

        <div class="dev-actions">
          <button class="dev-save" @click="devices.saveActiveProfile()">Save current EQ to this device</button>
          <button v-if="devices.activeHasProfile()" class="dev-del" @click="devices.deleteProfile(devices.activeKey())">Remove profile</button>
        </div>
        <p class="eq-extra-hint">A saved profile stores the current EQ + crossfeed and re-applies automatically whenever you switch back to that device — surviving unplug/replug.</p>
      </template>
    </div>
  </AppDialog>
</template>

<script setup lang="ts">
import type { SelectOption } from '~/components/ui/AppSelect.vue'
import type { DspBlockId } from '~/composables/useAudioSettings'

const props = defineProps<{ open: boolean }>()
defineEmits<{ close: [] }>()

const settings = useAudioSettings()
const eq = settings.eq
const crossfade = settings.crossfade
const replayGain = settings.replayGain
const crossfeed = settings.crossfeed
const dspChain = settings.dspChain

const TABS = [
  { id: 'eq', label: 'Equalizer' },
  { id: 'playback', label: 'Playback' },
  { id: 'effects', label: 'Effects' },
  { id: 'output', label: 'Output' },
] as const
const tab = ref<typeof TABS[number]['id']>('eq')

const devices = useAudioDevices()
const { availableDevices, activeDeviceId, labelsAvailable, supported } = devices

// Enumerate outputs the first time the modal opens. Deferred (not on mount) so
// the enumerate/permission surface only touches the API when the user actually
// looks at audio settings.
watch(() => props.open, (isOpen) => {
  if (isOpen) void devices.init()
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

function startDrag(e: MouseEvent, index: number) {
  dragIndex = index
  dragRect = (e.currentTarget as HTMLElement).getBoundingClientRect()
  applyDrag(e.clientY)
}
function applyDrag(clientY: number) {
  if (dragIndex < 0 || !dragRect) return
  const y = (clientY - dragRect.top) / dragRect.height
  // y=0 (top) → +12; y=1 (bottom) → -12; linear in between.
  const value = Math.round(((1 - Math.max(0, Math.min(1, y))) * 24 - 12) * 2) / 2
  settings.setEQBand(dragIndex, value)
}
useEventListener(window, 'mousemove', (e: MouseEvent) => { if (dragIndex >= 0) applyDrag(e.clientY) })
useEventListener(window, 'mouseup', () => { dragIndex = -1; dragRect = null })
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
  background: rgba(255, 255, 255, 0.04);
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
/* Standalone tab panes (Playback / Effects) — no top border, the tab bar
   already separates them from the header. */
.eq-pane { display: flex; flex-direction: column; gap: 12px; }

.eq-master-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 14px;
  margin-bottom: 18px;
  background: rgba(255, 255, 255, 0.03);
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
  background: rgba(255,255,255,0.04);
  border: 1px solid var(--border);
  cursor: pointer;
  transition: all 0.15s;
}
.eq-preset:hover { background: rgba(255,255,255,0.08); }
.eq-preset.active { background: var(--gold-soft); border-color: rgba(230,185,74,0.4); color: var(--gold-bright); }

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
  background: rgba(255,255,255,0.06);
  border-radius: 6px;
  position: relative;
  cursor: ns-resize;
  user-select: none;
}
.eq-bar-baseline {
  position: absolute;
  left: 0; right: 0;
  top: 50%;
  height: 1px;
  background: rgba(255,255,255,0.18);
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
  background: rgba(255, 255, 255, 0.03);
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
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid var(--border);
  border-radius: 3px;
  cursor: pointer;
  transition: background 0.12s, color 0.12s;
}
.chain-arrow:hover:not(:disabled) { background: rgba(255, 255, 255, 0.12); color: var(--fg-0); }
.chain-arrow:disabled { opacity: 0.3; cursor: default; }

/* Output devices */
.dev-head { display: flex; align-items: center; justify-content: space-between; }
.dev-reveal {
  font-size: 11px;
  color: var(--fg-2);
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid var(--border);
  border-radius: 999px;
  padding: 3px 10px;
  cursor: pointer;
  transition: background 0.12s, color 0.12s;
}
.dev-reveal:hover { background: rgba(255, 255, 255, 0.1); color: var(--fg-0); }

.dev-list { display: flex; flex-direction: column; gap: 4px; }
.dev-row {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 8px 10px;
  border-radius: var(--r-sm);
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid var(--border);
  cursor: pointer;
  text-align: left;
  transition: background 0.12s, border-color 0.12s;
}
.dev-row:hover { background: rgba(255, 255, 255, 0.06); }
.dev-row.active { border-color: rgba(230, 185, 74, 0.4); background: var(--gold-soft); }
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
  background: rgba(255, 255, 255, 0.06);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  cursor: pointer;
  transition: background 0.12s;
}
.dev-save:hover { background: rgba(255, 255, 255, 0.12); }
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
.dev-del:hover { background: rgba(220, 80, 80, 0.14); color: #f0a0a0; border-color: rgba(220, 80, 80, 0.4); }
</style>
