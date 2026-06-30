<template>
  <footer class="playbar" v-if="currentTrack">
    <!-- Left: now playing -->
    <div class="pb-left">
      <AppTooltip label="Expand">
        <button class="pb-cover-btn" @click="nowPlayingOpen = true">
          <Poster :idx="currentTrack.id" :src="currentTrack.poster" aspect="1/1" style="width: 56px; height: 56px; border-radius: 6px; flex-shrink: 0" />
        </button>
      </AppTooltip>
      <div class="pb-info" @click="nowPlayingOpen = true" style="cursor: pointer">
        <div class="pb-title">{{ currentTrack.title }}</div>
        <div class="pb-artist">{{ currentTrack.artist }} — {{ currentTrack.album }}</div>
      </div>
      <div v-if="currentTrack" class="pb-rate" @click.stop>
        <StarRating
          :model-value="ratings.get(currentTrack.id) ?? 0"
          size="sm"
          @update:model-value="(v) => onRate(currentTrack!.id, v)"
        />
      </div>
      <AppMenu v-if="currentTrack">
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
    </div>

    <!-- Center: controls + scrubber -->
    <div class="pb-center">
      <div class="pb-controls">
        <AppTooltip :label="shuffled ? 'Shuffle on' : 'Shuffle'">
          <button class="btn-icon" :class="{ active: shuffled }" @click="toggleShuffle">
            <Icon name="shuffle" :size="16" />
          </button>
        </AppTooltip>
        <AppTooltip label="Previous">
          <button class="btn-icon" @click="prevTrack"><Icon name="prev" :size="16" /></button>
        </AppTooltip>
        <button class="pb-play" @click="togglePlay">
          <Icon :name="playing ? 'pause' : 'play'" :size="20" />
        </button>
        <AppTooltip label="Next">
          <button class="btn-icon" @click="nextTrack"><Icon name="next" :size="16" /></button>
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
      <div class="pb-scrubber">
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
        <button class="btn-icon" :class="{ active: lyricsOpen }" @click="toggleLyrics"><Icon name="lyrics" :size="16" /></button>
      </AppTooltip>
      <AppTooltip label="Queue">
        <button class="btn-icon" :class="{ active: queueOpen }" @click="toggleQueue"><Icon name="queue" :size="16" /></button>
      </AppTooltip>
      <AppTooltip label="Equalizer">
        <button class="btn-icon" :class="{ active: eqOpen }" @click="eqOpen = !eqOpen"><Icon name="eq" :size="16" /></button>
      </AppTooltip>
      <SleepTimer />
      <div class="pb-volume">
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
  shuffled, repeatMode, queueOpen, lyricsOpen,
  togglePlay, seek, setVolume, toggleMute, toggleShuffle,
  cycleRepeat, nextTrack, prevTrack,
  toggleQueue, toggleLyrics, formatTime,
} = usePlayer()

// Bridge OS media keys / lock-screen transport to the player. Mounted here
// because the Playbar is the one always-present music surface.
useMediaSession()

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
  seek(pct)
}

// Reka Slider emits the new value as a number; piped straight to the
// player. If the user drags from 0 while muted, also unmute — same
// behaviour as the old rail (clicking on it implicitly unmuted).
function onVolumeChange(v: number) {
  if (muted.value && v > 0) toggleMute()
  setVolume(v)
}
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
.pb-left { display: flex; align-items: center; gap: 12px; }
.pb-cover-btn { background: transparent; border: 0; padding: 0; cursor: pointer; }
.pb-rate { display: flex; align-items: center; padding: 0 6px; }
.pb-info { flex: 1; min-width: 0; margin-right: 8px; }
.pb-title { font-size: 13px; font-weight: 500; color: var(--fg-0); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.pb-artist { font-size: 11px; color: var(--fg-2); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.pb-center { display: flex; flex-direction: column; align-items: center; gap: 6px; }
.pb-controls { display: flex; align-items: center; gap: 8px; position: relative; }
.pb-quality-slot { position: absolute; left: 100%; margin-left: 14px; top: 50%; transform: translateY(-50%); }
.pb-play {
  width: 36px; height: 36px;
  border-radius: 50%;
  background: var(--fg-0);
  color: var(--bg-0);
  display: flex; align-items: center; justify-content: center;
  transition: transform 0.15s ease, background 0.15s ease;
}
.pb-play:hover { transform: scale(1.06); background: var(--gold); }
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
</style>
