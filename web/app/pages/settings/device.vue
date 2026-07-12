<script setup lang="ts">
definePageMeta({ layout: 'settings' })

// Per-device playback prefs — see useDeviceSettings.ts for why this is
// localStorage-only (never synced to the account). This page is purely
// local: every control writes straight through update(), no save button.
import type { StreamQuality } from '~/composables/useDeviceSettings'
import {
  deviceStorage,
  type DeviceStorageArea,
  type DeviceStorageSnapshot,
  type StorageAreaUsage,
} from '~/storage/deviceStorage.client'

const { settings, update } = useDeviceSettings()
const { toast } = useToast()
const { user } = useAuth()
const userId = computed(() => (user.value?.id ?? Number(localStorage.getItem('heya_user_id'))) || null)

const QUALITY_OPTIONS: { value: StreamQuality, label: string }[] = [
  { value: 'original', label: 'Original (bit-perfect / best playable)' },
  { value: 'aac-320',  label: 'AAC 320 kbps' },
  { value: 'aac-256',  label: 'AAC 256 kbps' },
  { value: 'aac-192',  label: 'AAC 192 kbps' },
  { value: 'aac-128',  label: 'AAC 128 kbps' },
]

const PREFETCH_OPTIONS = [0, 1, 2, 5, 10, 25, 50]

const qualityChoice = computed<StreamQuality>({
  get: () => settings.value.streamQuality,
  set: (v) => update({ streamQuality: v }),
})

const prefetchChoice = computed<string>({
  get: () => String(settings.value.prefetchCount),
  set: (v) => update({ prefetchCount: Number(v) }),
})

// forceDirectEngine is boolean|null — <select> only speaks strings, so this
// round-trips null <-> 'auto' at the edge and nowhere else.
type EngineChoice = 'auto' | 'on' | 'off'
const engineChoice = computed<EngineChoice>({
  get: () => settings.value.forceDirectEngine === true ? 'on'
    : settings.value.forceDirectEngine === false ? 'off'
    : 'auto',
  set: (v) => update({ forceDirectEngine: v === 'auto' ? null : v === 'on' }),
})

function onWifiOnlyChange(e: Event) {
  update({ wifiOnlyPrefetch: (e.target as HTMLInputElement).checked })
}

const storageLoading = ref(true)
const clearing = ref<DeviceStorageArea | null>(null)
const storage = ref<DeviceStorageSnapshot | null>(null)

async function loadStorage() {
  storageLoading.value = true
  try {
    if (userId.value) storage.value = await deviceStorage.snapshot(userId.value)
  } catch {
    toast.err('Could not read browser storage.')
  } finally {
    storageLoading.value = false
  }
}

async function clearStorage(area: DeviceStorageArea, label: string) {
  if (!userId.value) return
  clearing.value = area
  try {
    await deviceStorage.clear(area, userId.value)
    await loadStorage()
    toast.ok(`${label} cleared.`)
  } catch {
    toast.err(`Could not clear ${label.toLowerCase()}.`)
  } finally {
    clearing.value = null
  }
}

function areaSub(area: StorageAreaUsage, noun: string) {
  if (!area.available) return 'unsupported by this browser'
  const count = `${area.entries.toLocaleString()} ${area.entries === 1 ? noun : `${noun}s`}`
  return `${count} · ${area.exact ? '' : 'at least '}${fmtBytes(area.bytes)}`
}

onMounted(loadStorage)
</script>

