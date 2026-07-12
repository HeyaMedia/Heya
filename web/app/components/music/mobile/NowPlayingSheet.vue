<!--
  NowPlayingSheet — full-screen now-playing surface for phones, built on
  AppSheet(size="full"). Reads everything from the global usePlayerBindings()
  singleton except the lyrics fetch, which mirrors the pattern in
  QueuePanel.vue's lyrics tab ($heya, GET /api/music/tracks/{id}/lyrics).

  The queue used to be a separate stacked AppSheet (QueueSheet); it's now
  merged in as a second scroll-snap section below the now-playing UI, inside
  one shared scroll container (`.nps-scroll`). Reka's drawer dismiss-on-
  swipe-down logic walks up from the touch target to the nearest scrollable
  ancestor and only treats the drag as a dismiss when THAT element is
  scrolled to the top — so a single scroller is load-bearing here, not just
  a style choice. Do not add another `overflow-y: auto` element inside
  `.nps-scroll` (e.g. nesting a second internal scroll region) — a nested
  scroller at its own top would hijack a swipe-down into a premature sheet
  dismiss. The pre-existing `.nps-lyrics` internal scroll is the one
  exception (it predates this merge and only applies while lyrics replace
  the artwork, never at the same time as the queue pane).

  Props/model:
    v-model:open — boolean, sheet visibility

  Note: content rendered by AppSheet is portaled to <body>, so anything that
  needs to reach it must live in an unscoped <style> block, not scoped — see
  docs/ui.md gotcha #2.
-->
<template>
  <AppSheet v-model:open="open" size="full" title="Now Playing">
    <template #header>
      <header class="app-sheet-header nps-header">
        <DrawerTitle as="h3" class="app-sheet-title">Now Playing</DrawerTitle>
        <button type="button" class="nps-close" aria-label="Close" @click="open = false">
          <Icon name="close" :size="18" />
        </button>
      </header>
    </template>
    <div class="nps-scroll">
      <div class="nps-body nps-pane-np">
        <div class="nps-visual">
          <div v-if="open && !showLyrics" class="nps-art-wrap" @click="cycleVisual">
            <Poster v-if="effectiveVisualMode === 'art'" :idx="currentTrack?.id ?? 0" :src="currentTrack?.poster ?? null" aspect="1/1" class="nps-art" />
            <div v-else class="nps-viz-wrap">
              <VisualizerMilkdrop v-if="effectiveVisualMode === 'milkdrop'" />
              <VisualizerStarfield v-else-if="effectiveVisualMode === 'starfield'" />
              <VisualizerSpectrum v-else :variant="spectrumVariant" :active="playing" />
            </div>
            <Transition name="nps-viz-toast-fade">
              <div v-if="visualToastVisible" class="nps-viz-toast">{{ visualModeLabel }}</div>
            </Transition>
          </div>
          <div v-else-if="open" ref="lyricsScrollEl" class="nps-lyrics scroll">
            <div v-if="lyricsLoading" class="nps-lyrics-empty">Loading lyrics…</div>
            <template v-else-if="lyrics && lyrics.lines.length">
              <p
                v-for="(line, i) in lyrics.lines"
                :key="i"
                class="nps-lyric-line"
                :class="{
                  active: lyrics.synced && i === activeLyricIdx,
                  past: lyrics.synced && i < activeLyricIdx,
                  unsynced: !lyrics.synced,
                }"
                :ref="(el) => bindLyricRef(el as HTMLElement | null, i)"
              >
                {{ line.text || '♪' }}
              </p>
            </template>
            <div v-else class="nps-lyrics-empty">
              <p>No lyrics for this track.</p>
            </div>
          </div>
        </div>

        <div class="nps-meta">
          <NuxtLink v-if="albumTo" :to="albumTo" class="nps-title nps-link" @click="open = false">{{ currentTrack?.title }}</NuxtLink>
          <div v-else class="nps-title">{{ currentTrack?.title ?? '—' }}</div>
          <NuxtLink v-if="artistTo" :to="artistTo" class="nps-artist nps-link" @click="open = false">{{ currentTrack?.artist }}</NuxtLink>
          <div v-else class="nps-artist">{{ currentTrack?.artist ?? '' }}</div>
        </div>

        <div class="nps-seek">
          <span class="nps-time">{{ formatTime(position) }}</span>
          <!-- Waveform scrubber — same component + facet source as the desktop
               Playbar. Click/drag-to-seek is built in (touch-action: none, so
               a drag scrubs rather than scrolling the sheet). Falls back to a
               flat bar for un-analysed tracks. -->
          <MusicWaveform
            :peaks="waveform"
            :progress="waveProgress"
            aria-label="Seek"
            :value-text="`${formatTime(position)} of ${formatTime(duration)}`"
            class="nps-waveform"
            @seek="onWaveformSeek"
          />
          <span class="nps-time">{{ formatTime(duration) }}</span>
        </div>

        <div class="nps-transport">
          <button type="button" class="nps-icon" :class="{ active: shuffled }" aria-label="Shuffle" :aria-pressed="shuffled" @click="toggleShuffle">
            <Icon name="shuffle" :size="20" />
          </button>
          <button type="button" class="nps-icon" aria-label="Previous" @click="prevTrack">
            <Icon name="prev" :size="26" />
          </button>
          <button
            type="button"
            class="nps-play"
            :aria-label="playing ? 'Pause' : 'Play'"
            @pointerdown="onPlayPointerDown"
            @pointerup="clearPlayHold"
            @pointercancel="clearPlayHold"
            @pointerleave="clearPlayHold"
            @click="onPlayClick"
          >
            <Icon :name="playing ? 'pause' : 'play'" :size="30" />
          </button>
          <button type="button" class="nps-icon" aria-label="Next" @click="nextTrack">
            <Icon name="next" :size="26" />
          </button>
          <button type="button" class="nps-icon" :class="{ active: repeatMode !== 'off' }" aria-label="Repeat" :aria-pressed="repeatMode !== 'off'" @click="cycleRepeat">
            <Icon name="repeat" :size="20" />
            <span v-if="repeatMode === 'one'" class="nps-repeat-badge">1</span>
          </button>
        </div>

        <div class="nps-secondary">
          <button type="button" class="nps-sicon" aria-label="Queue" @click="scrollToQueue">
            <Icon name="queue" :size="18" />
          </button>
          <button type="button" class="nps-sicon" :class="{ active: showLyrics }" aria-label="Lyrics" :aria-pressed="showLyrics" @click="showLyrics = !showLyrics">
            <Icon name="lyrics" :size="18" />
          </button>
        </div>

        <button type="button" class="nps-queue-hint" @click="scrollToQueue">
          <Icon name="chevdown" :size="14" />
          <span>Queue</span>
        </button>
      </div>

      <div ref="queuePaneEl" class="nps-pane-queue">
        <QueuePane />
      </div>
    </div>
  </AppSheet>
