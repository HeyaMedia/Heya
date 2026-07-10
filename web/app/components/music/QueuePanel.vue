<template>
  <aside class="queue-panel scroll" v-if="queueOpen">
    <!-- Header with tabs (hibiki-style) -->
    <div class="qp-tabs">
      <button class="qp-tab" :class="{ active: tab === 'queue' }" @click="tab = 'queue'">Queue</button>
      <button class="qp-tab" :class="{ active: tab === 'lyrics' }" @click="tab = 'lyrics'">Lyrics</button>
    </div>

    <!-- Queue tab — Played / Now Playing / Up Next, three discrete buckets so
         the user can see what's already happened and what's coming. -->
    <div v-if="tab === 'queue'" class="qp-body">
      <!-- Played (faded, clickable to jump back) -->
      <template v-if="playedTracks.length">
        <div class="qp-section-label">
          Played · {{ playedTracks.length }} {{ playedTracks.length === 1 ? 'item' : 'items' }}
        </div>
        <div
          v-for="(t, i) in playedTracks"
          :key="`played-${t.id}-${i}`"
          class="qp-row played"
          @click="jumpTo(i)"
        >
          <Poster :idx="t.id" :src="t.poster ?? null" aspect="1/1" :width="80" class="qp-thumb" />
          <div class="qp-row-info">
            <div class="qp-row-title">{{ t.title }}</div>
            <div class="qp-row-artist">{{ t.artist }}</div>
          </div>
          <span class="qp-row-dur">{{ formatTime(t.duration) }}</span>
        </div>
      </template>

      <!-- Now Playing (highlighted) -->
      <template v-if="currentTrack">
        <div class="qp-section-label">Now Playing</div>
        <div class="qp-row current">
          <VuMeter :playing="playing" />
          <div class="qp-row-info">
            <div class="qp-row-title">{{ currentTrack.title }}</div>
            <div class="qp-row-artist">{{ currentTrack.artist }}</div>
          </div>
          <span v-if="currentTrack.isStream" class="qp-live-badge">
            <span class="qp-live-dot" /> LIVE
          </span>
          <span v-else class="qp-row-dur">{{ formatTime(currentTrack.duration) }}</span>
        </div>
        <!-- Repeat-one chip directly under the active row so the user knows
             the next "next" will replay this track. -->
        <div v-if="repeatMode === 'one'" class="qp-chip">
          <Icon name="repeat" :size="12" /> Repeat one
        </div>
      </template>

      <!-- Up Next (draggable, removable, with a Clear shortcut) -->
      <template v-if="upcomingTracks.length">
        <div class="qp-section-head">
          <span class="qp-section-label" style="margin-bottom: 0">
            Up Next · {{ upcomingTracks.length }} {{ upcomingTracks.length === 1 ? 'item' : 'items' }}
          </span>
          <button class="qp-clear" @click="clearUpcoming">Clear</button>
        </div>
        <div v-if="shuffled" class="qp-chip">
          <Icon name="shuffle" :size="12" /> Shuffled
        </div>
        <div
          v-for="(t, i) in upcomingTracks"
          :key="`upcoming-${t.id}-${i}`"
          class="qp-row upcoming"
          :draggable="true"
          @click="jumpTo(currentIndex + 1 + i)"
          @dragstart="onDragStart($event, i)"
          @dragover.prevent="onDragOver($event, i)"
          @drop="onDrop($event, i)"
          @dragend="dragIndex = -1"
        >
          <div class="qp-drag-handle" title="Drag to reorder">
            <Icon name="dots-six-vertical" :size="14" />
          </div>
          <Poster :idx="t.id" :src="t.poster ?? null" aspect="1/1" :width="80" class="qp-thumb" />
          <div class="qp-row-info">
            <div class="qp-row-title">{{ t.title }}</div>
            <div class="qp-row-artist">{{ t.artist }}</div>
          </div>
          <span class="qp-row-dur">{{ formatTime(t.duration) }}</span>
          <button
            class="qp-remove"
            title="Remove from queue"
            @click.stop="removeFromQueue(currentIndex + 1 + i)"
          >
            <Icon name="close" :size="12" />
          </button>
        </div>
        <div v-if="repeatMode === 'all'" class="qp-chip">
          <Icon name="repeat" :size="12" /> Repeat all
        </div>
      </template>

      <!-- Empty state — only when there's literally nothing on the deck. -->
      <div v-if="!queue.length && !currentTrack" class="qp-empty">
        <Icon name="music" :size="32" style="opacity: 0.4; margin-bottom: 8px" />
        <p>Queue is empty</p>
        <p style="font-size: 11px; color: var(--fg-3); margin-top: 4px">Play something to get started</p>
      </div>
    </div>

    <!-- Lyrics tab — hibiki-style: big-text active line with glow, timing
         offset slider at the bottom for when the .lrc isn't quite aligned. -->
    <div v-if="tab === 'lyrics'" class="qp-lyrics-wrap">
      <div v-if="lyricsLoading" class="qp-empty">Loading lyrics…</div>
      <template v-else-if="lyrics && lyrics.lines.length">
        <!-- Now-playing card at the top: anchors which track the lyrics are for. -->
        <div v-if="currentTrack" class="qp-np-card">
          <div class="qp-np-title">{{ currentTrack.title }}</div>
          <div class="qp-np-artist">{{ currentTrack.artist }}</div>
        </div>

        <div ref="lyricsScroll" class="qp-lyrics qp-lyrics-fade">
          <template v-for="(line, i) in lyrics.lines" :key="i">
            <div v-if="!line.text.trim()" class="qp-lyric-gap" />
            <button
              v-else
              type="button"
              class="lyric-line"
              :class="{
                active: lyrics.synced && i === activeLyricIdx,
                past: lyrics.synced && i < activeLyricIdx,
                unsynced: !lyrics.synced,
              }"
              :ref="(el) => bindLyricRef(el as HTMLElement | null, i)"
              @click="onLyricClick(line)"
            >
              {{ line.text }}
            </button>
          </template>
        </div>

        <!-- Timing offset slider (only for synced lyrics) — drag, wheel, or
             double-click the readout to reset to 0. -->
        <div
          v-if="lyrics.synced"
          class="qp-timing"
          @wheel.prevent="onTimingWheel"
        >
          <span class="qp-timing-label">Timing</span>
          <AppSlider
            :model-value="lyricsOffsetMs"
            :min="-5000"
            :max="5000"
            :step="100"
            bipolar
            aria-label="Lyrics timing offset"
            class="qp-timing-slider"
            @update:model-value="onTimingValue"
          />
          <span class="qp-timing-value mono">{{ formatOffset(lyricsOffsetMs) }}</span>
          <AppTooltip v-if="lyricsOffsetMs !== 0" label="Reset to zero">
            <button
              type="button"
              class="qp-timing-reset"
              aria-label="Reset timing offset"
              @click="lyricsOffsetMs = 0"
            >
              <Icon name="close" :size="10" />
            </button>
          </AppTooltip>
        </div>
      </template>
      <div v-else class="qp-empty">
        <Icon name="music" :size="32" style="opacity: 0.4; margin-bottom: 8px" />
        <p>No lyrics available</p>
        <p style="font-size: 11px; color: var(--fg-3); margin-top: 6px">
          Drop a matching .lrc next to the audio file and re-scan to add them.
        </p>
      </div>

      <!-- Fullscreen handoff: the lyrics side view is where the fullscreen
           now-playing view is launched from (no longer from clicking the art). -->
      <div v-if="currentTrack" class="qp-lyrics-footer">
        <button class="qp-fullscreen-btn" @click="openFullscreen" title="Open the fullscreen now-playing view">
          <Icon name="expand" :size="14" />
          <span>Fullscreen</span>
        </button>
      </div>
    </div>
  </aside>
