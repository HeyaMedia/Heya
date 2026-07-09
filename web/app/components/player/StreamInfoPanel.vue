<script setup lang="ts">
import type { StreamInfoResponse, TranscodeReasonTag } from '~~/shared/types'
import type { HeyaPlayerState } from '~/composables/useHeyaPlayer'
import type { TranscodeStatus } from '~/composables/useTranscodeStatus'

const props = defineProps<{
  streamInfo: StreamInfoResponse | null
  fileId: string | number
  activeQuality?: string
  usingHLS?: boolean
  playerState?: HeyaPlayerState
  transcodeStatus?: TranscodeStatus | null
  mode?: 'compact' | 'detailed'
}>()

const emit = defineEmits<{
  'update:mode': [mode: 'compact' | 'detailed']
}>()

const mode = computed(() => props.mode ?? 'compact')
function toggleMode() {
  emit('update:mode', mode.value === 'compact' ? 'detailed' : 'compact')
}

const video = computed(() => props.streamInfo?.video?.[0])
const audio = computed(() => props.streamInfo?.audio || [])
const subs = computed(() => props.streamInfo?.subtitle || [])

// Effective playback decision reflects what's currently happening:
//   - Direct play: no HLS in use
//   - User picked a specific quality: transcode override (regardless of base plan)
//   - Otherwise: backend's base decision (remux / transcode)
const decision = computed(() => {
  const base = props.streamInfo?.playback
  if (!base) return null
  if (props.usingHLS === false) {
    return { ...base, action: 'direct_play', copy_video: true, copy_audio: true, reason: 'direct play (raw file)', reasons: [] }
  }
  if (props.activeQuality && props.activeQuality !== 'auto') {
    return {
      ...base,
      action: 'transcode',
      copy_video: false,
      copy_audio: false,
      reason: `transcoding to ${props.activeQuality}`,
      profile: props.activeQuality,
    }
  }
  return base
})

const REASON_LABELS: Record<TranscodeReasonTag, string> = {
  container: 'Container',
  video_codec: 'Video Codec',
  audio_codec: 'Audio Codec',
  bit_depth: 'Bit Depth',
  hdr: 'HDR',
  audio_channels: 'Audio Channels',
  quality_override: 'Quality',
  codec_tag: 'Codec Tag',
  rotation: 'Rotation',
  interlaced: 'Interlaced',
  anamorphic: 'Anamorphic',
  lossless_audio: 'Lossless Audio',
  dolby_vision: 'Dolby Vision',
}
function reasonLabel(tag: TranscodeReasonTag): string { return REASON_LABELS[tag] ?? tag }

// --- Derived player metrics -----------------------------------------------

const bufferAhead = computed(() => {
  const s = props.playerState
  if (!s) return 0
  return Math.max(0, s.buffered - s.currentTime)
})

const droppedRatio = computed(() => {
  const s = props.playerState
  if (!s || !s.decodedFrames) return 0
  return s.droppedFrames / s.decodedFrames
})

// Transcoder pacing — how many seconds of video ahead of the player has
// ffmpeg already produced. Useful to spot "encode can't keep up" situations.
const transcoderLead = computed(() => {
  const t = props.transcodeStatus
  const p = props.playerState
  if (!t || !p || !t.active) return null
  return t.out_time_seconds - p.currentTime
})

const eta = computed(() => {
  const t = props.transcodeStatus
  if (!t || !t.active || t.speed <= 0) return null
  const dur = props.streamInfo?.duration ?? 0
  if (!dur) return null
  const remaining = Math.max(0, dur - t.out_time_seconds)
  return remaining / t.speed
})

// --- Formatters -----------------------------------------------------------

function fmtBr(br: number | string | undefined) {
  if (!br) return '—'
  const n = typeof br === 'string' ? parseInt(br, 10) : br
  if (isNaN(n)) return '—'
  if (n > 1_000_000) return `${(n / 1_000_000).toFixed(1)} Mbps`
  if (n > 1_000) return `${Math.round(n / 1_000)} kbps`
  return `${n} bps`
}