</template>

<script setup lang="ts">
import { useQuery } from '@pinia/colada'
import { DrawerTitle } from 'reka-ui'
import { trackLyricsQuery } from '~/queries/music'

const open = defineModel<boolean>('open', { default: false })

const {
  currentTrack, playing, position, duration,
  shuffled, repeatMode,
  togglePlay, seek, stop,
  toggleShuffle, cycleRepeat, nextTrack, prevTrack, formatTime,
} = usePlayerBindings()

// --- Links (mirrors Playbar.vue's artistTo/albumTo computeds) --------------
const artistTo = computed(() =>
  currentTrack.value?.artist_slug ? `/music/artist/${currentTrack.value.artist_slug}` : null)
const albumTo = computed(() =>
  currentTrack.value?.artist_slug && currentTrack.value?.album_slug
    ? `/music/artist/${currentTrack.value.artist_slug}/${currentTrack.value.album_slug}`
    : null)

// --- Seek (waveform scrubber) ------------------------------------------
// Reactive waveform peaks for the current track — same facet source the
// desktop Playbar uses; resolves to null for un-analysed tracks (MusicWaveform
// then draws a flat bar). MusicWaveform seeks live on pointerdown/drag and
// player.seek() writes position.value synchronously, so the played fill tracks
// the finger without a separate drag-local value.
const facetTrackId = computed<number | null>(() => currentTrack.value?.id ?? null)
const { waveform } = useTrackFacets(facetTrackId)
const waveProgress = computed(() => duration.value > 0 ? position.value / duration.value : 0)
function onWaveformSeek(pct: number) {
  if (!currentTrack.value) return
  seek(pct)
}

