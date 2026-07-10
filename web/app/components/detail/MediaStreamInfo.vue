<script setup lang="ts">
import type { StreamInfoResponse, TranscodeReasonTag } from '~~/shared/types'

defineProps<{ stream: StreamInfoResponse }>()

// Subtitles are collapsed by default — a well-tagged release can carry dozens
// of tracks, which would otherwise dwarf the video/audio rows in this panel.
const subsExpanded = ref(false)

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

const LANG_LABELS: Record<string, string> = {
  eng: 'English', jpn: 'Japanese', ger: 'German', fre: 'French', spa: 'Spanish',
  ita: 'Italian', por: 'Portuguese', rus: 'Russian', kor: 'Korean', chi: 'Chinese',
  ara: 'Arabic', hin: 'Hindi', dan: 'Danish', swe: 'Swedish', nor: 'Norwegian',
  fin: 'Finnish', dut: 'Dutch', pol: 'Polish', tur: 'Turkish', tha: 'Thai',
  vie: 'Vietnamese', und: 'Unknown', zho: 'Chinese', deu: 'German', fra: 'French',
  nld: 'Dutch', nob: 'Norwegian', ces: 'Czech', hun: 'Hungarian', ron: 'Romanian',
}
function langLabel(code: string) { return LANG_LABELS[code] || code.toUpperCase() }

function formatDuration(s: number) {
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  return h > 0 ? `${h}h ${m}m` : `${m}m`
}
function formatBitrate(bps: number) {
  if (!bps) return ''
  if (bps >= 1e6) return `${(bps / 1e6).toFixed(1)} Mbps`
  return `${(bps / 1e3).toFixed(0)} Kbps`
}
function playbackLabel(action: string) {
  return ({ direct_play: 'Direct Play', remux: 'Remux', transcode: 'Transcode' } as Record<string, string>)[action] || action
}
</script>

<template>
  <div class="stream-details">
    <div class="stream-header">
      <span class="stream-header-main">{{ stream.container.toUpperCase() }} &middot; {{ formatBytes(stream.size) }}</span>
      <span v-if="stream.playback" class="playback-decision" :class="`pd-${stream.playback.action}`">{{ playbackLabel(stream.playback.action) }}</span>
    </div>
    <div class="stream-subhead">{{ formatBitrate(stream.bit_rate) }} &middot; {{ formatDuration(stream.duration) }}</div>
    <!-- Reasons the source can't direct-play to this client. Hidden when empty
         (direct play case) so the panel stays clean. -->
    <div v-if="stream.playback?.reasons?.length" class="playback-reasons">
      <span v-for="r in stream.playback.reasons" :key="r" class="reason-chip">{{ reasonLabel(r) }}</span>
    </div>
    <div v-for="v in stream.video" :key="'v' + v.index" class="stream-track">
      <span class="track-badge vid">V{{ v.index }}</span>
      <span class="track-info">
        {{ v.width }}x{{ v.height }} &middot; {{ v.codec.toUpperCase() }}<span v-if="v.profile"> ({{ v.profile }})</span>
        <span v-if="v.hdr" class="stag hdr">HDR</span>
        <span v-if="v.bit_rate"> &middot; {{ formatBitrate(parseInt(v.bit_rate)) }}</span>
        <span v-if="v.is_default" class="stag default">Default</span>
      </span>
    </div>
    <div v-for="a in stream.audio" :key="'a' + a.index" class="stream-track">
      <span class="track-badge aud">A{{ a.index }}</span>
      <span class="track-info">
        {{ langLabel(a.language) }} &middot; {{ a.codec.toUpperCase() }} &middot; {{ a.channels }}ch
        <span v-if="a.title"> &middot; {{ a.title }}</span>
        <span v-if="a.is_default" class="stag default">Default</span>
      </span>
    </div>
    <!-- Subtitles collapse behind a single toggle row — see subsExpanded. -->
    <template v-if="stream.subtitle?.length">
      <button
        type="button"
        class="stream-track subs-toggle"
        :aria-expanded="subsExpanded"
        @click="subsExpanded = !subsExpanded"
      >
        <span class="track-badge sub">S</span>
        <span class="track-info subs-toggle-label">
          {{ stream.subtitle.length }} subtitle track<span v-if="stream.subtitle.length !== 1">s</span>
          <Icon name="chevdown" :size="12" class="subs-caret" :class="{ open: subsExpanded }" />
        </span>
      </button>
      <div v-for="s in (subsExpanded ? stream.subtitle : [])" :key="'s' + s.index" class="stream-track subs-child">
        <span class="track-badge sub">S{{ s.index }}</span>
        <span class="track-info">
          {{ langLabel(s.language) }} &middot; {{ s.codec.toUpperCase() }}
          <span v-if="s.title"> &middot; {{ s.title }}</span>
          <span v-if="s.is_forced" class="stag forced">Forced</span>
          <span v-if="s.is_hearing_impaired" class="stag hi">HI</span>
          <span v-if="s.is_default" class="stag default">Default</span>
        </span>
      </div>
    </template>
  </div>
