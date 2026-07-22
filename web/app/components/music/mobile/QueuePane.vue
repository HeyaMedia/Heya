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

  Up Next rows carry two hand-rolled touch gestures (no drag library in this
  repo, and none should be added):
    - Long-press (400ms) + vertical drag to reorder, replacing the old
      up/down arrow buttons. Siblings "part" out of the way via a translateY
      transform while the grabbed row follows the pointer.
    - Horizontal swipe-to-remove (Apple Mail style), replacing the old X
      button. Partial swipe snaps open to reveal a Remove action; a swipe
      past ~55% of the row width commits immediately.
  Both gestures share one `activeGesture` discriminator so at most one row is
  ever mid-gesture at a time. See the pointer handlers below for the full
  state machine; the geometry (drag hover-target math, auto-scroll) is
  commented at each function since none of it is obvious from the code shape
  alone.

  No props, no emits — reads/mutates the global usePlayerBindings() singleton.
-->
<template>
  <div ref="rootEl" class="qp-root">
    <nav class="qp-tabs" role="tablist" aria-label="Queue views">
      <button
        v-for="tab in queueTabs"
        :id="`qp-tab-${tab.id}`"
        :key="tab.id"
        type="button"
        role="tab"
        class="qp-tab"
        :class="{ active: activeTab === tab.id }"
        :aria-selected="activeTab === tab.id"
        :aria-controls="`qp-panel-${tab.id}`"
        @click="activeTab = tab.id"
      >
        {{ tab.label }}
      </button>
    </nav>

    <section
      v-if="activeTab === 'back-to'"
      id="qp-panel-back-to"
      role="tabpanel"
      aria-labelledby="qp-tab-back-to"
      class="qp-tab-panel"
    >
      <div v-if="historyTracks.length" class="qp-list qp-tab-list">
        <!-- History is intentionally newest-first: the track immediately
             before the current one is at the top; the oldest is at bottom. -->
        <button
          v-for="entry in historyTracks"
          :key="`played-${entry.track.id}-${entry.queueIndex}`"
          type="button"
          class="qp-row qp-row-played"
          @click="jumpTo(entry.queueIndex)"
        >
          <Poster :idx="entry.track.id" :src="entry.track.poster ?? null" aspect="1/1" :width="88" class="qp-thumb" />
          <div class="qp-row-info">
            <div class="qp-row-title">{{ entry.track.title }}</div>
            <div class="qp-row-artist">{{ entry.track.artist }}</div>
          </div>
          <span class="qp-row-dur">{{ formatTime(entry.track.duration) }}</span>
        </button>
      </div>
      <div v-else class="qp-empty">
        <Icon name="prev" :size="28" />
        <p>Nothing played in this queue yet.</p>
      </div>
    </section>

    <section
      v-else-if="activeTab === 'up-next'"
      id="qp-panel-up-next"
      role="tabpanel"
      aria-labelledby="qp-tab-up-next"
      class="qp-tab-panel"
    >
      <div class="qp-up-next-tools">
        <div class="qp-toolbar">
          <button type="button" class="qp-chip" :class="{ active: shuffled }" aria-label="Shuffle" @click="toggleShuffle">
            <Icon name="shuffle" :size="15" />
            <span>Shuffle</span>
          </button>
          <button type="button" class="qp-chip" :class="{ active: repeatMode !== 'off' }" aria-label="Repeat" @click="cycleRepeat">
            <Icon name="repeat" :size="15" />
            <span>{{ repeatMode === 'one' ? 'Repeat one' : 'Repeat' }}</span>
          </button>
          <button type="button" class="qp-clear" :disabled="!upcomingTracks.length" @click="clearUpcoming">Clear</button>
        </div>
        <div class="qp-mobile-autoplay">
          <div class="qp-mobile-autoplay-copy">
            <div class="qp-mobile-autoplay-title">Play tracks like this…</div>
            <div class="qp-mobile-autoplay-hint">
              {{ localMode
                ? 'Unavailable for live streams'
                : similarAutoplayLoading
                  ? 'Finding more tracks…'
                  : similarAutoplayEnabled
                    ? 'Keeps this queue going'
                    : 'Stops when the queue ends' }}
            </div>
          </div>
          <AppSwitch
            :model-value="similarAutoplayEnabled"
            :disabled="localMode"
            size="md"
            aria-label="Play tracks like this"
            @update:model-value="setSimilarAutoplayEnabled"
          />
        </div>
      </div>

      <div v-if="upcomingTracks.length" class="qp-list qp-tab-list">
        <div
          v-for="(t, i) in upcomingTracks"
          :key="`upcoming-${t.id}-${i}`"
          :ref="(el) => setRowEl(el as HTMLElement | null, i)"
          class="qp-row qp-row-upcoming"
          :style="rowStyle(i)"
        >
          <div class="qp-swipe-mask">
            <button
              type="button"
              class="qp-swipe-reveal"
              :class="{ visible: rowOffsetX(i) < -1 }"
              :tabindex="rowOffsetX(i) < -1 ? 0 : -1"
              :style="{ width: OPEN_WIDTH + 'px' }"
              aria-label="Remove from queue"
              @click="commitRemove(i)"
            >
              <Icon name="trash" :size="18" />
              <span>Remove</span>
            </button>
            <button
              type="button"
              class="qp-swipe-content"
              :style="contentStyle(i)"
              @pointerdown="onRowPointerDown($event, i)"
              @pointermove="onRowPointerMove($event, i)"
              @pointerup="onRowPointerUp($event, i)"
              @pointercancel="onRowPointerCancel($event, i)"
              @click="onRowClick(i)"
              :aria-label="`${t.title} by ${t.artist}. Long press and drag to reorder.`"
            >
              <Poster :idx="t.id" :src="t.poster ?? null" aspect="1/1" :width="88" class="qp-thumb" />
              <div class="qp-row-info">
                <div class="qp-row-title">{{ t.title }}</div>
                <div class="qp-row-artist">{{ t.artist }}</div>
              </div>
              <span class="qp-row-dur">{{ formatTime(t.duration) }}</span>
            </button>
          </div>
        </div>
      </div>
      <div v-else class="qp-empty">
        <Icon name="queue" :size="28" />
        <p>Nothing up next.</p>
      </div>
    </section>

    <section
      v-else
      id="qp-panel-related"
      role="tabpanel"
      aria-labelledby="qp-tab-related"
      class="qp-tab-panel"
    >
      <p v-if="currentTrack && relatedTrackId > 0" class="qp-related-intro">
        Ranked by sonic similarity to <strong>{{ currentTrack.title }}</strong>
      </p>
      <div v-if="relatedLoading" class="qp-empty">
        <Icon name="pulse" :size="28" />
        <p>Finding sonic neighbours…</p>
      </div>
      <div v-else-if="relatedRows.length" class="qp-list qp-tab-list">
        <button
          v-for="(row, i) in relatedRows"
          :key="row.track_id"
          type="button"
          class="qp-row qp-related-row"
          :class="{ queued: upcomingTrackIds.has(row.track_id) }"
          :disabled="relatedAdding === row.track_id"
          :aria-label="`${row.track_title} by ${row.artist_name}. Play next.`"
          @click="addRelatedNext(row)"
        >
          <span class="qp-related-rank">{{ i + 1 }}</span>
          <Poster :idx="row.track_id" :src="useAlbumCoverUrl(row.artist_slug, row.album_slug)" aspect="1/1" :width="88" class="qp-thumb" />
          <div class="qp-row-info">
            <div class="qp-row-title">{{ row.track_title }}</div>
            <div class="qp-row-artist">{{ row.artist_name }}</div>
          </div>
          <span class="qp-related-score">
            {{ upcomingTrackIds.has(row.track_id) ? 'Queued' : `${similarityPercent(row.distance)}%` }}
          </span>
        </button>
      </div>
      <div v-else class="qp-empty">
        <Icon name="pulse" :size="28" />
        <p>{{ relatedUnavailable ? 'Sonic neighbours are unavailable for this track.' : 'No related tracks found.' }}</p>
      </div>

      <template v-if="currentTrack?.source === 'radio' && radioSuggestions.length">
        <div class="qp-section-label">Also worth finding</div>
        <component
          :is="suggestion.provider_url ? 'a' : 'div'"
          v-for="suggestion in radioSuggestions"
          :key="suggestion.recording_entity_id"
          class="qp-row qp-catalog-suggestion"
          :href="suggestion.provider_url || undefined"
          :target="suggestion.provider_url ? '_blank' : undefined"
          :rel="suggestion.provider_url ? 'noopener noreferrer' : undefined"
        >
          <div class="qp-row-info">
            <div class="qp-row-title">{{ suggestion.title }}</div>
            <div class="qp-row-artist">{{ suggestion.artist_name }}</div>
            <div class="qp-suggestion-reason">{{ suggestion.reason }}</div>
          </div>
          <Icon v-if="suggestion.provider_url" name="external-link" :size="15" />
        </component>
      </template>
    </section>
  </div>
