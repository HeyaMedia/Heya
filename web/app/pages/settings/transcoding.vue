<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

import { transcodeStatusQuery, transcodeSessionsQuery, type TranscodeSession } from '~/queries/admin'

const { $heya } = useNuxtApp()
const { confirm } = useConfirm()
const { isLocked, lockTooltip, ensure: ensureSources } = useConfigSources()

const statusData = useQuery(transcodeStatusQuery())
const status = computed(() => statusData.data.value ?? null)

const sessionsData = useQuery(transcodeSessionsQuery())
const sessions = computed(() => sessionsData.data.value?.sessions ?? [])
let sessionsTimer: ReturnType<typeof setInterval> | null = null

const STATE_LABELS: Record<string, string> = {
  running: 'Encoding',
  throttled: 'Buffered ahead',
  completed: 'Completed',
  killed: 'Stopped',
  exited: 'Exited',
  idle: 'Idle',
}

function stateLabel(s: TranscodeSession) {
  return STATE_LABELS[s.state] ?? s.state
}

function fmtTime(sec?: number) {
  if (!sec || sec < 0) return '0:00'
  const t = Math.floor(sec)
  const h = Math.floor(t / 3600)
  const m = Math.floor((t % 3600) / 60)
  const s = t % 60
  return h > 0
    ? `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
    : `${m}:${String(s).padStart(2, '0')}`
}

function fmtBitrate(kbps?: number) {
  if (!kbps) return ''
  if (kbps >= 1000) return `${(kbps / 1000).toFixed(1)} Mbps`
  return `${Math.round(kbps)} Kbps`
}

function pct(v: number, total: number) {
  if (!total || total <= 0) return 0
  return Math.min(100, Math.max(0, (v / total) * 100))
}

function codecLabel(s: TranscodeSession) {
  const v = s.video_codec === 'copy' || !s.video_codec ? 'video copy' : s.video_codec.replace(/^lib/, '')
  const a = s.audio_codec === 'copy' || !s.audio_codec ? 'audio copy' : s.audio_codec
  return `${v} · ${a}`
}
const dirty = ref(false)
const saving = ref(false)
const clearing = ref(false)
const { flash } = useFlash()

const form = reactive({
  hwAccel: 'auto',
  cacheMaxGB: 50,
})

const allFieldsLocked = computed(() =>
  isLocked('transcoder.hwaccel') && isLocked('transcoder.cache_max_gb'),
)

const HW_OPTIONS = [
  { value: 'auto',         label: 'Auto detect' },
  { value: 'none',         label: 'CPU (software)' },
  { value: 'videotoolbox', label: 'Apple VideoToolbox' },
  { value: 'nvenc',        label: 'NVIDIA NVENC' },
  { value: 'vaapi',        label: 'VA-API' },
  { value: 'qsv',          label: 'Intel Quick Sync' },
]

const QUALITY_LADDER = [
  { name: '4320p (8K)', h264: '60 Mbps',  hevc: '40 Mbps',  av1: '25 Mbps' },
  { name: '2160p (4K)', h264: '20 Mbps',  hevc: '15 Mbps',  av1: '10 Mbps' },
  { name: '1440p',      h264: '14 Mbps',  hevc: '10 Mbps',  av1: '7 Mbps' },
  { name: '1080p',      h264: '8 Mbps',   hevc: '6 Mbps',   av1: '4 Mbps' },
  { name: '720p',       h264: '4 Mbps',   hevc: '3 Mbps',   av1: '2 Mbps' },
  { name: '480p',       h264: '2.5 Mbps', hevc: '1.8 Mbps', av1: '1.2 Mbps' },
  { name: '360p',       h264: '1.4 Mbps', hevc: '1 Mbps',   av1: '700 Kbps' },
  { name: '240p',       h264: '700 Kbps', hevc: '500 Kbps', av1: '350 Kbps' },
]

async function loadStatus() {
  try {
    await statusData.refetch()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to load transcoder status.' }
  }
}

watch(() => statusData.data.value, value => {
  if (!value) return
  form.hwAccel = value.config_mode || 'auto'
  form.cacheMaxGB = value.cache_max_gb ?? 50
  dirty.value = false
}, { immediate: true })

async function save() {
  saving.value = true
  flash.value = null
  try {
    await $heya('/api/transcode/settings', {
      method: 'PUT',
      body: { hw_accel: form.hwAccel, cache_max_gb: form.cacheMaxGB } as any,
    })
    flash.value = { kind: 'ok', text: 'Saved. New transcode sessions will use these settings.' }
    await loadStatus()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Save failed.' }
  } finally {
    saving.value = false
  }
}

async function clearCache() {
  if (!status.value?.cache_items) return
  const ok = await confirm({
    title: 'Clear transcode cache?',
    message: `Drop ${status.value.cache_items} cached files (${fmtMB(status.value.cache_size_mb)}). Active sessions may need to recreate cleared output.`,
    destructive: true,
    confirmLabel: 'Clear cache',
  })
  if (!ok) return
  clearing.value = true
  try {
    await $heya('/api/transcode/cache', { method: 'DELETE' })
    flash.value = { kind: 'ok', text: 'Cache cleared.' }
    await loadStatus()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Clear failed.' }
  } finally {
    clearing.value = false
  }
}

function fmtMB(mb?: number) {
  if (!mb) return '0 MB'
  if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`
  return `${mb} MB`
}