</template>

<style scoped>
.stream-details {
  padding: 12px;
  /* Theme-aware glass: the old shade-derived fill was a dark slab under
     dark text on the light theme. */
  background: color-mix(in oklab, var(--bg-2) 82%, transparent);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  box-shadow: var(--shadow-el);
}
.stream-header {
  display: flex; align-items: center; justify-content: space-between; gap: 6px;
  margin-bottom: 2px;
  font-size: 10px; font-family: var(--font-mono); color: var(--fg-2);
  letter-spacing: 0.02em;
}
.stream-header-main { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.stream-subhead {
  font-size: 9px; color: var(--fg-3); font-family: var(--font-mono);
  margin-bottom: 8px; letter-spacing: 0.02em;
}
.playback-decision {
  font-size: 9px; font-weight: 700; font-family: var(--font-mono);
  padding: 2px 7px; border-radius: 100px;
  text-transform: uppercase; letter-spacing: 0.06em;
  background: rgb(var(--ink) / 0.06); color: var(--fg-2);
}
.playback-decision.pd-direct_play { background: rgba(76,175,80,0.16); color: var(--good); }
.playback-decision.pd-remux { background: rgba(96,165,250,0.16); color: rgb(96,165,250); }
.playback-decision.pd-transcode { background: color-mix(in srgb, var(--gold) 16%, transparent); color: var(--gold); }

.stream-track {
  display: flex; align-items: center; gap: 8px;
  padding: 5px 8px; margin-bottom: 2px;
  border-radius: var(--r-xs);
  background: rgb(var(--ink) / 0.03);
}
.track-badge {
  min-width: 26px; padding: 1px 4px;
  text-align: center; border-radius: var(--r-xs);
  font-size: 9px; font-weight: 700; font-family: var(--font-mono);
}
.track-badge.vid { background: rgba(96,165,250,0.12); color: rgb(96,165,250); }
.track-badge.aud { background: rgba(168,85,247,0.12); color: rgb(168,85,247); }
.track-badge.sub { background: color-mix(in srgb, var(--gold) 12%, transparent); color: var(--gold); }
.track-info { font-size: 11px; color: var(--fg-2); line-height: 1.4; }

/* Subtitle group: a full-width toggle row, then the tracks indented under it.
   Button reset so it lays out identically to the sibling track rows. */
.subs-toggle {
  width: 100%; text-align: left; cursor: pointer;
  background: rgb(var(--ink) / 0.03); border: 0;
  transition: background 0.12s ease;
}
.subs-toggle:hover { background: rgb(var(--ink) / 0.06); }
.subs-toggle-label { display: inline-flex; align-items: center; gap: 6px; }
.subs-caret { opacity: 0.5; transition: transform 0.15s ease; }
.subs-caret.open { transform: rotate(180deg); }
.subs-child { margin-left: 12px; }
.stag {
  font-size: 8px; font-weight: 700; font-family: var(--font-mono);
  padding: 1px 4px; border-radius: 2px; margin-left: 3px;
  text-transform: uppercase; letter-spacing: 0.03em;
  vertical-align: middle;
}
.stag.default { background: rgb(var(--ink) / 0.06); color: var(--fg-3); }
.stag.hdr { background: rgba(76,175,80,0.15); color: var(--good); }
.stag.forced { background: color-mix(in srgb, var(--gold) 12%, transparent); color: var(--gold); }
.stag.hi { background: rgba(96,165,250,0.12); color: rgb(96,165,250); }
.playback-reasons {
  display: flex; flex-wrap: wrap; gap: 4px;
  margin-bottom: 8px;
}
.reason-chip {
  font-size: 9px; font-weight: 600; font-family: var(--font-mono);
  padding: 2px 6px; border-radius: 100px;
  background: rgba(255,180,80,0.10); color: rgba(255,180,80,0.85);
  text-transform: uppercase; letter-spacing: 0.04em;
}
</style>
