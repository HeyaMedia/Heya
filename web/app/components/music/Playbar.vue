<template>
  <footer class="playbar">
    <!-- Left: now playing (idle placeholder when nothing is loaded). When the
         cover is folded out into the sidebar, the small art hides and the text
         shifts right to sit beside the big cover. -->
    <div class="pb-left" :class="{ 'pb-left-expanded': coverExpanded && !!currentTrack }">
      <template v-if="currentTrack">
        <!-- Artwork: clicking navigates to the album; hover reveals the full-
             image and fold-out-into-sidebar actions. Hidden while folded out. -->
        <div v-show="!coverExpanded" class="pb-cover-wrap">
          <NuxtLink v-if="albumTo" :to="albumTo" class="pb-cover-btn" aria-label="Go to album">
            <Poster :idx="currentTrack.id" :src="currentTrack.poster" aspect="1/1" class="pb-cover-img" />
          </NuxtLink>
          <div v-else class="pb-cover-btn">
            <Poster :idx="currentTrack.id" :src="currentTrack.poster" aspect="1/1" class="pb-cover-img" />
          </div>
          <div class="pb-cover-actions">
            <AppTooltip label="Show full image">
              <button class="pb-cover-action" @click.prevent.stop="openCoverLightbox"><Icon name="expand" :size="13" /></button>
            </AppTooltip>
            <AppTooltip label="Fold out into sidebar">
              <button class="pb-cover-action" @click.prevent.stop="toggleCoverExpanded"><Icon name="grid" :size="13" /></button>
            </AppTooltip>
          </div>
        </div>
        <div class="pb-info">
          <NuxtLink v-if="albumTo" :to="albumTo" class="pb-title pb-link">{{ currentTrack.title }}</NuxtLink>
          <div v-else class="pb-title">{{ currentTrack.title }}</div>
          <div class="pb-artist">
            <NuxtLink v-if="artistTo" :to="artistTo" class="pb-link">{{ currentTrack.artist }}</NuxtLink>
            <span v-else>{{ currentTrack.artist }}</span>
            <span class="pb-dash"> — </span>
            <NuxtLink v-if="albumTo" :to="albumTo" class="pb-link">{{ currentTrack.album }}</NuxtLink>
            <span v-else>{{ currentTrack.album }}</span>
          </div>
        </div>
        <AppMenu>
          <template #trigger>
            <button class="btn-icon" title="Add to playlist"><Icon name="plus" :size="16" /></button>
          </template>
          <DropdownMenuItem
            v-for="p in playlistsApi.playlists.value"
            :key="p.id"
            class="surface-item app-context-item"
            @select="addCurrentToPlaylist(p.id)"
          >
            <Icon name="list" :size="14" class="surface-item-icon" />
            <span>{{ p.name }}</span>
          </DropdownMenuItem>
          <DropdownMenuSeparator class="surface-divider" />
          <DropdownMenuItem
            class="surface-item app-context-item"
            @select="createPlaylistFromCurrent"
          >
            <Icon name="plus" :size="14" class="surface-item-icon" />
            <span>New playlist…</span>
          </DropdownMenuItem>
        </AppMenu>
      </template>
      <template v-else>
        <div class="pb-cover-placeholder"><Icon name="music" :size="22" /></div>
        <div class="pb-info">
          <div class="pb-title pb-idle-title">Nothing playing</div>
          <div class="pb-artist">Queue a track to start</div>
        </div>
      </template>
    </div>

    <!-- Center: controls + scrubber -->
    <div class="pb-center">
      <div class="pb-controls">
        <!-- Rating floats to the LEFT of the transport cluster (absolute, out of
             the flex flow) so the play button stays dead-centered — muscle
             memory. Mirrors the quality readout that floats to the right. -->
        <div v-if="currentTrack" class="pb-rate-slot" @click.stop>
          <StarRating
            :model-value="ratings.get(currentTrack.id) ?? 0"
            size="sm"
            @update:model-value="(v) => onRate(currentTrack!.id, v)"
          />
        </div>
        <AppTooltip :label="shuffled ? 'Shuffle on' : 'Shuffle'">
          <button class="btn-icon" :class="{ active: shuffled }" @click="toggleShuffle">
            <Icon name="shuffle" :size="16" />
          </button>
        </AppTooltip>
        <AppTooltip label="Previous">
          <button class="btn-icon" :disabled="!currentTrack" @click="prevTrack"><Icon name="prev" :size="16" /></button>
        </AppTooltip>
        <AppTooltip label="Hold to stop &amp; clear queue">
          <button
            class="pb-play"
            :class="{ 'pb-play-pressing': pressing, 'pb-play-armed': holdArmed }"
            :disabled="!currentTrack"
            @click="onPlayClick"
            @pointerdown="onPlayPointerDown"
            @pointerup="cancelHold"
            @pointerleave="cancelHold"
            @pointercancel="cancelHold"
          >
            <Transition name="pb-play-icon" mode="out-in">
              <Icon
                :key="holdArmed ? 'stop' : 'toggle'"
                :name="holdArmed ? 'stop' : (playing ? 'pause' : 'play')"
                :weight="holdArmed ? 'fill' : undefined"
                :size="20"
              />
            </Transition>
            <Transition name="pb-play-ring">
              <svg v-if="holdArmed" class="pb-play-ring" viewBox="0 0 36 36" aria-hidden="true">
                <circle class="pb-play-ring-track" cx="18" cy="18" r="16" />
                <circle class="pb-play-ring-fill" cx="18" cy="18" r="16" />
              </svg>
            </Transition>
          </button>
        </AppTooltip>
        <AppTooltip label="Next">
          <button class="btn-icon" :disabled="!currentTrack" @click="nextTrack"><Icon name="next" :size="16" /></button>
        </AppTooltip>
        <AppTooltip :label="repeatLabel">
          <button class="btn-icon" :class="{ active: repeatMode !== 'off' }" @click="cycleRepeat" style="position: relative">
            <Icon name="repeat" :size="16" />
            <span v-if="repeatMode === 'one'" class="repeat-badge">1</span>
          </button>
        </AppTooltip>
        <!-- Quality readout floats to the right of the transport so the play
             button stays dead-centered (absolute → out of the flex flow). -->
        <div v-if="currentTrack && !currentTrack.isStream && currentTrack.id > 0" class="pb-quality-slot">
          <PlaybarQuality :key="currentTrack.id" :track-id="currentTrack.id" />
        </div>
      </div>
      <div class="pb-scrubber" :class="{ 'pb-scrubber-idle': !currentTrack }">
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
      <AppTooltip label="Lyrics">
        <button class="btn-icon" :class="{ active: queueOpen && sideTab === 'lyrics' }" @click="toggleLyrics"><Icon name="lyrics" :size="16" /></button>
      </AppTooltip>
      <AppTooltip label="Queue">
        <button class="btn-icon" :class="{ active: queueOpen && sideTab === 'queue' }" @click="toggleQueue"><Icon name="queue" :size="16" /></button>
      </AppTooltip>
      <AppTooltip label="Equalizer">
        <button class="btn-icon" :class="{ active: eqOpen }" @click="eqOpen = !eqOpen"><Icon name="eq" :size="16" /></button>
      </AppTooltip>
      <AppTooltip label="Visualizer">
        <button class="pb-viz-btn" :class="{ active: vis.fullscreenOpen.value }" @click="vis.fullscreenOpen.value = true">
          <VisualizerSpectrum variant="mini" :active="playing" class="pb-viz-meter" />
        </button>
      </AppTooltip>
      <SleepTimer />
      <div class="pb-volume" @wheel.prevent="onVolumeWheel">
        <AppTooltip :label="muted ? 'Unmute' : 'Mute'">
          <button class="btn-icon" @click="toggleMute">
            <Icon :name="muted || volume === 0 ? 'volmute' : 'vol'" :size="16" />
          </button>
        </AppTooltip>
        <AppSlider
          :model-value="muted ? 0 : volume"
          :min="0"
          :max="100"
          :step="1"
          aria-label="Volume"
          class="pb-volume-slider"
          @update:model-value="onVolumeChange"
        />
      </div>
      <AppTooltip label="Expand">
        <button class="btn-icon" @click="nowPlayingOpen = !nowPlayingOpen">
          <Icon name="expand" :size="16" />
        </button>
      </AppTooltip>
    </div>
  </footer>
  <NowPlayingView :open="nowPlayingOpen" @close="nowPlayingOpen = false" />
