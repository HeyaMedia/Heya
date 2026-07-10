<!--
  PlaybarQuality — a compact audio-quality label that sits next to the repeat
  button and opens a "nerdy info" popover above it on click. Surfaces the file
  specs (codec / bitrate / sample rate / bit depth), the replay-gain decision,
  the sonic analysis (BPM / key / smart-crossfade boundaries), and the live
  engine state. Inspired by the equivalent in the sibling player.
-->
<template>
  <PopoverRoot v-model:open="open">
    <PopoverTrigger as-child>
      <button class="pb-quality" :class="{ hires: isHiRes, open }" :title="qualityLabel">
        <span class="pb-quality-text">{{ qualityLabel }}</span>
      </button>
    </PopoverTrigger>
    <PopoverPortal>
      <PopoverContent class="surface pbq-pop" side="top" :side-offset="12" align="center" :collision-padding="12">
        <div class="pbq-head">
          <span class="pbq-dot" />
          <span>Now Playing — Tech Info</span>
          <span v-if="file" class="pbq-fid">#{{ file.id }}</span>
        </div>

        <!-- File -->
        <div class="pbq-sec">
          <div class="pbq-sec-label">File</div>
          <div v-if="file" class="pbq-rows">
            <div class="pbq-row"><span>Format</span><span class="pbq-v">{{ (file.format || '—').toUpperCase() }}</span></div>
            <div class="pbq-row"><span>Bitrate</span><span class="pbq-v">{{ file.bitrate_kbps ? `${file.bitrate_kbps} kbps` : '—' }}</span></div>
            <div class="pbq-row"><span>Sample rate</span><span class="pbq-v">{{ file.sample_rate_hz ? fmtHz(file.sample_rate_hz) : '—' }}</span></div>
            <div class="pbq-row"><span>Bit depth</span><span class="pbq-v">{{ file.bit_depth ? `${file.bit_depth}-bit` : '—' }}</span></div>
            <div class="pbq-row"><span>Channels</span><span class="pbq-v">{{ chLabel(file.channels) }}</span></div>
            <div class="pbq-row"><span>Size</span><span class="pbq-v">{{ fmtSize(file.size_bytes) }}</span></div>
          </div>
          <div v-else class="pbq-empty">No file metadata.</div>
        </div>

        <!-- Replay gain -->
        <div class="pbq-sec">
          <div class="pbq-sec-label">Replay gain</div>
          <div class="pbq-rows">
            <div class="pbq-row"><span>Mode</span><span class="pbq-v">{{ rg.mode }}</span></div>
            <div class="pbq-row"><span>Track</span><span class="pbq-v">{{ lufs != null ? `${lufs.toFixed(1)} LUFS` : '—' }}</span></div>
            <div class="pbq-row"><span>Album</span><span class="pbq-v">{{ albumLufs != null ? `${albumLufs.toFixed(1)} LUFS` : '—' }}</span></div>
            <div class="pbq-row"><span>True peak</span><span class="pbq-v">{{ truePeak != null ? `${truePeak.toFixed(1)} dBTP` : '—' }}</span></div>
            <div class="pbq-row">
              <span>Applied</span>
              <span class="pbq-v" :class="{ gold: appliedGainDb != null && appliedGainDb !== 0 }">{{ appliedGainLabel }}</span>
            </div>
          </div>
        </div>

        <!-- Analysis -->
        <div class="pbq-sec">
          <div class="pbq-sec-label">Analysis</div>
          <div class="pbq-rows">
            <div class="pbq-row"><span>BPM</span><span class="pbq-v">{{ facets?.bpm ? Math.round(facets.bpm) : '—' }}</span></div>
            <div class="pbq-row"><span>Key</span><span class="pbq-v">{{ facets?.key?.display || '—' }}</span></div>
            <div class="pbq-row"><span>Intro ends</span><span class="pbq-v">{{ msClock(file?.intro_end_ms) }}</span></div>
            <div class="pbq-row"><span>Outro / fade</span><span class="pbq-v">{{ msClock(file?.outro_start_ms) }} / {{ msClock(file?.fade_start_ms) }}</span></div>
            <div class="pbq-row"><span>Silence</span><span class="pbq-v">{{ msClock(file?.silence_start_ms) }}</span></div>
          </div>
        </div>

        <!-- Engine -->
        <div class="pbq-sec">
          <div class="pbq-sec-label">Engine</div>
          <div class="pbq-rows">
            <div class="pbq-row"><span>Output</span><span class="pbq-v">{{ outRate ? fmtHz(outRate) : '—' }}</span></div>
            <div class="pbq-row"><span>EQ</span><span class="pbq-v">{{ eq.enabled ? `on (${eq.presetName || 'custom'})` : 'off' }}</span></div>
            <div class="pbq-row"><span>Crossfade</span><span class="pbq-v">{{ crossfadeLabel }}</span></div>
            <div class="pbq-row"><span>Crossfeed</span><span class="pbq-v">{{ crossfeed.enabled ? crossfeed.preset : 'off' }}</span></div>
          </div>
        </div>

        <PopoverArrow class="pbq-arrow" :width="12" :height="6" />
      </PopoverContent>
    </PopoverPortal>
  </PopoverRoot>
