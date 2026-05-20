<template>
  <div>
    <div class="page-header">
      <h2 class="page-title">Transcoding</h2>
      <p class="page-desc">Hardware acceleration, quality profiles, and transcode cache</p>
    </div>

    <!-- Status -->
    <section class="section">
      <h3 class="section-heading">
        <Icon name="pulse" :size="14" />
        Status
      </h3>
      <div class="health-cards">
        <div class="hc">
          <div class="hc-dot" :class="status?.available ? 'good' : 'bad'" />
          <div class="hc-body">
            <div class="hc-label">FFmpeg</div>
            <div class="hc-val" :class="status?.available ? 'good' : 'bad'">
              {{ status?.available ? 'Available' : 'Not Found' }}
            </div>
          </div>
        </div>
        <div class="hc">
          <div class="hc-dot" :class="status?.hw_accel !== 'none' ? 'good' : 'idle'" />
          <div class="hc-body">
            <div class="hc-label">Hardware Accel</div>
            <div class="hc-val">{{ status?.hw_accel_label || 'Unknown' }}</div>
          </div>
        </div>
        <div class="hc">
          <div class="hc-dot idle" />
          <div class="hc-body">
            <div class="hc-label">Active Jobs</div>
            <div class="hc-val">{{ status?.active_jobs ?? 0 }}</div>
          </div>
        </div>
      </div>
    </section>

    <!-- Encoder info -->
    <section v-if="status?.available" class="section">
      <h3 class="section-heading">
        <Icon name="cpu" :size="14" />
        Detected Encoders
      </h3>
      <div class="info-table">
        <div class="info-row">
          <span class="info-key">H.264 Encoder</span>
          <span class="info-val mono">{{ status?.encoder_h264 || 'none' }}</span>
        </div>
        <div class="info-row">
          <span class="info-key">H.265 Encoder</span>
          <span class="info-val mono">{{ status?.encoder_hevc || 'none' }}</span>
        </div>
        <div class="info-row">
          <span class="info-key">Config Mode</span>
          <span class="info-val mono">{{ status?.config_mode || 'auto' }}</span>
        </div>
      </div>
    </section>

    <!-- Settings -->
    <section class="section">
      <h3 class="section-heading">
        <Icon name="settings" :size="14" />
        Configuration
      </h3>
      <div class="settings-card">
        <div class="setting-row">
          <div class="setting-info">
            <div class="setting-label">Hardware Acceleration</div>
            <div class="setting-hint">
              Controls which GPU encoder FFmpeg uses. <strong>Auto</strong> probes your system at startup and picks the best available.
              Set manually if auto-detection is wrong or to force CPU encoding.
            </div>
          </div>
          <div class="setting-control">
            <select v-model="form.hwAccel" class="select-input" @change="dirty = true">
              <option value="auto">Auto Detect</option>
              <option value="none">CPU (Software)</option>
              <option value="videotoolbox">Apple VideoToolbox</option>
              <option value="nvenc">NVIDIA NVENC</option>
              <option value="vaapi">VA-API</option>
              <option value="qsv">Intel Quick Sync</option>
            </select>
          </div>
        </div>

        <div class="setting-row">
          <div class="setting-info">
            <div class="setting-label">Transcode Cache Size</div>
            <div class="setting-hint">
              Maximum disk space for cached HLS segments. Oldest segments are evicted when this limit is reached.
            </div>
          </div>
          <div class="setting-control">
            <div class="input-with-unit">
              <input
                v-model.number="form.cacheMaxGB"
                type="number"
                min="1"
                max="500"
                class="number-input"
                @input="dirty = true"
              />
              <span class="input-unit">GB</span>
            </div>
          </div>
        </div>
      </div>

      <div class="actions-row">
        <button
          class="btn btn-primary"
          :disabled="!dirty || saving"
          @click="saveSettings"
        >
          {{ saving ? 'Saving...' : 'Save Changes' }}
        </button>
        <span v-if="saved" class="save-ok">
          <Icon name="check" :size="14" />
          Saved. Restart server to apply hardware acceleration changes.
        </span>
      </div>
    </section>

    <!-- Cache -->
    <section class="section">
      <h3 class="section-heading">
        <Icon name="hard-drives" :size="14" />
        Cache
      </h3>
      <div class="info-table">
        <div class="info-row">
          <span class="info-key">Location</span>
          <span class="info-val mono">{{ status?.cache_dir || '-' }}</span>
        </div>
        <div class="info-row">
          <span class="info-key">Used</span>
          <span class="info-val">
            {{ formatSize(status?.cache_size_mb) }}
            <span class="info-sub">/ {{ status?.cache_max_gb ?? 50 }} GB limit</span>
          </span>
        </div>
        <div class="info-row">
          <span class="info-key">Items</span>
          <span class="info-val">{{ status?.cache_items ?? 0 }} segments</span>
        </div>
      </div>
      <div class="actions-row">
        <button
          class="btn btn-danger"
          :disabled="clearing || !status?.cache_items"
          @click="clearCache"
        >
          {{ clearing ? 'Clearing...' : 'Clear Cache' }}
        </button>
      </div>
    </section>

    <!-- Quality ladder reference -->
    <section class="section">
      <h3 class="section-heading">
        <Icon name="film" :size="14" />
        Quality Profiles
      </h3>
      <div class="info-table">
        <div class="info-row header-row">
          <span class="info-key">Quality</span>
          <span class="info-val col-3">H.264</span>
          <span class="info-val col-3">HEVC</span>
          <span class="info-val col-3">AV1</span>
        </div>
        <div v-for="q in qualities" :key="q.name" class="info-row">
          <span class="info-key">{{ q.name }}</span>
          <span class="info-val col-3 mono">{{ q.h264 }}</span>
          <span class="info-val col-3 mono">{{ q.hevc }}</span>
          <span class="info-val col-3 mono">{{ q.av1 }}</span>
        </div>
      </div>
      <div class="quality-note">
        Quality variants are selected automatically based on source resolution. Clients receive all profiles at or below the source quality.
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
interface TranscodeStatus {
  available: boolean
  hw_accel: string
  hw_accel_label: string
  encoder_h264: string
  encoder_hevc: string
  cache_dir: string
  cache_max_gb: number
  cache_size_mb: number
  cache_items: number
  active_jobs: number
  config_mode: string
}

