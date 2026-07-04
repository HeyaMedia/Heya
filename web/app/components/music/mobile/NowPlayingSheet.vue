<!--
  NowPlayingSheet — full-screen now-playing surface for phones, built on
  AppSheet(size="full"). Reads everything from the global usePlayer()
  singleton except the lyrics fetch, which mirrors the pattern in
  QueuePanel.vue's lyrics tab ($heya, GET /api/music/tracks/{id}/lyrics).

  Props/model:
    v-model:open — boolean, sheet visibility

  Emits:
    open-queue — the "Queue" button was tapped (parent opens QueueSheet)

  Note: content rendered by AppSheet is portaled to <body>, so anything that
  needs to reach it (there's no such CSS here today, but keep it in mind if
  this file grows) must live in an unscoped <style> block, not scoped —
  see docs/ui.md gotcha #2.
-->
<template>
  <AppSheet v-model:open="open" size="full" title="Now Playing">
    <div class="nps-body">
      <div class="nps-visual">
        <div v-if="!showLyrics" class="nps-art-wrap">
          <Poster :idx="currentTrack?.id ?? 0" :src="currentTrack?.poster ?? null" aspect="1/1" class="nps-art" />
        </div>
        <div v-else ref="lyricsScrollEl" class="nps-lyrics scroll">
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
        <span class="nps-time">{{ formatTime(displayPosition) }}</span>
        <AppSlider
          :model-value="seekPct"
          :min="0"
          :max="100"
          :step="0.1"
          aria-label="Seek"
          class="nps-seek-slider"
          @update:model-value="onSeekInput"
          @value-commit="onSeekCommit"
        />
        <span class="nps-time">{{ formatTime(duration) }}</span>
      </div>

      <div class="nps-transport">
        <button type="button" class="nps-icon" :class="{ active: shuffled }" aria-label="Shuffle" @click="toggleShuffle">
          <Icon name="shuffle" :size="20" />
        </button>
        <button type="button" class="nps-icon" aria-label="Previous" @click="prevTrack">
          <Icon name="prev" :size="26" />
        </button>
        <button type="button" class="nps-play" :aria-label="playing ? 'Pause' : 'Play'" @click="togglePlay">
          <Icon :name="playing ? 'pause' : 'play'" :size="30" />
        </button>
        <button type="button" class="nps-icon" aria-label="Next" @click="nextTrack">
          <Icon name="next" :size="26" />
        </button>
        <button type="button" class="nps-icon" :class="{ active: repeatMode !== 'off' }" aria-label="Repeat" @click="cycleRepeat">
          <Icon name="repeat" :size="20" />
          <span v-if="repeatMode === 'one'" class="nps-repeat-badge">1</span>
        </button>
      </div>

      <div class="nps-secondary">
        <button type="button" class="nps-sicon" :class="{ active: queueOpenTab }" aria-label="Queue" @click="$emit('open-queue')">
          <Icon name="queue" :size="18" />
        </button>
        <button type="button" class="nps-sicon" :class="{ active: showLyrics }" aria-label="Lyrics" @click="showLyrics = !showLyrics">
          <Icon name="lyrics" :size="18" />
        </button>
        <div class="nps-volume">
          <button type="button" class="nps-sicon" aria-label="Mute" @click="toggleMute">
            <Icon :name="muted || volume === 0 ? 'volmute' : 'vol'" :size="18" />
          </button>
          <AppSlider
            :model-value="muted ? 0 : volume"
            :min="0"
            :max="100"
            :step="1"
            aria-label="Volume"
            class="nps-volume-slider"
            @update:model-value="onVolumeChange"
          />
        </div>
      </div>
    </div>
  </AppSheet>
</template>

<script setup lang="ts">
// $heya is grabbed once at script-setup top level (never inside computed()
// or an async body) — see docs/ui.md gotcha #1.
const { $heya } = useNuxtApp()

const open = defineModel<boolean>('open', { default: false })
defineEmits<{ 'open-queue': [] }>()

const {
  currentTrack, playing, position, duration, volume, muted,
  shuffled, repeatMode, queueOpen, sideTab,
  togglePlay, seek, setVolume, toggleMute,
  toggleShuffle, cycleRepeat, nextTrack, prevTrack, formatTime,
} = usePlayer()

