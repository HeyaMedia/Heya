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
const { isTauriClient } = useClientSurface()
const { applicationAvailable } = useApplicationBridge()
const { user } = useAuth()
const userId = computed(() => (user.value?.id ?? Number(localStorage.getItem('heya_user_id'))) || null)

const QUALITY_OPTIONS: { value: StreamQuality, label: string }[] = [
  { value: 'original', label: 'Original (best playable)' },
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

const wifiOnlyChoice = computed({
  get: () => settings.value.wifiOnlyPrefetch,
  set: (value: boolean) => update({ wifiOnlyPrefetch: value }),
})

const browserNotificationsSupported = ref(false)
const browserNotificationPermission = ref<NotificationPermission>('default')

async function setTrackChangeNotifications(enabled: boolean) {
  if (!enabled) {
    update({ trackChangeNotifications: false })
    return
  }
  const permission = await requestBrowserTrackNotificationPermission()
  browserNotificationPermission.value = permission
  if (permission === 'granted') {
    update({ trackChangeNotifications: true })
  } else {
    update({ trackChangeNotifications: false })
    toast.err(permission === 'denied'
      ? 'Notifications are blocked for Heya in this browser.'
      : 'Notification permission was not granted.')
  }
}

const qualityLabel = computed(() => QUALITY_OPTIONS.find(option => option.value === settings.value.streamQuality)?.label.split(' (')[0] ?? 'Original')
const storedBytes = computed(() => storage.value?.totalBytes == null ? '—' : fmtBytes(storage.value.totalBytes))
const prefetchedTracks = computed(() => storage.value?.audio.entries ?? 0)

// forceDirectEngine is boolean|null — <select> only speaks strings, so this
// round-trips null <-> 'auto' at the edge and nowhere else.
type EngineChoice = 'auto' | 'on' | 'off'
const engineChoice = computed<EngineChoice>({
  get: () => settings.value.forceDirectEngine === true ? 'on'
    : settings.value.forceDirectEngine === false ? 'off'
    : 'auto',
  set: (v) => update({ forceDirectEngine: v === 'auto' ? null : v === 'on' }),
})

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

onMounted(() => {
  void loadStorage()
  browserNotificationsSupported.value = browserTrackNotificationsSupported()
  if (browserNotificationsSupported.value) {
    browserNotificationPermission.value = Notification.permission
    if (Notification.permission === 'denied' && settings.value.trackChangeNotifications) {
      update({ trackChangeNotifications: false })
    }
  }
})
</script>

<template>
  <div>
    <SettingsContextHero
      title="This device"
      icon="cpu"
      eyebrow="Stored only in this browser"
      tone="local"
      description="Tune Heya for this screen, connection, and browser. Changes apply immediately and never alter your other devices."
    >
      <div class="context-fact"><strong>{{ qualityLabel }}</strong><span>Audio</span></div>
      <div class="context-fact"><strong>{{ settings.prefetchCount }}</strong><span>Prefetch</span></div>
      <div class="context-fact"><strong>{{ storedBytes }}</strong><span>Storage</span></div>
    </SettingsContextHero>

    <div class="device-grid">
      <SettingsSection title="Streaming quality" icon="vol"
        description="Choose the bandwidth and quality balance for music played on this device.">
        <SettingsField label="Quality" description="Takes effect when the next track starts." v-slot="{ fieldId }">
          <select :id="fieldId" class="sv2-select" v-model="qualityChoice">
            <option v-for="q in QUALITY_OPTIONS" :key="q.value" :value="q.value">{{ q.label }}</option>
          </select>
        </SettingsField>
      </SettingsSection>

      <SettingsSection title="Queue prefetch" icon="cloud-download"
        description="Keep upcoming music ready locally so track changes feel instant.">
        <SettingsField label="Upcoming tracks" description="Songs downloaded ahead of playback." v-slot="{ fieldId }">
          <select :id="fieldId" class="sv2-select" v-model="prefetchChoice">
            <option v-for="n in PREFETCH_OPTIONS" :key="n" :value="String(n)">{{ n === 0 ? 'Off' : n }}</option>
          </select>
        </SettingsField>
        <SettingsField label="Only prefetch on Wi-Fi"
          hint="Best-effort — Android and Chromium expose connection type; iOS does not."
          v-slot="{ fieldId, hintId }">
          <AppSwitch v-model="wifiOnlyChoice" :id="fieldId" :aria-describedby="hintId" size="md" aria-label="Only prefetch on Wi-Fi" />
        </SettingsField>
      </SettingsSection>
    </div>

    <SettingsSection title="System integration" icon="bell"
      description="Control how this device announces music while Heya is in the background.">
      <SettingsField
        v-if="!isTauriClient"
        label="Song-change notifications"
        description="Show one silent, replaceable notification when the song changes while Heya is hidden."
        :hint="browserNotificationPermission === 'denied' ? 'Blocked in browser settings.' : 'Permission is requested only when you turn this on.'"
        v-slot="{ fieldId, hintId }"
      >
        <AppSwitch
          :id="fieldId"
          :aria-describedby="hintId"
          :model-value="settings.trackChangeNotifications"
          :disabled="!browserNotificationsSupported || browserNotificationPermission === 'denied'"
          size="md"
          aria-label="Song-change notifications"
          @update:model-value="setTrackChangeNotifications"
        />
      </SettingsField>
      <SettingsField
        v-else-if="applicationAvailable"
        label="Native song-change notifications"
        description="HeyaClient owns OS notification permission and background eligibility."
        hint="Application-specific controls now live inside Heya."
      >
        <NuxtLink class="sv2-btn ghost" to="/settings/application">Open Application Settings</NuxtLink>
      </SettingsField>
      <SettingsField
        v-else
        label="Native song-change notifications"
        description="Waiting for the origin-validated HeyaClient application bridge."
      >
        <span class="local-owner">Native application settings unavailable</span>
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
            :sub="`${prefetchedTracks.toLocaleString()} ready · ${areaSub(storage.audio, 'track')}`"
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
      <SettingsField label="Engine mode" hint="Takes effect after reloading the app." v-slot="{ fieldId, hintId }">
        <select :id="fieldId" :aria-describedby="hintId" class="sv2-select" v-model="engineChoice">
          <option value="auto">Auto (recommended)</option>
          <option value="on">Compatibility mode (background-audio safe)</option>
          <option value="off">Full engine</option>
        </select>
      </SettingsField>
    </SettingsSection>
  </div>
</template>

<style scoped>
.device-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
  align-items: start;
}
.device-grid :deep(.sv2-section) { height: calc(100% - 16px); }
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
  color: var(--fg-2);
  font-size: 11.5px;
}
.local-owner {
  color: var(--fg-2);
  font-size: 12.5px;
}

@media (max-width: 720px) {
  .device-grid { grid-template-columns: 1fr; gap: 0; }
  .sv2-select { min-width: 0; width: 100%; }
}
</style>