</template>

<script setup lang="ts">
import { useQuery } from '@pinia/colada'
import type { SimilarTracksByTrackRichRow, TrackResultsBody } from '~~/shared/api/types.gen'
import type { Track } from '~/composables/usePlayer'

type QueueTab = 'back-to' | 'up-next' | 'related'
const queueTabs: Array<{ id: QueueTab, label: string }> = [
  { id: 'back-to', label: 'Previously' },
  { id: 'up-next', label: 'Next' },
  { id: 'related', label: 'Related' },
]
const activeTab = ref<QueueTab>('up-next')

const {
  currentTrack, currentIndex, playedTracks, upcomingTracks,
  shuffled, repeatMode, formatTime,
  localMode, similarAutoplayEnabled, similarAutoplayLoading,
  jumpTo, moveInQueue, removeFromQueue, clearUpcoming, toggleShuffle, cycleRepeat,
  playNext, setSimilarAutoplayEnabled,
} = usePlayerBindings()
const radioSuggestions = useState<import('~/composables/useRadio').MusicCatalogSuggestion[]>('music_radio_suggestions', () => [])

const historyTracks = computed(() => playedTracks.value
  .map((track, queueIndex) => ({ track, queueIndex }))
  .reverse())
const upcomingTrackIds = computed(() => new Set(upcomingTracks.value.map(track => track.id)))

