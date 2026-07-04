<!--
  QueueSheet — full-screen queue view for phones, built on AppSheet
  (size="full", title="Queue"). Reads Played / Now Playing / Up Next
  straight from the global usePlayer() singleton.

  Index math mirrors QueuePanel.vue exactly: playedTracks rows are already
  absolute queue indices (0..currentIndex-1), so `jumpTo(i)` needs no offset.
  upcomingTracks rows are relative to currentIndex, so every call into
  jumpTo/moveInQueue/removeFromQueue re-derives the absolute index as
  `currentIndex + 1 + i`.

  Props/model:
    v-model:open — boolean, sheet visibility

  No props beyond the model; no emits.
-->
<template>
  <AppSheet v-model:open="open" size="full" title="Queue">
    <div class="qs-body">
      <div class="qs-toolbar">
        <button type="button" class="qs-chip" :class="{ active: shuffled }" aria-label="Shuffle" @click="toggleShuffle">
          <Icon name="shuffle" :size="15" />
          <span>Shuffle</span>
        </button>
        <button type="button" class="qs-chip" :class="{ active: repeatMode !== 'off' }" aria-label="Repeat" @click="cycleRepeat">
          <Icon name="repeat" :size="15" />
          <span>{{ repeatMode === 'one' ? 'Repeat one' : 'Repeat' }}</span>
        </button>
        <button type="button" class="qs-clear" @click="clearUpcoming">Clear</button>
      </div>

      <template v-if="playedTracks.length">
        <div class="qs-section-label">Played</div>
        <button
          v-for="(t, i) in playedTracks"
          :key="`played-${t.id}-${i}`"
          type="button"
          class="qs-row qs-row-played"
          @click="jumpTo(i)"
        >
          <Poster :idx="t.id" :src="t.poster ?? null" aspect="1/1" :width="88" class="qs-thumb" />
          <div class="qs-row-info">
            <div class="qs-row-title">{{ t.title }}</div>
            <div class="qs-row-artist">{{ t.artist }}</div>
          </div>
          <span class="qs-row-dur">{{ formatTime(t.duration) }}</span>
        </button>
      </template>

      <template v-if="currentTrack">
        <div class="qs-section-label">Now Playing</div>
        <div ref="currentEl" class="qs-row qs-row-current">
          <Poster :idx="currentTrack.id" :src="currentTrack.poster ?? null" aspect="1/1" :width="88" class="qs-thumb" />
          <div class="qs-row-info">
            <div class="qs-row-title">{{ currentTrack.title }}</div>
            <div class="qs-row-artist">{{ currentTrack.artist }}</div>
          </div>
          <span v-if="currentTrack.isStream" class="qs-live-badge"><span class="qs-live-dot" />LIVE</span>
          <span v-else class="qs-row-dur">{{ formatTime(currentTrack.duration) }}</span>
        </div>
      </template>

      <template v-if="upcomingTracks.length">
        <div class="qs-section-label">Up Next · {{ upcomingTracks.length }}</div>
        <div
          v-for="(t, i) in upcomingTracks"
          :key="`upcoming-${t.id}-${i}`"
          class="qs-row qs-row-upcoming"
        >
          <button type="button" class="qs-row-tap" @click="jumpTo(currentIndex + 1 + i)">
            <Poster :idx="t.id" :src="t.poster ?? null" aspect="1/1" :width="88" class="qs-thumb" />
            <div class="qs-row-info">
              <div class="qs-row-title">{{ t.title }}</div>
              <div class="qs-row-artist">{{ t.artist }}</div>
            </div>
            <span class="qs-row-dur">{{ formatTime(t.duration) }}</span>
          </button>
          <div class="qs-row-actions">
            <button
              type="button"
              class="qs-icon-btn"
              :disabled="i === 0"
              aria-label="Move up"
              @click="moveUp(i)"
            >
              <Icon name="chevdown" :size="14" style="transform: rotate(180deg)" />
            </button>
            <button
              type="button"
              class="qs-icon-btn"
              :disabled="i === upcomingTracks.length - 1"
              aria-label="Move down"
              @click="moveDown(i)"
            >
              <Icon name="chevdown" :size="14" />
            </button>
            <button
              type="button"
              class="qs-icon-btn qs-remove"
              aria-label="Remove from queue"
              @click="removeFromQueue(currentIndex + 1 + i)"
            >
              <Icon name="close" :size="14" />
            </button>
          </div>
        </div>
      </template>

      <div v-if="!queue.length && !currentTrack" class="qs-empty">
        <Icon name="music" :size="32" style="opacity: 0.4; margin-bottom: 8px" />
        <p>Queue is empty</p>
      </div>
    </div>
  </AppSheet>
