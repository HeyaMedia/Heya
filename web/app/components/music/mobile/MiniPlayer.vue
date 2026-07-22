<!--
  MiniPlayer — compact persistent bar for phones. Position-agnostic: this
  component renders a plain 64px-tall row: the parent (W1c) is responsible
  for docking it (fixed, above BottomNav, safe-area padding etc). Reads
  everything from the global usePlayerBindings() singleton — no props.

  Emits:
    expand — whole-bar tap (except the two transport buttons)
-->
<template>
  <div v-if="currentTrack" class="mini-player" @click="onBarTap">
    <MusicUltrablur :target="ultrablur" variant="bar" class="mp-ultrablur" />

    <MusicWaveform
      :peaks="waveform"
      :progress="waveProgress"
      class="mp-waveform"
      role="presentation"
      tabindex="-1"
      aria-hidden="true"
    />

    <Poster :idx="currentTrack.id" :src="currentTrack.poster ?? null" aspect="1/1" :width="88" class="mp-art" />

    <div class="mp-info">
      <div class="mp-title">{{ currentTrack.title }}</div>
      <div class="mp-meta">
        <div class="mp-artist">{{ currentTrack.artist }}</div>
        <div class="mp-time">{{ formatTime(position) }} / {{ formatTime(duration) }}</div>
      </div>
    </div>

    <DJMenu variant="mini" />

    <div class="mp-controls">
      <button
        type="button"
        class="mp-btn"
        :aria-label="playing ? 'Pause' : 'Play'"
        @pointerdown.stop="onPlayPointerDown"
        @pointerup.stop="clearPlayHold"
        @pointercancel.stop="clearPlayHold"
        @pointerleave.stop="clearPlayHold"
        @click.stop="onPlayClick"
      >
        <Icon :name="playing ? 'pause' : 'play'" :size="20" />
      </button>
      <button type="button" class="mp-btn" aria-label="Next" @click.stop="nextTrack">
        <Icon name="next" :size="18" />
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { MusicUltrablurTarget } from '~/composables/useMusicUltrablur'

defineProps<{ ultrablur: MusicUltrablurTarget | null }>()

const { currentTrack, playing, position, duration, togglePlay, nextTrack, stop, formatTime } = usePlayerBindings()

const emit = defineEmits<{ expand: [] }>()

const trackId = computed<number | null>(() => currentTrack.value?.id ?? null)
const { waveform } = useTrackFacets(trackId)
const waveProgress = ref(0)

// The background waveform is deliberately low-frequency decoration. Sample
// playback every ten seconds instead of feeding the canvas every position tick;
// track/duration changes still reset it immediately for the new song.
function sampleWaveProgress() {
  waveProgress.value = duration.value > 0
    ? Math.max(0, Math.min(1, position.value / duration.value))
    : 0
}
watch([trackId, duration], sampleWaveProgress, { immediate: true })

let waveformTimer: ReturnType<typeof setInterval> | null = null
onMounted(() => {
  waveformTimer = setInterval(sampleWaveProgress, 10_000)
})

function onBarTap() {
  emit('expand')
}

// Long-press play/pause = full stop (mirrors Playbar's 3s hold-to-stop and
// NowPlayingSheet's matching gesture on its own big play button, just
// shorter here since the mini bar has no room for arm/ring staging).
// `holdFired` suppresses the trailing click so release doesn't also toggle
// play/pause.
const HOLD_MS = 650
let holdTimer: ReturnType<typeof setTimeout> | null = null
let holdFired = false

function clearPlayHold() {
  if (holdTimer) { clearTimeout(holdTimer); holdTimer = null }
}
function onPlayPointerDown(e: PointerEvent) {
  if (e.button !== 0) return // primary button / touch only
  // Touch pointers get implicit pointer capture on pointerdown, which
  // suppresses pointerleave — release it so sliding off the button still
  // cancels the hold.
  ;(e.currentTarget as Element).releasePointerCapture?.(e.pointerId)
  holdFired = false
  clearPlayHold()
  holdTimer = setTimeout(() => {
    holdFired = true
    holdTimer = null
    navigator.vibrate?.(35)
    nextTick(() => stop())
  }, HOLD_MS)
}
function onPlayClick() {
  if (holdFired) { holdFired = false; return } // long-press already handled it
  togglePlay()
}
onScopeDispose(() => {
  clearPlayHold()
  if (waveformTimer) clearInterval(waveformTimer)
})
</script>

<style scoped>
.mini-player {
  position: relative;
  display: flex;
  align-items: center;
  gap: 10px;
  height: 64px;
  padding: 0 10px;
  background: var(--bg-2);
  border-top: 1px solid var(--border);
  cursor: pointer;
  overflow: hidden;
  isolation: isolate;
  -webkit-tap-highlight-color: transparent;
}

/* A dim, non-interactive full-bar waveform sits behind every foreground
   element. Its progress is sampled in script every ten seconds. */
.mini-player :deep(.mp-waveform) {
  position: absolute;
  inset: 6px 0;
  z-index: 1;
  flex: none;
  width: 100%;
  min-width: 0;
  height: auto;
  min-height: 0;
  max-height: none;
  opacity: 0.14;
  pointer-events: none;
  -webkit-mask-image: linear-gradient(to right, transparent, #000 8%, #000 92%, transparent);
  mask-image: linear-gradient(to right, transparent, #000 8%, #000 92%, transparent);
}

.mp-art,
.mp-info,
.mini-player :deep(.dj-trigger),
.mp-controls {
  position: relative;
  z-index: 2;
}
.mini-player :deep(.dj-trigger) { flex-shrink: 0; }

.mp-ultrablur { z-index: 0; }

.mp-art {
  width: 44px;
  height: 44px;
  border-radius: 6px;
  flex-shrink: 0;
}

.mp-info {
  display: flex;
  flex: 1;
  min-width: 0;
  height: 44px;
  flex-direction: column;
  justify-content: center;
  overflow: hidden;
}
.mp-title {
  font-size: 13px;
  font-weight: 500;
  color: var(--fg-0);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mp-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.mp-artist {
  flex: 1;
  min-width: 0;
  font-size: 11px;
  color: var(--fg-2);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mp-time {
  flex-shrink: 0;
  font: 500 9px var(--font-mono);
  color: var(--fg-2);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
.mp-controls {
  display: flex;
  align-items: center;
  flex-shrink: 0;
}
.mp-btn {
  width: 44px;
  height: 44px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: transparent;
  border: 0;
  color: var(--fg-0);
  cursor: pointer;
  user-select: none;
  -webkit-touch-callout: none;
}
.mp-btn:active { color: var(--gold); }
</style>