</template>

<script setup lang="ts">
const {
  playing, currentTrack, position, duration, volume, muted,
  shuffled, repeatMode, queueOpen, sideTab,
  togglePlay, seek, setVolume, toggleMute, toggleShuffle,
  cycleRepeat, nextTrack, prevTrack, stop,
  toggleQueue, toggleLyrics, formatTime,
} = usePlayer()

// Visualizer overlay toggle (fullscreen host is mounted at the shell level).
const vis = useVisualizer()

// --- Left-zone navigation + artwork actions --------------------------------
// The now-playing art/title/artist/album navigate rather than opening the
// fullscreen view (that now launches from the lyrics side view). Links are
// gated on the slugs being present so radio/podcast rows degrade to plain text.
const artistTo = computed(() =>
  currentTrack.value?.artist_slug ? `/music/artist/${currentTrack.value.artist_slug}` : null)
const albumTo = computed(() =>
  currentTrack.value?.artist_slug && currentTrack.value?.album_slug
    ? `/music/artist/${currentTrack.value.artist_slug}/${currentTrack.value.album_slug}`
    : null)

// Full cover art in the shared lightbox (same overlay TV/movies/artists use).
const lightbox = useLightbox()
function openCoverLightbox() {
  const src = currentTrack.value?.poster
  if (src) lightbox.open(src)
}