// Related is the direct audio-embedding KNN endpoint: it is already ordered
// by ascending cosine distance, so preserving response order preserves rank.
const { $heya } = useNuxtApp()
const relatedTrackId = computed(() => {
  const track = currentTrack.value
  return track && !track.isStream && track.id > 0 ? track.id : 0
})
const relatedQuery = useQuery(() => ({
  key: ['music', 'track', 'sonic-similar', relatedTrackId.value],
  query: async () => await $heya('/api/music/tracks/{id}/sonic-similar', {
    path: { id: relatedTrackId.value },
    query: { limit: 50 },
  }) as TrackResultsBody,
  enabled: activeTab.value === 'related' && relatedTrackId.value > 0,
  staleTime: 1000 * 60 * 10,
  retry: 0,
}))
const relatedRows = computed(() => relatedQuery.data.value?.items ?? [])
const relatedLoading = computed(() => activeTab.value === 'related'
  && relatedTrackId.value > 0
  && relatedQuery.isPending.value)
const relatedUnavailable = computed(() => relatedTrackId.value <= 0 || relatedQuery.error.value != null)
const relatedAdding = ref<number | null>(null)

function similarityPercent(distance: number) {
  return Math.round(Math.max(0, Math.min(1, 1 - distance)) * 100)
}

function relatedRowToTrack(row: SimilarTracksByTrackRichRow): Track {
  return {
    id: row.track_id,
    title: row.track_title,
    artist: row.artist_name,
    album: row.album_title,
    duration: row.duration,
    stream_url: `/api/music/tracks/${row.track_id}/stream`,
    album_id: row.album_id,
    artist_id: row.artist_id,
    artist_slug: row.artist_slug,
    album_slug: row.album_slug,
    poster: useAlbumCoverUrl(row.artist_slug, row.album_slug) ?? undefined,
    source: 'related',
  }
}

async function addRelatedNext(row: SimilarTracksByTrackRichRow) {
  if (relatedAdding.value !== null || upcomingTrackIds.value.has(row.track_id)) return
  relatedAdding.value = row.track_id
  try { await playNext(relatedRowToTrack(row)) }
  finally { relatedAdding.value = null }
}

function clamp(v: number, min: number, max: number) {
  return Math.min(max, Math.max(min, v))
}

// --- Tunables ---------------------------------------------------------
const HOLD_MS = 400 // press-and-hold duration to arm drag-to-reorder
const MOVE_SLOP = 10 // px of movement allowed before hold-pending resolves to drag/swipe/scroll
const OPEN_WIDTH = 84 // px the row settles to when swiped partially open
const PARTIAL_OPEN_THRESHOLD = 72 // px dragged past which a released swipe snaps open instead of closed
const FULL_SWIPE_RATIO = 0.55 // fraction of row width past which release commits an immediate remove
const AUTO_SCROLL_EDGE = 64 // px from the scroll viewport edge that engages auto-scroll while dragging
const AUTO_SCROLL_MAX_SPEED = 16 // px/frame at the very edge
const REMOVE_ANIM_MS = 190 // slide-out + collapse duration before the track actually leaves the queue

