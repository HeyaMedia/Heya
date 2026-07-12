<template>
  <footer class="playbar" :style="pbToneStyle">
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
            <!-- Folds into the compact-band sidebar, which is hidden in that band
                 (parallel crew) — CSS drops this affordance there too. -->
            <AppTooltip label="Fold out into sidebar">
              <button class="pb-cover-action pb-cover-action-fold" @click.prevent.stop="toggleCoverExpanded"><Icon name="grid" :size="13" /></button>
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
        <div v-if="currentTrack && !isCompact" class="pb-rate-slot" @click.stop>
          <ReactionControl
            :model-value="ratings.get(currentTrack.id) ?? 0"
            size="sm"
            @update:model-value="(v) => onRate(currentTrack!.id, v)"
          />
        </div>
        <AppTooltip :label="shuffled ? 'Shuffle on' : 'Shuffle'">
          <button class="btn-icon" :class="{ active: shuffled }" :aria-pressed="shuffled" @click="toggleShuffle">
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
          <button class="btn-icon" :class="{ active: repeatMode !== 'off' }" :aria-pressed="repeatMode !== 'off'" @click="cycleRepeat" style="position: relative">
            <Icon name="repeat" :size="16" />
            <span v-if="repeatMode === 'one'" class="repeat-badge">1</span>
          </button>
        </AppTooltip>
        <!-- Quality readout floats to the right of the transport so the play
             button stays dead-centered (absolute → out of the flex flow). -->
        <div v-if="currentTrack && !currentTrack.isStream && currentTrack.id > 0 && !isCompact" class="pb-quality-slot">
          <PlaybarQuality :key="currentTrack.id" :track-id="currentTrack.id" />
        </div>
      </div>
      <div class="pb-scrubber" :class="{ 'pb-scrubber-idle': !currentTrack }">
        <span class="pb-time">{{ formatTime(position) }}</span>
        <MusicWaveform
          :peaks="waveform"
          :progress="scrubPct / 100"
          :accent="waveAccent"
          @seek="onWaveformSeek"
        />
        <span class="pb-time">{{ formatTime(duration) }}</span>
      </div>
    </div>

    <!-- Right: queue, vol, etc. Compact band (720.02-1200px) can't fit the
         full cluster (lyrics/eq/viz/sleep/volume squash or clip past ~977px
         of viewport) — it keeps queue + expand + a ⋯ overflow menu that holds
         everything else. v-if/v-else (not CSS-hiding) so components that own
         mounted side effects (SleepTimer's 1Hz interval, PlaybarQuality's
         popover state) never double-mount. -->
    <div v-if="!isCompact" class="pb-right">
      <AppTooltip label="Lyrics">
        <button class="btn-icon" :class="{ active: queueOpen && sideTab === 'lyrics' }" :aria-pressed="queueOpen && sideTab === 'lyrics'" @click="toggleLyrics"><Icon name="lyrics" :size="16" /></button>
      </AppTooltip>
      <AppTooltip label="Queue">
        <button class="btn-icon" :class="{ active: queueOpen && sideTab === 'queue' }" :aria-pressed="queueOpen && sideTab === 'queue'" @click="toggleQueue"><Icon name="queue" :size="16" /></button>
      </AppTooltip>
      <AppTooltip label="Equalizer">
        <button class="btn-icon" :class="{ active: eqOpen }" :aria-pressed="eqOpen" @click="eqOpen = !eqOpen"><Icon name="eq" :size="16" /></button>
      </AppTooltip>
      <AppTooltip label="Visualizer">
        <button class="pb-viz-btn" :class="{ active: vis.fullscreenOpen.value }" :aria-pressed="vis.fullscreenOpen.value" @click="vis.fullscreenOpen.value = true">
          <VisualizerSpectrum variant="mini" :active="playing" class="pb-viz-meter" />
        </button>
      </AppTooltip>
      <SleepTimer />
      <div class="pb-volume" @wheel.prevent="onVolumeWheel">
        <AppTooltip :label="muted ? 'Unmute' : 'Mute'">
          <button class="btn-icon" :aria-pressed="muted" @click="toggleMute">
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
    <div v-else class="pb-right">
      <AppTooltip label="Queue">
        <button class="btn-icon" :class="{ active: queueOpen && sideTab === 'queue' }" @click="toggleQueue"><Icon name="queue" :size="16" /></button>
      </AppTooltip>
      <AppMenu align="end" :width="260" trigger-class="btn-icon" trigger-title="More">
        <template #trigger>
          <Icon name="more" :size="16" />
        </template>

        <!-- Tech-info pill that otherwise floats off the transport controls
             on the wide layout. Nested popover-in-menu verified in Eye. -->
        <template v-if="currentTrack && !currentTrack.isStream && currentTrack.id > 0">
          <div class="surface-section-label pb-more-label">Quality</div>
          <div class="pb-more-row" @click.stop>
            <PlaybarQuality :key="currentTrack.id" :track-id="currentTrack.id" />
          </div>
          <DropdownMenuSeparator class="surface-divider" />
        </template>

        <DropdownMenuItem
          class="surface-item app-context-item pb-more-item"
          :class="{ 'pb-more-active': queueOpen && sideTab === 'lyrics' }"
          @select="toggleLyrics"
        >
          <Icon name="lyrics" :size="15" class="surface-item-icon" />
          <span class="pb-more-item-label">Lyrics</span>
          <Icon v-if="queueOpen && sideTab === 'lyrics'" name="check" :size="13" class="pb-more-check" />
        </DropdownMenuItem>
        <DropdownMenuItem
          class="surface-item app-context-item pb-more-item"
          :class="{ 'pb-more-active': eqOpen }"
          @select="eqOpen = !eqOpen"
        >
          <Icon name="eq" :size="15" class="surface-item-icon" />
          <span class="pb-more-item-label">Equalizer</span>
          <Icon v-if="eqOpen" name="check" :size="13" class="pb-more-check" />
        </DropdownMenuItem>
        <DropdownMenuItem
          class="surface-item app-context-item pb-more-item"
          @select="vis.fullscreenOpen.value = true"
        >
          <Icon name="pulse" :size="15" class="surface-item-icon" />
          <span class="pb-more-item-label">Visualizer</span>
        </DropdownMenuItem>

        <DropdownMenuSeparator class="surface-divider" />

        <!-- Flattened, not <SleepTimer/> nested — see the script comment by
             `chooseSleep` for why nesting its popover doesn't work here. -->
        <div class="surface-section-label pb-more-label">
          Sleep timer<span v-if="sleep.active.value"> — {{ sleepCountdownLabel }}</span>
        </div>
        <DropdownMenuItem
          v-for="opt in SLEEP_OPTIONS"
          :key="opt.label"
          class="surface-item app-context-item pb-more-item"
          :class="{ 'pb-more-active': isSleepActive(opt) }"
          @select="chooseSleep(opt)"
        >
          <span class="pb-more-item-label">{{ opt.label }}</span>
          <Icon v-if="isSleepActive(opt)" name="check" :size="13" class="pb-more-check" />
        </DropdownMenuItem>
        <DropdownMenuItem
          v-if="sleep.active.value"
          class="surface-item app-context-item pb-more-item pb-more-destructive"
          @select="turnOffSleep"
        >
          <span class="pb-more-item-label">Turn off</span>
        </DropdownMenuItem>

        <DropdownMenuSeparator class="surface-divider" />

        <!-- Native range, not <AppSlider/> — verified in Eye that reka's
             SliderThumb interaction closes the parent dropdown out from under
             it the same way the nested sleep-timer popover did (drag/focus
             reads as an outside interaction). A styled native input sidesteps
             reka entirely. -->
        <div class="pb-more-row pb-more-volume-row" @click.stop @wheel.prevent="onVolumeWheel">
          <button class="btn-icon" :class="{ active: muted }" :aria-pressed="muted" @click="toggleMute">
            <Icon :name="muted || volume === 0 ? 'volmute' : 'vol'" :size="16" />
          </button>
          <input
            type="range"
            min="0"
            max="100"
            step="1"
            :value="muted ? 0 : volume"
            :style="{ '--pb-vol': muted ? 0 : volume }"
            aria-label="Volume"
            class="pb-more-volume-range"
            @input="onVolumeChange(Number(($event.target as HTMLInputElement).value))"
          >
          <span class="pb-more-volume-value">{{ muted ? 0 : volume }}</span>
        </div>
      </AppMenu>
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
  cycleRepeat, nextTrack, prevTrack, stop, pause,
  toggleQueue, toggleLyrics, formatTime,
} = usePlayerBindings()

