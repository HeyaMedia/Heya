<template>
  <Teleport to="body">
    <div v-if="open" class="eq-overlay" @click.self="$emit('close')">
      <div class="eq-modal">
        <div class="eq-header">
          <h3 style="font-size: 16px; font-weight: 600">Equalizer</h3>
          <button class="btn-icon" @click="$emit('close')"><Icon name="close" :size="18" /></button>
        </div>

        <div class="eq-presets">
          <button
            v-for="p in presets"
            :key="p.name"
            class="eq-preset"
            :class="{ active: activePreset === p.name }"
            @click="applyPreset(p)"
          >
            {{ p.name }}
          </button>
        </div>

        <div class="eq-bands">
          <div v-for="(band, i) in bands" :key="i" class="eq-band">
            <div class="eq-bar-track">
              <div class="eq-bar-fill" :style="{ height: band.value + '%' }" />
            </div>
            <span class="eq-freq">{{ band.label }}</span>
          </div>
        </div>

        <div class="eq-extras">
          <div class="eq-extra-row">
            <span>Pre-amp</span>
            <div class="rail" style="flex: 1" @click="preamp = getClickPct($event)">
              <div class="fill" :style="{ width: preamp + '%' }" />
              <div class="knob" :style="{ left: preamp + '%' }" />
            </div>
          </div>
          <div class="eq-extra-row">
            <span>Crossfade</span>
            <div class="rail" style="flex: 1" @click="crossfade = getClickPct($event)">
              <div class="fill" :style="{ width: crossfade + '%' }" />
              <div class="knob" :style="{ left: crossfade + '%' }" />
            </div>
            <span style="font-family: var(--font-mono); font-size: 11px; color: var(--fg-3); min-width: 28px; text-align: right">{{ Math.round(crossfade / 10) }}s</span>
          </div>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
defineProps<{ open: boolean }>()
defineEmits<{ close: [] }>()

const activePreset = ref('Flat')
const preamp = ref(50)
const crossfade = ref(0)

const bands = ref([
  { label: '32', value: 50 },
  { label: '64', value: 50 },
  { label: '125', value: 50 },
  { label: '250', value: 50 },
  { label: '500', value: 50 },
  { label: '1K', value: 50 },
  { label: '2K', value: 50 },
  { label: '4K', value: 50 },
  { label: '8K', value: 50 },
  { label: '16K', value: 50 },
])

const presets = [
  { name: 'Flat', values: [50,50,50,50,50,50,50,50,50,50] },
  { name: 'Bass Boost', values: [80,75,65,55,50,50,50,50,50,50] },
  { name: 'Vocal', values: [40,45,55,70,75,75,65,55,45,40] },
  { name: 'Treble', values: [50,50,50,50,50,55,65,75,80,85] },
  { name: 'Rock', values: [70,65,55,50,45,55,65,70,70,65] },
  { name: 'Electronic', values: [75,70,50,45,55,60,55,65,75,70] },
]

function applyPreset(p: typeof presets[0]) {
  activePreset.value = p.name
  p.values.forEach((v, i) => { bands.value[i].value = v })
}

function getClickPct(e: MouseEvent) {
  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
  return Math.round(((e.clientX - rect.left) / rect.width) * 100)
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
  width: 520px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  padding: 28px;
  box-shadow: var(--shadow-3);
}
.eq-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
.eq-presets { display: flex; gap: 6px; flex-wrap: wrap; margin-bottom: 24px; }
.eq-preset {
  padding: 6px 14px;
  border-radius: 999px;
  font-size: 12px;
  font-family: var(--font-mono);
  color: var(--fg-1);
  background: rgba(255,255,255,0.05);
  border: 1px solid var(--border);
  transition: all 0.15s;
}
.eq-preset:hover { background: rgba(255,255,255,0.1); }
.eq-preset.active { background: var(--gold-soft); border-color: rgba(230,185,74,0.4); color: var(--gold-bright); }
.eq-bands { display: flex; gap: 12px; justify-content: center; margin-bottom: 28px; padding: 0 8px; }
.eq-band { display: flex; flex-direction: column; align-items: center; gap: 8px; }
.eq-bar-track {
  width: 6px; height: 100px;
  background: rgba(255,255,255,0.08);
  border-radius: 3px;
  position: relative;
  display: flex;
  align-items: flex-end;
}
.eq-bar-fill {
  width: 100%;
  background: var(--gold);
  border-radius: inherit;
  transition: height 0.2s ease;
}
.eq-freq { font-size: 9px; font-family: var(--font-mono); color: var(--fg-3); }
.eq-extras { display: flex; flex-direction: column; gap: 14px; padding-top: 16px; border-top: 1px solid var(--border); }
.eq-extra-row { display: flex; align-items: center; gap: 14px; font-size: 12px; color: var(--fg-1); }
</style>