</template>

<script setup lang="ts">
const open = defineModel<boolean>('open', { default: false })

const {
  queue, currentTrack, currentIndex, playedTracks, upcomingTracks,
  shuffled, repeatMode, formatTime,
  jumpTo, moveInQueue, removeFromQueue, clearUpcoming, toggleShuffle, cycleRepeat,
} = usePlayer()

function moveUp(i: number) {
  if (i <= 0) return
  const from = currentIndex.value + 1 + i
  moveInQueue(from, from - 1)
}
function moveDown(i: number) {
  if (i >= upcomingTracks.value.length - 1) return
  const from = currentIndex.value + 1 + i
  moveInQueue(from, from + 1)
}

// Auto-scroll so the current track is visible whenever the sheet opens.
const currentEl = ref<HTMLElement | null>(null)
watch(open, async (v) => {
  if (!v) return
  await nextTick()
  currentEl.value?.scrollIntoView({ block: 'center' })
})
</script>

<!--
  AppSheet portals its content to <body>, so styling for anything rendered
  inside it must be unscoped (docs/ui.md gotcha #2).
-->
<style>
.qs-body {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.qs-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding-bottom: 10px;
  margin-bottom: 6px;
  border-bottom: 1px solid var(--border);
}
.qs-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 36px;
  padding: 0 12px;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid var(--border);
  color: var(--fg-2);
  font-size: 12px;
  cursor: pointer;
}
.qs-chip.active { color: var(--gold); border-color: rgba(230, 185, 74, 0.4); background: var(--gold-soft); }
.qs-clear {
  margin-left: auto;
  height: 36px;
  padding: 0 10px;
  background: transparent;
  border: 0;
  color: var(--fg-3);
  font-size: 13px;
  cursor: pointer;
}
.qs-clear:active { color: var(--gold); }

.qs-section-label {
  font-size: 10px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--fg-3);
  padding: 14px 4px 6px;
}

.qs-row {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  padding: 8px 4px;
  background: transparent;
  border: 0;
  border-left: 2px solid transparent;
  text-align: left;
  cursor: pointer;
}
.qs-row-played { opacity: 0.5; }
.qs-row-current {
  background: var(--gold-soft);
  border-left-color: var(--gold);
  border-radius: var(--r-sm);
  cursor: default;
}
.qs-row-upcoming { padding: 4px 0; gap: 6px; }

.qs-row-tap {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 12px;
  background: transparent;
  border: 0;
  padding: 4px;
  text-align: left;
  cursor: pointer;
}

.qs-thumb {
  width: 44px;
  height: 44px;
  border-radius: 6px;
  flex-shrink: 0;
}
.qs-row-info { flex: 1; min-width: 0; }
.qs-row-title {
  font-size: 14px;
  font-weight: 500;
  color: var(--fg-0);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.qs-row-current .qs-row-title { color: var(--gold); }
.qs-row-artist {
  font-size: 12px;
  color: var(--fg-2);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.qs-row-dur {
  font-size: 11px;
  font-family: var(--font-mono);
  color: var(--fg-3);
  flex-shrink: 0;
}

.qs-row-actions {
  display: flex;
  align-items: center;
  gap: 2px;
  flex-shrink: 0;
}
.qs-icon-btn {
  width: 44px;
  height: 44px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: transparent;
  border: 0;
  color: var(--fg-2);
  cursor: pointer;
}
.qs-icon-btn:disabled { opacity: 0.25; cursor: default; }
.qs-icon-btn:active:not(:disabled) { color: var(--gold); }
.qs-remove:active { color: var(--bad); }

.qs-live-badge {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 9px;
  font-weight: 700;
  letter-spacing: 0.06em;
  color: #f87171;
  background: rgba(239, 68, 68, 0.15);
  padding: 2px 6px;
  border-radius: 999px;
  font-family: var(--font-mono);
  flex-shrink: 0;
}
.qs-live-dot {
  width: 5px;
  height: 5px;
  background: #f87171;
  border-radius: 50%;
}

.qs-empty { text-align: center; padding: 40px 16px; color: var(--fg-2); font-size: 13px; }
</style>
