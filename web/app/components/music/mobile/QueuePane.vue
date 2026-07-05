<!--
  QueuePane — queue content for the merged phone now-playing sheet. Used to
  be a standalone QueueSheet (its own AppSheet); now it's plain content
  mounted as the second scroll-snap section inside NowPlayingSheet's
  `.nps-scroll` (see that file). No AppSheet wrapper, no `open` model — the
  parent owns visibility/scroll position entirely.

  Index math mirrors QueuePanel.vue exactly: playedTracks rows are already
  absolute queue indices (0..currentIndex-1), so `jumpTo(i)` needs no offset.
  upcomingTracks rows are relative to currentIndex, so every call into
  jumpTo/moveInQueue/removeFromQueue re-derives the absolute index as
  `currentIndex + 1 + i`.

  No props, no emits — reads/mutates the global usePlayer() singleton.
-->
<template>
  <div class="qp-root">
    <div class="qp-sticky-header">
      <div class="qp-title">Queue</div>
      <div class="qp-toolbar">
        <button type="button" class="qp-chip" :class="{ active: shuffled }" aria-label="Shuffle" @click="toggleShuffle">
          <Icon name="shuffle" :size="15" />
          <span>Shuffle</span>
        </button>
        <button type="button" class="qp-chip" :class="{ active: repeatMode !== 'off' }" aria-label="Repeat" @click="cycleRepeat">
          <Icon name="repeat" :size="15" />
          <span>{{ repeatMode === 'one' ? 'Repeat one' : 'Repeat' }}</span>
        </button>
        <button type="button" class="qp-clear" @click="clearUpcoming">Clear</button>
      </div>
    </div>

    <div class="qp-list">
      <template v-if="playedTracks.length">
        <div class="qp-section-label">Played</div>
        <button
          v-for="(t, i) in playedTracks"
          :key="`played-${t.id}-${i}`"
          type="button"
          class="qp-row qp-row-played"
          @click="jumpTo(i)"
        >
          <Poster :idx="t.id" :src="t.poster ?? null" aspect="1/1" :width="88" class="qp-thumb" />
          <div class="qp-row-info">
            <div class="qp-row-title">{{ t.title }}</div>
            <div class="qp-row-artist">{{ t.artist }}</div>
          </div>
          <span class="qp-row-dur">{{ formatTime(t.duration) }}</span>
        </button>
      </template>

      <template v-if="currentTrack">
        <div class="qp-section-label">Now Playing</div>
        <div class="qp-row qp-row-current">
          <Poster :idx="currentTrack.id" :src="currentTrack.poster ?? null" aspect="1/1" :width="88" class="qp-thumb" />
          <div class="qp-row-info">
            <div class="qp-row-title">{{ currentTrack.title }}</div>
            <div class="qp-row-artist">{{ currentTrack.artist }}</div>
          </div>
          <span v-if="currentTrack.isStream" class="qp-live-badge"><span class="qp-live-dot" />LIVE</span>
          <span v-else class="qp-row-dur">{{ formatTime(currentTrack.duration) }}</span>
        </div>
      </template>

      <template v-if="upcomingTracks.length">
        <div class="qp-section-label">Up Next · {{ upcomingTracks.length }}</div>
        <div
          v-for="(t, i) in upcomingTracks"
          :key="`upcoming-${t.id}-${i}`"
          class="qp-row qp-row-upcoming"
        >
          <button type="button" class="qp-row-tap" @click="jumpTo(currentIndex + 1 + i)">
            <Poster :idx="t.id" :src="t.poster ?? null" aspect="1/1" :width="88" class="qp-thumb" />
            <div class="qp-row-info">
              <div class="qp-row-title">{{ t.title }}</div>
              <div class="qp-row-artist">{{ t.artist }}</div>
            </div>
            <span class="qp-row-dur">{{ formatTime(t.duration) }}</span>
          </button>
          <div class="qp-row-actions">
            <button
              type="button"
              class="qp-icon-btn"
              :disabled="i === 0"
              aria-label="Move up"
              @click="moveUp(i)"
            >
              <Icon name="chevdown" :size="14" style="transform: rotate(180deg)" />
            </button>
            <button
              type="button"
              class="qp-icon-btn"
              :disabled="i === upcomingTracks.length - 1"
              aria-label="Move down"
              @click="moveDown(i)"
            >
              <Icon name="chevdown" :size="14" />
            </button>
            <button
              type="button"
              class="qp-icon-btn qp-remove"
              aria-label="Remove from queue"
              @click="removeFromQueue(currentIndex + 1 + i)"
            >
              <Icon name="close" :size="14" />
            </button>
          </div>
        </div>
      </template>

      <div v-if="!queue.length && !currentTrack" class="qp-empty">
        <Icon name="music" :size="32" style="opacity: 0.4; margin-bottom: 8px" />
        <p>Queue is empty</p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
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
</script>