// Visualizer overlay toggle (fullscreen host is mounted at the shell level).
const vis = useVisualizer()

// Tone-follow accent: the playbar adopts the playing album's palette — the
// cover is sampled (same sampleImageTone as the detail-page hero buttons)
// and published as --pb-accent/--pb-accent-ink on the bar. Consumers: the
// play button (0.9s glide between palettes) and the waveform's played bars.
// Falls back to the theme accent when idle or sampling fails. Sequence-
// guarded so a slow sample can't land after the track already changed.
const pbToneStyle = ref<Record<string, string> | undefined>()
let toneSeq = 0
watch(() => currentTrack.value?.poster, (src) => {
  const seq = ++toneSeq
  if (!src) { pbToneStyle.value = undefined; return }
  sampleImageTone(src).then((t) => {
    if (seq !== toneSeq) return
    pbToneStyle.value = t ? { '--pb-accent': t.main, '--pb-accent-ink': t.ink } : undefined
  })
}, { immediate: true })
// Canvas can't read a CSS var transition — hand the resolved color straight
// to the waveform so its next paint uses it.
const waveAccent = computed(() => pbToneStyle.value?.['--pb-accent'] ?? null)

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

// Compact band (720.02-1200px): sidebars hide behind the topbar burger
// (parallel compact-sidebar work), so folding the cover out into a hidden
// sidebar would strand the track with no artwork at all. Drop the affordance
// (CSS, below) and unwind any expanded state carried in from a wider window.
const { isCompact } = useViewport()
watch(isCompact, (compact) => {
  if (compact && coverExpanded.value) coverExpanded.value = false
})

