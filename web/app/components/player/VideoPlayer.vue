<script setup lang="ts">
import AkariSub from 'akarisub'
import type { StreamAudio, StreamSubtitle } from '~~/shared/types'

const props = defineProps<{ fileId: number; mediaItemId: number | null; title?: string }>()
const emit = defineEmits<{ close: [] }>()

const { token } = useAuth()
const videoEl = ref<HTMLVideoElement>()
const { state, controls, loadSource, destroyHLS } = useHeyaPlayer(videoEl)
const fileIdRef = computed(() => props.fileId)
const mediaItemIdRef = computed(() => props.mediaItemId)
const { state: streamState, loadStreamInfo, subtitleUrl } = useVideoPlayer(fileIdRef, mediaItemIdRef)

const controlsVisible = ref(true)
const showInfoPanel = ref(false)
const showSubMenu = ref(false)
const showAudioMenu = ref(false)
const seekHover = ref<number | null>(null)
const activeSubIdx = ref(-1)
const activeAudioIdx = ref(0)
let assRenderer: AkariSub | null = null
let hideTimer: ReturnType<typeof setTimeout> | null = null
const sessionId = Math.random().toString(36).slice(2, 10)

const knownDuration = computed(() => streamState.streamInfo?.duration || state.duration)
const progress = computed(() => knownDuration.value > 0 ? (state.currentTime / knownDuration.value) * 100 : 0)
const bufferProgress = computed(() => knownDuration.value > 0 ? (state.buffered / knownDuration.value) * 100 : 0)
const audioTracks = computed<StreamAudio[]>(() => streamState.streamInfo?.audio || [])
const subtitleTracks = computed<StreamSubtitle[]>(() => streamState.streamInfo?.subtitle || [])

function buildHLSUrl() {
  const caps = useClientCaps()
  const params = new URLSearchParams({ token: token.value!, sid: sessionId })
  for (const [k, v] of Object.entries(caps)) { if (v) params.set(k, '1') }
  if (activeAudioIdx.value > 0) params.set('audio', String(activeAudioIdx.value))
  return `/api/stream/${props.fileId}/hls/master.m3u8?${params}`
}

async function init() {
  await loadStreamInfo()
  const action = streamState.streamInfo?.playback?.action
  if (action === 'direct_play') {
    loadSource(`/api/stream/${props.fileId}?token=${token.value}`, token.value!)
  } else {
    loadSource(buildHLSUrl(), token.value!)
  }
  const defaultSub = subtitleTracks.value.findIndex(s => s.is_default)
  if (defaultSub >= 0) { activeSubIdx.value = defaultSub; initASS() }
}

function destroyASS() { if (assRenderer) { assRenderer.destroy(); assRenderer = null } }

function initASS() {
  destroyASS()
  if (activeSubIdx.value < 0 || !videoEl.value) return
  const sub = subtitleTracks.value[activeSubIdx.value]
  if (!sub) return
  const isASS = sub.codec === 'ass' || sub.codec === 'ssa'
  if (!isASS) return
  try {
    assRenderer = new AkariSub({
      video: videoEl.value,
      subUrl: subtitleUrl(sub.index),
      workerUrl: '/akarisub/akarisub-worker.js',
      wasmUrl: '/akarisub/akarisub-worker.wasm',
      availableFonts: { 'liberation sans': '/akarisub/default.woff2' },
      timeOffset: 0,
    })
    assRenderer.addEventListener('error', (e: any) => {
      console.warn('AkariSub render error:', e?.error?.message || e)
      destroyASS()
    })
  } catch (e) {
    console.warn('AkariSub init failed:', e)
    assRenderer = null
  }
}