</template>

<script setup lang="ts">
const {
  playing, currentTrack, queue, queueOpen, position, formatTime,
  shuffled, repeatMode, currentIndex, playedTracks, upcomingTracks,
  jumpTo, removeFromQueue, moveInQueue, clearUpcoming, seek, sideTab,
} = usePlayer()

// The active tab lives in shared player state so the playbar's Queue / Lyrics
// buttons can open the panel straight onto the tab they name.
const tab = sideTab

// Fullscreen now-playing overlay — opened from the lyrics footer. Shared state
// slot (same one the playbar's Expand button uses).
const nowPlayingOpen = useState('now_playing_open', () => false)
function openFullscreen() { nowPlayingOpen.value = true }

// --- Lyrics fetch + sync ----------------------------------------------------

interface LyricsLine { time_ms: number; text: string }
interface LyricsResponse { synced: boolean; lines: LyricsLine[] }

const lyrics = ref<LyricsResponse | null>(null)
const lyricsLoading = ref(false)
const lyricRefs = ref<Array<HTMLElement | null>>([])
const lyricsScroll = ref<HTMLElement | null>(null)
const lyricsOffsetMs = ref(0)  // user-tunable lyric/audio offset
let lastLoadedTrackId: number | null = null

function bindLyricRef(el: HTMLElement | null, i: number) {
  lyricRefs.value[i] = el
}

