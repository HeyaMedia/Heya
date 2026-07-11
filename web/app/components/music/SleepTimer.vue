<!--
  SleepTimer — playbar control to pause playback after a countdown or at the end
  of the current track. Owns the 1 Hz tick that drives the countdown + expiry.
-->
<template>
  <PopoverRoot v-model:open="open">
    <PopoverTrigger as-child>
      <button class="btn-icon st-trigger" :class="{ active: sleep.active.value }" :title="triggerTitle">
        <Icon name="timer" :size="16" />
        <span v-if="countdownLabel" class="st-badge">{{ countdownLabel }}</span>
      </button>
    </PopoverTrigger>
    <PopoverPortal>
      <PopoverContent class="surface st-pop" side="top" :side-offset="12" align="end" :collision-padding="12">
        <div class="st-head">Sleep timer</div>
        <button
          v-for="opt in OPTIONS"
          :key="opt.label"
          class="surface-item st-item"
          :class="{ on: isActive(opt) }"
          @click="choose(opt)"
        >
          <span>{{ opt.label }}</span>
          <Icon v-if="isActive(opt)" name="check" :size="13" class="st-check" />
        </button>
        <div v-if="sleep.active.value" class="st-divider" />
        <button v-if="sleep.active.value" class="surface-item st-item st-off" @click="turnOff">
          <span>Turn off</span>
        </button>
      </PopoverContent>
    </PopoverPortal>
  </PopoverRoot>
</template>

<script setup lang="ts">
import { PopoverContent, PopoverPortal, PopoverRoot, PopoverTrigger } from 'reka-ui'

const sleep = useSleepTimer()
const player = usePlayerBindings()
const open = ref(false)

interface Opt { label: string, minutes?: number, endOfTrack?: boolean }
const OPTIONS: Opt[] = [
  { label: '15 minutes', minutes: 15 },
  { label: '30 minutes', minutes: 30 },
  { label: '45 minutes', minutes: 45 },
  { label: '60 minutes', minutes: 60 },
  { label: 'End of track', endOfTrack: true },
]

function choose(opt: Opt) {
  if (opt.endOfTrack) sleep.setEndOfTrack()
  else if (opt.minutes) sleep.setMinutes(opt.minutes)
  open.value = false
}
function turnOff() {
  sleep.cancel()
  open.value = false
}
function isActive(opt: Opt): boolean {
  if (opt.endOfTrack) return sleep.atTrackEnd.value
  // A timed option is "active" only while its own countdown is the running one;
  // we can't tell which preset is running once started, so highlight none.
  return false
}

const countdownLabel = computed(() => {
  if (sleep.atTrackEnd.value) return 'EOT'
  const ms = sleep.remainingMs.value
  if (ms <= 0) return ''
  const total = Math.ceil(ms / 1000)
  const m = Math.floor(total / 60)
  const s = total % 60
  return m >= 1 ? `${m}:${String(s).padStart(2, '0')}` : `${s}s`
})

const triggerTitle = computed(() => {
  if (sleep.atTrackEnd.value) return 'Sleep: end of current track'
  if (sleep.remainingMs.value > 0) return `Sleep in ${countdownLabel.value}`
  return 'Sleep timer'
})

let intervalId: ReturnType<typeof setInterval> | null = null
function startTicking() {
  if (intervalId) return
  intervalId = setInterval(() => sleep.tick(() => player.pause()), 1000)
}
function stopTicking() {
  if (!intervalId) return
  clearInterval(intervalId)
  intervalId = null
}
watch(sleep.timed, (timed) => {
  if (timed) startTicking()
  else stopTicking()
}, { immediate: true })
onUnmounted(() => {
  stopTicking()
})
</script>

<style scoped>
.st-trigger { position: relative; }
.st-badge {
  position: absolute;
  bottom: -2px;
  left: 50%;
  transform: translateX(-50%);
  font-size: 8px;
  font-weight: 700;
  font-family: var(--font-mono, monospace);
  line-height: 1;
  padding: 1px 3px;
  border-radius: 3px;
  background: var(--gold-soft, rgba(230, 185, 74, 0.15));
  color: var(--gold-bright, var(--gold));
  white-space: nowrap;
  pointer-events: none;
}
</style>

<!-- Unscoped: popover content is portaled out of this subtree. -->
<style>
.st-pop {
  min-width: 180px;
  padding: 6px;
}
.st-head {
  font-size: 9px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--fg-3);
  padding: 6px 10px 8px;
}
.st-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  padding: 8px 10px;
  font-size: 12px;
  border-radius: var(--r-xs);
  cursor: pointer;
}
.st-item.on { color: var(--gold-bright, var(--gold)); }
.st-check { color: var(--gold-bright, var(--gold)); }
.st-off { color: var(--fg-2); }
.st-divider { height: 1px; background: var(--border); margin: 4px 6px; }
</style>