// --- Gesture state ------------------------------------------------------
// One shared discriminator plus a handful of refs drive both gestures. Only
// one Up Next row can be mid-gesture (or settled "open") at a time.
type GesturePhase = 'none' | 'hold-pending' | 'drag' | 'swipe'
const activeGesture = ref<GesturePhase>('none')
const activeRowIndex = ref<number | null>(null) // index into upcomingTracks the current gesture started on

// Drag-to-reorder
const dragDeltaY = ref(0) // live translateY applied to the grabbed row
const dragTargetIndex = ref<number | null>(null) // hover slot the grabbed row would land in

// Swipe-to-remove
const swipeX = ref(0) // live translateX applied to the active row's content
const openRowIndex = ref<number | null>(null) // row settled open (revealing Remove), at most one

// Remove animation (slide out + collapse, then splice)
const removingRowIndex = ref<number | null>(null)
const removingRowHeight = ref(0)
const removingCollapsed = ref(false)

const rootEl = ref<HTMLElement | null>(null)
const rowRefs = ref<(HTMLElement | null)[]>([])
function setRowEl(el: HTMLElement | null, i: number) {
  rowRefs.value[i] = el
}

// Transient, non-rendered gesture bookkeeping — plain vars, not refs (mirrors
// the holdTimer/holdFired pattern in NowPlayingSheet.vue's play-hold gesture).
let holdTimer: ReturnType<typeof setTimeout> | null = null
let activePointerId: number | null = null
let startX = 0
let startY = 0
let lastClientY = 0
let suppressNextClick = false
// Drag geometry, captured once when the hold fires (armDrag).
let rowHeightPx = 0
let grabOffsetWithinRow = 0 // finger offset within the grabbed row, captured at pointerdown
let row0ContentTop = 0 // extrapolated content-space top of upcomingTracks[0]
let minScrollTop = 0 // auto-scroll never scrolls above the queue pane's own top
let autoScrollRaf: number | null = null
// Swipe geometry, captured once when the direction lock resolves (armSwipe).
let swipeBase = 0 // offset swipeX starts from (0 when closed, -OPEN_WIDTH when reopening an open row)
let swipeRowWidth = 0

let scrollerElCache: HTMLElement | null = null
function getScroller(): HTMLElement | null {
  if (!scrollerElCache) scrollerElCache = (rootEl.value?.closest('.nps-scroll') as HTMLElement | null) ?? null
  return scrollerElCache
}
// Content-space top of `.nps-pane-queue` within the scroller — the floor for
// auto-scroll during a drag, so reordering never yanks the view up into the
// now-playing pane above it.
function getQueuePaneTop(): number {
  const scroller = getScroller()
  const pane = rootEl.value?.closest('.nps-pane-queue') as HTMLElement | null
  if (!scroller || !pane) return 0
  const scRect = scroller.getBoundingClientRect()
  const paneRect = pane.getBoundingClientRect()
  return paneRect.top - scRect.top + scroller.scrollTop
}

function clearHoldTimer() {
  if (holdTimer) { clearTimeout(holdTimer); holdTimer = null }
}

// Full abort — used on pointercancel, on any upcomingTracks length change
// (e.g. Clear tapped mid-gesture), and on unmount. No commit of any kind.
function resetAllGestureState() {
  clearHoldTimer()
  stopAutoScrollLoop()
  const scroller = getScroller()
  if (scroller) scroller.style.scrollSnapType = ''
  activeGesture.value = 'none'
  activeRowIndex.value = null
  activePointerId = null
  dragTargetIndex.value = null
  dragDeltaY.value = 0
  swipeX.value = 0
  openRowIndex.value = null
  removingRowIndex.value = null
  removingRowHeight.value = 0
  removingCollapsed.value = false
}
watch(() => upcomingTracks.value.length, () => resetAllGestureState())
watch(activeTab, () => resetAllGestureState())