</template>

<script setup lang="ts">
import { useQuery } from '@tanstack/vue-query'
import { PopoverArrow, PopoverContent, PopoverPortal, PopoverRoot, PopoverTrigger } from 'reka-ui'
import { useAudioContextState } from '~/engine/context'
import { computeNormalizationGain } from '~/engine/dsp/normalization'

const props = defineProps<{ trackId: number }>()

const open = ref(false)

// Primary (best-quality) file for the track. vue-query keyed on the track id so
// it dedupes with anything else that asks, and survives the popover closing.
interface TrackFileInfo {
  id: number
  format: string
  bitrate_kbps: number
  sample_rate_hz: number
  bit_depth: number
  channels: number
  size_bytes: number
  integrated_lufs: number | string | null
  true_peak_db: number | string | null
  intro_end_ms: number | null
  outro_start_ms: number | null
  fade_start_ms: number | null
  silence_start_ms: number | null
}

interface TrackDetail {
  album_integrated_lufs?: number | string | null
  album_true_peak_db?: number | string | null
  files?: TrackFileInfo[]
}

const trackIdRef = computed(() => props.trackId)
const { data: detailData } = useQuery({
  queryKey: ['music', 'track', 'detail', trackIdRef],
  queryFn: async () => {
    const { $heya } = useNuxtApp()
    return (await $heya('/api/music/tracks/{id}', { path: { id: props.trackId } })) as TrackDetail
  },
  enabled: computed(() => props.trackId > 0),
  staleTime: 1000 * 60 * 60,
  retry: false,
})
const file = computed<TrackFileInfo | null>(() => detailData.value?.files?.[0] ?? null)
const albumLufs = computed(() => toNum(detailData.value?.album_integrated_lufs))
const albumPeak = computed(() => toNum(detailData.value?.album_true_peak_db))

const { facets } = useTrackFacets(trackIdRef)

const player = usePlayer()
const settings = useAudioSettings()
const eq = settings.eq
const crossfade = settings.crossfade
const crossfeed = settings.crossfeed
const rg = settings.replayGain
const { sampleRate: outRate } = useAudioContextState()

function toNum(v: unknown): number | null {
  if (v == null) return null
  const n = typeof v === 'number' ? v : Number.parseFloat(String(v))
  return Number.isFinite(n) ? n : null
}
const lufs = computed(() => toNum(file.value?.integrated_lufs))
const truePeak = computed(() => toNum(file.value?.true_peak_db))

// The loudness actually fed to the engine, honoring the mode (track / album /
// auto-by-shuffle) with the same album→track fallback the player uses.
const effective = computed<{ lufs: number; peak: number; source: 'track' | 'album' } | null>(() => {
  if (rg.value.mode === 'off') return null
  const useAlbum = rg.value.mode === 'album' || (rg.value.mode === 'auto' && !player.shuffled.value)
  if (useAlbum && albumLufs.value != null && albumPeak.value != null) {
    return { lufs: albumLufs.value, peak: albumPeak.value, source: 'album' }
  }
  if (lufs.value != null && truePeak.value != null) {
    return { lufs: lufs.value, peak: truePeak.value, source: 'track' }
  }
  return null
})
// Engine's own normalization function so the readout can never drift from what's
// applied (it reserves ~1 dB of true-peak headroom, then clamps ±12 dB).
const appliedGainDb = computed<number | null>(() => {
  const eff = effective.value
  return eff ? 20 * Math.log10(computeNormalizationGain(eff.lufs, eff.peak)) : null
})
const appliedGainLabel = computed(() => {
  if (rg.value.mode === 'off') return 'off'
  const g = appliedGainDb.value
  if (g == null) return '—'
  const src = effective.value?.source
  return `${g > 0 ? '+' : ''}${g.toFixed(1)} dB${src ? ` (${src})` : ''}`
})

