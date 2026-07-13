<template>
  <section
    v-if="session"
    class="cast-video-remote"
    :class="{ compact }"
    aria-label="Chromecast video remote"
    @click.stop
    @keydown.stop
  >
    <div v-if="!compact" class="cvr-art" :style="artworkStyle">
      <div class="cvr-art-fallback"><Icon name="television-simple" :size="38" /></div>
      <div class="cvr-art-scrim" />
      <span class="cvr-art-badge"><Icon name="cast" :size="12" /> Chromecast</span>
    </div>

    <div class="cvr-heading">
      <span v-if="compact" class="cvr-device-icon"><Icon name="television-simple" :size="17" /></span>
      <span class="cvr-heading-text">
        <strong>{{ session.title || 'Video' }}</strong>
        <span><Icon name="cast" :size="11" /> {{ session.device_name }}</span>
      </span>
      <span class="cvr-state" :class="{ active: playing }">{{ stateLabel }}</span>
    </div>

    <div class="cvr-seek">
      <span>{{ formatTime(displayPosition) }}</span>
      <input
        class="cvr-range cvr-position"
        type="range"
        min="0"
        :max="duration || 1"
        step="1"
        :value="displayPosition"
        aria-label="Video position"
        @pointerdown="scrubbing = true"
        @input="onSeekInput"
        @change="commitSeek"
      >
      <span>{{ formatTime(duration) }}</span>
    </div>

    <div class="cvr-transport">
      <button type="button" aria-label="Rewind 10 seconds" @click="skip(-10)"><Icon name="skipback" :size="compact ? 17 : 20" /></button>
      <button type="button" class="cvr-play" :aria-label="playing ? 'Pause' : 'Play'" @click="togglePlayback">
        <Icon :name="busy ? 'loading' : (playing ? 'pause' : 'play')" :size="compact ? 22 : 28" :class="{ 'cvr-spin': busy }" />
      </button>
      <button type="button" aria-label="Forward 10 seconds" @click="skip(10)"><Icon name="skipforward" :size="compact ? 17 : 20" /></button>
    </div>

    <div class="cvr-settings" :class="{ loading: cast.videoStreamInfoLoading }">
      <label>
        <span><Icon name="translate" :size="13" /> Audio</span>
        <select :value="session.audio_track ?? 0" :disabled="busy || !audioTracks.length" @change="changeAudio">
          <option v-for="(track, index) in audioTracks" :key="track.index" :value="index">{{ audioLabel(track, index) }}</option>
          <option v-if="!audioTracks.length" value="0">Default</option>
        </select>
      </label>

      <label>
        <span><Icon name="subtitles" :size="13" /> Subtitles</span>
        <select :value="session.subtitle_track ?? -1" :disabled="busy || !subtitleTracks.length" @change="changeSubtitle">
          <option value="-1">Off</option>
          <option
            v-for="(track, index) in subtitleTracks"
            :key="track.index"
            :value="index"
            :disabled="track.delivery !== 'external'"
          >{{ subtitleLabel(track, index) }}</option>
        </select>
      </label>

      <label>
        <span><Icon name="eq" :size="13" /> Quality</span>
        <select :value="session.quality || 'auto'" :disabled="busy" @change="changeQuality">
          <option value="auto">Auto</option>
          <option v-for="quality in qualities" :key="quality.label" :value="quality.label">{{ quality.label }} · {{ quality.height }}p</option>
        </select>
      </label>
    </div>

    <p v-if="cast.videoStreamInfoError" class="cvr-error">Track options could not be loaded. Playback controls still work.</p>

    <div class="cvr-volume">
      <button type="button" :aria-label="session.volume === 0 ? 'Unmute' : 'Mute'" @click="toggleMute">
        <Icon :name="session.volume === 0 ? 'volmute' : 'vol'" :size="17" />
      </button>
      <input
        class="cvr-range"
        type="range"
        min="0"
        max="100"
        step="1"
        :value="session.volume"
        aria-label="Chromecast volume"
        @input="changeVolume"
      >
      <span>{{ session.volume }}%</span>
    </div>

    <button v-if="!compact" type="button" class="cvr-disconnect" @click="disconnect">
      <Icon name="close" :size="15" /> Stop casting
    </button>
  </section>
</template>

<script setup lang="ts">
import type { StreamAudio, StreamSubtitle } from '~~/shared/types'

withDefaults(defineProps<{ compact?: boolean }>(), { compact: false })
const emit = defineEmits<{ disconnected: [] }>()
const cast = useCastStore()
const { toast } = useToast()

const session = computed(() => cast.session?.media_kind === 'video' ? cast.session : null)
const audioTracks = computed(() => cast.videoStreamInfo?.audio ?? [])
const subtitleTracks = computed(() => cast.videoStreamInfo?.subtitle ?? [])
const qualities = computed(() => cast.videoStreamInfo?.qualities ?? [])
const playing = computed(() => session.value?.state === 'playing' || session.value?.state === 'starting')
const busy = computed(() => cast.connecting || session.value?.state === 'starting')
const duration = computed(() => Math.max(0, session.value?.duration_sec ?? cast.videoStreamInfo?.duration ?? 0))
const stateLabel = computed(() => busy.value ? 'Loading' : playing.value ? 'Playing' : 'Paused')
const artworkStyle = computed(() => session.value?.media_item_id
  ? { backgroundImage: `url(${useBackdropUrl(session.value.media_item_id)})` }
  : {})