// --- Pointer lifecycle ---------------------------------------------------
function onRowPointerDown(e: PointerEvent, i: number) {
  if (e.button !== 0) return
  if (activeRowIndex.value !== null || removingRowIndex.value !== null) return
  // Keep receiving move/up for this pointer even if the finger drifts off
  // the row. Synthetic PointerEvents (used by headless-Chrome gesture
  // verification) may not correspond to a capturable hardware pointer, so
  // this is best-effort — the coordinate math below doesn't depend on it.
  try { (e.currentTarget as Element).setPointerCapture(e.pointerId) } catch { /* not capturable */ }
  activePointerId = e.pointerId
  activeRowIndex.value = i
  startX = e.clientX
  startY = e.clientY
  lastClientY = e.clientY
  const reopening = openRowIndex.value === i
  if (openRowIndex.value !== null && openRowIndex.value !== i) openRowIndex.value = null
  if (reopening) {
    // Grabbing an already-open row skips the hold timer entirely — any
    // drag from here adjusts/closes the reveal, a plain tap closes it.
    armSwipe(i)
  } else {
    activeGesture.value = 'hold-pending'
    clearHoldTimer()
    holdTimer = setTimeout(() => armDrag(i), HOLD_MS)
  }
}

function onRowPointerMove(e: PointerEvent, i: number) {
  if (activeRowIndex.value !== i || e.pointerId !== activePointerId) return
  lastClientY = e.clientY

  if (activeGesture.value === 'hold-pending') {
    const dx = e.clientX - startX
    const dy = e.clientY - startY
    if (Math.abs(dx) < MOVE_SLOP && Math.abs(dy) < MOVE_SLOP) return
    clearHoldTimer()
    if (Math.abs(dx) > Math.abs(dy) && dx < 0) {
      // Leftward and horizontal-dominant within the slop window -> swipe.
      armSwipe(i)
    } else {
      // Vertical (or rightward) -> not our gesture; let the container's
      // native scroll take over from here.
      activeGesture.value = 'none'
      activeRowIndex.value = null
      activePointerId = null
    }
    return
  }

  if (activeGesture.value === 'drag') {
    e.preventDefault()
    updateDragPositions()
    return
  }

  if (activeGesture.value === 'swipe') {
    e.preventDefault()
    const dx = e.clientX - startX
    swipeX.value = clamp(swipeBase + dx, -(swipeRowWidth + 80), 0)
  }
}

function onRowPointerUp(e: PointerEvent, i: number) {
  if (activeRowIndex.value !== i || e.pointerId !== activePointerId) return
  if (activeGesture.value === 'hold-pending') {
    // Plain tap: no movement, hold never fired. Don't preventDefault
    // anywhere in this path, so the browser's own trailing click still
    // fires -> onRowClick -> jumpTo.
    resetAllGestureState()
    return
  }
  if (activeGesture.value === 'drag') { commitDrag(i); return }
  if (activeGesture.value === 'swipe') { commitSwipe(e, i); return }
  resetAllGestureState()
}

function onRowPointerCancel(e: PointerEvent, i: number) {
  if (activeRowIndex.value !== i || e.pointerId !== activePointerId) return
  resetAllGestureState()
}

function onRowClick(i: number) {
  if (suppressNextClick) { suppressNextClick = false; return }
  if (openRowIndex.value !== null) {
    // Tapping the row while a reveal is showing closes it instead of
    // jumping — matches iOS Mail's swipe-action behavior.
    openRowIndex.value = null
    return
  }
  jumpTo(currentIndex.value + 1 + i)
}

// --- Drag to reorder ------------------------------------------------------
// Everything below is computed in the scroller's own content-space
// (`getBoundingClientRect().top - scrollerTop + scrollTop`), which stays
// valid across auto-scroll without extra bookkeeping: as the container
// scrolls, both the pointer's content-space Y and each row's content-space
// Y shift together, so plain arithmetic on them keeps producing correct
// results without tracking a separate scroll delta.
function armDrag(i: number) {
  holdTimer = null
  const scroller = getScroller()
  const el = rowRefs.value[i]
  if (!scroller || !el) { resetAllGestureState(); return }
  navigator.vibrate?.(25)
  activeGesture.value = 'drag'
  scroller.style.scrollSnapType = 'none'
  const scRect = scroller.getBoundingClientRect()
  const rowRect = el.getBoundingClientRect()
  rowHeightPx = rowRect.height
  grabOffsetWithinRow = startY - rowRect.top
  const rowContentTop = rowRect.top - scRect.top + scroller.scrollTop
  row0ContentTop = rowContentTop - i * rowHeightPx
  minScrollTop = getQueuePaneTop()
  dragTargetIndex.value = i
  dragDeltaY.value = 0
  startAutoScrollLoop()
}