// Fold-out cover: a big square that grows into the sidebar column above the
// playbar. Shared state so the sidebar (reserves space) and the shell (renders
// the big cover) react to it. See MusicBigCover / MusicSidebar.
const coverExpanded = useState('music_cover_expanded', () => false)
function toggleCoverExpanded() { coverExpanded.value = !coverExpanded.value }

import { DropdownMenuItem, DropdownMenuSeparator } from 'reka-ui'

const trackRatings = useTrackRatings()
const ratings = trackRatings.ratings
const playlistsApi = usePlaylists()
if (import.meta.client) playlistsApi.ensureLoaded()

async function addCurrentToPlaylist(playlistId: number) {
  const t = currentTrack.value
  if (!t || t.id <= 0) return
  try { await playlistsApi.addTrack(playlistId, t.id) } catch { /* swallow */ }
}

async function createPlaylistFromCurrent() {
  const t = currentTrack.value
  if (!t || t.id <= 0) return
  const name = prompt('New playlist name', t.title)
  if (!name) return
  try {
    const created = await playlistsApi.create(name, '')
    await playlistsApi.addTrack(created.id, t.id)
    navigateTo(`/music/playlist/${created.id}`)
  } catch { /* swallow */ }
}

// Prime the rating of the currently-playing track so the playbar shows
// the right star count on first paint (rather than starting empty and
// jumping after the fetch).
watch(currentTrack, (t) => {
  if (t?.id && t.id > 0) trackRatings.load(t.id).catch(() => 0)
}, { immediate: true })

async function onRate(trackId: number, v: number) {
  try { await trackRatings.set(trackId, v) } catch { /* rollback handled */ }
}

// Now-playing overlay state. Kept locally — the playbar is the single
// mount point for the overlay so we don't need a global state slot.
const nowPlayingOpen = useState('now_playing_open', () => false)
// EQ panel state mirrors the one in the music shell so the playbar can
// toggle it without prop drilling.
const eqOpen = useState('music_eq_open', () => false)