// --- Compact-band ⋯ overflow menu -------------------------------------------
// Sleep timer flattens into plain rows here instead of nesting <SleepTimer/>'s
// own popover: verified in Eye that reka's PopoverContent auto-focuses its
// first focusable row on open, and since that content is teleported outside
// the ⋯ AppMenu's DOM, the focus jump reads as an outside interaction and
// closes the dropdown out from under it. (PlaybarQuality's popover has no
// focusable rows and nests fine — that's the one exception below.) Owns a
// single 1Hz tick, active only while compact; the wide layout's <SleepTimer/>
// instance ticks its own — never run both at once.
const sleep = useSleepTimer()
interface SleepOpt { label: string, minutes?: number, endOfTrack?: boolean }
const SLEEP_OPTIONS: SleepOpt[] = [
  { label: '15 minutes', minutes: 15 },
  { label: '30 minutes', minutes: 30 },
  { label: '45 minutes', minutes: 45 },
  { label: '60 minutes', minutes: 60 },
  { label: 'End of track', endOfTrack: true },
]
function chooseSleep(opt: SleepOpt) {
  if (opt.endOfTrack) sleep.setEndOfTrack()
  else if (opt.minutes) sleep.setMinutes(opt.minutes)
}
function turnOffSleep() { sleep.cancel() }
// A timed option can't be told apart from any other once running (see
// SleepTimer.vue) — only "end of track" has a distinct on/off state.
function isSleepActive(opt: SleepOpt): boolean {
  return !!opt.endOfTrack && sleep.atTrackEnd.value
}
const sleepCountdownLabel = computed(() => {
  if (sleep.atTrackEnd.value) return 'EOT'
  const ms = sleep.remainingMs.value
  if (ms <= 0) return ''
  const total = Math.ceil(ms / 1000)
  const m = Math.floor(total / 60)
  const s = total % 60
  return m >= 1 ? `${m}:${String(s).padStart(2, '0')}` : `${s}s`
})