function selectSub(idx: number) { activeSubIdx.value = idx; showSubMenu.value = false; initASS() }
function disableSubs() { activeSubIdx.value = -1; showSubMenu.value = false; destroyASS() }
function selectAudio(idx: number) {
  if (idx === activeAudioIdx.value) { showAudioMenu.value = false; return }
  const currentTime = state.currentTime
  activeAudioIdx.value = idx
  showAudioMenu.value = false
  if (streamState.streamInfo?.playback?.action !== 'direct_play') {
    loadSource(buildHLSUrl(), token.value!)
    const v = videoEl.value
    if (v) {
      const onReady = () => { v.currentTime = currentTime; v.removeEventListener('canplay', onReady) }
      v.addEventListener('canplay', onReady)
    }
  }
}
function closeMenus() { showSubMenu.value = false; showAudioMenu.value = false }
function audioLabel(a: StreamAudio) {
  const p: string[] = []
  if (a.language) p.push(a.language.toUpperCase())
  if (a.title) p.push(a.title)
  if (!a.language && !a.title) p.push(`Track ${a.index}`)
  p.push(a.codec.toUpperCase())
  if (a.channel_layout) p.push(a.channel_layout)
  else if (a.channels === 6) p.push('5.1'); else if (a.channels === 8) p.push('7.1'); else if (a.channels === 2) p.push('Stereo')
  return p.join(' · ')
}

function seek(e: MouseEvent) {
  if (!knownDuration.value) return
  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
  controls.seek(Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width)) * knownDuration.value)
}
function onSeekHover(e: MouseEvent) {
  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
  seekHover.value = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width)) * knownDuration.value
}
function setVolume(e: MouseEvent) {
  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
  controls.setVolume(Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width)))
}

function handleClose() { destroyHLS(); destroyASS(); if (document.fullscreenElement) document.exitFullscreen(); emit('close') }

function showCtrl() {
  controlsVisible.value = true
  if (hideTimer) clearTimeout(hideTimer)
  hideTimer = setTimeout(() => { if (state.playing) controlsVisible.value = false }, 3000)
}

let lastTap = 0, lastTapX = 0
function onVideoClick(e: MouseEvent) {
  const now = Date.now(), x = e.clientX
  if (now - lastTap < 350 && Math.abs(x - lastTapX) < 100) {
    const w = window.innerWidth
    if (x < w * 0.3) controls.skip(-10)
    else if (x > w * 0.7) controls.skip(10)
    else controls.toggleFullscreen()
    lastTap = 0; return
  }
  lastTap = now; lastTapX = x
  setTimeout(() => { if (Date.now() - lastTap >= 300) { controls.togglePlay(); showCtrl() } }, 320)
}

function handleKeydown(e: KeyboardEvent) {
  if (showInfoPanel.value && e.key === 'Escape') { showInfoPanel.value = false; e.preventDefault(); return }
  switch (e.key) {
    case 'Escape': handleClose(); break
    case ' ': case 'k': controls.togglePlay(); break
    case 'f': controls.toggleFullscreen(); break
    case 'm': controls.toggleMute(); break
    case 'ArrowLeft': case 'j': controls.skip(-10); break
    case 'ArrowRight': case 'l': controls.skip(10); break
    case 'ArrowUp': controls.setVolume(state.volume + 0.1); break
    case 'ArrowDown': controls.setVolume(state.volume - 0.1); break
    case 'i': if (!e.ctrlKey && !e.metaKey) showInfoPanel.value = !showInfoPanel.value; break
    default: return
  }
  e.preventDefault(); showCtrl()
}

function volIcon() {
  if (state.muted || state.volume === 0) return 'speakerx'
  if (state.volume < 0.3) return 'speakernone'
  if (state.volume < 0.7) return 'speakerlow'
  return 'speakerhigh'
}

onMounted(() => { init(); window.addEventListener('keydown', handleKeydown) })
onUnmounted(() => { window.removeEventListener('keydown', handleKeydown); destroyASS(); if (hideTimer) clearTimeout(hideTimer) })
</script>