// Cosmetic only — highlights the Queue button when the queue sheet/panel
// happens to already be open on the queue tab.
const queueOpenTab = computed(() => queueOpen.value && sideTab.value === 'queue')

// --- Links (mirrors Playbar.vue's artistTo/albumTo computeds) --------------
const artistTo = computed(() =>
  currentTrack.value?.artist_slug ? `/music/artist/${currentTrack.value.artist_slug}` : null)
const albumTo = computed(() =>
  currentTrack.value?.artist_slug && currentTrack.value?.album_slug
    ? `/music/artist/${currentTrack.value.artist_slug}/${currentTrack.value.album_slug}`
    : null)

// --- Seek --------------------------------------------------------------
// While the user is scrubbing, the local drag value drives the thumb (and
// the time label) — otherwise the position tick that arrives every fraction
// of a second would snap the thumb back under their finger. seek() fires
// once on release via reka's value-commit (the @value-commit listener falls
// through AppSlider onto its SliderRoot root element), not per drag step.
const scrubPct = ref<number | null>(null)
const seekPct = computed(() =>
  scrubPct.value ?? (duration.value > 0 ? (position.value / duration.value) * 100 : 0))
const displayPosition = computed(() =>
  scrubPct.value != null ? (scrubPct.value / 100) * duration.value : position.value)
function onSeekInput(v: number) {
  scrubPct.value = v
}
function onSeekCommit(v: number[] | undefined) {
  const pct = v?.[0] ?? scrubPct.value
  if (pct != null) seek(pct / 100)
  scrubPct.value = null
}

// --- Volume --------------------------------------------------------------
function onVolumeChange(v: number) {
  if (muted.value && v > 0) toggleMute()
  setVolume(v)
}

// --- Lyrics (inline, replaces the artwork area when toggled on) -----------
// Same fetch/sync shape as QueuePanel.vue's lyrics tab, simplified: no
// timing-offset slider, no click-to-seek — just a scrollable read view with
// the current line highlighted when synced timing data exists.
interface LyricsLine { time_ms: number; text: string }
interface LyricsResponse { synced: boolean; lines: LyricsLine[] }

const showLyrics = ref(false)
const lyrics = ref<LyricsResponse | null>(null)
const lyricsLoading = ref(false)
const lyricRefs = ref<Array<HTMLElement | null>>([])
const lyricsScrollEl = ref<HTMLElement | null>(null)
let lastLoadedTrackId: number | null = null

function bindLyricRef(el: HTMLElement | null, i: number) {
  lyricRefs.value[i] = el
}

async function loadLyrics(trackId: number | null | undefined) {
  // Negative IDs are synthetic radio/podcast tracks — no library lyrics.
  if (!trackId || trackId <= 0) { lyrics.value = null; lastLoadedTrackId = trackId ?? null; return }
  if (trackId === lastLoadedTrackId && lyrics.value) return
  lastLoadedTrackId = trackId
  lyricsLoading.value = true
  lyricRefs.value = []
  try {
    lyrics.value = await $heya('/api/music/tracks/{id}/lyrics', { path: { id: trackId } }) as LyricsResponse
  } catch {
    lyrics.value = null
  } finally {
    lyricsLoading.value = false
  }
}

// Only fetch while the lyrics view is actually showing.
watch(
  () => [showLyrics.value, currentTrack.value?.id] as const,
  ([show, id]) => {
    if (!show) return
    void loadLyrics(id ?? null)
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
.nps-body {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  gap: 18px;
}

.nps-visual {
  flex: 1;
  min-height: 0;
  display: flex;
  align-items: center;
  justify-content: center;
}
.nps-art-wrap { width: 100%; display: flex; justify-content: center; }
.nps-art {
  width: min(70vw, 360px);
  max-width: 100%;
  border-radius: var(--r-lg);
  box-shadow: var(--shadow-3);
}

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
.nps-seek-slider { flex: 1; min-width: 0; }
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
.nps-icon:active { background: rgba(255, 255, 255, 0.08); }
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
.nps-sicon:active { background: rgba(255, 255, 255, 0.08); }
.nps-sicon.active { color: var(--gold); }
.nps-volume { display: flex; align-items: center; gap: 4px; flex: 1; max-width: 160px; }
.nps-volume-slider { flex: 1; min-width: 0; }
</style>