async function loadLyrics(trackId: number | null | undefined) {
  // Negative IDs are synthetic radio / podcast tracks — they have no
  // music-library lyrics, so skip the round-trip rather than 422'ing.
  if (!trackId || trackId <= 0) { lyrics.value = null; lastLoadedTrackId = trackId ?? null; return }
  // Refetch only when the track changes; flipping tab doesn't hit the API.
  if (trackId === lastLoadedTrackId && lyrics.value) return
  lastLoadedTrackId = trackId
  lyricsLoading.value = true
  lyricRefs.value = []
  lyricsOffsetMs.value = 0
  try {
    const { $heya } = useNuxtApp()
    lyrics.value = await $heya('/api/music/tracks/{id}/lyrics', { path: { id: trackId } }) as LyricsResponse
  } catch {
    lyrics.value = null
  } finally {
    lyricsLoading.value = false
  }
}

// Fetch lazily: only when the lyrics tab is open AND the queue panel is open.
// Saves a round trip per track on the queue tab.
watch(
  () => [currentTrack.value?.id, tab.value, queueOpen.value] as const,
  ([id, t, open]) => {
    if (!open || t !== 'lyrics') return
    void loadLyrics(id ?? null)
  },
  { immediate: true },
)

const activeLyricIdx = computed(() => {
  const list = lyrics.value?.lines
  if (!lyrics.value?.synced || !list || !list.length) return -1
  const posMs = position.value * 1000 + lyricsOffsetMs.value
  // Binary-search for the last line whose timestamp has passed.
  let lo = 0, hi = list.length - 1, ans = -1
  while (lo <= hi) {
    const mid = (lo + hi) >> 1
    if ((list[mid]?.time_ms ?? -1) <= posMs) { ans = mid; lo = mid + 1 }
    else hi = mid - 1
  }
  return ans
})

// Auto-scroll to keep the active lyric centered in the scroller. Smooth
// motion so the eye can track it without jumping.
watch(activeLyricIdx, (i) => {
  if (i < 0) return
  const el = lyricRefs.value[i]
  if (!el || !lyricsScroll.value) return
  const container = lyricsScroll.value
  const target = el.offsetTop - container.clientHeight / 2 + el.clientHeight / 2
  container.scrollTo({ top: Math.max(0, target), behavior: 'smooth' })
})

// Click a synced lyric to seek to its timestamp. Offset is applied to keep
// the listening experience consistent with what the user sees.
function onLyricClick(line: LyricsLine) {
  if (!lyrics.value?.synced) return
  if (!currentTrack.value?.duration) return
  const targetSec = Math.max(0, (line.time_ms - lyricsOffsetMs.value) / 1000)
  const pct = currentTrack.value.duration > 0 ? targetSec / currentTrack.value.duration : 0
  seek(pct)
}

function onTimingWheel(e: WheelEvent) {
  lyricsOffsetMs.value = Math.max(-5000, Math.min(5000, lyricsOffsetMs.value + (e.deltaY < 0 ? 100 : -100)))
}
function onTimingValue(v: number) {
  lyricsOffsetMs.value = v
}
function formatOffset(ms: number) {
  const sec = ms / 1000
  const sign = sec >= 0 ? '+' : ''
  return `${sign}${sec.toFixed(1)}s`
}