<!--
  Mounted inside NowPlayingSheet, whose AppSheet content is portaled to
  <body> — so this must stay unscoped too (docs/ui.md gotcha #2).
-->
<style>
.qp-root {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

/* Pinned above the Played/Now Playing/Up Next rows as the pane scrolls —
   solid (not the translucent `.surface` glass) so rows fully disappear
   underneath it rather than ghosting through. A backdrop-filter here would
   also just render ~30% opaque anyway: the AppSheet ancestor already has one
   (docs/ui.md gotcha #4), so a flat opaque color is both simpler and correct. */
.qp-sticky-header {
  position: sticky;
  top: 0;
  z-index: 2;
  background: var(--bg-2);
  padding-top: 4px;
  padding-bottom: 10px;
  margin-bottom: 6px;
  border-bottom: 1px solid var(--border);
}
.qp-title {
  font-size: 12px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--fg-3);
  padding: 2px 4px 10px;
}
.qp-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
}
.qp-chip {
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
.qp-chip.active { color: var(--gold); border-color: rgba(230, 185, 74, 0.4); background: var(--gold-soft); }
.qp-clear {
  margin-left: auto;
  height: 36px;
  padding: 0 10px;
  background: transparent;
  border: 0;
  color: var(--fg-3);
  font-size: 13px;
  cursor: pointer;
}
.qp-clear:active { color: var(--gold); }

.qp-section-label {
  font-size: 10px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--fg-3);
  padding: 14px 4px 6px;
}

.qp-row {
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
.qp-row-played { opacity: 0.5; }
.qp-row-current {
  background: var(--gold-soft);
  border-left-color: var(--gold);
  border-radius: var(--r-sm);
  cursor: default;
}
.qp-row-upcoming { padding: 4px 0; gap: 6px; }

.qp-row-tap {
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

.qp-thumb {
  width: 44px;
  height: 44px;
  border-radius: 6px;
  flex-shrink: 0;
}
.qp-row-info { flex: 1; min-width: 0; }
.qp-row-title {
  font-size: 14px;
  font-weight: 500;
  color: var(--fg-0);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.qp-row-current .qp-row-title { color: var(--gold); }
.qp-row-artist {
  font-size: 12px;
  color: var(--fg-2);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.qp-row-dur {
  font-size: 11px;
  font-family: var(--font-mono);
  color: var(--fg-3);
  flex-shrink: 0;
}

.qp-row-actions {
  display: flex;
  align-items: center;
  gap: 2px;
  flex-shrink: 0;
}
.qp-icon-btn {
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
.qp-icon-btn:disabled { opacity: 0.25; cursor: default; }
.qp-icon-btn:active:not(:disabled) { color: var(--gold); }
.qp-remove:active { color: var(--bad); }

.qp-live-badge {
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
.qp-live-dot {
  width: 5px;
  height: 5px;
  background: #f87171;
  border-radius: 50%;
}

.qp-empty { text-align: center; padding: 40px 16px; color: var(--fg-2); font-size: 13px; }
</style>