let sleepTickId: ReturnType<typeof setInterval> | null = null
watch([isCompact, sleep.timed], ([compact, timed]) => {
  if (compact && timed && !sleepTickId) {
    sleepTickId = setInterval(() => sleep.tick(() => pause()), 1000)
  } else if ((!compact || !timed) && sleepTickId) {
    clearInterval(sleepTickId)
    sleepTickId = null
  }
}, { immediate: true })
onScopeDispose(() => {
  if (sleepTickId) clearInterval(sleepTickId)
})

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
    navigateTo(`/music/playlist/${created.slug || created.id}`)
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
  /* Design-system glass over the ambient-backdrop layer. No border-top —
     the upward shadow defines the edge (mirror of FilterBar's downward
     one), keeping the bar and the content plane one continuous surface. */
  background: color-mix(in oklab, var(--bg-2) 78%, transparent);
  backdrop-filter: blur(20px);
  -webkit-backdrop-filter: blur(20px);
  box-shadow: 0 -10px 28px rgb(var(--shade) / 0.14);
  z-index: 40;
}
/* Firefox: backdrop-filter renders seam lines on gradient-adjacent panels
   (same workaround as the sidebars) — solid-enough glass, no blur. */
@supports (-moz-appearance: none) {
  .playbar {
    backdrop-filter: none;
    background: color-mix(in srgb, var(--bg-2) 84%, transparent);
  }
}
.pb-left { display: flex; align-items: center; gap: 12px; transition: padding-left 0.28s ease; }
/* When the cover folds out into the sidebar, the big art occupies the corner
   (left:8px, width: sidebar-w − 16px → right edge at sidebar-w − 8px); shove the
   text past it (+ ~12px gap) so it doesn't sit under the enlarged cover. */
.pb-left-expanded { padding-left: calc(var(--music-sidebar-w) + 4px); }