const isHiRes = computed(() => {
  const f = file.value
  return !!f && (f.sample_rate_hz > 48000 || f.bit_depth > 16)
})

const qualityLabel = computed(() => {
  const f = file.value
  if (!f) return '···'
  const fmt = (f.format || '').toUpperCase()
  if (f.bit_depth && f.sample_rate_hz) return `${fmt} ${khz(f.sample_rate_hz)}/${f.bit_depth}`
  if (f.bitrate_kbps) return `${fmt} ${f.bitrate_kbps}k`
  return fmt || '···'
})

const crossfadeLabel = computed(() => {
  const cf = crossfade.value
  if (cf.mode === 'gapless') return 'gapless'
  return `${cf.mode} · ${cf.durationSeconds}s`
})

function khz(hz: number) {
  const v = hz / 1000
  return Number.isInteger(v) ? `${v}` : v.toFixed(1)
}
function fmtHz(hz: number) {
  return `${khz(hz)} kHz`
}
function fmtSize(b: number) {
  if (!b) return '—'
  if (b > 1_073_741_824) return `${(b / 1_073_741_824).toFixed(2)} GiB`
  if (b > 1_048_576) return `${(b / 1_048_576).toFixed(1)} MiB`
  return `${Math.round(b / 1024)} KiB`
}
function chLabel(n: number) {
  return ({ 1: 'Mono', 2: 'Stereo', 6: '5.1', 8: '7.1' } as Record<number, string>)[n] || (n ? `${n}ch` : '—')
}
function msClock(ms: number | null | undefined) {
  if (ms == null) return '—'
  const s = ms / 1000
  const m = Math.floor(s / 60)
  return `${m}:${String(Math.floor(s % 60)).padStart(2, '0')}`
}
</script>

<style scoped>
.pb-quality {
  display: inline-flex;
  align-items: center;
  height: 24px;
  padding: 0 9px;
  border-radius: 999px;
  font-family: var(--font-mono, monospace);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.02em;
  color: var(--fg-2);
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  cursor: pointer;
  white-space: nowrap;
  transition: background 0.12s, color 0.12s, border-color 0.12s;
}
.pb-quality:hover,
.pb-quality.open {
  background: rgb(var(--ink) / 0.1);
  color: var(--fg-0);
  border-color: var(--border-strong);
}
.pb-quality.hires {
  color: var(--gold-bright, var(--gold));
  border-color: color-mix(in srgb, var(--gold) 35%, transparent);
  background: var(--gold-soft, rgba(230, 185, 74, 0.08));
}
</style>

<!-- Unscoped: the popover content is portaled out of this component's subtree,
     so scoped styles wouldn't reach it. -->
<style>
.pbq-pop {
  width: 300px;
  padding: 14px 16px;
  font-family: var(--font-mono, monospace);
  font-size: 11px;
}
.pbq-head {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 9px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.12em;
  color: var(--fg-3);
  margin-bottom: 12px;
}
.pbq-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--gold, #e6b94a);
  box-shadow: 0 0 8px color-mix(in srgb, var(--gold) 50%, transparent);
}
.pbq-fid {
  margin-left: auto;
  color: var(--fg-3);
  opacity: 0.6;
}
.pbq-sec + .pbq-sec {
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid var(--border);
}
.pbq-sec-label {
  font-size: 9px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--fg-3);
  margin-bottom: 7px;
}
.pbq-rows {
  display: flex;
  flex-direction: column;
  gap: 5px;
}
.pbq-row {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  color: var(--fg-2);
}
.pbq-v {
  color: var(--fg-0);
  text-align: right;
}
.pbq-v.gold {
  color: var(--gold-bright, var(--gold));
}
.pbq-empty {
  color: var(--fg-3);
}
.pbq-arrow {
  fill: var(--bg-2);
}
</style>