const cachePct = computed(() => {
  if (!status.value || !status.value.cache_max_gb) return 0
  const maxMb = status.value.cache_max_gb * 1024
  if (maxMb <= 0) return 0
  return Math.min(100, Math.round((status.value.cache_size_mb / maxMb) * 100))
})

onMounted(async () => {
  // Poll sessions only while the tab is visible — an admin page left open in
  // a background tab must not keep the server (or the phone) busy.
  sessionsTimer = setInterval(() => {
    if (!document.hidden) void sessionsData.refetch()
  }, 2000)
  await ensureSources()
})

onUnmounted(() => {
  if (sessionsTimer) clearInterval(sessionsTimer)
})
</script>

<template>
  <div>
    <SettingsContextHero
      title="Transcoding"
      icon="film"
      eyebrow="Media · Delivery pipeline"
      description="Control hardware acceleration, the HLS quality ladder, and temporary transcode storage used when clients cannot direct play."
    />

    <div v-if="!status" class="loading-state"><Icon name="spinner" :size="14" /> Probing ffmpeg…</div>

    <template v-else>
      <div class="tiles">
        <MetricTile
          label="FFmpeg"
          :value="status.available ? 'Available' : 'Not found'"
          icon="film"
          :tone="status.available ? 'good' : 'bad'"
        />
        <MetricTile
          label="Hardware"
          :value="status.hw_accel_label || status.hw_accel || 'Software'"
          icon="cpu"
          :tone="status.hw_accel !== 'none' ? 'good' : 'neutral'"
        />
        <MetricTile
          label="Active jobs"
          :value="status.active_jobs"
          icon="pulse"
          :tone="status.active_jobs > 0 ? 'good' : 'neutral'"
        />
        <MetricTile
          label="Cache"
          :value="fmtMB(status.cache_size_mb)"
          icon="hard-drives"
          :sub="status.cache_max_gb === 0 ? `${status.cache_items} items · unlimited` : `${status.cache_items} items · ${cachePct}% of cap`"
        />
      </div>

      <SettingsSection title="Active sessions" icon="pulse"
        description="Live HLS transcode sessions: what ffmpeg is encoding right now, how far ahead of the player it is, and how fast it's running. Updates every 2 seconds.">
        <div v-if="!sessions.length" class="sess-empty">No active transcode sessions.</div>
        <div v-else class="sess-list">
          <div v-for="s in sessions" :key="s.key" class="sess">
            <div class="sess-top">
              <span class="sess-file" :title="s.path">{{ s.file }}</span>
              <span class="sess-pill" :class="`st-${s.state}`">
                <Icon v-if="s.running" name="spinner" :size="11" />
                {{ stateLabel(s) }}
              </span>
            </div>

            <div class="sess-chips">
              <span class="sess-chip">{{ codecLabel(s) }}</span>
              <span class="sess-chip">{{ s.container }}</span>
              <span v-if="s.quality" class="sess-chip">{{ s.quality }}</span>
            </div>

            <div class="sess-bar" :title="`player ${fmtTime(s.player_pos_seconds)} · encoder ${fmtTime(s.encoder_pos_seconds)}`">
              <div class="sess-bar-encoded" :style="{ width: pct(s.encoder_pos_seconds, s.duration_seconds) + '%' }" />
              <div class="sess-bar-player" :style="{ left: pct(s.player_pos_seconds, s.duration_seconds) + '%' }" />
            </div>

            <div class="sess-stats">
              <span>player {{ fmtTime(s.player_pos_seconds) }}</span>
              <span>encoder {{ fmtTime(s.encoder_pos_seconds) }} / {{ fmtTime(s.duration_seconds) }}</span>
              <span v-if="s.running && s.speed">{{ s.speed.toFixed(2) }}×</span>
              <span v-if="s.running && s.fps">{{ Math.round(s.fps) }} fps</span>
              <span v-if="s.running && s.bitrate_kbps > 0">{{ fmtBitrate(s.bitrate_kbps) }}</span>
              <span>{{ s.ready_segments }}/{{ s.total_segments }} segs</span>
              <span class="dim">seen {{ Math.round(s.idle_seconds) }}s ago</span>
            </div>
          </div>
        </div>
      </SettingsSection>

      <SettingsSection title="Detected encoders" icon="cpu">
        <KVTable :rows="[
          { key: 'H.264 encoder', value: status.encoder_h264 || 'none', mono: true, copy: true },
          { key: 'HEVC encoder',  value: status.encoder_hevc || 'none', mono: true, copy: true },
          { key: 'Config mode',   value: status.config_mode || 'auto',  mono: true },
        ]" />
      </SettingsSection>

      <SettingsSection title="Pipeline configuration" icon="settings">
        <SettingsField
          label="Hardware acceleration"
          description="Which GPU encoder ffmpeg uses. Auto probes the system at boot and picks the strongest. Set manually only to override detection or force CPU."
          :lockedBy="isLocked('transcoder.hwaccel') ? lockTooltip('transcoder.hwaccel') : undefined"
          v-slot="{ fieldId }"
        >
          <select
            :id="fieldId"
            v-model="form.hwAccel"
            class="sv2-select"
            :disabled="isLocked('transcoder.hwaccel')"
            @change="dirty = true"
          >
            <option v-for="o in HW_OPTIONS" :key="o.value" :value="o.value">{{ o.label }}</option>
          </select>
        </SettingsField>

        <SettingsField
          label="Transcode cache size"
          description="Maximum disk used for cached transcodes. Oldest items are evicted when reached; set 0 for unlimited."
          :lockedBy="isLocked('transcoder.cache_max_gb') ? lockTooltip('transcoder.cache_max_gb') : undefined"
          v-slot="{ fieldId }"
        >
          <div class="num-with-unit">
            <input
              :id="fieldId"
              v-model.number="form.cacheMaxGB"
              type="number" min="0" max="500"
              class="sv2-input num"
              :disabled="isLocked('transcoder.cache_max_gb')"
              @input="dirty = true"
            />
            <span class="unit">GB</span>
          </div>
        </SettingsField>

        <div class="save-bar">
          <div v-if="allFieldsLocked" class="locked-note">
            <Icon name="key" :size="12" /> All transcoder fields are env-locked.
          </div>
          <span class="save-spacer" />
          <button class="sv2-btn primary" :disabled="!dirty || saving || allFieldsLocked" @click="save">
            <Icon v-if="saving" name="spinner" :size="13" />
            {{ saving ? 'Saving…' : 'Save changes' }}
          </button>
        </div>
      </SettingsSection>

      <SettingsSection title="Cache" icon="hard-drives">
        <KVTable :rows="[
          { key: 'Location', value: status.cache_dir || '—', mono: true, copy: true },
          { key: 'Used',     value: status.cache_max_gb === 0 ? `${fmtMB(status.cache_size_mb)} · unlimited` : `${fmtMB(status.cache_size_mb)} of ${status.cache_max_gb} GB` },
          { key: 'Items',    value: status.cache_items },
        ]" />

        <div v-if="status.cache_max_gb > 0" class="cache-bar">
          <div class="cache-fill" :class="{ warn: cachePct > 80, bad: cachePct >= 95 }" :style="{ width: cachePct + '%' }" />
        </div>
        <div class="cache-meta">
          <template v-if="status.cache_max_gb > 0">
            <span>{{ cachePct }}% used</span>
            <span class="dim">· evicts oldest first</span>
          </template>
          <span v-else>Unlimited · automatic eviction disabled</span>
        </div>

        <div class="save-bar">
          <span class="save-spacer" />
          <button class="sv2-btn danger" :disabled="clearing || !status.cache_items" @click="clearCache">
            <Icon name="trash" :size="12" />
            {{ clearing ? 'Clearing…' : 'Clear cache' }}
          </button>
        </div>
      </SettingsSection>

      <SettingsSection title="Quality ladder" icon="film"
        description="Bitrates per resolution and codec. Variants are picked automatically based on source resolution — clients only see profiles at or below source quality.">
        <div class="ladder">
          <div class="ladder-head">
            <span class="col-q">Quality</span>
            <span class="col-c">H.264</span>
            <span class="col-c">HEVC</span>
            <span class="col-c">AV1</span>
          </div>
          <div v-for="q in QUALITY_LADDER" :key="q.name" class="ladder-row">
            <span class="col-q">{{ q.name }}</span>
            <span class="col-c mono">{{ q.h264 }}</span>
            <span class="col-c mono">{{ q.hevc }}</span>
            <span class="col-c mono">{{ q.av1 }}</span>
          </div>
        </div>
      </SettingsSection>
    </template>

    <SettingsFlash :flash="flash" />
  </div>