const status = ref<TranscodeStatus | null>(null)
const dirty = ref(false)
const saving = ref(false)
const saved = ref(false)
const clearing = ref(false)

const form = reactive({
  hwAccel: 'auto',
  cacheMaxGB: 50,
})

const qualities = [
  { name: '4320p (8K)', h264: '60 Mbps', hevc: '40 Mbps', av1: '25 Mbps' },
  { name: '2160p (4K)', h264: '20 Mbps', hevc: '15 Mbps', av1: '10 Mbps' },
  { name: '1440p', h264: '14 Mbps', hevc: '10 Mbps', av1: '7 Mbps' },
  { name: '1080p', h264: '8 Mbps', hevc: '6 Mbps', av1: '4 Mbps' },
  { name: '720p', h264: '4 Mbps', hevc: '3 Mbps', av1: '2 Mbps' },
  { name: '480p', h264: '2.5 Mbps', hevc: '1.8 Mbps', av1: '1.2 Mbps' },
  { name: '360p', h264: '1.4 Mbps', hevc: '1 Mbps', av1: '700 Kbps' },
  { name: '240p', h264: '700 Kbps', hevc: '500 Kbps', av1: '350 Kbps' },
]

function formatSize(mb: number | undefined) {
  if (!mb) return '0 MB'
  if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`
  return `${mb} MB`
}

async function loadStatus() {
  try {
    status.value = await apiFetch<TranscodeStatus>('/api/transcode/status')
    form.hwAccel = status.value.config_mode || 'auto'
    form.cacheMaxGB = status.value.cache_max_gb || 50
  } catch {}
}

async function saveSettings() {
  saving.value = true
  saved.value = false
  try {
    await apiFetch('/api/transcode/settings', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        hw_accel: form.hwAccel,
        cache_max_gb: form.cacheMaxGB,
      }),
    })
    dirty.value = false
    saved.value = true
    await loadStatus()
  } catch {}
  saving.value = false
}

async function clearCache() {
  clearing.value = true
  try {
    await apiFetch('/api/transcode/cache', { method: 'DELETE' })
    await loadStatus()
  } catch {}
  clearing.value = false
}

onMounted(loadStatus)
</script>

<style scoped>
.page-header { margin-bottom: 32px; }
.page-title { font-size: 26px; font-weight: 600; letter-spacing: -0.02em; margin: 0; }
.page-desc { font-size: 13px; color: var(--fg-3); margin: 6px 0 0; }

.section { margin-bottom: 36px; }
.section-heading {
  display: flex; align-items: center; gap: 8px;
  font-size: 11px; font-weight: 600; color: var(--fg-3);
  font-family: var(--font-mono); text-transform: uppercase;
  letter-spacing: 0.1em; margin: 0 0 14px; padding-bottom: 10px;
  border-bottom: 1px solid var(--border);
}

/* Health cards */
.health-cards { display: grid; grid-template-columns: repeat(3, 1fr); gap: 8px; }
.hc { display: flex; align-items: center; gap: 12px; background: var(--bg-2); border: 1px solid var(--border); border-radius: var(--r-md); padding: 16px 18px; }
.hc-dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
.hc-dot.good { background: var(--good); box-shadow: 0 0 8px rgba(111, 191, 124, 0.4); }
.hc-dot.bad { background: var(--bad); box-shadow: 0 0 8px rgba(217, 107, 107, 0.4); }
.hc-dot.idle { background: var(--fg-4); }
.hc-body { min-width: 0; }
.hc-label { font-size: 10px; font-family: var(--font-mono); text-transform: uppercase; letter-spacing: 0.1em; color: var(--fg-3); }
.hc-val { font-size: 15px; font-weight: 600; margin-top: 2px; }
.hc-val.good { color: var(--good); }
.hc-val.bad { color: var(--bad); }

/* Info table */
.info-table { background: var(--bg-2); border: 1px solid var(--border); border-radius: var(--r-md); overflow: hidden; }
.info-row { display: flex; justify-content: space-between; align-items: center; padding: 12px 18px; border-bottom: 1px solid var(--border); font-size: 13px; }
.info-row:last-child { border-bottom: none; }
.info-row.header-row { background: var(--bg-3); }
.info-key { color: var(--fg-3); font-family: var(--font-mono); font-size: 11px; text-transform: uppercase; letter-spacing: 0.08em; min-width: 120px; }
.info-val { color: var(--fg-1); font-weight: 500; }
.info-val.mono { font-family: var(--font-mono); font-size: 12px; }
.info-val.col-3 { width: 100px; text-align: right; }
.info-sub { color: var(--fg-3); font-size: 11px; font-weight: 400; }

/* Settings card */
.settings-card {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  overflow: hidden;
}

.setting-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 24px;
  padding: 20px 18px;
  border-bottom: 1px solid var(--border);
}
.setting-row:last-child { border-bottom: none; }

.setting-info { flex: 1; min-width: 0; }
.setting-label { font-size: 14px; font-weight: 600; color: var(--fg-0); }
.setting-hint { font-size: 12px; color: var(--fg-3); margin-top: 4px; line-height: 1.5; }
.setting-hint strong { color: var(--fg-2); }

.setting-control { flex-shrink: 0; }

.select-input {
  appearance: none;
  background: var(--bg-1);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  padding: 8px 32px 8px 12px;
  font-size: 13px;
  font-weight: 500;
  color: var(--fg-1);
  cursor: pointer;
  background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 12 12'%3E%3Cpath d='M3 5l3 3 3-3' stroke='%23888' stroke-width='1.5' fill='none'/%3E%3C/svg%3E");
  background-repeat: no-repeat;
  background-position: right 10px center;
  min-width: 180px;
}
.select-input:focus {
  outline: none;
  border-color: var(--gold);
  box-shadow: 0 0 0 2px rgba(230, 185, 74, 0.15);
}

.input-with-unit {
  display: flex;
  align-items: center;
  gap: 0;
  background: var(--bg-1);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  overflow: hidden;
}
.input-with-unit:focus-within {
  border-color: var(--gold);
  box-shadow: 0 0 0 2px rgba(230, 185, 74, 0.15);
}

.number-input {
  border: none;
  background: transparent;
  padding: 8px 12px;
  font-size: 13px;
  font-weight: 500;
  color: var(--fg-1);
  width: 80px;
  text-align: right;
}
.number-input:focus { outline: none; }
.number-input::-webkit-inner-spin-button { opacity: 0.3; }

.input-unit {
  padding: 8px 12px 8px 4px;
  font-size: 12px;
  font-family: var(--font-mono);
  color: var(--fg-3);
}

/* Actions */
.actions-row {
  display: flex;
  align-items: center;
  gap: 14px;
  margin-top: 14px;
}

.btn {
  padding: 8px 20px;
  border-radius: var(--r-sm);
  font-size: 13px;
  font-weight: 600;
  border: 1px solid var(--border);
  cursor: pointer;
  transition: all 0.15s ease;
}
.btn:disabled { opacity: 0.4; cursor: default; }

.btn-primary {
  background: var(--gold);
  color: var(--bg-0);
  border-color: var(--gold);
}
.btn-primary:hover:not(:disabled) { filter: brightness(1.1); }

.btn-danger {
  background: transparent;
  color: var(--bad);
  border-color: var(--bad);
}
.btn-danger:hover:not(:disabled) { background: rgba(217, 107, 107, 0.1); }

.save-ok {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--good);
}

.quality-note {
  font-size: 12px;
  color: var(--fg-3);
  margin-top: 10px;
  padding: 0 4px;
  line-height: 1.5;
}
</style>
