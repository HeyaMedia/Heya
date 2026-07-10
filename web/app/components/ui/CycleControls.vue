<template>
  <div class="cyc-ctls" :class="{ inline }">
    <button class="cyc-ctl" :aria-label="`Previous ${itemLabel}`" @click="$emit('prev')">
      <Icon name="chevleft" :size="12" />
    </button>
    <button
      class="cyc-ctl cyc-pause"
      :aria-pressed="paused"
      :aria-label="paused ? 'Resume rotation' : 'Pause rotation'"
      @click="paused = !paused"
    >
      <!-- The ring IS the rotation clock: a duration-long stroke animation
           whose end advances the carousel — indicator and timer are the same
           thing, so they can't drift. Re-keyed by the owner on every
           advance/retreat/jump, which restarts the window. Reduced motion:
           no ring, no auto-advance; prev/next still work. -->
      <svg class="cyc-ring" viewBox="0 0 26 26" aria-hidden="true">
        <circle
          v-if="!reducedMotion"
          :key="cycleKey"
          class="cyc-ring-fill"
          :class="{ paused: paused || ringPaused }"
          :style="{ animationDuration: `${duration}ms` }"
          cx="13" cy="13" r="11.5"
          @animationend="$emit('next')"
        />
      </svg>
      <Icon :name="paused ? 'play' : 'pause'" :size="12" />
    </button>
    <button class="cyc-ctl" :aria-label="`Next ${itemLabel}`" @click="$emit('next')">
      <Icon name="chevright" :size="12" />
    </button>
  </div>
</template>

<script setup lang="ts">
// The standard prev / pause / next cluster for everything that auto-rotates
// artwork: the home heroes (Featured, New, Music) and the detail-page
// backdrop carousels. The pause button doubles as the cycle-progress ring.
//
// Owner contract: keep a `cycleKey` counter and bump it on EVERY slide
// change (auto or manual) — that re-keys the ring and starts a fresh
// window. Handle @next/@prev by moving the carousel. `paused` is the
// sticky user pause (v-model); `ringPaused` composes transient pause
// sources on top (hover, focus, an owning trailer) without overwriting
// the user's wish.
withDefaults(defineProps<{
  /** Bumped by the owner on every slide change — re-keys the ring. */
  cycleKey: number
  /** Full rotation window in ms (= the ring's animation duration). */
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

defineEmits<{ prev: []; next: [] }>()

const reducedMotion = import.meta.client
  ? window.matchMedia('(prefers-reduced-motion: reduce)').matches
  : false
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
.cyc-pause { position: relative; }
/* Cycle-progress ring: full = handoff. Drawn just inside the button edge,
   rotated so it fills from 12 o'clock. */
.cyc-ring {
  position: absolute;
  inset: -1px;
  transform: rotate(-90deg);
  pointer-events: none;
}
.cyc-ring-fill {
  fill: none;
  stroke: var(--gold);
  stroke-width: 2;
  stroke-linecap: round;
  stroke-dasharray: 72.3; /* 2π · r(11.5) */
  stroke-dashoffset: 72.3;
  animation: cyc-ring-fill linear forwards; /* duration bound inline */
}
.cyc-ring-fill.paused { animation-play-state: paused; }
@keyframes cyc-ring-fill { to { stroke-dashoffset: 0; } }
</style>
