<!--
  MiniPlayer — compact persistent bar for phones. Position-agnostic: this
  component renders a plain 64px-tall row: the parent (W1c) is responsible
  for docking it (fixed, above BottomNav, safe-area padding etc). Reads
  everything from the global usePlayer() singleton — no props.

  Emits:
    expand — whole-bar tap (except the two transport buttons)
-->
<template>
  <div v-if="currentTrack" class="mini-player" @click="onBarTap">
    <div class="mp-progress" :style="{ width: progressPct + '%' }" />

    <Poster :idx="currentTrack.id" :src="currentTrack.poster ?? null" aspect="1/1" :width="88" class="mp-art" />

    <div class="mp-info">
      <div class="mp-title">{{ currentTrack.title }}</div>
      <div class="mp-artist">{{ currentTrack.artist }}</div>
    </div>

    <div class="mp-controls">
      <button
        type="button"
        class="mp-btn"
        :aria-label="playing ? 'Pause' : 'Play'"
        @click.stop="togglePlay"
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
const { currentTrack, playing, position, duration, togglePlay, nextTrack } = usePlayer()

const emit = defineEmits<{ expand: [] }>()

const progressPct = computed(() =>
  duration.value > 0 ? Math.max(0, Math.min(100, (position.value / duration.value) * 100)) : 0)

function onBarTap() {
  emit('expand')
}
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
  -webkit-tap-highlight-color: transparent;
}

/* Progress line pinned to the top edge of the bar. */
.mp-progress {
  position: absolute;
  top: 0;
  left: 0;
  height: 2px;
  background: var(--gold);
  transition: width 0.2s linear;
}

.mp-art {
  width: 44px;
  height: 44px;
  border-radius: 6px;
  flex-shrink: 0;
}

.mp-info { flex: 1; min-width: 0; }
.mp-title {
  font-size: 13px;
  font-weight: 500;
  color: var(--fg-0);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mp-artist {
  font-size: 11px;
  color: var(--fg-3);
  overflow: hidden;
  text-overflow: ellipsis;
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
}
.mp-btn:active { color: var(--gold); }
</style>