const clockPosition = ref(0)
const seekPreview = ref(0)
const scrubbing = ref(false)
const displayPosition = computed(() => scrubbing.value ? seekPreview.value : clockPosition.value)
let clock: ReturnType<typeof setInterval> | null = null

function syncClock() {
  if (!scrubbing.value) clockPosition.value = cast.livePositionSec()
}

watch(() => [session.value?.id, session.value?.position_sec, session.value?.state], syncClock, { immediate: true })
watch(() => session.value?.file_id, (fileID) => {
  if (fileID) void cast.loadVideoStreamInfo(fileID)
}, { immediate: true })
onMounted(() => { clock = setInterval(syncClock, 500) })
onScopeDispose(() => { if (clock) clearInterval(clock) })

async function act(action: () => Promise<unknown>, fallback: string) {
  try {
    await action()
    return true
  } catch (error) {
    toast.err(error instanceof Error ? error.message : fallback)
    return false
  }
}

function togglePlayback() {
  if (!session.value || busy.value) return
  void act(() => playing.value ? cast.pause() : cast.resume(), 'Could not control Chromecast playback')
}

function skip(delta: number) {
  const target = Math.max(0, Math.min(duration.value || Number.MAX_SAFE_INTEGER, cast.livePositionSec() + delta))
  void act(() => cast.seekTo(target), 'Could not seek Chromecast playback')
}

function onSeekInput(event: Event) {
  scrubbing.value = true
  seekPreview.value = Number((event.target as HTMLInputElement).value)
}

function commitSeek(event: Event) {
  const target = Number((event.target as HTMLInputElement).value)
  scrubbing.value = false
  clockPosition.value = target
  void act(() => cast.seekTo(target), 'Could not seek Chromecast playback')
}

function changeVolume(event: Event) {
  const value = Number((event.target as HTMLInputElement).value)
  if (value > 0) volumeBeforeMute = value
  cast.setVolume(value)
}

let volumeBeforeMute = 30
function toggleMute() {
  const volume = session.value?.volume ?? 0
  if (volume > 0) {
    volumeBeforeMute = volume
    cast.setVolume(0)
  } else {
    cast.setVolume(volumeBeforeMute)
  }
}

function changeAudio(event: Event) {
  const audioTrack = Number((event.target as HTMLSelectElement).value)
  void act(() => cast.updateVideo({ audioTrack }), 'Could not change the audio track')
}

function changeSubtitle(event: Event) {
  const value = Number((event.target as HTMLSelectElement).value)
  void act(() => cast.updateVideo({ subtitleTrack: value >= 0 ? value : null }), 'Could not change subtitles')
}

function changeQuality(event: Event) {
  const quality = (event.target as HTMLSelectElement).value
  void act(() => cast.updateVideo({ quality }), 'Could not change Chromecast quality')
}

async function disconnect() {
  if (await act(() => cast.disconnect(), 'Could not stop Chromecast playback')) emit('disconnected')
}

function audioLabel(track: StreamAudio, index: number) {
  const language = track.language?.toUpperCase()
  const detail = [language, track.title, track.channels ? `${track.channels}ch` : ''].filter(Boolean).join(' · ')
  return detail || `Track ${index + 1}`
}

function subtitleLabel(track: StreamSubtitle, index: number) {
  const name = track.title || track.language?.toUpperCase() || `Track ${index + 1}`
  return track.delivery === 'external' ? name : `${name} · burn-in unavailable`
}

function formatTime(seconds: number) {
  if (!Number.isFinite(seconds) || seconds < 0) return '0:00'
  const whole = Math.floor(seconds)
  const hours = Math.floor(whole / 3600)
  const minutes = Math.floor((whole % 3600) / 60)
  const secs = whole % 60
  return hours > 0
    ? `${hours}:${String(minutes).padStart(2, '0')}:${String(secs).padStart(2, '0')}`
    : `${minutes}:${String(secs).padStart(2, '0')}`
}
</script>

