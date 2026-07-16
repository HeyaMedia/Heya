<!--
  AppHoldButton — a button with a press-and-hold secondary action.

  A quick press emits `click` as normal. Holding the pointer down draws a
  ring that fills around the button over `holdMs`; if the hold completes,
  `hold` fires instead (and the release's click is swallowed). Used by the
  rail left-arrows: click steps left, hold rewinds to the rail's start.

  The ring is decorative (aria-hidden) and inherits the accent via
  var(--gold). All classes/attrs fall through to the <button>, so consumers
  keep their existing arrow styling untouched.
-->
<template>
  <button
    class="ahb"
    :class="{ holding }"
    v-bind="$attrs"
    @pointerdown="startHold"
    @pointerup="releaseHold"
    @pointerleave="cancelHold"
    @pointercancel="cancelHold"
    @click="onClick"
  >
    <slot />
    <svg class="ahb-ring" viewBox="0 0 36 36" aria-hidden="true">
      <circle
        class="ahb-ring-fill"
        cx="18" cy="18" r="16.5"
        :style="{ transitionDuration: holding ? `${holdMs}ms` : '150ms' }"
      />
    </svg>
  </button>
</template>

<script setup lang="ts">
defineOptions({ inheritAttrs: false })

const props = withDefaults(defineProps<{
  /** Hold duration before the `hold` action fires. */
  holdMs?: number
}>(), {
  holdMs: 2000,
})

const emit = defineEmits<{ click: []; hold: [] }>()

const holding = ref(false)
let timer: ReturnType<typeof setTimeout> | null = null
let completed = false

function startHold(e: PointerEvent) {
  // Primary button / touch only — right-click shouldn't arm the ring.
  if (e.button !== 0) return
  completed = false
  holding.value = true
  timer = setTimeout(() => {
    timer = null
    holding.value = false
    completed = true
    emit('hold')
  }, props.holdMs)
}

function stopTimer() {
  if (timer) { clearTimeout(timer); timer = null }
  holding.value = false
}

function releaseHold() {
  stopTimer()
}

function cancelHold() {
  stopTimer()
  // Pointer left the button mid-hold: no click will fire, and a completed
  // hold has already acted — nothing else to do.
}

function onClick() {
  // A completed hold consumes the release's click.
  if (completed) { completed = false; return }
  emit('click')
}

onScopeDispose(() => { if (timer) clearTimeout(timer) })
</script>

<style scoped>
.ahb { position: relative; }

.ahb-ring {
  position: absolute;
  inset: -3px;
  width: calc(100% + 6px);
  height: calc(100% + 6px);
  pointer-events: none;
  transform: rotate(-90deg); /* fill starts at 12 o'clock */
  opacity: 0;
  transition: opacity 0.15s;
}
.ahb.holding .ahb-ring { opacity: 1; }

.ahb-ring-fill {
  fill: none;
  stroke: var(--gold);
  stroke-width: 2.5;
  stroke-linecap: round;
  /* r=16.5 → C = 2π·16.5 ≈ 103.7 */
  stroke-dasharray: 103.7;
  stroke-dashoffset: 103.7;
  transition-property: stroke-dashoffset;
  transition-timing-function: linear;
}
.ahb.holding .ahb-ring-fill { stroke-dashoffset: 0; }
</style>