// --- Drag & drop reorder ----------------------------------------------------
// dragIndex is the absolute index within `upcomingTracks` (NOT the queue
// index — caller-side methods convert to absolute via currentIndex+1+i).
const dragIndex = ref(-1)
function onDragStart(event: DragEvent, index: number) {
  dragIndex.value = index
  if (event.dataTransfer) event.dataTransfer.effectAllowed = 'move'
}
function onDragOver(event: DragEvent, index: number) {
  if (dragIndex.value === index) return
  if (event.dataTransfer) event.dataTransfer.dropEffect = 'move'
}
function onDrop(_event: DragEvent, toIndex: number) {
  if (dragIndex.value < 0 || dragIndex.value === toIndex) return
  moveInQueue(currentIndex.value + 1 + dragIndex.value, currentIndex.value + 1 + toIndex)
  dragIndex.value = -1
}
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

/* Compact band (720.02-1200px): the docked panel becomes a floating overlay
   instead of a flex sibling squeezing `.music-main` on an already-tight
   viewport — same `queueOpen` toggle from the playbar, same content, just
   repositioned + elevated so it reads as "on top of" rather than "beside".
   Above 1200px nothing here applies; the desktop dock is untouched. */
@media (min-width: 720.02px) and (max-width: 1200px) {
  .queue-panel {
    position: fixed;
    top: var(--topbar-h);
    right: 0;
    bottom: var(--playbar-h);
    height: auto;
    width: min(var(--music-queue-w), 90vw);
    z-index: 60;
    border-left: 1px solid var(--border-strong);
    box-shadow: var(--shadow-3);
  }
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
  background: transparent;
  border-top: 0;
  border-left: 0;
  border-right: 0;
  cursor: pointer;
}
.qp-tab:hover { color: var(--fg-1); }
.qp-tab.active { color: var(--gold); border-bottom-color: var(--gold); }

.qp-body { flex: 1; overflow-y: auto; padding: 6px 0 12px; }
.qp-section-label {
  font-size: 10px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--fg-3);
  padding: 14px 16px 4px;
}
.qp-section-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 16px 4px;
}
.qp-clear {
  background: transparent;
  border: 0;
  font-size: 11px;
  color: var(--fg-3);
  cursor: pointer;
  transition: color 0.12s;
}
.qp-clear:hover { color: var(--gold); }

.qp-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  margin: 4px 16px;
  font-size: 10px;
  color: var(--gold);
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.08em;
}

.qp-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 16px;
  cursor: pointer;
  transition: background 0.12s;
  border-left: 2px solid transparent;
}
.qp-row:hover { background: rgb(var(--ink) / 0.04); }
.qp-row.current {
  background: var(--gold-soft, rgba(255, 196, 50, 0.08));
  border-left-color: var(--gold);
}
.qp-row.played { opacity: 0.5; }
.qp-row.played:hover { opacity: 0.85; }
.qp-row.upcoming { cursor: grab; }
.qp-row.upcoming:active { cursor: grabbing; }
.qp-drag-handle {
  display: flex;
  color: var(--fg-3);
  opacity: 0;
  transition: opacity 0.12s;
  flex-shrink: 0;
}
.qp-row:hover .qp-drag-handle { opacity: 1; }

