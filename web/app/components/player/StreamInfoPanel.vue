<script setup lang="ts">
import type { StreamInfoResponse } from '~~/shared/types'

const props = defineProps<{
  streamInfo: StreamInfoResponse | null
  fileId: number
}>()

const video = computed(() => props.streamInfo?.video?.[0])
const audio = computed(() => props.streamInfo?.audio || [])
const subs = computed(() => props.streamInfo?.subtitle || [])
const decision = computed(() => props.streamInfo?.playback)

function fmtBr(br: number | string | undefined) {
  if (!br) return '—'
  const n = typeof br === 'string' ? parseInt(br, 10) : br
  if (isNaN(n)) return '—'
  if (n > 1_000_000) return `${(n / 1_000_000).toFixed(1)} Mbps`
  if (n > 1_000) return `${Math.round(n / 1_000)} kbps`
  return `${n} bps`
}

function fmtSize(b: number) {
  if (!b) return '—'
  if (b > 1_073_741_824) return `${(b / 1_073_741_824).toFixed(2)} GiB`
  if (b > 1_048_576) return `${(b / 1_048_576).toFixed(1)} MiB`
  return `${Math.round(b / 1024)} KiB`
}

function channels(n: number, layout?: string) {
  if (layout) return layout
  return ({ 8: '7.1', 6: '5.1', 2: 'Stereo', 1: 'Mono' } as Record<number, string>)[n] || `${n}ch`
}

function fmtDuration(s: number) {
  if (!s || !isFinite(s)) return '—'
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), sec = Math.floor(s % 60)
  return h > 0 ? `${h}h ${m}m ${sec}s` : `${m}m ${sec}s`
}
</script>

<template>
  <div class="sip">
    <!-- Header -->
    <div class="sip-header">
      <div class="sip-dot" />
      <span>Stream Information</span>
      <span class="sip-id">#{{ fileId }}</span>
    </div>

    <!-- Decision -->
    <div v-if="decision" class="sip-section">
      <div class="sip-row">
        <span class="sip-tag" :class="decision.action.replace('_', '')">{{ decision.action.replace('_', ' ') }}</span>
        <template v-if="decision.action !== 'direct_play'">
          <span class="sip-tag" :class="decision.copy_video ? 'copy' : 'encode'">V: {{ decision.copy_video ? 'copy' : 'encode' }}</span>
          <span class="sip-tag" :class="decision.copy_audio ? 'copy' : 'encode'">A: {{ decision.copy_audio ? 'copy' : 'encode' }}</span>
        </template>
      </div>
      <div class="sip-reason">{{ decision.reason }}</div>
    </div>

    <div class="sip-divider" />

    <!-- File -->
    <div class="sip-section">
      <div class="sip-label">File</div>
      <div class="sip-val">{{ streamInfo?.container || '—' }} · {{ fmtSize(streamInfo?.size || 0) }} · {{ fmtBr(streamInfo?.bit_rate) }} · {{ fmtDuration(streamInfo?.duration || 0) }}</div>
    </div>

    <!-- Video -->
    <div v-if="video" class="sip-section">
      <div class="sip-label">Video</div>
      <div class="sip-val">
        {{ video.codec.toUpperCase() }} · {{ video.width }}×{{ video.height }}
        <span v-if="video.hdr" class="sip-tag hdr">HDR</span>
        <span v-if="video.profile"> · {{ video.profile }}</span>
      </div>
      <div class="sip-sub">
        <template v-if="video.pix_fmt">{{ video.pix_fmt }}</template>
        <template v-if="video.color_space"> · {{ video.color_space }}</template>
        <template v-if="video.bit_rate"> · {{ fmtBr(video.bit_rate) }}</template>
      </div>
    </div>

    <!-- Audio tracks -->
    <div v-if="audio.length" class="sip-section">
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
    </div>

    <!-- Subtitle tracks -->
    <div v-if="subs.length" class="sip-section">
      <div class="sip-label">Subtitles · {{ subs.length }} track{{ subs.length > 1 ? 's' : '' }}</div>
      <div v-for="s in subs" :key="s.index" class="sip-track">
        <span class="sip-track-codec">{{ s.codec.toUpperCase() }}</span>
        <span v-if="s.language" class="sip-tag lang">{{ s.language.toUpperCase() }}</span>
        <span v-if="s.title" class="sip-dim">{{ s.title }}</span>
        <span v-if="s.is_forced" class="sip-tag forced">forced</span>
        <span v-if="s.is_hearing_impaired" class="sip-tag sdh">SDH</span>
        <span v-if="s.is_default" class="sip-tag default">default</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.sip {
  font-family: var(--font-mono, monospace);
  font-size: 11px;
  color: rgba(255, 255, 255, 0.8);
  min-width: 340px;
  max-width: 480px;
}

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
.sip-id { margin-left: auto; color: rgba(255,255,255,0.2); }

.sip-section { margin-bottom: 10px; }
.sip-label { font-size: 9px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.08em; color: rgba(255,255,255,0.35); margin-bottom: 3px; }
.sip-val { color: rgba(255,255,255,0.9); line-height: 1.5; }
.sip-sub { color: rgba(255,255,255,0.4); font-size: 10px; margin-top: 1px; }
.sip-dim { color: rgba(255,255,255,0.35); }
.sip-reason { color: rgba(255,255,255,0.4); font-size: 10px; margin-top: 2px; }
.sip-divider { height: 1px; background: rgba(255,255,255,0.06); margin: 10px 0; }

.sip-row { display: flex; align-items: center; gap: 5px; flex-wrap: wrap; }

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
</style>
