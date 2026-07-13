<template>
  <AppSheet v-model:open="open" size="full" title="Now Playing">
    <template #header>
      <header class="app-sheet-header cvrs-header">
        <span>
          <DrawerTitle as="h3" class="app-sheet-title">Now Playing</DrawerTitle>
          <small v-if="session">{{ transportLabel }} · {{ session.device_name }}</small>
        </span>
        <button type="button" aria-label="Close" @click="open = false"><Icon name="close" :size="18" /></button>
      </header>
    </template>

    <div class="cvrs-scroll">
      <section v-if="session" class="cvrs-now">
        <div class="cvrs-visual" :style="artworkStyle">
          <Icon name="television-simple" :size="42" />
          <div class="cvrs-visual-scrim" />
          <span class="cvrs-output"><Icon name="cast" :size="12" /> {{ transportLabel }}</span>
        </div>

        <div class="cvrs-meta">
          <strong>{{ session.title || 'Video' }}</strong>
          <span>{{ stateLabel }} on {{ session.device_name }}</span>
        </div>

        <div class="cvrs-seek">
          <span>{{ formatTime(displayPosition) }}</span>
          <AppSlider
            :model-value="displayPosition"
            :min="0"
            :max="duration || 1"
            :step="1"
            aria-label="Video position"
            class="cvrs-seek-slider"
            @update:model-value="queueSeek"
          />
          <span>{{ formatTime(duration) }}</span>
        </div>

        <div class="cvrs-transport">
          <button type="button" aria-label="Rewind 10 seconds" @click="skip(-10)"><Icon name="skipback" :size="24" /></button>
          <button type="button" class="cvrs-play" :aria-label="playing ? 'Pause' : 'Play'" @click="togglePlayback">
            <Icon :name="busy ? 'loading' : (playing ? 'pause' : 'play')" :size="30" :class="{ 'cvrs-spin': busy }" />
          </button>
          <button type="button" aria-label="Forward 10 seconds" @click="skip(10)"><Icon name="skipforward" :size="24" /></button>
          <button type="button" :disabled="!cast.videoQueue.length" aria-label="Next episode" @click="playNext"><Icon name="next" :size="24" /></button>
        </div>

        <div class="cvrs-volume">
          <button type="button" :aria-label="session.volume === 0 ? 'Unmute' : 'Mute'" @click="toggleMute">
            <Icon :name="session.volume === 0 ? 'volmute' : 'vol'" :size="18" />
          </button>
          <AppSlider
            :model-value="session.volume"
            :min="0"
            :max="100"
            :step="1"
            aria-label="Remote volume"
            class="cvrs-volume-slider"
            @update:model-value="setVolume"
          />
        </div>

        <div class="cvrs-options" :class="{ loading: cast.videoStreamInfoLoading }">
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
              <option v-for="(track, index) in subtitleTracks" :key="track.index" :value="index" :disabled="!cast.isClientDevice && track.delivery !== 'external'">
                {{ subtitleLabel(track, index) }}
              </option>
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

        <p v-if="cast.videoStreamInfoError" class="cvrs-error">Video options could not be loaded. Playback controls still work.</p>

        <button type="button" class="cvrs-queue-hint" @click="scrollToQueue">
          <Icon name="chevdown" :size="14" />
          <span>{{ cast.videoQueue.length ? `${cast.videoQueue.length} up next` : 'Up next' }}</span>
        </button>
      </section>

      <section ref="queuePane" class="cvrs-queue">
        <header>
          <span><small>VIDEO QUEUE</small><strong>Up Next</strong></span>
          <Icon name="queue" :size="19" />
        </header>
        <div v-if="cast.videoQueueLoading" class="cvrs-queue-empty">Loading episodes…</div>
        <div v-else-if="!cast.videoQueue.length" class="cvrs-queue-empty">Nothing else is queued.</div>
        <button
          v-for="(item, index) in cast.videoQueue"
          :key="`${item.entityId}:${item.fileId}`"
          type="button"
          class="cvrs-queue-item"
          @click="playQueueItem(item)"
        >
          <span class="cvrs-queue-index">{{ index + 1 }}</span>
          <span class="cvrs-queue-copy">
            <strong>{{ item.title }}</strong>
            <small>{{ item.runtimeSeconds ? formatTime(item.runtimeSeconds) : 'Episode' }}</small>
          </span>
          <Icon name="play" :size="16" />
        </button>
        <button v-if="session" type="button" class="cvrs-stop" @click="stopPlayback">
          <Icon name="close" :size="15" /> Stop playback on {{ session.device_name }}
        </button>
      </section>
    </div>
  </AppSheet>
