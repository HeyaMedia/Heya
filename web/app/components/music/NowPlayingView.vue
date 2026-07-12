<template>
  <Teleport to="body">
    <Transition name="np-fade">
      <div v-if="open" class="np-root" role="dialog" aria-modal="true" @click.self="$emit('close')">
        <!-- Backdrop: the album cover blurred + darkened. Falls back to a flat
             gradient when there's no cover. -->
        <div class="np-backdrop" :style="backdropStyle" />
        <div class="np-backdrop-tint" />

        <button class="np-close" @click="$emit('close')" title="Collapse">
          <Icon name="chevdown" :size="22" />
        </button>

        <div class="np-content">
          <!-- Left: artwork + meta -->
          <div class="np-art-col">
            <div class="np-art-frame">
              <NuxtImg v-if="coverUrl" :src="coverUrl" :width="800" :quality="80" :alt="`${title} cover`" class="np-art-img" draggable="false" />
              <div v-else class="np-art-placeholder">
                <Icon name="music" :size="64" />
              </div>
            </div>
            <div class="np-meta">
              <div class="np-meta-kind">{{ track ? 'Now Playing' : 'Nothing playing' }}</div>
              <h2 class="np-title">{{ title || '—' }}</h2>
              <div class="np-sub">
                <NuxtLink v-if="artistSlug" :to="`/music/${artistSlug}`" class="np-sub-link" @click="$emit('close')">{{ artist }}</NuxtLink>
                <span v-else>{{ artist }}</span>
                <template v-if="album">
                  <span class="np-sub-dot">·</span>
                  <span>{{ album }}</span>
                </template>
              </div>
            </div>
          </div>

          <!-- Right: lyrics column -->
          <div class="np-lyrics-col" ref="lyricsCol">
            <div v-if="lyricsLoading" class="np-lyrics-state">Loading lyrics…</div>
            <template v-else-if="lyrics && lyrics.lines.length">
              <div class="np-lyrics-spacer" />
              <p
                v-for="(line, i) in lyrics.lines"
                :key="i"
                class="np-lyric"
                :class="{
                  active: lyrics.synced && i === activeLyricIdx,
                  past: lyrics.synced && i < activeLyricIdx,
                  unsynced: !lyrics.synced,
                }"
                :ref="(el) => bindLyricRef(el as HTMLElement | null, i)"
                @click="lyrics?.synced ? seekToLine(line.time_ms) : null"
              >
                {{ line.text || '♪' }}
              </p>
              <div class="np-lyrics-spacer" />
            </template>
            <div v-else class="np-lyrics-state">
              <p>No lyrics for this track.</p>
              <p class="np-lyrics-hint">Drop a matching .lrc next to the audio file and re-scan.</p>
            </div>
          </div>
        </div>

        <!-- Bottom: scrubber + controls -->
        <div class="np-bottom">
          <div class="np-scrubber">
            <span class="np-time">{{ formatTime(position) }}</span>
            <div class="rail gold" @click="onSeek">
              <div class="fill" :style="{ width: scrubPct + '%' }" />
              <div class="knob" :style="{ left: scrubPct + '%' }" />
            </div>
            <span class="np-time">{{ formatTime(duration) }}</span>
          </div>
          <div class="np-controls">
            <button class="np-icon" :class="{ active: shuffled }" @click="toggleShuffle" title="Shuffle">
              <Icon name="shuffle" :size="18" />
            </button>
            <button class="np-icon" @click="prevTrack" title="Previous">
              <Icon name="prev" :size="22" />
            </button>
            <button class="np-play" @click="togglePlay" :title="playing ? 'Pause' : 'Play'">
              <Icon :name="playing ? 'pause' : 'play'" :size="28" />
            </button>
            <button class="np-icon" @click="nextTrack" title="Next">
              <Icon name="next" :size="22" />
            </button>
            <button class="np-icon" :class="{ active: repeatMode !== 'off' }" @click="cycleRepeat" title="Repeat">
              <Icon name="repeat" :size="18" />
              <span v-if="repeatMode === 'one'" class="np-repeat-badge">1</span>
            </button>
          </div>
          <div class="np-sidekicks">
            <div v-if="track" class="np-rate" @click.stop>
              <StarRating
                :model-value="ratings.get(track.id) ?? 0"
                size="md"
                @update:model-value="(v) => onRate(track!.id, v)"
              />
            </div>
            <button
              v-if="track"
              class="np-icon"
              @click="startTrackRadio"
              :disabled="radio.starting.value"
              title="Start radio from this track"
            >
              <Icon name="radio" :size="18" />
            </button>
            <button
              v-if="track"
              class="np-icon"
              @click="startDJMixHere"
              :disabled="radio.starting.value"
              title="DJ mix (harmonically-compatible tracks)"
            >
              <Icon name="shuffle" :size="18" />
            </button>
            <button class="np-icon" @click="openVisualizer" title="Visualizer">
              <Icon name="pulse" :size="18" />
            </button>
            <button class="np-icon" @click="toggleQueue" title="Queue">
              <Icon name="queue" :size="18" />
            </button>
            <div class="np-volume" @wheel.prevent="onVolumeWheel">
              <button class="np-icon" @click="toggleMute">
                <Icon :name="muted || volume === 0 ? 'volmute' : 'vol'" :size="18" />
              </button>
              <div class="rail" style="width: 110px" @click="onVolume">
                <div class="fill" :style="{ width: (muted ? 0 : volume) + '%' }" />
                <div class="knob" :style="{ left: (muted ? 0 : volume) + '%' }" />
              </div>
            </div>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { useQuery } from '@pinia/colada'