const scrubPct = computed(() => duration.value > 0 ? (position.value / duration.value) * 100 : 0)

const repeatLabel = computed(() => {
  if (repeatMode.value === 'all') return 'Repeat queue'
  if (repeatMode.value === 'one') return 'Repeat track'
  return 'Repeat'
})

// Reactive waveform fetch keyed on the current track. Resolves to
// null for tracks that haven't been analyzed yet — MusicWaveform
// falls back to a plain neutral bar in that case.
const trackId = computed<number | null>(() => currentTrack.value?.id ?? null)
const { waveform } = useTrackFacets(trackId)

function onWaveformSeek(pct: number) {
  if (!currentTrack.value) return // idle scrubber is inert (see .pb-scrubber-idle)
  seek(pct)
}

// Reka Slider emits the new value as a number; piped straight to the
// player. If the user drags from 0 while muted, also unmute — same
// behaviour as the old rail (clicking on it implicitly unmuted).
function onVolumeChange(v: number) {
  if (muted.value && v > 0) toggleMute()
  setVolume(v)
}

// Scroll anywhere over the volume control to nudge it ±5. Up = louder.
// `.prevent` on the handler stops the page from scrolling under the cursor.
// Base the delta on the REAL stored level (not the muted-display 0) so a
// scroll while muted adjusts from — and restores — the remembered volume
// rather than snapping to ±5 and losing it.
function onVolumeWheel(e: WheelEvent) {
  const next = Math.max(0, Math.min(100, volume.value + (e.deltaY < 0 ? 5 : -5)))
  onVolumeChange(next)
}

// Play/pause is a tap; holding it is the "panic" gesture — stop playback and
// clear the whole queue. The full hold is 3s. For the first second nothing
// visible changes (so a slightly-too-long click doesn't look alarming); at 1s
// the button flips to a stop icon and a red ring begins closing clockwise from
// the top, completing exactly as the 3s mark fires stop(). `holdFired`
// suppresses the trailing `click` so the release doesn't also toggle play.
const HOLD_MS = 3000
const RING_DELAY_MS = 1000
const pressing = ref(false)  // pointer is down on the button
const holdArmed = ref(false) // past the 1s mark: stop icon + closing ring shown
let holdTimer: ReturnType<typeof setTimeout> | null = null
let armTimer: ReturnType<typeof setTimeout> | null = null
let holdFired = false

function cancelHold() {
  // If the hold had already armed (stop icon + ring were showing), releasing
  // early is a deliberate bail-out — swallow the trailing click so it doesn't
  // fall through to a play/pause toggle. A release before arming is just a
  // slightly-long tap and toggles as normal.
  if (holdArmed.value) holdFired = true
  pressing.value = false
  holdArmed.value = false
  if (holdTimer) { clearTimeout(holdTimer); holdTimer = null }
  if (armTimer) { clearTimeout(armTimer); armTimer = null }
}
function onPlayPointerDown(e: PointerEvent) {
  if (e.button !== 0) return // primary button / touch only
  // Touch pointers get implicit pointer capture on pointerdown, which
  // suppresses pointerleave — so "slide finger off to bail" would silently not
  // work. Release capture so leaving the button fires pointerleave → cancelHold.
  ;(e.currentTarget as Element).releasePointerCapture?.(e.pointerId)
  holdFired = false
  cancelHold()
  pressing.value = true
  armTimer = setTimeout(() => { holdArmed.value = true; armTimer = null }, RING_DELAY_MS)
  holdTimer = setTimeout(() => {
    holdFired = true
    holdTimer = null
    cancelHold()
    stop()
  }, HOLD_MS)
}
function onPlayClick() {
  if (holdFired) { holdFired = false; return } // long-press already handled it
  togglePlay()
}

