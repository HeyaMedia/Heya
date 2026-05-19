<template>
  <aside class="queue-panel scroll" v-if="queueOpen">
    <div class="qp-tabs">
      <button class="qp-tab" :class="{ active: tab === 'queue' }" @click="tab = 'queue'">Queue</button>
      <button class="qp-tab" :class="{ active: tab === 'lyrics' }" @click="tab = 'lyrics'">Lyrics</button>
    </div>

    <!-- Queue list -->
    <div v-if="tab === 'queue'" class="qp-body">
      <div class="qp-section-label">Now Playing</div>
      <div v-if="currentTrack" class="qp-row current">
        <VuMeter :playing="playing" />
        <div class="qp-row-info">
          <div class="qp-row-title">{{ currentTrack.title }}</div>
          <div class="qp-row-artist">{{ currentTrack.artist }}</div>
        </div>
        <span class="qp-row-dur">{{ formatTime(currentTrack.duration) }}</span>
      </div>

      <div class="qp-section-label" style="margin-top: 16px">Up Next</div>
      <div
        v-for="track in upNext"
        :key="track.id"
        class="qp-row"
        @click="play(track)"
      >
        <Poster :idx="track.id" :src="track.poster" aspect="1/1" style="width: 40px; height: 40px; border-radius: 4px; flex-shrink: 0" />
        <div class="qp-row-info">
          <div class="qp-row-title">{{ track.title }}</div>
          <div class="qp-row-artist">{{ track.artist }}</div>
        </div>
        <span class="qp-row-dur">{{ formatTime(track.duration) }}</span>
      </div>

      <div v-if="!queue.length && !currentTrack" class="qp-empty">
        <p>Queue is empty</p>
        <p style="font-size: 11px; color: var(--fg-3)">Play something to get started</p>
      </div>
    </div>

    <!-- Lyrics -->
    <div v-if="tab === 'lyrics'" class="qp-body qp-lyrics">
      <div
        v-for="(line, i) in sampleLyrics"
        :key="i"
        class="lyric-line"
        :class="{ active: i === activeLyricIdx, past: i < activeLyricIdx }"
      >
        {{ line }}
      </div>
      <div v-if="!sampleLyrics.length" class="qp-empty">
        <p>No lyrics available</p>
      </div>
    </div>
  </aside>
</template>

<script setup lang="ts">
const { playing, currentTrack, queue, queueOpen, position, duration, formatTime, play } = usePlayer()

const tab = ref<'queue' | 'lyrics'>('queue')

const upNext = computed(() => {
  if (!currentTrack.value) return queue.value
  const idx = queue.value.findIndex(t => t.id === currentTrack.value?.id)
  return idx >= 0 ? queue.value.slice(idx + 1) : queue.value
})

const sampleLyrics = [
  'Under the neon lights we dance',
  'Through the city of a thousand nights',
  'Every shadow tells a story',
  'Every silence hides a song',
  '',
  'We are the dreamers of the dawn',
  'Chasing echoes through the storm',
  'Hold my hand and don\'t let go',
  'We\'ll find our way back home',
  '',
  'The stars align for those who wait',
  'And time will heal what words cannot',
  'So sing with me one final time',
  'Before the morning steals the light',
]

const activeLyricIdx = computed(() => {
  if (!playing.value || duration.value === 0) return -1
  const pct = position.value / duration.value
  return Math.floor(pct * sampleLyrics.length)
})
</script>

<style scoped>
.queue-panel {
  width: var(--music-queue-w);
  flex-shrink: 0;
  background: var(--bg-2);
  border-left: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  height: 100%;
}
.qp-tabs {
  display: flex;
  gap: 0;
  padding: 12px 16px 0;
  border-bottom: 1px solid var(--border);
}
.qp-tab {
  flex: 1;
  padding: 10px 0;
  font-size: 11px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--fg-3);
  border-bottom: 2px solid transparent;
  text-align: center;
  transition: color 0.15s, border-color 0.15s;
}
.qp-tab:hover { color: var(--fg-1); }
.qp-tab.active { color: var(--gold); border-bottom-color: var(--gold); }
.qp-body { padding: 16px; flex: 1; overflow-y: auto; }
.qp-section-label {
  font-size: 10px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--fg-3);
  margin-bottom: 8px;
}
.qp-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px;
  border-radius: var(--r-sm);
  cursor: pointer;
  transition: background 0.12s;
}
.qp-row:hover { background: rgba(255,255,255,0.04); }
.qp-row.current { background: var(--gold-soft); }
.qp-row-info { flex: 1; min-width: 0; }
.qp-row-title { font-size: 13px; font-weight: 500; color: var(--fg-0); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.qp-row-artist { font-size: 11px; color: var(--fg-2); }
.qp-row-dur { font-size: 11px; font-family: var(--font-mono); color: var(--fg-3); }
.qp-empty { text-align: center; padding: 40px 0; color: var(--fg-2); font-size: 13px; }
.qp-lyrics { padding: 24px 20px; }
.lyric-line {
  font-size: 18px;
  font-weight: 500;
  line-height: 2.2;
  color: var(--fg-3);
  transition: color 0.3s ease, transform 0.3s ease;
}
.lyric-line.active { color: var(--gold); transform: scale(1.02); }
.lyric-line.past { color: var(--fg-2); }
</style>