function updateDragPositions() {
  const scroller = getScroller()
  if (!scroller || activeRowIndex.value === null || rowHeightPx <= 0) return
  const scRect = scroller.getBoundingClientRect()
  const pointerContentY = (lastClientY - scRect.top) + scroller.scrollTop
  const desiredContentTop = pointerContentY - grabOffsetWithinRow
  const i0 = activeRowIndex.value
  const nativeContentTop = row0ContentTop + i0 * rowHeightPx
  dragDeltaY.value = desiredContentTop - nativeContentTop
  const desiredCenter = desiredContentTop + rowHeightPx / 2
  const raw = Math.floor((desiredCenter - row0ContentTop) / rowHeightPx)
  dragTargetIndex.value = clamp(raw, 0, Math.max(0, upcomingTracks.value.length - 1))
}

function startAutoScrollLoop() {
  stopAutoScrollLoop()
  const tick = () => {
    autoScrollRaf = requestAnimationFrame(tick)
    if (activeGesture.value !== 'drag') return
    const scroller = getScroller()
    if (!scroller) return
    const rect = scroller.getBoundingClientRect()
    let dy = 0
    if (lastClientY < rect.top + AUTO_SCROLL_EDGE) {
      const p = clamp((rect.top + AUTO_SCROLL_EDGE - lastClientY) / AUTO_SCROLL_EDGE, 0, 1)
      dy = -AUTO_SCROLL_MAX_SPEED * p
    } else if (lastClientY > rect.bottom - AUTO_SCROLL_EDGE) {
      const p = clamp((lastClientY - (rect.bottom - AUTO_SCROLL_EDGE)) / AUTO_SCROLL_EDGE, 0, 1)
      dy = AUTO_SCROLL_MAX_SPEED * p
    }
    if (dy !== 0) {
      const next = Math.max(minScrollTop, scroller.scrollTop + dy)
      if (next !== scroller.scrollTop) {
        scroller.scrollTop = next
        updateDragPositions()
      }
    }
  }
  autoScrollRaf = requestAnimationFrame(tick)
}
function stopAutoScrollLoop() {
  if (autoScrollRaf !== null) { cancelAnimationFrame(autoScrollRaf); autoScrollRaf = null }
}

function commitDrag(i: number) {
  stopAutoScrollLoop()
  const scroller = getScroller()
  if (scroller) scroller.style.scrollSnapType = ''
  const target = dragTargetIndex.value ?? i
  activeGesture.value = 'none'
  activeRowIndex.value = null
  dragTargetIndex.value = null
  dragDeltaY.value = 0
  activePointerId = null
  suppressNextClick = true // swallow the trailing click the browser fires after this pointerup
  if (target !== i) moveInQueue(currentIndex.value + 1 + i, currentIndex.value + 1 + target)
}

// Sibling rows "part" out of the grabbed row's way by one slot height,
// mirroring the standard reorder-list pattern (Reminders, Spotify, etc).
function rowPartTranslate(i: number): number {
  if (activeGesture.value !== 'drag' || activeRowIndex.value === null || dragTargetIndex.value === null) return 0
  const from = activeRowIndex.value
  const target = dragTargetIndex.value
  if (i === from) return 0
  if (from < target && i > from && i <= target) return -rowHeightPx
  if (from > target && i >= target && i < from) return rowHeightPx
  return 0
}

// --- Swipe to remove --------------------------------------------------
function armSwipe(i: number) {
  activeGesture.value = 'swipe'
  const el = rowRefs.value[i]
  swipeRowWidth = el?.getBoundingClientRect().width ?? 320
  swipeBase = openRowIndex.value === i ? -OPEN_WIDTH : 0
  swipeX.value = swipeBase
}

function commitSwipe(e: PointerEvent, i: number) {
  const dx = e.clientX - startX
  const moved = Math.abs(dx) > MOVE_SLOP
  activeGesture.value = 'none'
  activeRowIndex.value = null
  activePointerId = null
  if (!moved) {
    // Tap on an already-open row (armSwipe skipped hold-pending for these,
    // so reaching here with no movement means it was just a tap) -> close.
    resetAllGestureState()
    return
  }
  suppressNextClick = true
  const width = rowRefs.value[i]?.getBoundingClientRect().width || swipeRowWidth
  const dist = -swipeX.value
  if (width > 0 && dist >= width * FULL_SWIPE_RATIO) {
    commitRemove(i)
  } else if (dist > PARTIAL_OPEN_THRESHOLD) {
    swipeX.value = -OPEN_WIDTH
    openRowIndex.value = i
  } else {
    swipeX.value = 0
    openRowIndex.value = null
  }
}

