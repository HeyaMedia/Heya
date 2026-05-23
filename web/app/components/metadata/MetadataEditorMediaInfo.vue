<template>
  <div v-if="loading" class="mi-empty">
    <Icon name="loading" :size="20" />
    Loading media info...
  </div>
  <div v-else-if="!files.length" class="mi-empty">
    <Icon name="film" :size="28" />
    <span>No file information available.</span>
  </div>
  <div v-else class="mf">
    <div v-for="file in files" :key="file.id" class="mi-file">
      <div class="mf-card">
        <div class="mf-card-head">File</div>
        <div class="mi-rows">
          <div class="mi-row">
            <span class="mi-label">Filename</span>
            <span class="mi-value mi-mono">{{ file.filename }}</span>
          </div>
          <div class="mi-row">
            <span class="mi-label">Path</span>
            <span class="mi-value mi-mono mi-path">{{ file.path }}</span>
          </div>
          <div class="mi-row">
            <span class="mi-label">Size</span>
            <span class="mi-value">{{ formatSize(file.size) }}</span>
          </div>
          <div v-if="file.container" class="mi-row">
            <span class="mi-label">Container</span>
            <span class="mi-value">{{ formatContainer(file.container) }}</span>
          </div>
          <div v-if="file.duration" class="mi-row">
            <span class="mi-label">Duration</span>
            <span class="mi-value">{{ formatDuration(file.duration) }}</span>
          </div>
          <div v-if="file.bit_rate" class="mi-row">
            <span class="mi-label">Bitrate</span>
            <span class="mi-value">{{ formatBitrate(file.bit_rate) }}</span>
          </div>
        </div>
      </div>

      <div v-if="videoStreams(file).length" class="mf-card">
        <div class="mf-card-head">Video</div>
        <div v-for="s in videoStreams(file)" :key="s.index" class="mi-stream">
          <div class="mi-stream-header">
            <span class="mi-codec-badge">{{ s.codec_name.toUpperCase() }}</span>
            <span v-if="s.width && s.height" class="mi-res">{{ s.width }}×{{ s.height }}</span>
            <span v-if="s.profile" class="mi-tag">{{ s.profile }}</span>
            <span v-if="s.pix_fmt" class="mi-tag">{{ s.pix_fmt }}</span>
            <span v-if="s.color_space && s.color_space !== 'unknown'" class="mi-tag">{{ s.color_space }}</span>
            <span v-if="s.default" class="mi-default-badge">Default</span>
          </div>
          <div v-if="s.title" class="mi-stream-title">{{ s.title }}</div>
          <div v-if="s.bit_rate" class="mi-stream-detail">{{ formatBitrate(Number(s.bit_rate)) }}</div>
        </div>
      </div>

      <div v-if="audioStreams(file).length" class="mf-card">
        <div class="mf-card-head">Audio</div>
        <div v-for="s in audioStreams(file)" :key="s.index" class="mi-stream">
          <div class="mi-stream-header">
            <span class="mi-codec-badge">{{ s.codec_name.toUpperCase() }}</span>
            <span v-if="s.channel_layout" class="mi-tag">{{ s.channel_layout }}</span>
            <span v-else-if="s.channels" class="mi-tag">{{ s.channels }}ch</span>
            <span v-if="s.sample_rate" class="mi-tag">{{ (Number(s.sample_rate) / 1000).toFixed(1) }} kHz</span>
            <span v-if="s.language" class="mi-lang-badge">{{ s.language.toUpperCase() }}</span>
            <span v-if="s.default" class="mi-default-badge">Default</span>
          </div>
          <div v-if="s.title" class="mi-stream-title">{{ s.title }}</div>
          <div v-if="s.bit_rate" class="mi-stream-detail">{{ formatBitrate(Number(s.bit_rate)) }}</div>
        </div>
      </div>

      <div v-if="subtitleStreams(file).length" class="mf-card">
        <div class="mf-card-head">Subtitles</div>
        <div v-for="s in subtitleStreams(file)" :key="s.index" class="mi-stream">
          <div class="mi-stream-header">
            <span class="mi-codec-badge">{{ s.codec_name.toUpperCase() }}</span>
            <span v-if="s.language" class="mi-lang-badge">{{ s.language.toUpperCase() }}</span>
            <span v-if="s.forced" class="mi-tag mi-tag-forced">Forced</span>
            <span v-if="s.default" class="mi-default-badge">Default</span>
          </div>
          <div v-if="s.title" class="mi-stream-title">{{ s.title }}</div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
const props = defineProps<{
  mediaId: number
  fileId?: number | null
}>()

interface StreamInfo {
  index: number
  codec_name: string
  codec_type: string
  codec_long_name?: string
  language?: string
  title?: string
  width?: number
  height?: number
  pix_fmt?: string
  profile?: string
  color_space?: string
  channels?: number
  channel_layout?: string
  sample_rate?: string
  bit_rate?: string
  default: boolean
  forced: boolean
}

