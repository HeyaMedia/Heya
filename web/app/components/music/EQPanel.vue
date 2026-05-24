<template>
  <Teleport to="body">
    <div v-if="open" class="eq-overlay" @click.self="$emit('close')">
      <div class="eq-modal">
        <div class="eq-header">
          <h3 class="eq-title">Equalizer</h3>
          <div class="eq-header-actions">
            <button
              class="eq-toggle"
              :class="{ on: eq.enabled }"
              @click="settings.setEQEnabled(!eq.enabled)"
              :title="eq.enabled ? 'Disable EQ' : 'Enable EQ'"
            >
              <span class="eq-toggle-dot" />
              <span>{{ eq.enabled ? 'On' : 'Off' }}</span>
            </button>
            <button class="btn-icon" @click="$emit('close')"><Icon name="close" :size="18" /></button>
          </div>
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
            <input
              type="range" min="-12" max="12" step="0.5"
              :value="eq.preamp"
              @input="(e) => settings.setPreamp(parseFloat((e.target as HTMLInputElement).value))"
              class="eq-slider"
            />
            <span class="eq-extra-val">{{ eq.preamp > 0 ? `+${eq.preamp}` : eq.preamp }} dB</span>
          </div>
          <div class="eq-extra-row">
            <span class="eq-extra-label">Post-gain</span>
            <input
              type="range" min="-12" max="12" step="0.5"
              :value="eq.postgain"
              @input="(e) => settings.setPostgain(parseFloat((e.target as HTMLInputElement).value))"
              class="eq-slider"
            />
            <span class="eq-extra-val">{{ eq.postgain > 0 ? `+${eq.postgain}` : eq.postgain }} dB</span>
          </div>

          <div class="eq-divider" />

          <div class="eq-extra-row">
            <span class="eq-extra-label">Crossfade</span>
            <select
              class="eq-select"
              :value="crossfade.mode"
              @change="(e) => settings.setCrossfadeMode((e.target as HTMLSelectElement).value as 'gapless' | 'crossfade')"
            >
              <option value="gapless">Gapless</option>
              <option value="crossfade">Crossfade</option>
            </select>
            <input
              v-if="crossfade.mode === 'crossfade'"
              type="range" min="1" max="12" step="1"
              :value="crossfade.durationSeconds"
              @input="(e) => settings.setCrossfadeDuration(parseInt((e.target as HTMLInputElement).value, 10))"
              class="eq-slider"
            />
            <span v-if="crossfade.mode === 'crossfade'" class="eq-extra-val">{{ crossfade.durationSeconds }}s</span>
          </div>

          <div class="eq-extra-row">
            <span class="eq-extra-label">Replay Gain</span>
            <select
              class="eq-select"
              :value="replayGain.mode"
              @change="(e) => settings.setReplayGainMode((e.target as HTMLSelectElement).value as 'off' | 'track' | 'album' | 'auto')"
            >
              <option value="off">Off — native level</option>
              <option value="track">Track — each song normalized</option>
              <option value="album">Album — preserve album dynamics</option>
              <option value="auto">Auto — track on shuffle, album otherwise</option>
            </select>
          </div>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
defineProps<{ open: boolean }>()
defineEmits<{ close: [] }>()

const settings = useAudioSettings()
const eq = settings.eq
const crossfade = settings.crossfade
const replayGain = settings.replayGain

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
  window.addEventListener('mousemove', onDragMove)
  window.addEventListener('mouseup', stopDrag)
}
function applyDrag(clientY: number) {
  if (dragIndex < 0 || !dragRect) return
  const y = (clientY - dragRect.top) / dragRect.height
  // y=0 (top) → +12; y=1 (bottom) → -12; linear in between.
  const value = Math.round(((1 - Math.max(0, Math.min(1, y))) * 24 - 12) * 2) / 2
  settings.setEQBand(dragIndex, value)
}
function onDragMove(e: MouseEvent) { applyDrag(e.clientY) }
function stopDrag() {
  dragIndex = -1
  dragRect = null
  window.removeEventListener('mousemove', onDragMove)
  window.removeEventListener('mouseup', stopDrag)
}
</script>

<style scoped>
.eq-overlay {
  position: fixed; inset: 0; z-index: 200;
  background: rgba(0,0,0,0.6);
  backdrop-filter: blur(12px);
  display: flex; align-items: center; justify-content: center;
}
.eq-modal {
  width: 560px;
  max-width: 92vw;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  padding: 24px 28px 28px;
  box-shadow: var(--shadow-3);
}
.eq-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 18px; }
.eq-title { font-size: 16px; font-weight: 600; }
.eq-header-actions { display: flex; align-items: center; gap: 10px; }
.eq-toggle {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 5px 14px 5px 8px;
  border-radius: 999px;
  background: rgba(255,255,255,0.05);
  border: 1px solid var(--border);
  font-size: 11px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--fg-2);
  cursor: pointer;
}
.eq-toggle.on { background: var(--gold-soft); border-color: rgba(230,185,74,0.4); color: var(--gold-bright); }
.eq-toggle-dot {
  width: 8px; height: 8px;
  border-radius: 50%;
  background: var(--fg-3);
  transition: background 0.15s;
}
.eq-toggle.on .eq-toggle-dot { background: var(--gold); box-shadow: 0 0 6px var(--gold); }

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
.eq-slider {
  flex: 1;
  -webkit-appearance: none;
  appearance: none;
  height: 4px;
  background: rgba(255,255,255,0.1);
  border-radius: 2px;
  outline: none;
  cursor: pointer;
}
.eq-slider::-webkit-slider-thumb {
  -webkit-appearance: none;
  width: 14px;
  height: 14px;
  border-radius: 50%;
  background: var(--gold);
  cursor: pointer;
}
.eq-slider::-moz-range-thumb {
  width: 14px;
  height: 14px;
  border-radius: 50%;
  background: var(--gold);
  border: 0;
  cursor: pointer;
}
.eq-select {
  flex: 1;
  background: var(--bg-3);
  color: var(--fg-1);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  padding: 6px 10px;
  font-size: 12px;
  font-family: inherit;
}
.eq-divider { height: 1px; background: var(--border); margin: 6px 0; }
</style>
