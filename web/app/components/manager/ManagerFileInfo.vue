<script setup lang="ts">
// Everything the probe knows about one on-disk file — the absolute path,
// container facts, and every stream. Loaded lazily when a file row expands;
// shared by the movie files table, TV episode rows, and album track rows.
import { useQuery } from '@pinia/colada'
import type { ManagerFileDetailView, ManagerStreamView } from '~~/shared/api/types.gen'

const props = defineProps<{ fileId: number }>()

const { data, asyncStatus, error } = useQuery(() => ({
  key: ['manager', 'file', props.fileId],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya(`/api/manager/file/${props.fileId}`) as ManagerFileDetailView
  },
  staleTime: 1000 * 60 * 5,
  meta: { prefetch: 'none' as const, sensitivity: 'private' as const },
}))

const streams = computed(() => data.value?.streams ?? [])
const mainStreams = computed(() => streams.value.filter(s => s.kind === 'video' || s.kind === 'audio'))
const subs = computed(() => streams.value.filter(s => s.kind === 'subtitle'))
const others = computed(() => streams.value.filter(s => s.kind !== 'video' && s.kind !== 'audio' && s.kind !== 'subtitle'))

function fmtDuration(sec: number): string {
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  const s = sec % 60
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}` : `${m}:${String(s).padStart(2, '0')}`
}

function streamLine(s: ManagerStreamView): string {
  const parts: string[] = []
  if (s.codec) parts.push(s.codec)
  if (s.kind === 'video') {
    if (s.profile) parts.push(s.profile)
    if (s.width && s.height) parts.push(`${s.width}×${s.height}`)
    if (s.frame_rate) parts.push(`${s.frame_rate} fps`)
    if (s.bit_depth) parts.push(`${s.bit_depth}-bit`)
    if (s.hdr) parts.push(s.hdr)
  } else {
    if (s.profile) parts.push(s.profile)
    if (s.layout) parts.push(s.layout)
    else if (s.channels) parts.push(`${s.channels}ch`)
    if (s.language) parts.push(s.language)
    if (s.sample_rate_hz) parts.push(`${(s.sample_rate_hz / 1000).toLocaleString(undefined, { maximumFractionDigits: 1 })} kHz`)
  }
  if (s.bitrate_kbps) parts.push(`${s.bitrate_kbps.toLocaleString()} kbps`)
  if (s.forced) parts.push('forced')
  return parts.join(' · ')
}

function subLabel(s: ManagerStreamView): string {
  let label = s.title || (s.language ? s.language.toUpperCase() : s.codec || 'sub')
  if (s.forced) label += ' · forced'
  return label
}
</script>

<template>
  <div class="mfi">
    <div v-if="asyncStatus === 'loading' && !data" class="mfi-loading"><span class="mgr-spin" /> Probing file…</div>
    <div v-else-if="error" class="mfi-err"><Icon name="warning" :size="12" /> Couldn't load file details.</div>
    <template v-else-if="data">
      <div class="mfi-path mono">{{ data.path }}</div>
      <div class="mfi-facts mono">
        <span v-if="data.container" class="mgr-quality">{{ data.container }}</span>
        <span v-if="data.duration_sec">{{ fmtDuration(data.duration_sec) }}</span>
        <span v-if="data.bitrate_kbps">{{ data.bitrate_kbps.toLocaleString() }} kbps overall</span>
        <span>{{ fmtBytes(data.size_bytes) }}</span>
        <span>added {{ new Date(data.added_at).toLocaleDateString() }}</span>
      </div>
      <div v-if="!streams.length" class="mfi-none">No probe data for this file yet — media info fills in as the scanner analyzes it.</div>
      <div v-for="(s, i) in mainStreams" :key="`m${i}`" class="mfi-stream">
        <span class="mfi-kind mono" :class="s.kind">{{ s.kind }}</span>
        <span class="mfi-desc mono">{{ streamLine(s) }}</span>
        <span v-if="s.title" class="mfi-title">{{ s.title }}</span>
        <span v-if="s.default" class="mfi-default mono">default</span>
      </div>
      <div v-if="subs.length" class="mfi-stream">
        <span class="mfi-kind mono">subs</span>
        <span class="mfi-subs">
          <span v-for="(s, i) in subs" :key="`s${i}`" class="mgr-cf" :title="s.codec">{{ subLabel(s) }}</span>
        </span>
      </div>
      <div v-for="(s, i) in others" :key="`o${i}`" class="mfi-stream">
        <span class="mfi-kind mono">{{ s.kind }}</span>
        <span class="mfi-desc mono">{{ streamLine(s) }}</span>
      </div>
    </template>
  </div>
</template>

<style scoped>
.mfi {
  display: flex;
  flex-direction: column;
  gap: 7px;
  padding: 12px 14px;
  background: rgb(var(--shade) / 0.16);
  border-radius: var(--r-sm);
}
.mfi-loading,
.mfi-err { display: flex; align-items: center; gap: 8px; color: var(--fg-3); font-size: 12px; }
.mfi-err { color: var(--bad); }
.mfi-none { font-size: 11.5px; color: var(--fg-3); font-style: italic; }
.mfi-path {
  font-size: 11.5px;
  color: var(--fg-1);
  overflow-wrap: anywhere;
  line-height: 1.5;
}
.mfi-facts {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
  font-size: 11px;
  color: var(--fg-2);
}
.mfi-stream {
  display: flex;
  align-items: baseline;
  gap: 10px;
  min-width: 0;
  flex-wrap: wrap;
}
.mfi-kind {
  flex-shrink: 0;
  width: 44px;
  font-size: 9.5px;
  font-weight: 600;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--fg-3);
}
.mfi-kind.video { color: var(--gold-bright); }
.mfi-desc { font-size: 11.5px; color: var(--fg-1); overflow-wrap: anywhere; }
.mfi-title { font-size: 11px; color: var(--fg-3); }
.mfi-default {
  font-size: 9.5px;
  color: var(--fg-3);
  border: 1px solid var(--border);
  border-radius: 999px;
  padding: 0 6px;
}
.mfi-subs { display: flex; gap: 5px; flex-wrap: wrap; }
.mono { font-family: var(--font-mono); }
</style>