import { trackLyricsQuery, type LyricsLine } from '~/queries/music'

const props = defineProps<{ open: boolean }>()
defineEmits<{ close: [] }>()

const {
  playing, currentTrack, position, duration, volume, muted,
  shuffled, repeatMode,
  togglePlay, seek, setVolume, toggleMute, toggleShuffle, cycleRepeat,
  nextTrack, prevTrack, formatTime, toggleQueue,
} = usePlayerBindings()

const trackRatings = useTrackRatings()
const ratings = trackRatings.ratings
async function onRate(trackId: number, v: number) {
  try { await trackRatings.set(trackId, v) } catch { /* rollback handled */ }
}

const radio = useRadio()
const vis = useVisualizer()

// Open the immersive visualizer over the Now Playing view (it sits at a higher
// z-index, so the NP view stays mounted underneath).
function openVisualizer() {
  vis.fullscreenOpen.value = true
}

async function startTrackRadio() {
  const t = track.value
  if (!t || t.id <= 0) return
  // Pass the seed as the first queue item so radio-from-NowPlaying continues
  // with the current track, then the diversified KNN tail.
  await radio.startRadio({ kind: 'track', track_id: t.id }, t)
}

async function startDJMixHere() {
  const t = track.value
  if (!t || t.id <= 0) return
  await radio.startDJMix(t.id, t)
}
const track = computed(() => currentTrack.value)
// Prime rating once track is defined.
watch(track, (t) => {
  if (t?.id && t.id > 0) trackRatings.load(t.id).catch(() => 0)
}, { immediate: true })
const title = computed(() => track.value?.title ?? '')
const artist = computed(() => track.value?.artist ?? '')
const album = computed(() => track.value?.album ?? '')
const coverUrl = computed(() => track.value?.poster ?? null)
const artistSlug = computed(() => {
  // Mirrors Playbar.vue's artistTo: Track carries `artist_slug` directly.
  // The template below interpolates this into `/music/${artistSlug}`, so
  // fold in the `artist/` segment here to land on the real route
  // (`/music/artist/{slug}`, same as Playbar's link) with a one-line fix.
  return track.value?.artist_slug ? `artist/${track.value.artist_slug}` : ''
})

const backdropStyle = computed(() => {
  if (coverUrl.value) return { backgroundImage: `url(${coverUrl.value})` }
  return {}
})

const scrubPct = computed(() => (duration.value > 0 ? (position.value / duration.value) * 100 : 0))

function onSeek(e: MouseEvent) {
  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
  seek((e.clientX - rect.left) / rect.width)
}
function onVolume(e: MouseEvent) {
  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
  setVolume(Math.round(((e.clientX - rect.left) / rect.width) * 100))
}
// Scroll over the volume control to nudge it ±5. Up = louder. Base the delta on
// the real stored level (not the muted-display 0) so scrolling while muted
// adjusts/restores the remembered volume instead of losing it.
function onVolumeWheel(e: WheelEvent) {
  const next = Math.max(0, Math.min(100, volume.value + (e.deltaY < 0 ? 5 : -5)))
  if (muted.value && next > 0) toggleMute()
  setVolume(next)
}

// ---------------------------------------------------------------------------
// Lyrics — same model as QueuePanel but rendered larger.
// ---------------------------------------------------------------------------