// After a full hold-to-stop the button is disabled (no track), so the trailing
// click that normally clears holdFired never fires — leaving it stranded true
// would swallow the next keyboard-activated play. Clear it when the deck empties.
watch(currentTrack, (t) => { if (!t) holdFired = false })

// Timers are otherwise only cleared through pointer handlers; if the component
// unmounts mid-hold (e.g. a programmatic route out of /music while pressed),
// the orphaned holdTimer would fire stop() on a dead component. Clear on teardown.
onScopeDispose(() => {
  if (holdTimer) clearTimeout(holdTimer)
  if (armTimer) clearTimeout(armTimer)
})
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
.pb-left { display: flex; align-items: center; gap: 12px; transition: padding-left 0.28s ease; }
/* When the cover folds out into the sidebar, the big art occupies the corner
   (left:8px, width: sidebar-w − 16px → right edge at sidebar-w − 8px); shove the
   text past it (+ ~12px gap) so it doesn't sit under the enlarged cover. */
.pb-left-expanded { padding-left: calc(var(--music-sidebar-w) + 4px); }

.pb-cover-wrap { position: relative; width: 56px; height: 56px; flex-shrink: 0; border-radius: 6px; }
.pb-cover-btn { display: block; width: 100%; height: 100%; background: transparent; border: 0; padding: 0; cursor: pointer; }
.pb-cover-img { width: 56px; height: 56px; border-radius: 6px; }
/* Hover affordances over the art: full-image + fold-out. */
.pb-cover-actions {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  border-radius: 6px;
  background: rgba(0, 0, 0, 0.55);
  opacity: 0;
  transition: opacity 0.15s ease;
}
.pb-cover-wrap:hover .pb-cover-actions { opacity: 1; }
.pb-cover-action {
  width: 24px; height: 24px;
  display: flex; align-items: center; justify-content: center;
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.12);
  border: 0;
  color: #fff;
  cursor: pointer;
  transition: background 0.12s;
}
.pb-cover-action:hover { background: var(--gold); color: var(--bg-0); }