</template>

<script setup lang="ts">
import { DrawerTitle } from 'reka-ui'
import type { StreamAudio, StreamSubtitle } from '~~/shared/types'
import type { VideoQueueItem } from '~/composables/useCast'

const open = defineModel<boolean>('open', { default: false })
const cast = useCastStore()
const { toast } = useToast()
const session = computed(() => cast.session?.media_kind === 'video' ? cast.session : null)
const audioTracks = computed(() => cast.videoStreamInfo?.audio ?? [])
const subtitleTracks = computed(() => cast.videoStreamInfo?.subtitle ?? [])
const qualities = computed(() => cast.videoStreamInfo?.qualities ?? [])
const playing = computed(() => session.value?.state === 'playing' || session.value?.state === 'starting')
const busy = computed(() => cast.connecting || session.value?.state === 'starting')
const duration = computed(() => Math.max(0, session.value?.duration_sec ?? cast.videoStreamInfo?.duration ?? 0))
const stateLabel = computed(() => busy.value ? 'Connecting' : playing.value ? 'Playing' : 'Paused')
const transportLabel = computed(() => cast.isClientDevice ? 'HeyaConnect' : 'Chromecast')
const artworkStyle = computed(() => session.value?.media_item_id
  ? { backgroundImage: `url(${useBackdropUrl(session.value.media_item_id)})` }
  : {})

const queuePane = ref<HTMLElement | null>(null)
const clockPosition = ref(0)
let clock: ReturnType<typeof setInterval> | null = null
let seekTimer: ReturnType<typeof setTimeout> | null = null
let volumeBeforeMute = 30

const displayPosition = computed(() => clockPosition.value)
function syncClock() { clockPosition.value = cast.livePositionSec() }
watch(() => [session.value?.id, session.value?.position_sec, session.value?.state], syncClock, { immediate: true })
watch(() => session.value?.file_id, (fileID) => { if (fileID) void cast.loadVideoStreamInfo(fileID) }, { immediate: true })
watch(session, (value) => { if (!value) open.value = false })
onMounted(() => { clock = setInterval(syncClock, 500) })
onScopeDispose(() => {
  if (clock) clearInterval(clock)
  if (seekTimer) clearTimeout(seekTimer)
})