interface FileInfo {
  id: number
  path: string
  filename: string
  size: number
  container?: string
  duration?: number
  bit_rate?: number
  streams?: StreamInfo[]
}

const files = ref<FileInfo[]>([])
const loading = ref(false)

function videoStreams(f: FileInfo) { return (f.streams || []).filter(s => s.codec_type === 'video') }
function audioStreams(f: FileInfo) { return (f.streams || []).filter(s => s.codec_type === 'audio') }
function subtitleStreams(f: FileInfo) { return (f.streams || []).filter(s => s.codec_type === 'subtitle') }

function formatSize(bytes: number): string {
  if (bytes >= 1e9) return (bytes / 1e9).toFixed(2) + ' GB'
  if (bytes >= 1e6) return (bytes / 1e6).toFixed(1) + ' MB'
  return (bytes / 1e3).toFixed(0) + ' KB'
}

function formatDuration(seconds: number): string {
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = Math.floor(seconds % 60)
  if (h > 0) return `${h}h ${m}m ${s}s`
  return `${m}m ${s}s`
}

function formatBitrate(bps: number): string {
  if (bps >= 1e6) return (bps / 1e6).toFixed(1) + ' Mbps'
  if (bps >= 1e3) return (bps / 1e3).toFixed(0) + ' kbps'
  return bps + ' bps'
}

function formatContainer(c: string): string {
  const map: Record<string, string> = {
    'matroska,webm': 'Matroska (MKV)',
    'mov,mp4,m4a,3gp,3g2,mj2': 'MP4',
    'avi': 'AVI',
  }
  return map[c] || c
}

async function fetchFiles() {
  loading.value = true
  try {
    const all = await apiFetch<FileInfo[]>(`/api/media/${props.mediaId}/files`)
    if (props.fileId) {
      files.value = all.filter(f => f.id === props.fileId)
    } else {
      files.value = all
    }
  } catch {
    files.value = []
  }
  loading.value = false
}

watch(() => [props.mediaId, props.fileId], () => {
  if (props.mediaId) fetchFiles()
  else files.value = []
}, { immediate: true })
</script>

<style scoped>
.mf {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.mi-file {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.mf-card {
  background: var(--bg-1);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 20px;
}

.mf-card-head {
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--fg-2);
  margin-bottom: 16px;
  padding-bottom: 10px;
  border-bottom: 1px solid var(--border);
}

.mi-rows {
  display: flex;
  flex-direction: column;
}

.mi-row {
  display: grid;
  grid-template-columns: 120px 1fr;
  padding: 8px 0;
  border-bottom: 1px solid var(--border);
  font-size: 13px;
}
.mi-row:last-child {
  border-bottom: none;
}

.mi-label {
  color: var(--fg-3);
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  padding-top: 1px;
}

.mi-value {
  color: var(--fg-1);
}

.mi-mono {
  font-family: var(--font-mono);
  font-size: 12px;
  word-break: break-all;
}

.mi-path {
  font-size: 11px;
  color: var(--fg-2);
}

.mi-stream {
  padding: 10px 0;
  border-bottom: 1px solid var(--border);
}
.mi-stream:last-child {
  border-bottom: none;
}

.mi-stream-header {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.mi-codec-badge {
  padding: 2px 8px;
  border-radius: 4px;
  background: var(--gold-soft);
  color: var(--gold-bright);
  font-size: 10px;
  font-weight: 700;
  font-family: var(--font-mono);
  letter-spacing: 0.04em;
}

.mi-res {
  font-size: 13px;
  font-weight: 600;
  color: var(--fg-0);
  font-family: var(--font-mono);
}

.mi-tag {
  font-size: 10px;
  padding: 2px 6px;
  border-radius: 4px;
  background: rgba(255, 255, 255, 0.06);
  color: var(--fg-2);
  font-family: var(--font-mono);
}

.mi-tag-forced {
  background: rgba(217, 107, 107, 0.15);
  color: var(--bad);
}

.mi-lang-badge {
  padding: 2px 6px;
  border-radius: 4px;
  background: rgba(96, 165, 250, 0.12);
  color: rgb(96, 165, 250);
  font-size: 10px;
  font-weight: 700;
  font-family: var(--font-mono);
  letter-spacing: 0.04em;
}

.mi-default-badge {
  padding: 2px 6px;
  border-radius: 4px;
  background: rgba(74, 222, 128, 0.12);
  color: var(--good);
  font-size: 9px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

.mi-stream-title {
  font-size: 12px;
  color: var(--fg-2);
  margin-top: 4px;
}

.mi-stream-detail {
  font-size: 11px;
  color: var(--fg-3);
  font-family: var(--font-mono);
  margin-top: 2px;
}

.mi-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  padding: 64px 0;
  color: var(--fg-3);
  font-size: 14px;
  width: 100%;
}
</style>