.pb-info { flex: 1; min-width: 0; margin-right: 8px; }
.pb-title { display: block; font-size: 13px; font-weight: 500; color: var(--fg-0); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; text-decoration: none; }
.pb-artist { font-size: 11px; color: var(--fg-2); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.pb-dash { color: var(--fg-3); }
/* Clickable title/artist/album — underline on hover, gold accent. */
.pb-link { color: inherit; text-decoration: none; cursor: pointer; transition: color 0.12s; }
.pb-link:hover { color: var(--gold); text-decoration: underline; }
.pb-center { display: flex; flex-direction: column; align-items: center; gap: 6px; }
.pb-controls { display: flex; align-items: center; gap: 8px; position: relative; }
.pb-quality-slot { position: absolute; left: 100%; margin-left: 14px; top: 50%; transform: translateY(-50%); }
/* Rating floats left of the centered transport cluster so the play button
   never moves. Mirror of .pb-quality-slot on the right. */
.pb-rate-slot { position: absolute; right: 100%; margin-right: 14px; top: 50%; transform: translateY(-50%); display: flex; align-items: center; white-space: nowrap; }
/* Idle scrubber: keep it visible but inert (no cursor, no hover ghost, no seek). */
.pb-scrubber-idle :deep(.wf-wrap) { pointer-events: none; cursor: default; }
.pb-play {
  width: 36px; height: 36px;
  border-radius: 50%;
  background: var(--fg-0);
  color: var(--bg-0);
  display: flex; align-items: center; justify-content: center;
  /* background + color use the slower 0.3s so the armed red field eases back
     to the resting button after stop() fires, matching the icon/ring fades.
     opacity too, so it dims gently to the idle/disabled state after a stop. */
  transition: transform 0.15s ease, background 0.3s ease, color 0.3s ease, opacity 0.3s ease;
  position: relative;
}
.pb-play:hover { transform: scale(1.06); background: var(--gold); }
/* Idle (no track): the transport is inert. Dim it and drop the hover pop. */
.pb-play:disabled { opacity: 0.4; cursor: default; }
.pb-play:disabled:hover { transform: none; background: var(--fg-0); }
.pb-controls .btn-icon:disabled { opacity: 0.3; cursor: default; }
/* Left-side idle placeholder standing in for the cover + track text. */
.pb-cover-placeholder {
  width: 56px; height: 56px;
  border-radius: 6px;
  flex-shrink: 0;
  display: flex; align-items: center; justify-content: center;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid var(--border);
  color: var(--fg-3);
}
.pb-idle-title { color: var(--fg-2); }
/* First second of the hold: a subtle press-in so the gesture registers.
   Compound selector to out-specify (and out-order) the :hover scale. */
.pb-play.pb-play-pressing { transform: scale(0.95); }
/* Past the 1s mark: the button becomes a stop control on a dark field so the
   red icon + red ring read as an alert. */
.pb-play.pb-play-armed {
  transform: scale(1);
  background: #2a1416;
  color: #ff5b5b;
}
/* The ring overlays the button edge and closes clockwise from 12 o'clock over
   the remaining 2s (rotate(-90deg) puts the stroke's start point at the top;
   dashoffset animates C→0 to fill it in). It completes exactly as stop() fires. */
.pb-play-ring {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  transform: rotate(-90deg);
  transform-origin: center;
  pointer-events: none;
  overflow: visible;
}
.pb-play-ring-track {
  fill: none;
  stroke: rgba(255, 91, 91, 0.18);
  stroke-width: 3;
}
.pb-play-ring-fill {
  fill: none;
  stroke: #ff5b5b;
  stroke-width: 3;
  stroke-linecap: round;
  stroke-dasharray: 100.53;
  stroke-dashoffset: 100.53;
  animation: pb-ring-close 2s linear forwards;
}
@keyframes pb-ring-close {
  to { stroke-dashoffset: 0; }
}
/* Icon cross-fade: play/pause ⇄ stop. `out-in` lets the old glyph clear before
   the new one fades in, so the swap reads as a morph rather than a snap. */
.pb-play-icon-enter-active,
.pb-play-icon-leave-active { transition: opacity 0.18s ease; }
.pb-play-icon-enter-from,
.pb-play-icon-leave-to { opacity: 0; }
/* Ring fade: appears with the stop icon, and on stop()/cancel the full (or
   partial) ring fades out over 0.35s while the field eases back. */
.pb-play-ring-enter-active { transition: opacity 0.15s ease; }
.pb-play-ring-leave-active { transition: opacity 0.35s ease; }
.pb-play-ring-enter-from,
.pb-play-ring-leave-to { opacity: 0; }
.pb-scrubber { display: flex; align-items: center; gap: 10px; width: 100%; }
.pb-time { font-size: 10px; font-family: var(--font-mono); color: var(--fg-3); min-width: 32px; text-align: center; }
.pb-right { display: flex; align-items: center; gap: 4px; justify-content: flex-end; }
.pb-volume { display: flex; align-items: center; gap: 4px; }
/* Compact volume slider — 80px wide, matches the old custom rail width. */
.pb-volume-slider { width: 80px; }
.repeat-badge {
  position: absolute;
  bottom: 2px; right: 2px;
  font-size: 8px; font-weight: 700;
  color: var(--gold);
  font-family: var(--font-mono);
}
/* Live mini spectrum that doubles as the visualizer entry button. */
.pb-viz-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 42px;
  height: 30px;
  padding: 5px 4px;
  border: 1px solid transparent;
  border-radius: 6px;
  background: transparent;
  cursor: pointer;
  transition: background 0.15s, border-color 0.15s;
}
.pb-viz-btn:hover { background: rgba(255, 255, 255, 0.06); border-color: var(--border); }
.pb-viz-btn.active { border-color: rgba(230, 185, 74, 0.4); background: var(--gold-soft); }
.pb-viz-meter { width: 34px; height: 20px; }
</style>