// --- Long-press play/pause = stop (the phone equivalent of Playbar's 3s
// hold-to-stop "panic" gesture, shortened since there's no ring/arm staging
// here — just a flat 650ms hold). `holdFired` suppresses the trailing click
// so releasing after the hold fired doesn't also toggle play/pause. ---------
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
  // fires pointerleave and cancels the hold (same trick as Playbar.vue).
  ;(e.currentTarget as Element).releasePointerCapture?.(e.pointerId)
  holdFired = false
  clearPlayHold()
  holdTimer = setTimeout(() => {
    holdFired = true
    holdTimer = null
    navigator.vibrate?.(35)
    // Close the sheet before tearing the player down. stop() clears
    // currentTrack, which flips MobilePlayerHost's `v-if="isPhone &&
    // currentTrack"` and force-unmounts this whole subtree — closing first
    // (and yielding a tick) lets reka's drawer-close bookkeeping (the
    // DismissableLayer watcher that restores `body.style.pointerEvents`)
    // run on a live component instead of racing the unmount.
    open.value = false
    nextTick(() => stop())
  }, HOLD_MS)
}
function onPlayClick() {
  if (holdFired) { holdFired = false; return } // long-press already handled it
  togglePlay()
}
onScopeDispose(() => clearPlayHold())

// --- Visualizer cycling (tap artwork) ---------------------------------
// Cycles art -> milkdrop -> bars -> scope -> vu -> starfield -> art. Persisted
// so the choice survives closing/reopening the sheet and reloading the page.
// Milkdrop is mounted with v-if (not v-show) so its WebGL context actually
// tears down the instant it's cycled away, rather than idling offscreen.
// Parity with the desktop VisualizerFullscreen mode set (useVisualizer.VisMode).
type VisualMode = 'art' | 'milkdrop' | 'bars' | 'scope' | 'vu' | 'starfield'
const VISUAL_MODES: VisualMode[] = ['art', 'milkdrop', 'bars', 'scope', 'vu', 'starfield']
const VISUAL_LABELS: Record<VisualMode, string> = {
  art: 'Album Art',
  milkdrop: 'Milkdrop',
  bars: 'Spectrum',
  scope: 'Scope',
  vu: 'VU Meter',
  starfield: 'Starfield',
}
const visualMode = useLocalStorage<VisualMode>('heya_np_visual_v1', 'art')
// The direct-element engine (iOS compatibility mode, see
// engine/directEngine.ts) has no AnalyserNode — milkdrop and the canvas
// spectrum/scope/VU modes all read one. Render 'art' regardless of what a
// prior desktop session persisted, without clobbering that preference (it
// applies again once this device is back on the graph engine).
const engine = useAudioEngine()
const effectiveVisualMode = computed<VisualMode>(() => engine.directMode ? 'art' : visualMode.value)
const spectrumVariant = computed<'bars' | 'scope' | 'vu'>(() =>
  effectiveVisualMode.value === 'scope' || effectiveVisualMode.value === 'vu' ? effectiveVisualMode.value : 'bars')
const visualModeLabel = computed(() => VISUAL_LABELS[effectiveVisualMode.value])

const visualToastVisible = ref(false)
let visualToastTimer: ReturnType<typeof setTimeout> | null = null
function cycleVisual() {
  // No AnalyserNode to feed any of the other modes — collapse the cycle to
  // art-only (tap does nothing). Lyrics is a separate button/toggle and
  // stays fully functional.
  if (engine.directMode) return
  const i = VISUAL_MODES.indexOf(visualMode.value)
  visualMode.value = VISUAL_MODES[(i + 1) % VISUAL_MODES.length]!
  visualToastVisible.value = true
  if (visualToastTimer) clearTimeout(visualToastTimer)
  visualToastTimer = setTimeout(() => { visualToastVisible.value = false }, 900)
}
onScopeDispose(() => { if (visualToastTimer) clearTimeout(visualToastTimer) })

// --- Queue pane (merged in below the now-playing UI) -----------------------
// The queue button no longer opens a second stacked sheet — it smooth-scrolls
// the queue section of the shared `.nps-scroll` container into view.
const queuePaneEl = ref<HTMLElement | null>(null)
function scrollToQueue() {
  queuePaneEl.value?.scrollIntoView({ behavior: 'smooth' })
}

