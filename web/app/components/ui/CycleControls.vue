<template>
  <div class="cyc-ctls" :class="{ inline: props.inline }">
    <button class="cyc-ctl" :aria-label="`Previous ${props.itemLabel}`" @click="$emit('prev')">
      <Icon name="chevleft" :size="12" />
    </button>
    <button
      class="cyc-ctl"
      :aria-pressed="paused"
      :aria-label="paused ? 'Resume rotation' : 'Pause rotation'"
      @click="paused = !paused"
    >
      <Icon :name="paused ? 'play' : 'pause'" :size="12" />
    </button>
    <button class="cyc-ctl" :aria-label="`Next ${props.itemLabel}`" @click="$emit('next')">
      <Icon name="chevright" :size="12" />
    </button>
  </div>
</template>

<script setup lang="ts">
// The standard prev / pause / next cluster for everything that auto-rotates
// artwork: the home heroes (Featured, New, Music) and the detail-page
// backdrop carousels. A timeout owns the clock so an idle page produces no
// animation frames; the pause/play button is deliberately unanimated.
//
// Owner contract: keep a `cycleKey` counter and bump it on EVERY slide
// change (auto or manual) — that restarts a fresh timer window. Handle
// @next/@prev by moving the carousel. `paused` is the sticky user pause
// (v-model); `ringPaused` composes transient pause
// sources on top (hover, focus, an owning trailer) without overwriting
// the user's wish.
const props = withDefaults(defineProps<{
  /** Bumped by the owner on every slide change — restarts the clock. */
  cycleKey: number
  /** Full rotation window in ms. */
  duration: number
  /** Transient pause sources composed by the owner (hover/focus/trailer). */
  ringPaused?: boolean
  /** Accessible noun: "Previous ${itemLabel}". */
  itemLabel?: string
  /** Inline (non-teleported) placement — phone layouts. */
  inline?: boolean
}>(), {
  ringPaused: false,
  itemLabel: 'slide',
  inline: false,
})

/** Sticky user pause — only the button toggles it. */
const paused = defineModel<boolean>('paused', { default: false })

const emit = defineEmits<{ prev: []; next: [] }>()

const reducedMotion = ref(false)
const pageHidden = ref(false)
let timer: ReturnType<typeof setTimeout> | null = null
let remainingMs = 0
let startedAt = 0
let clockMounted = false

function clockBlocked() {
  return reducedMotion.value || paused.value || props.ringPaused || pageHidden.value
}

function stopClock(preserveRemaining: boolean) {
  if (timer === null) return
  if (preserveRemaining) {
    remainingMs = Math.max(1, remainingMs - (performance.now() - startedAt))
  }
  clearTimeout(timer)
  timer = null
}

function armClock() {
  if (!clockMounted || clockBlocked() || timer !== null) return
  if (remainingMs <= 0) remainingMs = Math.max(0, props.duration)
  if (remainingMs <= 0) return
  startedAt = performance.now()
  timer = setTimeout(() => {
    timer = null
    remainingMs = Math.max(0, props.duration)
    emit('next')
  }, remainingMs)
}

function restartClock() {
  stopClock(false)
  remainingMs = Math.max(0, props.duration)
  armClock()
}

watch(() => [props.cycleKey, props.duration], restartClock)
watch([paused, () => props.ringPaused], () => {
  if (!clockMounted) return
  if (clockBlocked()) stopClock(true)
  else armClock()
})

function onVisibilityChange() {
  pageHidden.value = document.hidden
  if (pageHidden.value) stopClock(true)
  else armClock()
}

onMounted(() => {
  clockMounted = true
  reducedMotion.value = window.matchMedia('(prefers-reduced-motion: reduce)').matches
  pageHidden.value = document.hidden
  remainingMs = Math.max(0, props.duration)
  document.addEventListener('visibilitychange', onVisibilityChange)
  armClock()
})

onBeforeUnmount(() => {
  clockMounted = false
  stopClock(false)
  document.removeEventListener('visibilitychange', onVisibilityChange)
})
</script>

<style scoped>
.cyc-ctls { display: flex; align-items: center; gap: 6px; }
.cyc-ctls.inline { margin-left: auto; align-self: flex-start; }
.cyc-ctl {
  width: 26px;
  height: 26px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-1);
  background: color-mix(in oklab, var(--bg-2) 78%, transparent);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  border: 1px solid var(--border);
  box-shadow: var(--shadow-el);
  transition: background 0.12s, color 0.12s;
}
.cyc-ctl:hover { background: var(--bg-3); color: var(--fg-0); }
</style>