const lyricRefs = ref<Array<HTMLElement | null>>([])
const lyricsCol = ref<HTMLElement | null>(null)
const lyricTrackId = computed(() => track.value?.id && track.value.id > 0 ? track.value.id : 0)
const lyricsQuery = useQuery(() => ({
  ...trackLyricsQuery(lyricTrackId.value),
  enabled: props.open && lyricTrackId.value > 0,
}))
const lyrics = computed(() => lyricsQuery.data.value ?? null)
const lyricsLoading = computed(() => lyricsQuery.isPending.value && lyricTrackId.value > 0)

function bindLyricRef(el: HTMLElement | null, i: number) {
  lyricRefs.value[i] = el
}

// Only fetch when the view is open AND the track changes. Closing the view
// doesn't dump the cache — reopen for the same track returns instantly.
watch(
  [() => props.open, () => track.value?.id] as const,
  ([open]) => {
    lyricRefs.value = []
    if (!open) {
      // Allow scroll to jump fresh next open instead of inheriting position.
      requestAnimationFrame(() => {
        if (lyricsCol.value) lyricsCol.value.scrollTop = 0
      })
    }
  },
  { immediate: true },
)

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

// Keep the active lyric centered with smooth scrolling.
watch(activeLyricIdx, (i) => {
  if (!props.open || i < 0) return
  const el = lyricRefs.value[i]
  if (!el || !lyricsCol.value) return
  const container = lyricsCol.value
  const target = el.offsetTop - container.clientHeight / 2 + el.clientHeight / 2
  container.scrollTo({ top: Math.max(0, target), behavior: 'smooth' })
})

function seekToLine(timeMs: number) {
  if (timeMs < 0 || duration.value <= 0) return
  seek(Math.max(0, Math.min(1, timeMs / 1000 / duration.value)))
}

function closeViaEvent() {
  const el = document.querySelector('.np-close') as HTMLButtonElement | null
  el?.click()
}
useEventListener(window, 'keydown', (e: KeyboardEvent) => {
  if (e.key === 'Escape' && props.open) closeViaEvent()
})
</script>

<style scoped>
.np-root {
  position: fixed;
  inset: 0;
  z-index: 200;
  display: flex;
  flex-direction: column;
  background: var(--bg-0);
  color: var(--fg-0);
  overflow: hidden;
}
.np-backdrop {
  position: absolute;
  inset: -10%;
  background-size: cover;
  background-position: center;
  filter: blur(48px) saturate(140%);
  transform: scale(1.1);
  z-index: 0;
  opacity: 0.55;
}
.np-backdrop-tint {
  position: absolute;
  inset: 0;
  /* scrim over the blurred backdrop art — stays literal; already fades to
     the theme-aware var(--bg-0) where it meets the page canvas */
  background:
    radial-gradient(ellipse at top, rgba(0,0,0,0.35), rgba(0,0,0,0.7)),
    linear-gradient(180deg, rgba(0,0,0,0.4) 0%, rgba(0,0,0,0.75) 70%, var(--bg-0) 100%);
  z-index: 1;
}

.np-close {
  position: absolute;
  top: 18px;
  right: 22px;
  z-index: 4;
  width: 40px;
  height: 40px;
  border-radius: 50%;
  /* button floating over the backdrop art — stays literal */
  background: rgba(255, 255, 255, 0.08);
  border: 0;
  color: var(--fg-0);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background 0.15s;
}
.np-close:hover { background: rgba(255, 255, 255, 0.16); }

.np-content {
  position: relative;
  z-index: 2;
  flex: 1;
  display: grid;
  grid-template-columns: minmax(360px, 1fr) minmax(0, 1fr);
  gap: 48px;
  padding: 60px 60px 24px;
  min-height: 0;
}

.np-art-col {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 28px;
  min-width: 0;
}
.np-art-frame {
  width: min(46vmin, 480px);
  height: min(46vmin, 480px);
  border-radius: var(--r-lg);
  overflow: hidden;
  background: var(--bg-3);
  /* Hero-poster shadow formula (matches the detail pages). The previous
     80px blur reached past .np-content's 24px paddings on short/narrow
     viewports and got visibly cropped by .np-root's overflow:hidden. */
  box-shadow: 0 24px 60px rgb(var(--shade) / 0.5), 0 0 0 1px rgb(var(--ink) / 0.06);
}
.np-art-img { width: 100%; height: 100%; object-fit: cover; display: block; }
.np-art-placeholder {
  width: 100%; height: 100%;
  display: flex; align-items: center; justify-content: center;
  color: var(--fg-3);
}
.np-meta { text-align: center; max-width: 460px; }
.np-meta-kind {
  font-size: 11px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.18em;
  color: var(--gold);
  margin-bottom: 8px;
}
.np-title {
  font-size: clamp(22px, 3vw, 32px);
  font-weight: 700;
  line-height: 1.15;
  margin-bottom: 8px;
  color: var(--fg-0);
}
.np-sub {
  font-size: 14px;
  color: var(--fg-2);
  display: flex;
  justify-content: center;
  gap: 8px;
  flex-wrap: wrap;
}
.np-sub-link { color: var(--fg-1); text-decoration: none; }
.np-sub-link:hover { color: var(--gold); }
.np-sub-dot { color: var(--fg-3); }