// --- Lyrics (inline, replaces the artwork area when toggled on) -----------
// Same fetch/sync shape as QueuePanel.vue's lyrics tab, simplified: no
// timing-offset slider, no click-to-seek — just a scrollable read view with
// the current line highlighted when synced timing data exists.
const showLyrics = ref(false)
const lyricRefs = ref<Array<HTMLElement | null>>([])
const lyricsScrollEl = ref<HTMLElement | null>(null)
const lyricTrackId = computed(() => currentTrack.value?.id && currentTrack.value.id > 0 ? currentTrack.value.id : 0)
const lyricsQuery = useQuery(() => ({
  ...trackLyricsQuery(lyricTrackId.value),
  enabled: showLyrics.value && lyricTrackId.value > 0,
}))
const lyrics = computed(() => lyricsQuery.data.value ?? null)
const lyricsLoading = computed(() => lyricsQuery.isPending.value && lyricTrackId.value > 0)

function bindLyricRef(el: HTMLElement | null, i: number) {
  lyricRefs.value[i] = el
}

watch(lyricTrackId, () => { lyricRefs.value = [] })

const activeLyricIdx = computed(() => {
  const list = lyrics.value?.lines
  if (!lyrics.value?.synced || !list || !list.length) return -1
  const posMs = position.value * 1000
  let lo = 0, hi = list.length - 1, ans = -1
  while (lo <= hi) {
    const mid = (lo + hi) >> 1
    if ((list[mid]?.time_ms ?? -1) <= posMs) { ans = mid; lo = mid + 1 }
    else hi = mid - 1
  }
  return ans
})

watch(activeLyricIdx, (i) => {
  if (i < 0) return
  lyricRefs.value[i]?.scrollIntoView({ block: 'center', behavior: 'smooth' })
})
</script>