.pb-cover-wrap { position: relative; width: 56px; height: 56px; flex-shrink: 0; border-radius: 6px; box-shadow: var(--shadow-el); }
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
  background: rgba(0, 0, 0, 0.55); /* scrim over the cover art — stays literal */
  opacity: 0;
  transition: opacity 0.15s ease;
}
.pb-cover-wrap:hover .pb-cover-actions { opacity: 1; }
.pb-cover-action {
  width: 24px; height: 24px;
  display: flex; align-items: center; justify-content: center;
  border-radius: 50%;
  /* buttons painted over the cover art — stays literal */
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
  /* Tone-follow: --pb-accent carries the sampled palette of the playing
     album (script above); idle falls back to the neutral fg field. */
  background: var(--pb-accent, var(--fg-0));
  color: var(--pb-accent-ink, var(--bg-0));
  display: flex; align-items: center; justify-content: center;
  /* background/color glide 0.9s between album palettes (same curve as the
     detail-page tone buttons); it also eases the armed field back after a
     hold-to-stop. opacity dims gently to the idle/disabled state. */
  transition: transform 0.15s ease, filter 0.15s ease,
    background 0.9s cubic-bezier(0.22, 1, 0.36, 1),
    color 0.9s cubic-bezier(0.22, 1, 0.36, 1),
    opacity 0.3s ease;
  position: relative;
}
/* Brightness pop instead of a fixed hover color — works on any sampled tone. */
.pb-play:hover { transform: scale(1.06); filter: brightness(1.15); }
/* Idle (no track): the transport is inert. Dim it and drop the hover pop. */
.pb-play:disabled { opacity: 0.4; cursor: default; }
.pb-play:disabled:hover { transform: none; filter: none; }
.pb-controls .btn-icon:disabled { opacity: 0.3; cursor: default; }
/* Left-side idle placeholder standing in for the cover + track text. */
.pb-cover-placeholder {
  width: 56px; height: 56px;
  border-radius: 6px;
  flex-shrink: 0;
  display: flex; align-items: center; justify-content: center;
  background: rgb(var(--ink) / 0.03);
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
  background: color-mix(in srgb, var(--bad) 20%, var(--bg-1));
  color: var(--bad);
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
  stroke: color-mix(in srgb, var(--bad) 20%, transparent);
  stroke-width: 3;
}
.pb-play-ring-fill {
  fill: none;
  stroke: var(--bad);
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
.pb-viz-btn:hover { background: rgb(var(--ink) / 0.06); border-color: var(--border); }
.pb-viz-btn.active { border-color: color-mix(in srgb, var(--gold) 40%, transparent); background: var(--gold-soft); }
.pb-viz-meter { width: 34px; height: 20px; }

/* ── Compact band (720.02–1200px) ───────────────────────────────────────
   Desktop (>1200px) is untouched above this point. In this band: the grid
   tracks get minmax(0, …) so they can shrink below their content size (the
   base `1fr`/`1.6fr` tracks can't — nothing here relied on that before
   because nothing needed to shrink), the rating/quality floats that hang off
   `.pb-controls` are dropped (quality moves into the ⋯ menu; rating is an
   acceptable in-band loss, still reachable from the expanded NowPlayingView),
   and the fold-into-sidebar affordance hides since the sidebar it targets is
   hidden in this band too (script watcher above unwinds the state if it was
   set at a wider width). The template handles the `.pb-right` cluster
   collapse itself (v-if/v-else on isCompact, not CSS) so components that own
   mounted side effects never double-mount. */
@media (min-width: 720.02px) and (max-width: 1200px) {
  .playbar {
    grid-template-columns: minmax(0, 1fr) minmax(0, 1.6fr) minmax(0, 1fr);
  }
  .pb-cover-action-fold { display: none; }
  .pb-left-expanded { padding-left: 0; }
}
</style>

<!-- Overflow (⋯) menu content is portaled by AppMenu — scoped styles above
     don't reach it (see docs/ui.md "Scoped CSS doesn't reach portaled /
     child-rendered elements"), so its rules live here unscoped, same
     convention as PlaybarQuality's .pbq-pop / SleepTimer's .st-pop. -->
<style>
.pb-more-label {
  padding: 8px 14px 4px;
}
.pb-more-row {
  padding: 8px 14px;
}
.pb-more-item {
  min-height: 44px;
}
.pb-more-item-label {
  flex: 1;
  min-width: 0;
}
.pb-more-active { color: var(--gold-bright, var(--gold)); }
.pb-more-check { color: var(--gold-bright, var(--gold)); flex-shrink: 0; }
.pb-more-destructive { color: var(--bad); }
.pb-more-destructive[data-highlighted],
.pb-more-destructive:hover { background: color-mix(in srgb, var(--bad) 8%, transparent); color: var(--bad); }

.pb-more-volume-row {
  display: flex;
  align-items: center;
  gap: 10px;
  min-height: 44px;
}
.pb-more-volume-value {
  min-width: 26px;
  text-align: right;
  font-size: 11px;
  font-family: var(--font-mono);
  color: var(--fg-2);
}

/* Plain range input styled to match AppSlider's look (gold fill, round
   thumb) — see the template comment above this row for why it isn't
   <AppSlider/>. -webkit/-moz prefixed rule groups can't be comma-combined
   (an invalid prefixed selector invalidates the whole rule), so track/thumb
   are declared per-engine. */
.pb-more-volume-range {
  flex: 1;
  height: 20px;
  margin: 0;
  background: transparent;
  cursor: pointer;
  -webkit-appearance: none;
  appearance: none;
}
.pb-more-volume-range::-webkit-slider-runnable-track {
  height: 4px;
  border-radius: 999px;
  background: linear-gradient(
    to right,
    var(--gold) 0%,
    var(--gold) calc(1% * var(--pb-vol, 0)),
    rgb(var(--ink) / 0.08) calc(1% * var(--pb-vol, 0)),
    rgb(var(--ink) / 0.08) 100%
  );
}
.pb-more-volume-range::-moz-range-track {
  height: 4px;
  border-radius: 999px;
  background: rgb(var(--ink) / 0.08);
}
.pb-more-volume-range::-moz-range-progress {
  height: 4px;
  border-radius: 999px;
  background: var(--gold);
}
.pb-more-volume-range::-webkit-slider-thumb {
  -webkit-appearance: none;
  appearance: none;
  width: 14px;
  height: 14px;
  margin-top: -5px;
  border-radius: 50%;
  background: var(--fg-0);
  box-shadow: 0 1px 3px rgb(var(--shade) / 0.4);
}
.pb-more-volume-range::-moz-range-thumb {
  width: 14px;
  height: 14px;
  border: 0;
  border-radius: 50%;
  background: var(--fg-0);
  box-shadow: 0 1px 3px rgb(var(--shade) / 0.4);
}
</style>