function fmtBps(bps: number) {
  if (!bps || bps <= 0) return '—'
  if (bps > 1_000_000) return `${(bps / 1_000_000).toFixed(1)} MB/s`
  if (bps > 1_000) return `${Math.round(bps / 1_000)} KB/s`
  return `${Math.round(bps)} B/s`
}

function fmtSize(b: number | undefined) {
  if (!b || b <= 0) return '—'
  if (b > 1_073_741_824) return `${(b / 1_073_741_824).toFixed(2)} GiB`
  if (b > 1_048_576) return `${(b / 1_048_576).toFixed(1)} MiB`
  if (b > 1024) return `${Math.round(b / 1024)} KiB`
  return `${b} B`
}

function fmtKbps(kbps: number | undefined) {
  if (!kbps || kbps <= 0) return '—'
  if (kbps > 1000) return `${(kbps / 1000).toFixed(1)} Mbps`
  return `${Math.round(kbps)} kbps`
}

function channels(n: number, layout?: string) {
  if (layout) return layout
  return ({ 8: '7.1', 6: '5.1', 2: 'Stereo', 1: 'Mono' } as Record<number, string>)[n] || `${n}ch`
}

function fmtDuration(s: number | undefined) {
  if (!s || !isFinite(s)) return '—'
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), sec = Math.floor(s % 60)
  return h > 0 ? `${h}h ${m}m ${sec}s` : `${m}m ${sec}s`
}

