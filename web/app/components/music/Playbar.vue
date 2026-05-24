<template>
  <footer class="playbar" v-if="currentTrack">
    <!-- Left: now playing -->
    <div class="pb-left">
      <button class="pb-cover-btn" @click="nowPlayingOpen = true" title="Expand">
        <Poster :idx="currentTrack.id" :src="currentTrack.poster" aspect="1/1" style="width: 56px; height: 56px; border-radius: 6px; flex-shrink: 0" />
      </button>
      <div class="pb-info" @click="nowPlayingOpen = true" style="cursor: pointer">
        <div class="pb-title">{{ currentTrack.title }}</div>
        <div class="pb-artist">{{ currentTrack.artist }} — {{ currentTrack.album }}</div>
      </div>
      <button
        class="btn-icon"
        :class="{ active: loved.isLoved(currentTrack.id) }"
        @click="loved.toggle(currentTrack.id)"
        :title="loved.isLoved(currentTrack.id) ? 'Remove from Loved' : 'Add to Loved'"
      >
        <Icon :name="loved.isLoved(currentTrack.id) ? 'heartfill' : 'heart'" :size="16" />
      </button>
      <button class="btn-icon"><Icon name="plus" :size="16" /></button>
    </div>

    <!-- Center: controls + scrubber -->
    <div class="pb-center">
      <div class="pb-controls">
        <button class="btn-icon" :class="{ active: shuffled }" @click="toggleShuffle">
          <Icon name="shuffle" :size="16" />
        </button>
        <button class="btn-icon" @click="prevTrack"><Icon name="prev" :size="16" /></button>
        <button class="pb-play" @click="togglePlay">
          <Icon :name="playing ? 'pause' : 'play'" :size="20" />
        </button>
        <button class="btn-icon" @click="nextTrack"><Icon name="next" :size="16" /></button>
        <button class="btn-icon" :class="{ active: repeatMode !== 'off' }" @click="cycleRepeat" style="position: relative">
          <Icon name="repeat" :size="16" />
          <span v-if="repeatMode === 'one'" class="repeat-badge">1</span>
        </button>
      </div>
      <div class="pb-scrubber">
        <span class="pb-time">{{ formatTime(position) }}</span>
        <MusicWaveform
          :peaks="waveform"
          :progress="scrubPct / 100"
          @seek="onWaveformSeek"
        />
        <span class="pb-time">{{ formatTime(duration) }}</span>
      </div>
    </div>

    <!-- Right: queue, vol, etc -->
    <div class="pb-right">
      <button class="btn-icon" :class="{ active: lyricsOpen }" @click="toggleLyrics" title="Lyrics"><Icon name="lyrics" :size="16" /></button>
      <button class="btn-icon" :class="{ active: queueOpen }" @click="toggleQueue" title="Queue"><Icon name="queue" :size="16" /></button>
      <button class="btn-icon" :class="{ active: eqOpen }" @click="eqOpen = !eqOpen" title="Equalizer"><Icon name="eq" :size="16" /></button>
      <div class="pb-volume">
        <button class="btn-icon" @click="toggleMute">
          <Icon :name="muted || volume === 0 ? 'volmute' : 'vol'" :size="16" />
        </button>
        <div class="rail" style="width: 80px" @click="onVolume">
          <div class="fill" :style="{ width: (muted ? 0 : volume) + '%' }" />
          <div class="knob" :style="{ left: (muted ? 0 : volume) + '%' }" />
        </div>
      </div>
      <button class="btn-icon" @click="nowPlayingOpen = !nowPlayingOpen" title="Expand">
        <Icon name="expand" :size="16" />
      </button>
    </div>
  </footer>
  <NowPlayingView :open="nowPlayingOpen" @close="nowPlayingOpen = false" />
</template>

<script setup lang="ts">
const {
  playing, currentTrack, position, duration, volume, muted,
  shuffled, repeatMode, queueOpen, lyricsOpen,
  togglePlay, seek, setVolume, toggleMute, toggleShuffle,
  cycleRepeat, nextTrack, prevTrack,
  toggleQueue, toggleLyrics, formatTime,
} = usePlayer()

const loved = useLovedTracks()
if (import.meta.client) loved.ensureLoaded()

// Now-playing overlay state. Kept locally — the playbar is the single
// mount point for the overlay so we don't need a global state slot.
const nowPlayingOpen = useState('now_playing_open', () => false)
// EQ panel state mirrors the one in the music shell so the playbar can
// toggle it without prop drilling.
const eqOpen = useState('music_eq_open', () => false)

const scrubPct = computed(() => duration.value > 0 ? (position.value / duration.value) * 100 : 0)

// Reactive waveform fetch keyed on the current track. Resolves to
// null for tracks that haven't been analyzed yet — MusicWaveform
// falls back to a plain neutral bar in that case.
const trackId = computed<number | null>(() => currentTrack.value?.id ?? null)
const { waveform } = useTrackFacets(trackId)

function onWaveformSeek(pct: number) {
  seek(pct)
}

function onVolume(e: MouseEvent) {
  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
  setVolume(Math.round(((e.clientX - rect.left) / rect.width) * 100))
}
</script>

<style scoped>
.playbar {
  display: grid;
  grid-template-columns: 1fr 1.6fr 1fr;
  align-items: center;
  gap: 16px;
  padding: 0 16px;
  height: var(--playbar-h);
  background: var(--bg-0);
  border-top: 1px solid var(--border);
  z-index: 40;
}
.pb-left { display: flex; align-items: center; gap: 12px; }
.pb-cover-btn { background: transparent; border: 0; padding: 0; cursor: pointer; }
.pb-info { flex: 1; min-width: 0; margin-right: 8px; }
.pb-title { font-size: 13px; font-weight: 500; color: var(--fg-0); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.pb-artist { font-size: 11px; color: var(--fg-2); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.pb-center { display: flex; flex-direction: column; align-items: center; gap: 6px; }
.pb-controls { display: flex; align-items: center; gap: 8px; }
.pb-play {
  width: 36px; height: 36px;
  border-radius: 50%;
  background: var(--fg-0);
  color: var(--bg-0);
  display: flex; align-items: center; justify-content: center;
  transition: transform 0.15s ease, background 0.15s ease;
}
.pb-play:hover { transform: scale(1.06); background: var(--gold); }
.pb-scrubber { display: flex; align-items: center; gap: 10px; width: 100%; }
.pb-time { font-size: 10px; font-family: var(--font-mono); color: var(--fg-3); min-width: 32px; text-align: center; }
.pb-right { display: flex; align-items: center; gap: 4px; justify-content: flex-end; }
.pb-volume { display: flex; align-items: center; gap: 4px; }
.repeat-badge {
  position: absolute;
  bottom: 2px; right: 2px;
  font-size: 8px; font-weight: 700;
  color: var(--gold);
  font-family: var(--font-mono);
}
</style>