.qp-thumb {
  width: 36px;
  height: 36px;
  border-radius: 4px;
  flex-shrink: 0;
}
.qp-row-info { flex: 1; min-width: 0; }
.qp-row-title {
  font-size: 13px;
  font-weight: 500;
  color: var(--fg-0);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.qp-row.current .qp-row-title { color: var(--gold); }
.qp-row-artist {
  font-size: 11px;
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
.qp-remove {
  background: transparent;
  border: 0;
  padding: 4px;
  color: var(--fg-3);
  opacity: 0;
  transition: opacity 0.12s, color 0.12s;
  cursor: pointer;
  flex-shrink: 0;
}
.qp-row:hover .qp-remove { opacity: 1; }
.qp-remove:hover { color: var(--gold); }

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
  animation: qp-live-pulse 1.8s ease-in-out infinite;
}
@keyframes qp-live-pulse {
  0%, 100% { opacity: 0.45; }
  50% { opacity: 1; }
}

.qp-empty { text-align: center; padding: 40px 16px; color: var(--fg-2); font-size: 13px; }

/* Lyrics tab ----------------------------------------------------------- */
.qp-lyrics-wrap {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.qp-np-card {
  margin: 14px 16px 6px;
  padding: 10px 12px;
  background: color-mix(in srgb, var(--gold) 6%, transparent);
  border: 1px solid color-mix(in srgb, var(--gold) 15%, transparent);
  border-radius: var(--r-md);
}
.qp-np-title { font-size: 13px; font-weight: 700; color: var(--fg-0); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.qp-np-artist { font-size: 11px; color: var(--fg-2); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.qp-lyrics {
  flex: 1;
  overflow-y: auto;
  padding: 16px 20px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.qp-lyrics-fade {
  /* Soft mask so the active line stays the eye's anchor; older + future
     lines fade into the scrollable margins. */
  mask-image: linear-gradient(to bottom, transparent 0%, black 10%, black 90%, transparent 100%);
  -webkit-mask-image: linear-gradient(to bottom, transparent 0%, black 10%, black 90%, transparent 100%);
}
.qp-lyric-gap { height: 12px; }
.lyric-line {
  background: transparent;
  border: 0;
  text-align: left;
  font-size: 20px;
  font-weight: 700;
  line-height: 1.45;
  color: rgb(var(--ink) / 0.25);
  padding: 4px 8px;
  border-radius: var(--r-sm);
  cursor: pointer;
  transition: color 0.3s ease, transform 0.3s ease, background 0.15s;
}
.lyric-line:hover { background: color-mix(in srgb, var(--gold) 5%, transparent); color: rgb(var(--ink) / 0.4); }
.lyric-line.active {
  color: var(--gold);
  filter: drop-shadow(0 0 8px color-mix(in srgb, var(--gold) 60%, transparent));
  transform: scale(1.02);
  transform-origin: left center;
}
.lyric-line.past { color: rgb(var(--ink) / 0.5); }
.lyric-line.unsynced { font-size: 14px; line-height: 1.6; color: var(--fg-1); font-weight: 500; cursor: default; }
.lyric-line.unsynced:hover { background: transparent; }

.qp-timing {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 16px;
  border-top: 1px solid var(--border);
}
.qp-timing-label {
  font-size: 10px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--fg-3);
  flex-shrink: 0;
}
/* AppSlider provides the visual identity — only the row-layout sizing
   remains. The `bipolar` mode + AppSlider's own gold thumb match the old
   look without the native-range CSS plumbing. */
.qp-timing-slider { flex: 1; min-width: 0; }
.qp-timing-value {
  font-size: 11px;
  color: var(--fg-3);
  flex-shrink: 0;
  font-variant-numeric: tabular-nums;
  min-width: 42px;
  text-align: right;
}
/* Explicit reset button — replaces the old double-click affordance which
   was hidden behind a title="" tooltip nobody discovered. Only renders
   when the offset is non-zero so it doesn't clutter the row at rest. */
.qp-timing-reset {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  background: rgb(var(--ink) / 0.05);
  border: 0;
  color: var(--fg-3);
  cursor: pointer;
  flex-shrink: 0;
  transition: background 0.12s, color 0.12s;
}
.qp-timing-reset:hover { background: rgb(var(--ink) / 0.12); color: var(--fg-0); }
.mono { font-family: var(--font-mono); }

/* Fullscreen launcher — pinned at the bottom of the lyrics tab. */
.qp-lyrics-footer {
  padding: 10px 16px;
  border-top: 1px solid var(--border);
  flex-shrink: 0;
}
.qp-fullscreen-btn {
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 9px 0;
  background: rgb(var(--ink) / 0.04);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  color: var(--fg-1);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: background 0.15s, color 0.15s, border-color 0.15s;
}
.qp-fullscreen-btn:hover {
  background: var(--gold-soft, rgba(255, 196, 50, 0.08));
  color: var(--gold);
  border-color: var(--gold);
}
</style>