// Slide the row fully off-screen and collapse its height, then splice it
// out of the queue. Shared by a full swipe-release and a tap on the Remove
// button revealed by a partial swipe.
function commitRemove(i: number) {
  const el = rowRefs.value[i]
  const rect = el?.getBoundingClientRect()
  const width = rect?.width ?? swipeRowWidth ?? 320
  const height = rect?.height ?? 0
  activeGesture.value = 'none'
  activeRowIndex.value = null
  activePointerId = null
  openRowIndex.value = null
  removingRowIndex.value = i
  removingRowHeight.value = height
  removingCollapsed.value = false
  swipeX.value = -(width + 56)
  // Two-frame "measure then collapse" trick: the height above is fixed to
  // the row's current rendered size (no visual change), then flipped to 0
  // next frame so the height transition actually has something to animate.
  requestAnimationFrame(() => { removingCollapsed.value = true })
  const removeAt = currentIndex.value + 1 + i
  window.setTimeout(() => {
    removeFromQueue(removeAt)
    removingRowIndex.value = null
    removingRowHeight.value = 0
  }, REMOVE_ANIM_MS)
}

// --- Row style bindings -------------------------------------------------
function rowOffsetX(i: number): number {
  if (removingRowIndex.value === i) return swipeX.value
  if (activeGesture.value === 'swipe' && activeRowIndex.value === i) return swipeX.value
  if (openRowIndex.value === i) return -OPEN_WIDTH
  return 0
}

function contentStyle(i: number): Record<string, string> {
  const x = rowOffsetX(i)
  const liveTracking = activeGesture.value === 'swipe' && activeRowIndex.value === i
  return {
    transform: x ? `translateX(${x}px)` : '',
    transition: liveTracking ? 'none' : 'transform 200ms ease-out',
  }
}

function rowStyle(i: number): Record<string, string> {
  if (removingRowIndex.value === i) {
    return {
      overflow: 'hidden',
      height: `${removingCollapsed.value ? 0 : removingRowHeight.value}px`,
      opacity: removingCollapsed.value ? '0' : '1',
      transition: 'height 190ms ease, opacity 190ms ease',
    }
  }
  const isDraggedRow = activeGesture.value === 'drag' && activeRowIndex.value === i
  if (isDraggedRow) {
    return {
      transform: `translateY(${dragDeltaY.value}px) scale(1.03)`,
      transition: 'none',
      zIndex: '10',
      position: 'relative',
      boxShadow: '0 14px 30px rgb(var(--shade) / 0.45)',
    }
  }
  const part = rowPartTranslate(i)
  return {
    transform: part ? `translateY(${part}px)` : '',
    transition: 'transform 200ms ease-out',
  }
}

// --- Native touchmove: preventDefault is the only reliable cross-browser
// way to suppress scroll mid-gesture once armed. Registered manually (not
// via a Vue @touchmove) so it can be non-passive. -----------------------
function onTouchMoveNative(e: TouchEvent) {
  if (activeGesture.value === 'drag' || activeGesture.value === 'swipe') e.preventDefault()
}
onMounted(() => {
  rootEl.value?.addEventListener('touchmove', onTouchMoveNative, { passive: false })
})
onScopeDispose(() => {
  rootEl.value?.removeEventListener('touchmove', onTouchMoveNative)
  resetAllGestureState()
})
</script>