function fmtTime(s: number) {
  if (!isFinite(s) || s < 0) return '0:00'
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), sec = Math.floor(s % 60)
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}` : `${m}:${String(sec).padStart(2, '0')}`
}

function fmtPct(x: number) {
  if (!isFinite(x)) return '—'
  return `${(x * 100).toFixed(2)}%`
}

function fmtNum(n: number) {
  return n.toLocaleString()
}

function stateLabel(state: string): string {
  return ({
    running: 'running',
    throttled: 'paused — buffer full',
    completed: 'completed',
    killed: 'cancelled',
    exited: 'stopped',
    idle: 'idle',
  } as Record<string, string>)[state] || state
}
</script>

<template>
  <div class="sip" :class="`sip-${mode}`">
    <!-- Header -->
    <div class="sip-header">
      <div class="sip-dot" />
      <span>Stream Information</span>
      <button class="sip-mode-btn" :title="mode === 'compact' ? 'Show diagnostics' : 'Hide diagnostics'" @click="toggleMode">
        {{ mode === 'compact' ? 'Detailed' : 'Compact' }}
      </button>
      <span class="sip-id">#{{ fileId }}</span>
    </div>

    <!-- Decision (always visible) -->
    <section v-if="decision" class="sip-section">
      <div class="sip-label">Decision</div>
      <div class="sip-row">
        <span class="sip-tag" :class="decision.action.replace('_', '')">{{ decision.action.replace('_', ' ') }}</span>
        <template v-if="decision.action !== 'direct_play'">
          <span class="sip-tag" :class="decision.copy_video ? 'copy' : 'encode'">V: {{ decision.copy_video ? 'copy' : 'encode' }}</span>
          <span class="sip-tag" :class="decision.copy_audio ? 'copy' : 'encode'">A: {{ decision.copy_audio ? 'copy' : 'encode' }}</span>
        </template>
      </div>
      <div class="sip-reason">{{ decision.reason }}</div>
      <div v-if="decision.reasons?.length" class="sip-row sip-reasons">
        <span v-for="r in decision.reasons" :key="r" class="sip-tag reason">{{ reasonLabel(r) }}</span>
      </div>
      <div v-if="decision.strip_dovi_el || decision.retag_hevc || decision.deinterlace || decision.rotate || decision.fix_anamorphic || decision.needs_tonemap" class="sip-row sip-fixes">
        <span v-if="decision.needs_tonemap" class="sip-tag fix">tone-map</span>
        <span v-if="decision.strip_dovi_el" class="sip-tag fix">strip DV-EL</span>
        <span v-if="decision.retag_hevc" class="sip-tag fix">retag hvc1</span>
        <span v-if="decision.deinterlace" class="sip-tag fix">deinterlace</span>
        <span v-if="decision.rotate" class="sip-tag fix">rotate {{ decision.rotate }}°</span>
        <span v-if="decision.fix_anamorphic" class="sip-tag fix">fix SAR</span>
      </div>
    </section>

    <!-- Playback (always visible) -->
    <section v-if="playerState" class="sip-section">
      <div class="sip-label">Playback</div>
      <div class="sip-pb-bar">
        <div class="sip-pb-buf" :style="{ width: (playerState.duration > 0 ? (playerState.buffered / playerState.duration) * 100 : 0) + '%' }" />
        <div class="sip-pb-fill" :style="{ width: (playerState.duration > 0 ? (playerState.currentTime / playerState.duration) * 100 : 0) + '%' }" />
      </div>
      <div class="sip-pb-times">
        <span>{{ fmtTime(playerState.currentTime) }}</span>
        <span>{{ fmtTime(bufferAhead) }} ahead</span>
        <span>{{ fmtTime(playerState.duration) }}</span>
      </div>
    </section>

    <!-- Network (always visible — shows even in compact for quick triage) -->
    <section v-if="playerState && usingHLS" class="sip-section">
      <div class="sip-label">Network</div>
      <div class="sip-kv">
        <div><span class="k">Download</span><span class="v mono">{{ fmtBps(playerState.downloadBps) }}</span></div>
        <div><span class="k">Frags loaded</span><span class="v mono">{{ playerState.fragsLoaded }}</span></div>
        <div v-if="playerState.currentLevel >= 0"><span class="k">HLS variant</span><span class="v mono">#{{ playerState.currentLevel }}</span></div>
        <div v-if="playerState.lastFragBytes">
          <span class="k">Last frag</span>
          <span class="v mono">{{ fmtSize(playerState.lastFragBytes) }} in {{ playerState.lastFragMs }} ms</span>
        </div>
      </div>
    </section>

    <!-- Detailed-only sections below -->
    <template v-if="mode === 'detailed'">

      <div class="sip-divider" />

      <!-- Source file -->
      <section v-if="streamInfo" class="sip-section">
        <div class="sip-label">Source</div>
        <div class="sip-kv">
          <div><span class="k">Container</span><span class="v">{{ streamInfo.container || '—' }}</span></div>
          <div><span class="k">Size</span><span class="v mono">{{ fmtSize(streamInfo.size) }}</span></div>
          <div><span class="k">Bitrate</span><span class="v mono">{{ fmtBr(streamInfo.bit_rate) }}</span></div>
          <div><span class="k">Duration</span><span class="v mono">{{ fmtDuration(streamInfo.duration) }}</span></div>
        </div>
      </section>

      <!-- Video -->
      <section v-if="video" class="sip-section">
        <div class="sip-label">Video</div>
        <div class="sip-kv">
          <div><span class="k">Codec</span><span class="v">{{ video.codec.toUpperCase() }}<span v-if="video.profile" class="sip-dim"> · {{ video.profile }}</span></span></div>
          <div><span class="k">Resolution</span><span class="v mono">{{ video.width }}×{{ video.height }}</span></div>
          <div v-if="video.pix_fmt"><span class="k">Pixel format</span><span class="v mono">{{ video.pix_fmt }}</span></div>
          <div v-if="video.color_space || video.color_transfer"><span class="k">Color</span><span class="v mono">{{ [video.color_space, video.color_transfer, video.color_primaries].filter(Boolean).join(' / ') }}</span></div>
          <div v-if="video.hdr"><span class="k">HDR</span><span class="v"><span class="sip-tag hdr">HDR</span></span></div>
          <div v-if="video.bit_rate"><span class="k">Bitrate</span><span class="v mono">{{ fmtBr(video.bit_rate) }}</span></div>
        </div>
      </section>

      <!-- Audio tracks -->
      <section v-if="audio.length" class="sip-section">
        <div class="sip-label">Audio · {{ audio.length }} track{{ audio.length > 1 ? 's' : '' }}</div>
        <div v-for="a in audio" :key="a.index" class="sip-track">
          <span class="sip-track-codec">{{ a.codec.toUpperCase() }}</span>
          <span>{{ channels(a.channels, a.channel_layout) }}</span>
          <span v-if="a.language" class="sip-tag lang">{{ a.language.toUpperCase() }}</span>
          <span v-if="a.title" class="sip-dim">{{ a.title }}</span>
          <span v-if="a.sample_rate" class="sip-dim">{{ a.sample_rate }}Hz</span>
          <span v-if="a.bit_rate" class="sip-dim">{{ fmtBr(a.bit_rate) }}</span>
          <span v-if="a.is_default" class="sip-tag default">default</span>
        </div>
      </section>

      <!-- Subtitle tracks -->
      <section v-if="subs.length" class="sip-section">
        <div class="sip-label">Subtitles · {{ subs.length }} track{{ subs.length > 1 ? 's' : '' }}</div>
        <div v-for="s in subs" :key="s.index" class="sip-track">
          <span class="sip-track-codec">{{ s.codec.toUpperCase() }}</span>
          <span v-if="s.language" class="sip-tag lang">{{ s.language.toUpperCase() }}</span>
          <span v-if="s.title" class="sip-dim">{{ s.title }}</span>
          <span v-if="s.is_forced" class="sip-tag forced">forced</span>
          <span v-if="s.is_hearing_impaired" class="sip-tag sdh">SDH</span>
          <span v-if="s.is_default" class="sip-tag default">default</span>
        </div>
      </section>

      <!-- Quality metrics -->
      <section v-if="playerState && playerState.decodedFrames > 0" class="sip-section">
        <div class="sip-label">Decode</div>
        <div class="sip-kv">
          <div><span class="k">Decoded frames</span><span class="v mono">{{ fmtNum(playerState.decodedFrames) }}</span></div>
          <div><span class="k">Dropped frames</span><span class="v mono" :class="{ warn: droppedRatio > 0.005 }">{{ fmtNum(playerState.droppedFrames) }} ({{ fmtPct(droppedRatio) }})</span></div>
        </div>
      </section>

      <!-- Transcoder telemetry -->
      <section v-if="transcodeStatus?.active" class="sip-section">
        <div class="sip-label">
          Transcoder
          <span v-if="transcodeStatus.state === 'running'" class="sip-pulse" />
          <span v-else class="sip-state" :class="`state-${transcodeStatus.state}`">{{ stateLabel(transcodeStatus.state) }}</span>
        </div>
        <!-- Throttle explainer: the encoder isn't broken, it's just ahead. -->
        <div v-if="transcodeStatus.state === 'throttled'" class="sip-state-note">
          Paused — {{ Math.round(transcodeStatus.lead_cap_seconds) }}s of buffer is built up ahead of playback. Will resume automatically.
        </div>
        <div v-else-if="transcodeStatus.state === 'completed'" class="sip-state-note">
          Finished — encoded into existing segments.
        </div>
        <div class="sip-kv">
          <div><span class="k">Speed</span><span class="v mono" :class="{ warn: transcodeStatus.state === 'running' && transcodeStatus.speed > 0 && transcodeStatus.speed < 1 }">{{ transcodeStatus.speed > 0 ? transcodeStatus.speed.toFixed(2) + 'x' : '—' }}</span></div>
          <div><span class="k">FPS</span><span class="v mono">{{ transcodeStatus.fps > 0 ? transcodeStatus.fps.toFixed(1) : '—' }}</span></div>
          <div><span class="k">Bitrate</span><span class="v mono">{{ fmtKbps(transcodeStatus.bitrate_kbps) }}</span></div>
          <div><span class="k">Frame</span><span class="v mono">{{ fmtNum(transcodeStatus.frame) }}</span></div>
          <div><span class="k">Output</span><span class="v mono">{{ fmtSize(transcodeStatus.total_size_bytes) }} · {{ fmtTime(transcodeStatus.out_time_seconds) }}</span></div>
          <div v-if="transcoderLead !== null"><span class="k">Lead</span><span class="v mono" :class="{ warn: transcodeStatus.state === 'running' && transcoderLead < 5 }">{{ transcoderLead >= 0 ? '+' : '' }}{{ transcoderLead.toFixed(1) }}s</span></div>
          <div v-if="eta !== null && transcodeStatus.state === 'running'"><span class="k">ETA full</span><span class="v mono">{{ fmtDuration(eta) }}</span></div>
          <div><span class="k">Segments</span><span class="v mono">{{ transcodeStatus.ready_segments }} / {{ transcodeStatus.total_segments }}</span></div>
          <div v-if="transcodeStatus.head_current_segment !== undefined"><span class="k">Head</span><span class="v mono">seg #{{ transcodeStatus.head_current_segment }} (from #{{ transcodeStatus.head_start_segment }})</span></div>
          <div><span class="k">Lead cap</span><span class="v mono">{{ Math.round(transcodeStatus.lead_cap_seconds) }}s ahead of seg #{{ transcodeStatus.last_requested_segment }}</span></div>
          <div v-if="transcodeStatus.drop_frames > 0"><span class="k">Encode drops</span><span class="v mono warn">{{ fmtNum(transcodeStatus.drop_frames) }}</span></div>
        </div>
      </section>

    </template>
  </div>
</template>

<style scoped>
.sip {
  font-family: var(--font-mono, monospace);
  font-size: 11px;
  color: rgba(255, 255, 255, 0.8);
  min-width: 360px;
  max-width: 520px;
}
.sip-compact { min-width: 320px; }

.sip-header {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: rgba(255, 255, 255, 0.4);
  margin-bottom: 12px;
}
.sip-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--gold, #e6b94a); box-shadow: 0 0 8px rgba(230,185,74,0.4); }
.sip-id { margin-left: auto; color: rgba(255,255,255,0.2); font-size: 9px; }
.sip-mode-btn {
  font: inherit; font-size: 9px;
  background: rgba(255,255,255,0.06); color: rgba(255,255,255,0.6);
  border: 1px solid rgba(255,255,255,0.08);
  padding: 2px 8px; border-radius: 10px;
  cursor: pointer; letter-spacing: 0.06em;
  text-transform: uppercase;
}
.sip-mode-btn:hover { background: rgba(255,255,255,0.10); color: rgba(255,255,255,0.9); }

.sip-section { margin-bottom: 12px; }
.sip-label {
  display: flex; align-items: center; gap: 6px;
  font-size: 9px; font-weight: 600; text-transform: uppercase;
  letter-spacing: 0.08em; color: rgba(255,255,255,0.35);
  margin-bottom: 4px;
}
.sip-val { color: rgba(255,255,255,0.9); line-height: 1.5; }
.sip-dim { color: rgba(255,255,255,0.35); }
.sip-reason { color: rgba(255,255,255,0.4); font-size: 10px; margin-top: 2px; }
.sip-divider { height: 1px; background: rgba(255,255,255,0.06); margin: 8px 0 12px; }

.sip-row { display: flex; align-items: center; gap: 5px; flex-wrap: wrap; }
.sip-reasons, .sip-fixes { margin-top: 4px; gap: 3px; }

.sip-kv { display: grid; grid-template-columns: 1fr; gap: 2px 8px; }
.sip-kv > div { display: flex; justify-content: space-between; gap: 12px; padding: 2px 0; border-bottom: 1px solid rgba(255,255,255,0.03); }
.sip-kv > div:last-child { border-bottom: none; }
.sip-kv .k { color: rgba(255,255,255,0.45); font-size: 10px; }
.sip-kv .v { color: rgba(255,255,255,0.85); text-align: right; min-width: 0; }
.sip-kv .v.mono { font-variant-numeric: tabular-nums; }
.sip-kv .v.warn { color: #ff9d6b; }

.sip-track {
  display: flex; align-items: center; gap: 6px; flex-wrap: wrap;
  padding: 3px 0;
  border-bottom: 1px solid rgba(255,255,255,0.03);
}
.sip-track:last-child { border-bottom: none; }
.sip-track-codec { font-weight: 700; color: rgba(255,255,255,0.7); min-width: 40px; }

.sip-tag {
  display: inline-block; font-size: 8px; font-weight: 700; padding: 1px 5px;
  border-radius: 3px; text-transform: uppercase; letter-spacing: 0.03em;
}
.sip-tag.directplay, .sip-tag.copy { background: rgba(80,200,120,0.12); color: #50c878; }
.sip-tag.remux { background: rgba(100,180,255,0.12); color: rgb(100,180,255); }
.sip-tag.transcode, .sip-tag.encode { background: rgba(255,100,80,0.12); color: #ff7050; }
.sip-tag.hdr { background: rgba(255,180,0,0.15); color: #ffb400; }
.sip-tag.lang { background: rgba(255,255,255,0.06); color: rgba(255,255,255,0.5); }
.sip-tag.default { background: rgba(230,185,74,0.12); color: var(--gold, #e6b94a); }
.sip-tag.forced { background: rgba(200,130,255,0.12); color: rgb(200,130,255); }
.sip-tag.sdh { background: rgba(100,180,255,0.12); color: rgb(100,180,255); }
.sip-tag.reason { background: rgba(255,180,80,0.08); color: rgba(255,180,80,0.85); }
.sip-tag.fix { background: rgba(160,140,255,0.10); color: rgb(180,160,255); }

.sip-pb-bar { position: relative; height: 4px; background: rgba(255,255,255,0.08); border-radius: 2px; overflow: hidden; margin-bottom: 6px; }
.sip-pb-buf { position: absolute; inset: 0; right: auto; background: rgba(255,255,255,0.18); }
.sip-pb-fill { position: absolute; inset: 0; right: auto; background: var(--gold, #e6b94a); }
.sip-pb-times { display: flex; justify-content: space-between; font-size: 10px; color: rgba(255,255,255,0.4); font-variant-numeric: tabular-nums; }

.sip-pulse {
  display: inline-block; width: 6px; height: 6px; border-radius: 50%;
  background: #50c878;
  box-shadow: 0 0 6px rgba(80,200,120,0.6);
  animation: sip-pulse 1.4s ease-in-out infinite;
}
@keyframes sip-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.35; }
}

.sip-state {
  font-size: 9px; font-weight: 600; padding: 1px 6px; border-radius: 3px;
  text-transform: lowercase; letter-spacing: 0.04em;
}
.sip-state.state-throttled { background: rgba(96,165,250,0.15); color: rgb(96,165,250); }
.sip-state.state-completed { background: rgba(80,200,120,0.12); color: #50c878; }
.sip-state.state-killed { background: rgba(255,255,255,0.06); color: rgba(255,255,255,0.4); }
.sip-state.state-exited { background: rgba(255,255,255,0.06); color: rgba(255,255,255,0.4); }
.sip-state.state-idle { background: rgba(255,255,255,0.06); color: rgba(255,255,255,0.4); }

.sip-state-note {
  font-size: 10px; color: rgba(255,255,255,0.5);
  padding: 4px 8px; margin: 4px 0 8px;
  background: rgba(96,165,250,0.06);
  border-left: 2px solid rgba(96,165,250,0.4);
  border-radius: 3px;
  line-height: 1.4;
}
</style>