<template>
  <div class="p" @mousemove="showCtrl" @click="closeMenus">
    <!-- Loading / Error -->
    <div v-if="streamState.loading" class="p-center"><div class="spinner" /></div>
    <div v-else-if="state.error || streamState.error" class="p-center">
      <Icon name="warning" :size="28" />
      <div style="margin-top: 12px">{{ state.error || streamState.error }}</div>
      <button class="btn btn-secondary" style="margin-top: 16px" @click="handleClose">Go Back</button>
    </div>

    <template v-else>
      <video ref="videoEl" @click="onVideoClick" />

      <!-- Buffering -->
      <div v-if="state.buffering" class="p-center" style="pointer-events: none">
        <div class="spinner-lg" />
      </div>

      <!-- Controls -->
      <div class="ctrl" :class="{ visible: controlsVisible || state.paused || state.buffering }">
        <!-- Top -->
        <div class="ctrl-top">
          <button class="c-btn" @click="handleClose"><Icon name="chevleft" :size="20" /></button>
          <div class="ctrl-title">{{ title }}</div>
          <button class="c-btn" :class="{ active: showInfoPanel }" @click="showInfoPanel = !showInfoPanel"><Icon name="info" :size="18" /></button>
        </div>

        <!-- Center play -->
        <div class="ctrl-center" @click.stop="controls.togglePlay()">
          <button class="center-btn">
            <Icon :name="state.paused ? 'play' : 'pause'" :size="40" />
          </button>
        </div>

        <!-- Bottom -->
        <div class="ctrl-bottom" @click.stop>
          <!-- Seek -->
          <div class="seekbar" @click="seek" @mousemove="onSeekHover" @mouseleave="seekHover = null">
            <div class="seekbar-bg" />
            <div class="seekbar-buf" :style="{ width: bufferProgress + '%' }" />
            <div class="seekbar-fill" :style="{ width: progress + '%' }" />
            <div class="seekbar-thumb" :style="{ left: progress + '%' }" />
            <div v-if="seekHover !== null" class="seekbar-tip" :style="{ left: ((seekHover / knownDuration) * 100) + '%' }">{{ formatTime(seekHover) }}</div>
          </div>

          <div class="ctrl-row">
            <button class="c-btn" @click="controls.togglePlay()"><Icon :name="state.paused ? 'play' : 'pause'" :size="22" /></button>
            <button class="c-btn" @click="controls.skip(-10)"><Icon name="skipback" :size="18" /></button>
            <button class="c-btn" @click="controls.skip(10)"><Icon name="skipforward" :size="18" /></button>

            <div class="vol-group">
              <button class="c-btn" @click="controls.toggleMute()"><Icon :name="volIcon()" :size="18" /></button>
              <div class="vol-bar" @click="setVolume"><div class="vol-fill" :style="{ width: (state.muted ? 0 : state.volume * 100) + '%' }" /></div>
            </div>

            <div class="time">{{ formatTime(state.currentTime) }} <span class="time-sep">/</span> {{ formatTime(knownDuration) }}</div>
            <div style="flex: 1" />

            <!-- Audio -->
            <div v-if="audioTracks.length > 1" class="menu-anchor">
              <button class="c-btn" :class="{ active: showAudioMenu }" @click.stop="showAudioMenu = !showAudioMenu; showSubMenu = false">
                <Icon name="translate" :size="18" />
              </button>
              <Transition name="pop">
                <div v-if="showAudioMenu" class="popup" @click.stop>
                  <div class="popup-title">Audio</div>
                  <button v-for="(a, i) in audioTracks" :key="a.index" class="popup-item" :class="{ active: i === activeAudioIdx }" @click="selectAudio(i)">
                    <Icon v-if="i === activeAudioIdx" name="check" :size="14" />
                    <span>{{ audioLabel(a) }}</span>
                  </button>
                </div>
              </Transition>
            </div>

            <!-- Subs -->
            <div v-if="subtitleTracks.length" class="menu-anchor">
              <button class="c-btn" :class="{ active: showSubMenu || activeSubIdx >= 0 }" @click.stop="showSubMenu = !showSubMenu; showAudioMenu = false">
                <Icon name="subtitles" :size="18" />
              </button>
              <Transition name="pop">
                <div v-if="showSubMenu" class="popup" @click.stop>
                  <div class="popup-title">Subtitles</div>
                  <button class="popup-item" :class="{ active: activeSubIdx === -1 }" @click="disableSubs()">
                    <Icon v-if="activeSubIdx === -1" name="check" :size="14" />
                    <span>Off</span>
                  </button>
                  <button v-for="(s, i) in subtitleTracks" :key="s.index" class="popup-item" :class="{ active: i === activeSubIdx }" @click="selectSub(i)">
                    <Icon v-if="i === activeSubIdx" name="check" :size="14" />
                    <span>{{ s.title || s.language?.toUpperCase() || `Track ${s.index}` }}</span>
                    <span v-if="s.codec === 'ass' || s.codec === 'ssa'" class="sub-tag">ASS</span>
                  </button>
                </div>
              </Transition>
            </div>

            <button class="c-btn" @click="controls.toggleFullscreen()">
              <Icon :name="state.fullscreen ? 'shrink' : 'expand'" :size="18" />
            </button>
          </div>
        </div>
      </div>

      <!-- Stream info panel -->
      <Transition name="slide">
        <div v-if="showInfoPanel" class="info-panel-wrap">
          <div class="info-panel">
            <StreamInfoPanel :stream-info="streamState.streamInfo" :file-id="fileId" />
          </div>
        </div>
      </Transition>
    </template>
  </div>