<template>
  <div>
    <header class="sv2-page-head">
      <h2 class="sv2-page-title">Device</h2>
      <p class="sv2-page-desc">
        These apply to this browser/device only — they live in local storage,
        never sync to your account, and won't follow you to another device.
      </p>
    </header>

    <SettingsSection title="Streaming quality" icon="vol"
      description="Lower tiers ask the server to transcode down for less bandwidth. Takes effect on the next track.">
      <SettingsField label="Quality">
        <select class="sv2-select" v-model="qualityChoice">
          <option v-for="q in QUALITY_OPTIONS" :key="q.value" :value="q.value">{{ q.label }}</option>
        </select>
      </SettingsField>
    </SettingsSection>

    <SettingsSection title="Prefetch" icon="cloud-download"
      description="Cache upcoming queue tracks ahead of playback so transitions feel instant.">
      <SettingsField label="Upcoming tracks" description="Upcoming songs downloaded ahead of playback.">
        <select class="sv2-select" v-model="prefetchChoice">
          <option v-for="n in PREFETCH_OPTIONS" :key="n" :value="String(n)">{{ n === 0 ? 'Off' : n }}</option>
        </select>
      </SettingsField>
      <SettingsField label="Only prefetch on Wi-Fi"
        hint="Best-effort — only Android/Chrome expose the connection type. iOS doesn't, so prefetch always runs there regardless of this setting.">
        <label class="dev-switch">
          <input type="checkbox" :checked="settings.wifiOnlyPrefetch" @change="onWifiOnlyChange" />
          <span class="dev-slider" />
        </label>
      </SettingsField>
    </SettingsSection>

    <SettingsSection title="Storage" icon="hard-drives"
      description="Data kept on this device for instant and offline use. Clearing it never touches your server library or account.">
      <template #actions>
        <button class="sv2-btn ghost" :disabled="!!clearing || storageLoading" @click="loadStorage">
          <Icon name="refresh" :size="12" /> Refresh
        </button>
      </template>

      <div v-if="storageLoading" class="loading-state">
        <Icon name="spinner" :size="14" /> Reading browser storage…
      </div>
      <template v-else-if="storage">
        <div class="tiles">
          <MetricTile
            label="Browser storage"
            icon="hard-drives"
            :value="storage.totalBytes == null ? '—' : fmtBytes(storage.totalBytes)"
            :sub="storage.quotaBytes ? `of ${fmtBytes(storage.quotaBytes)} quota${storage.persisted ? ' · protected' : ''}` : 'estimate unavailable'"
          />
          <MetricTile
            label="Offline data"
            icon="database"
            :value="fmtBytes(storage.offlineData.bytes)"
            :sub="areaSub(storage.offlineData, 'query')"
          />
          <MetricTile
            label="Prefetched audio"
            icon="download"
            :value="fmtBytes(storage.audio.bytes)"
            :sub="areaSub(storage.audio, 'track')"
          />
          <MetricTile
            label="Artwork"
            icon="image"
            :value="storage.images.exact ? fmtBytes(storage.images.bytes) : 'Managed cache'"
            :sub="areaSub(storage.images, 'image')"
          />
          <MetricTile
            label="Offline app"
            icon="cloud-download"
            :value="fmtBytes(storage.appShell.bytes)"
            :sub="areaSub(storage.appShell, 'file')"
          />
        </div>

        <div class="storage-actions">
          <button class="sv2-btn ghost" :disabled="!!clearing" @click="clearStorage('offline-data', 'Offline data')">
            <Icon :name="clearing === 'offline-data' ? 'spinner' : 'trash'" :size="12" />
            Clear offline data
          </button>
          <button class="sv2-btn ghost" :disabled="!!clearing" @click="clearStorage('audio', 'Prefetched audio')">
            <Icon :name="clearing === 'audio' ? 'spinner' : 'trash'" :size="12" />
            Clear prefetched audio
          </button>
          <button class="sv2-btn ghost" :disabled="!!clearing" @click="clearStorage('images', 'Artwork cache')">
            <Icon :name="clearing === 'images' ? 'spinner' : 'trash'" :size="12" />
            Clear artwork
          </button>
        </div>
        <p class="storage-note">
          Offline app files are managed by the installed PWA and replaced automatically on updates.
        </p>
      </template>
    </SettingsSection>

    <SettingsSection title="Playback engine" icon="lightning"
      description="iOS uses compatibility mode automatically for background/lock-screen playback; it disables EQ, visualizers, and crossfade.">
      <SettingsField label="Engine mode" hint="Takes effect after reloading the app.">
        <select class="sv2-select" v-model="engineChoice">
          <option value="auto">Auto (recommended)</option>
          <option value="on">Compatibility mode (background-audio safe)</option>
          <option value="off">Full engine</option>
        </select>
      </SettingsField>
    </SettingsSection>
  </div>
</template>

<style scoped>
.sv2-select {
  padding: 9px 12px;
  background: var(--bg-0);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  color: var(--fg-0);
  font-size: 13px;
  font-family: var(--font-sans);
  min-width: 280px;
}
.sv2-select:focus { outline: none; border-color: var(--gold); }

.loading-state {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--fg-3);
  font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}

.tiles {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(170px, 1fr));
  gap: 8px;
}

.storage-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 12px;
}
.storage-note {
  margin: 10px 2px 0;
  color: var(--fg-4);
  font-size: 11.5px;
}

/* Checkbox-pill, cloned from settings/jellyfin.vue's .jf-switch/.jf-slider —
   this page owns the element outright (not portaled), so a scoped block is
   fine here. */
.dev-switch {
  position: relative;
  display: inline-block;
  width: 42px;
  height: 24px;
  flex: none;
}
.dev-switch input {
  opacity: 0;
  width: 0;
  height: 0;
}
.dev-slider {
  position: absolute;
  inset: 0;
  border-radius: 999px;
  background: color-mix(in oklab, var(--text) 18%, transparent);
  transition: background 0.15s ease;
  cursor: pointer;
}
.dev-slider::before {
  content: '';
  position: absolute;
  top: 3px;
  left: 3px;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  background: var(--surface-0, #fff);
  transition: transform 0.15s ease;
}
.dev-switch input:checked + .dev-slider {
  background: var(--accent, #7c5cff);
}
.dev-switch input:checked + .dev-slider::before {
  transform: translateX(18px);
}

@media (max-width: 720px) {
  .sv2-select { min-width: 0; width: 100%; }
}
</style>