<!--
  AppSheet portals its content to <body>, so anything here that needs to
  style content rendered inside it must be unscoped (docs/ui.md gotcha #2).
-->
<style>
/* Single scroll container for the whole sheet: the now-playing pane and the
   queue pane are its two scroll-snap children. `proximity` (not `mandatory`)
   is deliberate — it gives a slight "settle" resistance at the pane boundary
   without fighting free scrolling deep inside a long queue, which a
   `mandatory` snap would yank at on every scroll tick. */
.nps-scroll {
  height: 100%;
  overflow-y: auto;
  scroll-snap-type: y proximity;
  overscroll-behavior: contain;
}

/* Explicit close affordance — swipe-down/backdrop-tap dismiss the sheet but
   have no discoverable/keyboard equivalent, so this header slot (replacing
   AppSheet's default title-only header) adds a real, focusable close
   button. Reuses .app-sheet-header/.app-sheet-title for identical chrome,
   just in a flex row with the button pinned to the end. */
.nps-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.nps-header .app-sheet-title { margin: 0; }
.nps-close {
  flex-shrink: 0;
  width: 32px;
  height: 32px;
  border-radius: 50%;
  background: transparent;
  border: 0;
  color: var(--fg-2);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}
.nps-close:active { background: rgb(var(--ink) / 0.08); }

.nps-body {
  display: flex;
  flex-direction: column;
  min-height: 0;
  gap: 18px;
}
.nps-pane-np {
  height: 100%;
  scroll-snap-align: start;
  scroll-snap-stop: always;
}
.nps-pane-queue {
  min-height: 100%;
  scroll-snap-align: start;
}

.nps-visual {
  flex: 1;
  min-height: 0;
  display: flex;
  align-items: center;
  justify-content: center;
}
.nps-art-wrap {
  width: 100%;
  display: flex;
  justify-content: center;
  position: relative;
  cursor: pointer;
  -webkit-tap-highlight-color: transparent;
}
.nps-art {
  width: min(70vw, 360px);
  max-width: 100%;
  border-radius: var(--r-lg);
  box-shadow: var(--shadow-3);
}
.nps-viz-wrap {
  position: relative;
  width: min(70vw, 360px);
  aspect-ratio: 1 / 1;
  max-width: 100%;
  border-radius: var(--r-lg);
  overflow: hidden;
  box-shadow: var(--shadow-3);
  background: var(--bg-2);
}
.nps-viz-toast {
  position: absolute;
  bottom: 12px;
  left: 50%;
  transform: translateX(-50%);
  padding: 4px 12px;
  border-radius: 999px;
  background: rgba(0, 0, 0, 0.6); /* badge painted over the art/visualizer — stays literal */
  color: var(--fg-0);
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  pointer-events: none;
  white-space: nowrap;
}
.nps-viz-toast-fade-enter-active { transition: opacity 0.15s ease; }
.nps-viz-toast-fade-leave-active { transition: opacity 0.6s ease; }
.nps-viz-toast-fade-enter-from,
.nps-viz-toast-fade-leave-to { opacity: 0; }

.nps-lyrics {
  width: 100%;
  height: 100%;
  overflow-y: auto;
  padding: 8px 4px;
  text-align: center;
  -webkit-mask-image: linear-gradient(180deg, transparent 0%, #000 10%, #000 90%, transparent 100%);
          mask-image: linear-gradient(180deg, transparent 0%, #000 10%, #000 90%, transparent 100%);
}
.nps-lyric-line {
  margin: 0;
  padding: 8px 12px;
  font-size: 18px;
  font-weight: 600;
  line-height: 1.5;
  color: var(--fg-3);
  transition: color 0.25s ease, transform 0.25s ease;
}
.nps-lyric-line.past { color: var(--fg-3); opacity: 0.55; }
.nps-lyric-line.active { color: var(--gold); transform: scale(1.03); }
.nps-lyric-line.unsynced { font-size: 15px; color: var(--fg-1); }
.nps-lyrics-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: var(--fg-3);
  font-size: 14px;
}

.nps-meta { text-align: center; }
.nps-title {
  display: block;
  font-size: 19px;
  font-weight: 700;
  color: var(--fg-0);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-decoration: none;
}
.nps-artist {
  margin-top: 4px;
  font-size: 14px;
  color: var(--fg-2);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-decoration: none;
}
.nps-link { cursor: pointer; }
.nps-link:hover, .nps-link:active { color: var(--gold); }

.nps-seek { display: flex; align-items: center; gap: 10px; }
/* Targets MusicWaveform's `.wf-wrap` root (the class merges onto it); this is
   an unscoped block, so it reaches the child-component root fine. */
.nps-waveform { flex: 1; min-width: 0; }
.nps-time {
  font-size: 11px;
  font-family: var(--font-mono);
  color: var(--fg-3);
  min-width: 34px;
  text-align: center;
  flex-shrink: 0;
}

.nps-transport {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
}
.nps-icon {
  width: 44px;
  height: 44px;
  border-radius: 50%;
  background: transparent;
  border: 0;
  color: var(--fg-1);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  position: relative;
  cursor: pointer;
}
.nps-icon:active { background: rgb(var(--ink) / 0.08); }
.nps-icon.active { color: var(--gold); }
.nps-play {
  width: 64px;
  height: 64px;
  border-radius: 50%;
  background: var(--gold);
  color: var(--bg-0);
  border: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  box-shadow: 0 10px 24px var(--gold-glow);
  user-select: none;
  -webkit-touch-callout: none;
}
.nps-play:active { background: var(--gold-bright); }
.nps-repeat-badge {
  position: absolute;
  bottom: 4px;
  right: 4px;
  font-size: 8px;
  font-weight: 700;
  color: var(--gold);
  font-family: var(--font-mono);
}

.nps-secondary {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding-bottom: 4px;
}
.nps-sicon {
  width: 44px;
  height: 44px;
  border-radius: 50%;
  background: transparent;
  border: 0;
  color: var(--fg-2);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}
.nps-sicon:active { background: rgb(var(--ink) / 0.08); }
.nps-sicon.active { color: var(--gold); }

/* Bottom-of-pane hint that the queue lives one swipe below. */
.nps-queue-hint {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  margin: 0 auto;
  padding: 2px 10px 4px;
  background: transparent;
  border: 0;
  color: var(--fg-3);
  font-size: 11px;
  letter-spacing: 0.03em;
  cursor: pointer;
  opacity: 0.7;
}
.nps-queue-hint:active { opacity: 1; color: var(--gold); }
</style>