<style scoped>
.cast-video-remote {
  display: flex;
  flex-direction: column;
  gap: 16px;
  width: 100%;
  max-width: 560px;
  margin: 0 auto;
  padding: 18px;
  color: var(--fg-0);
}
.cast-video-remote.compact {
  gap: 10px;
  padding: 8px 7px 10px;
}
.cvr-art {
  position: relative;
  min-height: min(36vh, 290px);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  overflow: hidden;
  background-color: var(--bg-2);
  background-position: center;
  background-size: cover;
  box-shadow: var(--shadow-2);
}
.cvr-art-fallback {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  color: var(--fg-3);
}
.cvr-art-scrim {
  position: absolute;
  inset: 0;
  background: linear-gradient(180deg, transparent 45%, rgb(var(--shade) / 0.72));
}
.cvr-art-badge {
  position: absolute;
  left: 12px;
  bottom: 12px;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 5px 9px;
  border: 1px solid rgb(var(--ink) / 0.12);
  border-radius: var(--r-pill);
  background: color-mix(in srgb, var(--bg-1) 82%, transparent);
  backdrop-filter: blur(10px);
  color: var(--fg-1);
  font-family: var(--font-mono);
  font-size: 9px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
.cvr-heading {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}
.cvr-device-icon {
  display: inline-grid;
  place-items: center;
  width: 34px;
  height: 34px;
  flex: 0 0 34px;
  border: 1px solid color-mix(in srgb, var(--gold) 24%, var(--border));
  border-radius: var(--r-sm);
  background: color-mix(in srgb, var(--gold) 8%, transparent);
  color: var(--gold-bright, var(--gold));
}
.cvr-heading-text {
  display: flex;
  flex: 1;
  min-width: 0;
  flex-direction: column;
  gap: 3px;
}
.cvr-heading-text strong {
  overflow: hidden;
  color: var(--fg-0);
  font-size: 15px;
  line-height: 1.25;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.compact .cvr-heading-text strong { font-size: 13px; }
.cvr-heading-text > span {
  display: flex;
  align-items: center;
  gap: 4px;
  overflow: hidden;
  color: var(--fg-2);
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.cvr-state {
  flex-shrink: 0;
  color: var(--fg-3);
  font-family: var(--font-mono);
  font-size: 8px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
.cvr-state.active { color: var(--gold-bright, var(--gold)); }
.cvr-seek,
.cvr-volume {
  display: flex;
  align-items: center;
  gap: 10px;
}
.cvr-seek > span,
.cvr-volume > span {
  min-width: 34px;
  color: var(--fg-3);
  font-family: var(--font-mono);
  font-size: 9px;
  text-align: center;
}
.cvr-volume > span { min-width: 30px; text-align: right; }
.cvr-range {
  width: 100%;
  height: 20px;
  margin: 0;
  accent-color: var(--gold);
  cursor: pointer;
}
.cvr-transport {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 20px;
}
.cvr-transport button,
.cvr-volume button {
  display: inline-grid;
  place-items: center;
  width: 38px;
  height: 38px;
  flex: 0 0 38px;
  border: 0;
  border-radius: 50%;
  background: transparent;
  color: var(--fg-1);
  cursor: pointer;
}
.cvr-transport button:hover,
.cvr-volume button:hover { background: rgb(var(--ink) / 0.06); color: var(--fg-0); }
.cvr-transport .cvr-play {
  width: 52px;
  height: 52px;
  flex-basis: 52px;
  background: var(--gold);
  color: var(--bg-0);
  box-shadow: 0 8px 22px var(--gold-glow);
}
.compact .cvr-transport { gap: 14px; }
.compact .cvr-transport .cvr-play { width: 42px; height: 42px; flex-basis: 42px; }
.cvr-settings {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 8px;
  opacity: 1;
  transition: opacity 0.15s ease;
}
.cvr-settings.loading { opacity: 0.55; }
.cvr-settings label {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 5px;
}
.cvr-settings label > span {
  display: flex;
  align-items: center;
  gap: 5px;
  color: var(--fg-3);
  font-family: var(--font-mono);
  font-size: 8px;
  font-weight: 700;
  letter-spacing: 0.07em;
  text-transform: uppercase;
}
.cvr-settings select {
  min-width: 0;
  height: 34px;
  padding: 0 27px 0 9px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--fg-1);
  font: inherit;
  font-size: 11px;
  text-overflow: ellipsis;
  cursor: pointer;
}
.compact .cvr-settings { grid-template-columns: 1fr; gap: 6px; }
.compact .cvr-settings label { display: grid; grid-template-columns: 76px minmax(0, 1fr); align-items: center; }
.compact .cvr-settings select { height: 30px; }
.cvr-error {
  margin: 0;
  color: var(--bad);
  font-size: 10px;
  line-height: 1.35;
}
.cvr-disconnect {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 7px;
  width: 100%;
  min-height: 42px;
  border: 1px solid color-mix(in srgb, var(--bad) 24%, var(--border));
  border-radius: var(--r-md);
  background: color-mix(in srgb, var(--bad) 6%, transparent);
  color: var(--bad);
  font: inherit;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
}
.cvr-disconnect:hover { background: color-mix(in srgb, var(--bad) 10%, transparent); }
.cvr-spin { animation: cvr-spin 0.9s linear infinite; }
@keyframes cvr-spin { to { transform: rotate(360deg); } }

@media (max-width: 520px) {
  .cast-video-remote:not(.compact) { gap: 15px; padding: 14px 16px 24px; }
  .cvr-art { min-height: min(31vh, 240px); }
  .cvr-settings { grid-template-columns: 1fr; }
  .cvr-settings label { display: grid; grid-template-columns: 92px minmax(0, 1fr); align-items: center; }
  .cvr-settings select { height: 38px; }
}
</style>
