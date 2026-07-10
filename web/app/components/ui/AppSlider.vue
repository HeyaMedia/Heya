<!--
  AppSlider — value-on-a-track input.

  Wraps reka-ui's Slider primitives, exposing a single-value v-model (reka
  itself uses arrays so it can express ranges; we box/unbox for ergonomics).
  Keyboard a11y, drag, focus, touch — all reka. We only own the visual
  tokens so volume / pre-amp / post-gain / whatever-comes-next look the
  same.

  Usage:
    <AppSlider v-model="volume" :min="0" :max="100" />
    <AppSlider v-model="preamp" :min="-12" :max="12" :step="0.5" bipolar />

  `bipolar` styles the range fill from the centre of the track outward —
  pairs with min/max symmetric around 0 (e.g. -12..+12 dB). Otherwise the
  fill grows from min toward the thumb, the standard volume-style pattern.
-->
<template>
  <SliderRoot
    :model-value="boxed"
    :min="min"
    :max="max"
    :step="step"
    :disabled="disabled"
    class="app-slider"
    :class="{ 'is-bipolar': bipolar, 'is-disabled': disabled }"
    :orientation="orientation"
    :aria-label="ariaLabel"
    @update:model-value="onUpdate"
  >
    <SliderTrack class="app-slider-track">
      <SliderRange v-if="!bipolar" class="app-slider-range" />
      <!-- Bipolar: the fill is positioned absolutely between centre and
           thumb; reka's SliderRange would always anchor to min, so we
           render our own indicator. -->
      <div
        v-else
        class="app-slider-range app-slider-range-bipolar"
        :style="bipolarRangeStyle"
      />
    </SliderTrack>
    <SliderThumb class="app-slider-thumb" :aria-label="ariaLabel ?? 'value'" />
  </SliderRoot>
</template>

<script setup lang="ts">
import { SliderRoot, SliderTrack, SliderRange, SliderThumb } from 'reka-ui'

const props = withDefaults(defineProps<{
  min?: number
  max?: number
  step?: number
  bipolar?: boolean
  orientation?: 'horizontal' | 'vertical'
  disabled?: boolean
  ariaLabel?: string
}>(), {
  min: 0,
  max: 100,
  step: 1,
  orientation: 'horizontal',
})

const value = defineModel<number>({ default: 0 })

// Reka takes/returns an array — single-thumb sliders just box into `[v]`.
const boxed = computed(() => [value.value])
function onUpdate(v: number[] | undefined) {
  if (v && v.length) value.value = v[0]!
}

// Bipolar fill: stretches between the centre (50%) and the thumb's
// percentage along the track. Used for the dB sliders so it's obvious
// which side of zero you're on.
const bipolarRangeStyle = computed(() => {
  const pct = ((value.value - props.min) / (props.max - props.min)) * 100
  const centre = 50
  const start = Math.min(pct, centre)
  const width = Math.abs(pct - centre)
  return { left: `${start}%`, width: `${width}%` }
})
</script>

<style scoped>
.app-slider {
  position: relative;
  display: flex;
  align-items: center;
  user-select: none;
  touch-action: none;
  width: 100%;
  height: 20px;
  cursor: pointer;
}
.app-slider.is-disabled { opacity: 0.4; cursor: not-allowed; }

.app-slider-track {
  position: relative;
  flex-grow: 1;
  background: rgb(var(--ink) / 0.08);
  border-radius: 999px;
  height: 4px;
  overflow: hidden;
}

.app-slider-range {
  position: absolute;
  height: 100%;
  background: var(--gold);
  border-radius: 999px;
  transition: background 0.15s;
}
/* The bipolar variant positions itself via inline style (see script). The
   visual is identical to a fill from min — gold rail growing from centre
   outward — but the maths is computed instead of relying on reka. */
.app-slider-range-bipolar { /* base styling shared with .app-slider-range */ }

.app-slider:hover .app-slider-range { background: var(--gold-bright, var(--gold)); }

.app-slider-thumb {
  display: block;
  width: 14px;
  height: 14px;
  background: var(--fg-0);
  border-radius: 50%;
  box-shadow: 0 1px 3px rgb(var(--shade) / 0.4);
  outline: none;
  transition: transform 0.12s, box-shadow 0.12s;
  position: relative;
  z-index: 1;
}
.app-slider-thumb:hover { transform: scale(1.15); }
.app-slider-thumb:focus-visible {
  box-shadow: 0 0 0 4px color-mix(in srgb, var(--gold) 30%, transparent), 0 1px 3px rgb(var(--shade) / 0.4);
}
.app-slider-thumb[data-disabled] { background: var(--fg-3); }

/* Centre tick for bipolar — a subtle vertical mark at 50% so the user can
   see where zero is without reading the number. */
.app-slider.is-bipolar .app-slider-track::before {
  content: '';
  position: absolute;
  left: 50%;
  top: 50%;
  transform: translate(-50%, -50%);
  width: 1px;
  height: 8px;
  background: rgb(var(--ink) / 0.18);
  z-index: 0;
}
</style>