</template>

<style scoped>
.loading-state {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}

.tiles {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 8px;
  margin-bottom: 28px;
}

.sv2-select, .sv2-input {
  background: var(--bg-0);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  color: var(--fg-0);
  font-size: 13px;
  padding: 8px 12px;
  outline: none;
  transition: border-color 0.12s;
}
.sv2-select { min-width: 220px; cursor: pointer; }
.sv2-select:focus, .sv2-input:focus { border-color: var(--gold); }
.sv2-input.num { width: 100px; text-align: right; font-family: var(--font-mono); }

.num-with-unit { display: inline-flex; align-items: center; gap: 8px; }
.unit { font-family: var(--font-mono); font-size: 12px; color: var(--fg-3); }

.save-bar {
  display: flex; align-items: center; gap: 12px;
  padding: 16px 0 0;
  margin-top: 4px;
}
.save-spacer { flex: 1; }
.locked-note {
  display: inline-flex; align-items: center; gap: 6px;
  font-size: 11.5px; color: var(--fg-3);
}

.sess-empty {
  color: var(--fg-3); font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px dashed var(--border);
  border-radius: var(--r-md);
}

.sess-list { display: flex; flex-direction: column; gap: 10px; }

.sess {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 12px 14px;
}

.sess-top {
  display: flex; align-items: center; gap: 10px;
  margin-bottom: 8px;
}
.sess-file {
  flex: 1; min-width: 0;
  color: var(--fg-0); font-size: 13px; font-weight: 500;
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.sess-pill {
  display: inline-flex; align-items: center; gap: 5px;
  font-family: var(--font-mono); font-size: 10px; font-weight: 700;
  text-transform: uppercase; letter-spacing: 0.06em;
  padding: 3px 8px;
  border-radius: 999px;
  border: 1px solid var(--border);
  color: var(--fg-2);
}
.sess-pill.st-running   { color: var(--good); border-color: color-mix(in srgb, var(--good) 40%, transparent); }
.sess-pill.st-throttled { color: var(--gold); border-color: color-mix(in srgb, var(--gold) 40%, transparent); }
.sess-pill.st-exited    { color: var(--bad);  border-color: color-mix(in srgb, var(--bad) 40%, transparent); }

.sess-chips { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 10px; }
.sess-chip {
  font-family: var(--font-mono); font-size: 10.5px;
  color: var(--fg-2);
  background: var(--bg-1);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  padding: 2px 7px;
}

.sess-bar {
  position: relative;
  height: 6px;
  border-radius: 3px;
  background: var(--bg-0);
  overflow: hidden;
}
.sess-bar-encoded {
  height: 100%;
  background: color-mix(in srgb, var(--gold) 55%, transparent);
  transition: width 0.6s ease;
}
.sess-bar-player {
  position: absolute; top: 0; bottom: 0;
  width: 2px;
  margin-left: -1px;
  background: var(--fg-0);
  transition: left 0.6s ease;
}

.sess-stats {
  display: flex; flex-wrap: wrap; gap: 6px 14px;
  font-family: var(--font-mono); font-size: 11px;
  color: var(--fg-2);
  margin-top: 8px;
}
.sess-stats .dim { color: var(--fg-4); }

.cache-bar {
  height: 6px;
  border-radius: 3px;
  background: var(--bg-0);
  overflow: hidden;
  margin-top: 12px;
}
.cache-fill {
  height: 100%;
  background: var(--gold);
  transition: width 0.4s ease;
}
.cache-fill.warn { background: var(--gold-deep); }
.cache-fill.bad  { background: var(--bad); }
.cache-meta {
  display: flex; gap: 6px;
  font-family: var(--font-mono); font-size: 11px;
  color: var(--fg-2);
  margin-top: 6px;
}
.cache-meta .dim { color: var(--fg-4); }

.ladder {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  overflow: hidden;
}
.ladder-head, .ladder-row {
  display: grid;
  grid-template-columns: 1.5fr 1fr 1fr 1fr;
  gap: 12px;
  padding: 9px 14px;
  font-size: 12.5px;
  align-items: center;
}
.ladder-head {
  background: var(--bg-1);
  font-size: 10px; font-weight: 700; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.08em;
  color: var(--fg-3);
  border-bottom: 1px solid var(--border);
}
.ladder-row { border-bottom: 1px solid var(--border); }
.ladder-row:last-child { border-bottom: 0; }
.col-q { color: var(--fg-1); font-weight: 500; }
.col-c { color: var(--fg-2); }
.mono { font-family: var(--font-mono); font-size: 11.5px; }

/* Phone: the hardware-accel select has a fixed 220px min-width that's fine
   at 390px on its own, but let it fill the row like every other phone
   input; the quality ladder's 4 columns already fit without stacking. */
@media (max-width: 720px) {
  .sv2-select { min-width: 0; width: 100%; }
  .ladder-head, .ladder-row { gap: 6px; padding: 9px 10px; }

  /* minmax(180px) only fits 1 column at 390px — force 2. */
  .tiles { grid-template-columns: repeat(2, 1fr); }
}
</style>