<!--
  Mounted inside NowPlayingSheet, whose AppSheet content is portaled to
  <body> — so this must stay unscoped too (docs/ui.md gotcha #2).
-->
<style>
.qp-root {
  position: relative;
  z-index: 1;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.qp-tabs {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  margin-left: -20px;
  margin-right: -20px;
  padding-left: 20px;
  padding-right: 20px;
  border-bottom: 1px solid rgb(var(--ink) / 0.09);
}
.qp-tab {
  position: relative;
  height: 52px;
  padding: 0 6px;
  border: 0;
  background: transparent;
  color: var(--fg-3);
  font-size: 12px;
  font-weight: 650;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  cursor: pointer;
}
.qp-tab::after {
  content: '';
  position: absolute;
  left: 12px;
  right: 12px;
  bottom: -1px;
  height: 2px;
  border-radius: 999px 999px 0 0;
  background: var(--gold);
  opacity: 0;
  transform: scaleX(0.35);
  transition: opacity 160ms ease, transform 180ms ease;
}
.qp-tab.active { color: var(--fg-0); }
.qp-tab.active::after { opacity: 1; transform: scaleX(1); }
.qp-tab-panel { min-height: 240px; }
.qp-up-next-tools {
  padding-top: 14px;
  padding-bottom: 8px;
}
.qp-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
}
.qp-mobile-autoplay {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 10px;
  padding: 10px 11px;
  border: 1px solid rgb(var(--ink) / 0.09);
  border-radius: var(--r-md);
  background: rgba(0, 0, 0, 0.16);
  box-shadow: 0 14px 30px -26px rgba(0, 0, 0, 0.9);
}
.qp-mobile-autoplay-copy { flex: 1; min-width: 0; }
.qp-mobile-autoplay-title { font-size: 12px; font-weight: 650; color: var(--fg-0); }
.qp-mobile-autoplay-hint { margin-top: 2px; font-size: 10px; color: var(--fg-3); }
.qp-root .qp-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 36px;
  padding: 0 12px;
  border-radius: 999px;
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  color: var(--fg-2);
  font-size: 12px;
  cursor: pointer;
}
.qp-root .qp-chip.active { color: var(--gold); border-color: color-mix(in srgb, var(--gold) 40%, transparent); background: var(--gold-soft); }
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
.qp-clear:disabled { opacity: 0.35; cursor: default; }

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
.qp-tab-list { padding-top: 10px; }
.qp-row-played { opacity: 0.82; }
.qp-catalog-suggestion {
  margin: 3px 0;
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  color: inherit;
  text-decoration: none;
  background: rgb(var(--ink) / 0.025);
}
.qp-catalog-suggestion[href]:active { background: var(--gold-soft); }
.qp-suggestion-reason {
  margin-top: 2px;
  font-size: 11px;
  color: var(--fg-3);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Up Next rows are the gesture surface: the outer `.qp-row-upcoming` gets
   the vertical "parting"/lift transform (drag) and the collapse-on-remove
   animation; the inner `.qp-swipe-mask` clips the horizontal swipe so the
   red reveal never spills past the row edges. Splitting these two matters —
   `overflow: hidden` also clips an element's own box-shadow, which would
   silently kill the elevated-drag shadow if it lived on the masked element. */
.qp-row-upcoming {
  display: flex;
  align-items: center;
  padding: 0;
  border-left: none;
  touch-action: pan-y;
  user-select: none;
  -webkit-touch-callout: none;
  -webkit-tap-highlight-color: transparent;
  will-change: transform;
}
.qp-swipe-mask {
  position: relative;
  overflow: hidden;
  flex: 1;
  min-width: 0;
}
.qp-swipe-reveal {
  position: absolute;
  top: 0;
  right: 0;
  bottom: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 3px;
  border: 0;
  background: var(--bad);
  color: #fff;
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.02em;
  cursor: pointer;
  opacity: 0;
  transition: opacity 120ms ease;
}
.qp-swipe-reveal.visible { opacity: 1; }
.qp-swipe-content {
  position: relative;
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  padding: 6px 4px;
  /* The reveal is explicitly hidden at rest, so this can remain transparent
     and let the queue's feathered ultrablur treatment flow through the row. */
  background: transparent;
  border: 0;
  text-align: left;
  cursor: pointer;
  -webkit-tap-highlight-color: transparent;
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

.qp-related-intro {
  margin: 0;
  padding: 14px 4px 2px;
  color: var(--fg-3);
  font-size: 11px;
  line-height: 1.4;
}
.qp-related-intro strong { color: var(--fg-2); font-weight: 600; }
.qp-related-row { border-radius: var(--r-sm); }
.qp-related-row:active { background: rgb(var(--ink) / 0.06); }
.qp-related-row.queued { background: rgb(var(--ink) / 0.025); }
.qp-related-row:disabled { opacity: 0.65; }
.qp-related-rank {
  width: 18px;
  flex-shrink: 0;
  text-align: right;
  font-family: var(--font-mono);
  font-size: 9px;
  color: var(--fg-3);
}
.qp-related-score {
  min-width: 40px;
  flex-shrink: 0;
  text-align: right;
  font: 600 9px var(--font-mono);
  letter-spacing: 0.02em;
  color: var(--fg-3);
}
.qp-related-row.queued .qp-related-score { color: var(--gold); }

.qp-empty {
  display: flex;
  min-height: 210px;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 40px 16px;
  text-align: center;
  color: var(--fg-2);
  font-size: 13px;
}
.qp-empty p { margin: 0; }
</style>