.np-lyrics-col {
  overflow-y: auto;
  min-width: 0;
  font-size: 22px;
  line-height: 1.5;
  text-align: center;
  scroll-behavior: smooth;
  padding: 0 8px;
  -webkit-mask-image: linear-gradient(180deg, transparent 0%, #000 12%, #000 88%, transparent 100%);
          mask-image: linear-gradient(180deg, transparent 0%, #000 12%, #000 88%, transparent 100%);
}
.np-lyrics-spacer { height: 36vh; }
.np-lyric {
  padding: 8px 0;
  color: var(--fg-3);
  font-weight: 500;
  transition: color 0.25s ease, transform 0.25s ease, font-size 0.25s ease;
  cursor: pointer;
}
.np-lyric:hover { color: var(--fg-1); }
.np-lyric.past { color: var(--fg-3); opacity: 0.55; }
.np-lyric.active {
  color: var(--gold);
  transform: scale(1.04);
  font-weight: 600;
  cursor: default;
}
.np-lyric.unsynced { font-size: 17px; color: var(--fg-1); cursor: default; }
.np-lyrics-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: var(--fg-3);
  font-size: 16px;
  text-align: center;
}
.np-lyrics-hint { margin-top: 6px; font-size: 12px; color: var(--fg-3); }

.np-bottom {
  position: relative;
  z-index: 3;
  padding: 18px 60px 28px;
  display: grid;
  grid-template-columns: minmax(120px, 1fr) auto minmax(120px, 1fr);
  align-items: center;
  gap: 24px;
}
.np-scrubber {
  grid-column: 1 / -1;
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 8px;
}
.np-time {
  font-size: 11px;
  font-family: var(--font-mono);
  color: var(--fg-3);
  min-width: 38px;
  text-align: center;
}
.np-controls {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 14px;
  grid-column: 2;
}
.np-sidekicks {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  grid-column: 3;
}
.np-volume { display: flex; align-items: center; gap: 6px; }

.np-icon {
  width: 36px;
  height: 36px;
  border-radius: 50%;
  background: transparent;
  border: 0;
  color: var(--fg-2);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  position: relative;
  transition: background 0.15s, color 0.15s;
}
.np-icon:hover { background: rgba(255, 255, 255, 0.07); color: var(--fg-0); } /* floating over the backdrop art — stays literal */
.np-icon.active { color: var(--gold); }
.np-play {
  width: 60px;
  height: 60px;
  border-radius: 50%;
  background: var(--gold);
  color: var(--bg-0);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  border: 0;
  transition: transform 0.12s ease, background 0.15s;
  box-shadow: 0 10px 24px var(--gold-glow);
}
.np-play:hover { transform: scale(1.05); background: var(--gold-bright); }
.np-repeat-badge {
  position: absolute;
  bottom: 4px;
  right: 4px;
  font-size: 8px;
  font-weight: 700;
  color: var(--gold);
  font-family: var(--font-mono);
}

.np-fade-enter-active, .np-fade-leave-active { transition: opacity 0.18s ease; }
.np-fade-enter-from, .np-fade-leave-to { opacity: 0; }
.np-fade-enter-to, .np-fade-leave-from { opacity: 1; }

@media (max-width: 900px) {
  .np-content {
    grid-template-columns: 1fr;
    grid-template-rows: minmax(0, 1fr) minmax(0, 1fr);
    gap: 24px;
    padding: 80px 24px 16px;
  }
  .np-bottom { padding: 12px 24px 24px; grid-template-columns: 1fr; }
  .np-scrubber { grid-column: 1; }
  .np-controls { grid-column: 1; }
  .np-sidekicks { grid-column: 1; justify-content: center; }
  .np-art-frame { width: min(60vmin, 320px); height: min(60vmin, 320px); }
  .np-lyrics-col { font-size: 18px; }
}
</style>
