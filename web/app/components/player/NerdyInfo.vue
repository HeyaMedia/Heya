<script setup lang="ts">
import type { StreamInfoResponse } from '~~/shared/types'

const props = defineProps<{
  streamInfo: StreamInfoResponse | null
  currentTime: number
  duration: number
  buffered: number
  fileId: number
}>()

const video = computed(() => props.streamInfo?.video?.[0])
const audio = computed(() => props.streamInfo?.audio?.[0])
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
  if (b > 1_000_000_000) return `${(b / 1_073_741_824).toFixed(2)} GiB`
  if (b > 1_000_000) return `${(b / 1_048_576).toFixed(1)} MiB`
  return `${Math.round(b / 1024)} KiB`
}

function fmtTime(s: number) {
  if (!isFinite(s) || s < 0) return '0:00'
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), sec = Math.floor(s % 60)
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}` : `${m}:${String(sec).padStart(2, '0')}`
}

function channels(n: number, layout?: string) {
  if (layout) return layout
  return ({ 8: '7.1', 6: '5.1', 2: 'Stereo', 1: 'Mono' } as Record<number, string>)[n] || `${n}ch`
}
</script>

<template>
  <div class="ni">
    <div class="ni-header">
      <div class="ni-dot" />
      <span>Stream Info</span>
      <span class="ni-file-id">#{{ fileId }}</span>
    </div>

    <!-- Video -->
    <div v-if="video" class="ni-row">
      <div class="ni-icon video">V</div>
      <div class="ni-content">
        <div class="ni-primary">
          {{ video.codec.toUpperCase() }}
          <span class="ni-dim">{{ video.width }}×{{ video.height }}</span>
          <span v-if="video.hdr" class="ni-tag hdr">HDR</span>
          <span v-if="video.profile" class="ni-dim">{{ video.profile }}</span>
        </div>
        <div class="ni-secondary">
          {{ video.pix_fmt || '' }}
          <template v-if="video.color_space"> · {{ video.color_space }}</template>
          <template v-if="video.bit_rate"> · {{ fmtBr(video.bit_rate) }}</template>
        </div>
      </div>
    </div>

    <!-- Audio -->
    <div v-if="audio" class="ni-row">
      <div class="ni-icon audio">A</div>
      <div class="ni-content">
        <div class="ni-primary">
          {{ audio.codec.toUpperCase() }}
          <span class="ni-dim">{{ channels(audio.channels, audio.channel_layout) }}</span>
          <span v-if="audio.language" class="ni-tag lang">{{ audio.language.toUpperCase() }}</span>
        </div>
        <div class="ni-secondary">
          <template v-if="audio.sample_rate">{{ audio.sample_rate }} Hz</template>
          <template v-if="audio.bit_rate"> · {{ fmtBr(audio.bit_rate) }}</template>
          <template v-if="audio.title"> · {{ audio.title }}</template>
        </div>
      </div>
    </div>

    <!-- Subtitles -->
    <div v-if="streamInfo?.subtitle?.length" class="ni-row">
      <div class="ni-icon sub">S</div>
      <div class="ni-content">
        <div class="ni-primary">{{ streamInfo.subtitle.length }} track{{ streamInfo.subtitle.length > 1 ? 's' : '' }}</div>
        <div class="ni-secondary">{{ streamInfo.subtitle.map(s => s.language?.toUpperCase() || '?').join(', ') }}</div>
      </div>
    </div>

    <div class="ni-divider" />

    <!-- Decision -->
    <div v-if="decision" class="ni-row">
      <div class="ni-icon" :class="decision.action === 'direct_play' ? 'direct' : decision.action === 'remux' ? 'remux' : 'encode'">
        {{ decision.action === 'direct_play' ? '▶' : decision.action === 'remux' ? '⇄' : '⚙' }}
      </div>
      <div class="ni-content">
        <div class="ni-primary">
          <span class="ni-tag" :class="decision.action === 'direct_play' ? 'direct' : decision.action === 'remux' ? 'remux' : 'encode'">
            {{ decision.action.replace('_', ' ') }}
          </span>
          <template v-if="decision.action === 'transcode'">
            <span class="ni-tag" :class="decision.copy_video ? 'direct' : 'encode'">V:{{ decision.copy_video ? 'copy' : 'encode' }}</span>
            <span class="ni-tag" :class="decision.copy_audio ? 'direct' : 'encode'">A:{{ decision.copy_audio ? 'copy' : 'encode' }}</span>
          </template>
        </div>
        <div class="ni-secondary">{{ decision.reason }}</div>
      </div>
    </div>

    <!-- File -->
    <div class="ni-row">
      <div class="ni-icon file">F</div>
      <div class="ni-content">
        <div class="ni-primary">
          <span class="ni-dim">{{ streamInfo?.container || '—' }}</span>
          <template v-if="streamInfo?.size"> · {{ fmtSize(streamInfo.size) }}</template>
          <template v-if="streamInfo?.bit_rate"> · {{ fmtBr(streamInfo.bit_rate) }}</template>
        </div>
      </div>
    </div>

    <div class="ni-divider" />

    <!-- Playback -->
    <div class="ni-playback">
      <div class="ni-pb-bar">
        <div class="ni-pb-buf" :style="{ width: (duration > 0 ? (buffered / duration) * 100 : 0) + '%' }" />
        <div class="ni-pb-fill" :style="{ width: (duration > 0 ? (currentTime / duration) * 100 : 0) + '%' }" />
      </div>
      <div class="ni-pb-times">
        <span>{{ fmtTime(currentTime) }}</span>
        <span>{{ fmtTime(buffered) }} buffered</span>
        <span>{{ fmtTime(duration) }}</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ni {
  position: absolute;
  top: 60px;
  left: 20px;
  z-index: 100;
  background: rgba(8, 8, 14, 0.92);
  backdrop-filter: blur(20px) saturate(1.4);
  border: 1px solid rgba(255, 255, 255, 0.06);
  border-radius: 12px;
  padding: 16px 18px;
  font-family: var(--font-mono, monospace);
  font-size: 11px;
  color: rgba(255, 255, 255, 0.75);
  min-width: 320px;
  max-width: 440px;
  pointer-events: none;
  box-shadow: 0 8px 32px rgba(0,0,0,0.4);
}

.ni-header {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.12em;
  color: rgba(255, 255, 255, 0.35);
  margin-bottom: 14px;
}
.ni-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--gold, #e6b94a); box-shadow: 0 0 8px rgba(230,185,74,0.5); }
.ni-file-id { margin-left: auto; color: rgba(255,255,255,0.2); }

.ni-row { display: flex; gap: 10px; margin-bottom: 10px; }
.ni-icon {
  width: 24px; height: 24px; border-radius: 6px; flex-shrink: 0;
  display: flex; align-items: center; justify-content: center;
  font-size: 10px; font-weight: 800; letter-spacing: 0;
}
.ni-icon.video { background: rgba(100, 160, 255, 0.12); color: rgb(100, 160, 255); }
.ni-icon.audio { background: rgba(200, 130, 255, 0.12); color: rgb(200, 130, 255); }
.ni-icon.sub { background: rgba(255, 200, 80, 0.12); color: rgb(255, 200, 80); }
.ni-icon.file { background: rgba(255, 255, 255, 0.06); color: rgba(255, 255, 255, 0.4); }
.ni-icon.direct { background: rgba(80, 200, 120, 0.12); color: rgb(80, 200, 120); }
.ni-icon.remux { background: rgba(100, 180, 255, 0.12); color: rgb(100, 180, 255); }
.ni-icon.encode { background: rgba(255, 120, 80, 0.12); color: rgb(255, 120, 80); }

.ni-content { min-width: 0; }
.ni-primary { color: rgba(255, 255, 255, 0.9); line-height: 1.5; display: flex; flex-wrap: wrap; align-items: center; gap: 4px; }
.ni-secondary { color: rgba(255, 255, 255, 0.35); font-size: 10px; margin-top: 1px; }
.ni-dim { color: rgba(255, 255, 255, 0.5); }

.ni-tag {
  display: inline-block; font-size: 9px; font-weight: 700; padding: 1px 6px; border-radius: 4px;
  text-transform: uppercase; letter-spacing: 0.04em;
}
.ni-tag.hdr { background: rgba(255, 180, 0, 0.15); color: #ffb400; }
.ni-tag.lang { background: rgba(255, 255, 255, 0.06); color: rgba(255, 255, 255, 0.5); }
.ni-tag.direct { background: rgba(80, 200, 120, 0.12); color: #50c878; }
.ni-tag.remux { background: rgba(100, 180, 255, 0.12); color: rgb(100, 180, 255); }
.ni-tag.encode { background: rgba(255, 100, 80, 0.12); color: #ff7050; }

.ni-divider { height: 1px; background: rgba(255, 255, 255, 0.05); margin: 10px 0; }

.ni-playback { margin-top: 4px; }
.ni-pb-bar { position: relative; height: 3px; background: rgba(255,255,255,0.08); border-radius: 2px; overflow: hidden; margin-bottom: 6px; }
.ni-pb-buf { position: absolute; inset: 0; right: auto; background: rgba(255,255,255,0.12); }
.ni-pb-fill { position: absolute; inset: 0; right: auto; background: var(--gold, #e6b94a); }
.ni-pb-times { display: flex; justify-content: space-between; font-size: 10px; color: rgba(255,255,255,0.3); }
</style>