async function act(action: () => Promise<unknown>, fallback: string) {
  try { await action() } catch (error) { toast.err(error instanceof Error ? error.message : fallback) }
}
function togglePlayback() {
  if (!session.value || busy.value) return
  void act(() => playing.value ? cast.pause() : cast.resume(), 'Could not control remote playback')
}
function skip(delta: number) {
  const target = Math.max(0, Math.min(duration.value || Number.MAX_SAFE_INTEGER, cast.livePositionSec() + delta))
  clockPosition.value = target
  void act(() => cast.seekTo(target), 'Could not seek remote playback')
}
function queueSeek(value: number) {
  clockPosition.value = value
  if (seekTimer) clearTimeout(seekTimer)
  seekTimer = setTimeout(() => void act(() => cast.seekTo(value), 'Could not seek remote playback'), 180)
}
function setVolume(value: number) {
  if (value > 0) volumeBeforeMute = value
  cast.setVolume(value)
}
function toggleMute() {
  const level = session.value?.volume ?? 0
  if (level > 0) { volumeBeforeMute = level; cast.setVolume(0) }
  else cast.setVolume(volumeBeforeMute)
}
function changeAudio(event: Event) {
  void act(() => cast.updateVideo({ audioTrack: Number((event.target as HTMLSelectElement).value) }), 'Could not change the audio track')
}
function changeSubtitle(event: Event) {
  const value = Number((event.target as HTMLSelectElement).value)
  void act(() => cast.updateVideo({ subtitleTrack: value >= 0 ? value : null }), 'Could not change subtitles')
}
function changeQuality(event: Event) {
  void act(() => cast.updateVideo({ quality: (event.target as HTMLSelectElement).value }), 'Could not change video quality')
}
function scrollToQueue() { queuePane.value?.scrollIntoView({ behavior: 'smooth' }) }
function playNext() {
  const item = cast.videoQueue[0]
  if (item) void playQueueItem(item)
}
async function playQueueItem(item: VideoQueueItem) {
  await act(() => cast.playVideoQueueItem(item), 'Could not play this episode')
  document.querySelector('.cvrs-scroll')?.scrollTo({ top: 0, behavior: 'smooth' })
}
async function stopPlayback() {
  await act(() => cast.disconnect(), 'Could not stop remote playback')
  open.value = false
}
function audioLabel(track: StreamAudio, index: number) {
  return [track.language?.toUpperCase(), track.title, track.channels ? `${track.channels}ch` : ''].filter(Boolean).join(' · ') || `Track ${index + 1}`
}
function subtitleLabel(track: StreamSubtitle, index: number) {
  const name = track.title || track.language?.toUpperCase() || `Track ${index + 1}`
  return !cast.isClientDevice && track.delivery !== 'external' ? `${name} · unavailable` : name
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

<style>
.cvrs-header { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.cvrs-header > span { display: flex; min-width: 0; flex-direction: column; gap: 2px; }
.cvrs-header .app-sheet-title { margin: 0; }
.cvrs-header small { overflow: hidden; color: var(--fg-3); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.cvrs-header button { display: inline-grid; place-items: center; width: 34px; height: 34px; flex-shrink: 0; border: 0; border-radius: 50%; background: transparent; color: var(--fg-2); cursor: pointer; }
.cvrs-header button:active { background: rgb(var(--ink) / 0.08); }
.cvrs-scroll { height: 100%; overflow-y: auto; scroll-snap-type: y proximity; overscroll-behavior: contain; }
.cvrs-now { display: flex; min-height: 100%; flex-direction: column; gap: 17px; scroll-snap-align: start; scroll-snap-stop: always; }
.cvrs-visual { position: relative; display: grid; place-items: center; width: min(88vw, 520px); aspect-ratio: 16 / 9; margin: auto auto 0; overflow: hidden; border: 1px solid var(--border); border-radius: var(--r-lg); background-color: var(--bg-2); background-position: center; background-size: cover; color: var(--fg-3); box-shadow: var(--shadow-3); }
.cvrs-visual-scrim { position: absolute; inset: 0; background: linear-gradient(180deg, transparent 45%, rgb(var(--shade) / 0.68)); }
.cvrs-output { position: absolute; left: 12px; bottom: 11px; display: inline-flex; align-items: center; gap: 5px; color: var(--fg-1); font-family: var(--font-mono); font-size: 9px; font-weight: 700; letter-spacing: 0.08em; text-transform: uppercase; }
.cvrs-meta { display: flex; min-width: 0; flex-direction: column; gap: 4px; text-align: center; }
.cvrs-meta strong { overflow: hidden; color: var(--fg-0); font-size: 19px; text-overflow: ellipsis; white-space: nowrap; }
.cvrs-meta span { overflow: hidden; color: var(--fg-2); font-size: 13px; text-overflow: ellipsis; white-space: nowrap; }
.cvrs-seek { display: flex; align-items: center; gap: 10px; }
.cvrs-seek > span { min-width: 34px; color: var(--fg-3); font-family: var(--font-mono); font-size: 10px; text-align: center; }
.cvrs-seek-slider { flex: 1; min-width: 0; }
.cvrs-transport { display: flex; align-items: center; justify-content: center; gap: 10px; }
.cvrs-transport button, .cvrs-volume button { display: inline-grid; place-items: center; width: 46px; height: 46px; flex: 0 0 46px; border: 0; border-radius: 50%; background: transparent; color: var(--fg-1); cursor: pointer; }
.cvrs-transport button:active, .cvrs-volume button:active { background: rgb(var(--ink) / 0.08); }
.cvrs-transport button:disabled { opacity: 0.32; cursor: default; }
.cvrs-transport .cvrs-play { width: 64px; height: 64px; flex-basis: 64px; background: var(--gold); color: var(--bg-0); box-shadow: 0 10px 24px var(--gold-glow); }
.cvrs-volume { display: flex; align-items: center; gap: 10px; width: min(82vw, 380px); margin: 0 auto; }
.cvrs-volume-slider { flex: 1; min-width: 0; }
.cvrs-options { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; transition: opacity 0.15s ease; }
.cvrs-options.loading { opacity: 0.55; }
.cvrs-options label { display: flex; min-width: 0; flex-direction: column; gap: 5px; }
.cvrs-options label > span { display: flex; align-items: center; gap: 5px; color: var(--fg-3); font-family: var(--font-mono); font-size: 8px; font-weight: 700; letter-spacing: 0.07em; text-transform: uppercase; }
.cvrs-options select { min-width: 0; height: 38px; padding: 0 28px 0 10px; border: 1px solid var(--border); border-radius: var(--r-sm); background: var(--bg-2); color: var(--fg-1); font: inherit; font-size: 11px; text-overflow: ellipsis; }
.cvrs-error { margin: 0; color: var(--bad); font-size: 10px; text-align: center; }
.cvrs-queue-hint { display: flex; align-items: center; justify-content: center; gap: 5px; margin: auto auto 0; padding: 4px 12px 6px; border: 0; background: transparent; color: var(--fg-3); font: inherit; font-size: 11px; cursor: pointer; }
.cvrs-queue { min-height: 100%; padding-top: 14px; scroll-snap-align: start; }
.cvrs-queue > header { display: flex; align-items: center; justify-content: space-between; padding: 10px 4px 16px; color: var(--fg-2); }
.cvrs-queue > header > span { display: flex; flex-direction: column; gap: 3px; }
.cvrs-queue > header small { color: var(--gold); font-family: var(--font-mono); font-size: 8px; font-weight: 700; letter-spacing: 0.1em; }
.cvrs-queue > header strong { color: var(--fg-0); font-size: 20px; }
.cvrs-queue-empty { padding: 50px 10px; color: var(--fg-3); font-size: 13px; text-align: center; }
.cvrs-queue-item { display: flex; align-items: center; gap: 12px; width: 100%; min-height: 62px; padding: 8px 10px; border: 0; border-bottom: 1px solid var(--border); background: transparent; color: var(--fg-2); font: inherit; text-align: left; cursor: pointer; }
.cvrs-queue-item:active { background: rgb(var(--ink) / 0.06); }
.cvrs-queue-index { width: 22px; flex: 0 0 22px; color: var(--fg-3); font-family: var(--font-mono); font-size: 10px; text-align: center; }
.cvrs-queue-copy { display: flex; min-width: 0; flex: 1; flex-direction: column; gap: 4px; }
.cvrs-queue-copy strong { overflow: hidden; color: var(--fg-0); font-size: 13px; text-overflow: ellipsis; white-space: nowrap; }
.cvrs-queue-copy small { color: var(--fg-3); font-size: 10px; }
.cvrs-stop { display: flex; align-items: center; justify-content: center; gap: 7px; width: 100%; min-height: 44px; margin: 22px 0; border: 1px solid color-mix(in srgb, var(--bad) 24%, var(--border)); border-radius: var(--r-md); background: color-mix(in srgb, var(--bad) 6%, transparent); color: var(--bad); font: inherit; font-size: 12px; font-weight: 600; cursor: pointer; }
.cvrs-spin { animation: cvrs-spin 0.9s linear infinite; }
@keyframes cvrs-spin { to { transform: rotate(360deg); } }

@media (max-width: 520px) {
  .cvrs-now { padding-bottom: 4px; }
  .cvrs-visual { width: min(88vw, 420px); }
  .cvrs-options { grid-template-columns: 1fr; gap: 7px; }
  .cvrs-options label { display: grid; grid-template-columns: 92px minmax(0, 1fr); align-items: center; }
}
</style>