</template>

<style scoped>
.p { position: fixed; inset: 0; z-index: 9999; background: #000; }
video { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: contain; cursor: pointer; }
.p-center { position: absolute; inset: 0; display: flex; flex-direction: column; align-items: center; justify-content: center; color: rgba(255,255,255,0.5); font-size: 14px; gap: 8px; z-index: 20; }
.spinner { width: 28px; height: 28px; border: 2px solid rgba(255,255,255,0.1); border-top-color: var(--gold, #e6b94a); border-radius: 50%; animation: spin 0.7s linear infinite; }
.spinner-lg { width: 44px; height: 44px; border: 3px solid rgba(255,255,255,0.1); border-top-color: var(--gold, #e6b94a); border-radius: 50%; animation: spin 0.7s linear infinite; }
@keyframes spin { to { transform: rotate(360deg) } }

:deep(.AkariSub) { z-index: 2 !important; }

/* Controls */
.ctrl { position: absolute; inset: 0; z-index: 10; display: flex; flex-direction: column; opacity: 0; transition: opacity 0.3s; pointer-events: none; }
.ctrl.visible { opacity: 1; pointer-events: auto; }

.ctrl-top { display: flex; align-items: center; gap: 10px; padding: 16px 20px 40px; background: linear-gradient(to bottom, rgba(0,0,0,0.6), transparent); }
.ctrl-title { flex: 1; font-size: 15px; font-weight: 600; color: #fff; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

.ctrl-center { flex: 1; display: flex; align-items: center; justify-content: center; }
.center-btn { width: 72px; height: 72px; border-radius: 50%; background: rgba(0,0,0,0.4); backdrop-filter: blur(12px); border: 1px solid rgba(255,255,255,0.1); color: #fff; display: flex; align-items: center; justify-content: center; transition: all 0.2s; }
.center-btn:hover { background: rgba(0,0,0,0.6); transform: scale(1.08); }

.ctrl-bottom { padding: 40px 20px 16px; background: linear-gradient(to top, rgba(0,0,0,0.6), transparent); }

/* Seek bar */
.seekbar { position: relative; height: 28px; display: flex; align-items: center; cursor: pointer; margin-bottom: 4px; }
.seekbar-bg { position: absolute; left: 0; right: 0; height: 3px; background: rgba(255,255,255,0.12); border-radius: 2px; transition: height 0.12s; }
.seekbar:hover .seekbar-bg { height: 6px; }
.seekbar-buf { position: absolute; left: 0; height: 3px; background: rgba(255,255,255,0.18); border-radius: 2px; pointer-events: none; transition: height 0.12s; }
.seekbar:hover .seekbar-buf { height: 6px; }
.seekbar-fill { position: absolute; left: 0; height: 3px; background: var(--gold, #e6b94a); border-radius: 2px; pointer-events: none; transition: height 0.12s; }
.seekbar:hover .seekbar-fill { height: 6px; }
.seekbar-thumb { position: absolute; width: 14px; height: 14px; background: var(--gold, #e6b94a); border-radius: 50%; transform: translate(-50%, 0); opacity: 0; pointer-events: none; transition: opacity 0.15s; box-shadow: 0 0 6px rgba(230,185,74,0.4); }
.seekbar:hover .seekbar-thumb { opacity: 1; }
.seekbar-tip { position: absolute; bottom: 24px; transform: translateX(-50%); background: rgba(0,0,0,0.85); color: #fff; font-size: 11px; font-family: var(--font-mono, monospace); padding: 3px 8px; border-radius: 4px; pointer-events: none; white-space: nowrap; }

/* Controls row */
.ctrl-row { display: flex; align-items: center; gap: 2px; }
.c-btn { width: 38px; height: 38px; border-radius: 8px; display: flex; align-items: center; justify-content: center; color: rgba(255,255,255,0.8); background: transparent; transition: all 0.12s; flex-shrink: 0; }
.c-btn:hover { color: #fff; background: rgba(255,255,255,0.08); }
.c-btn.active { color: var(--gold, #e6b94a); }

/* Volume */
.vol-group { display: flex; align-items: center; gap: 4px; }
.vol-bar { width: 80px; height: 22px; display: flex; align-items: center; cursor: pointer; position: relative; }
.vol-bar::before { content: ''; position: absolute; left: 0; right: 0; height: 3px; background: rgba(255,255,255,0.15); border-radius: 2px; }
.vol-fill { position: absolute; left: 0; height: 3px; background: #fff; border-radius: 2px; pointer-events: none; }

/* Time */
.time { font-size: 12px; font-family: var(--font-mono, monospace); color: rgba(255,255,255,0.7); margin-left: 10px; white-space: nowrap; }
.time-sep { color: rgba(255,255,255,0.3); margin: 0 2px; }

/* Menus */
.menu-anchor { position: relative; }
.popup { position: absolute; bottom: 48px; right: 0; min-width: 220px; background: rgba(12,12,16,0.95); backdrop-filter: blur(16px); border: 1px solid rgba(255,255,255,0.08); border-radius: 10px; padding: 6px 0; box-shadow: 0 12px 40px rgba(0,0,0,0.5); z-index: 20; }
.popup-title { font-size: 10px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.1em; color: rgba(255,255,255,0.35); padding: 8px 14px 6px; }
.popup-item { display: flex; align-items: center; gap: 8px; width: 100%; padding: 8px 14px; font-size: 13px; color: rgba(255,255,255,0.7); transition: all 0.1s; text-align: left; }
.popup-item:hover { background: rgba(255,255,255,0.06); color: #fff; }
.popup-item.active { color: var(--gold, #e6b94a); }
.sub-tag { font-size: 9px; font-weight: 700; padding: 1px 5px; border-radius: 3px; background: rgba(200,130,255,0.12); color: rgb(200,130,255); margin-left: auto; }

.pop-enter-active { transition: all 0.15s cubic-bezier(0.2, 0, 0, 1); }
.pop-leave-active { transition: all 0.1s ease-in; }
.pop-enter-from { opacity: 0; transform: translateY(8px) scale(0.96); }
.pop-leave-to { opacity: 0; transform: translateY(4px); }

/* Info panel — no dimming, positioned top-right, doesn't block video */
.info-panel-wrap { position: absolute; top: 56px; right: 16px; z-index: 50; pointer-events: none; }
.info-panel { background: rgba(10,10,16,0.92); backdrop-filter: blur(20px) saturate(1.3); border: 1px solid rgba(255,255,255,0.06); border-radius: 12px; padding: 16px 18px; box-shadow: 0 8px 40px rgba(0,0,0,0.5); max-height: calc(100vh - 160px); overflow-y: auto; scrollbar-width: thin; scrollbar-color: rgba(255,255,255,0.1) transparent; pointer-events: auto; }

.slide-enter-active { transition: all 0.2s cubic-bezier(0.2, 0, 0, 1); }
.slide-leave-active { transition: all 0.12s ease-in; }
.slide-enter-from { opacity: 0; transform: translateX(12px); }
.slide-leave-to { opacity: 0; transform: translateX(8px); }

@media (max-width: 768px) { .vol-group { display: none; } }
</style>
