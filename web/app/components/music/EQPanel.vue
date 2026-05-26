<template>
  <AppDialog
    :model-value="open"
    title="Equalizer"
    size="md"
    @update:model-value="(v) => v ? null : $emit('close')"
  >
    <!-- Master enable toggle — promoted to a labeled row so the relationship
         between the switch and "the whole EQ" is explicit. The old design
         tucked this into the header next to the close button. -->
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

      <div class="eq-divider" />

      <div class="eq-extra-row">
        <span class="eq-extra-label">Crossfade</span>
        <div class="eq-select-wrap">
          <AppSelect
            :model-value="crossfade.mode"
            :options="CROSSFADE_OPTIONS"
            aria-label="Crossfade mode"
            @change="v => settings.setCrossfadeMode(v as 'gapless' | 'crossfade')"
          />
        </div>
        <AppSlider
          v-if="crossfade.mode === 'crossfade'"
          :model-value="crossfade.durationSeconds"
          :min="1"
          :max="12"
          :step="1"
          aria-label="Crossfade duration"
          class="eq-slider-flex"
          @update:model-value="settings.setCrossfadeDuration"
        />
        <span v-if="crossfade.mode === 'crossfade'" class="eq-extra-val">{{ crossfade.durationSeconds }}s</span>
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
  </AppDialog>
</template>

<script setup lang="ts">
defineProps<{ open: boolean }>()
defineEmits<{ close: [] }>()

import type { SelectOption } from '~/components/ui/AppSelect.vue'

const settings = useAudioSettings()
const eq = settings.eq
const crossfade = settings.crossfade
const replayGain = settings.replayGain

const CROSSFADE_OPTIONS: SelectOption[] = [
  { value: 'gapless', label: 'Gapless' },
  { value: 'crossfade', label: 'Crossfade' },
]

const REPLAY_GAIN_OPTIONS: SelectOption[] = [
  { value: 'off',   label: 'Off — native level' },
  { value: 'track', label: 'Track — each song normalized' },
  { value: 'album', label: 'Album — preserve album dynamics' },
  { value: 'auto',  label: 'Auto — track on shuffle, album otherwise' },
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
</style>
