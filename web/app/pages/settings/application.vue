<script setup lang="ts">
import type { ApplicationSettings } from '~/types/application'

definePageMeta({ layout: 'settings' })

const {
  applicationAvailable,
  applicationUpdateInstalling,
  applicationSnapshot,
  ensureBridge,
  refreshApplicationSnapshot,
  saveApplicationSettings,
  checkForApplicationUpdate,
  installApplicationUpdate,
  installNativePlaybackRuntime,
  openApplicationServerPicker,
  resetApplicationServerSession,
  forgetApplicationServer,
} = useApplicationBridge()
const { toast } = useToast()
const { confirm } = useConfirm()

const loading = ref(true)
const settingBusy = ref<keyof ApplicationSettings | null>(null)
const updateBusy = ref<'check' | 'install' | null>(null)
const mpvBusy = ref(false)
const sessionBusy = ref(false)

const platformLabel = computed(() => {
  const platform = applicationSnapshot.value?.capabilities.platform
  if (platform === 'macos') return 'macOS'
  if (platform === 'windows') return 'Windows'
  if (platform === 'linux') return 'Linux'
  if (platform === 'android') return 'Android'
  if (platform === 'ios') return 'iOS'
  return platform ?? 'Native app'
})
const update = computed(() => applicationSnapshot.value?.update)
const nativePlayback = computed(() => applicationSnapshot.value?.nativePlayback)
const nativeAudio = computed(() => applicationSnapshot.value?.nativeAudio)

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : 'The application request failed.'
}

function fmtBytes(value: number | null | undefined): string {
  if (!value) return ''
  return value >= 1024 ** 2 ? `${(value / 1024 ** 2).toFixed(0)} MB` : `${(value / 1024).toFixed(0)} KB`
}

async function setSetting<K extends keyof ApplicationSettings>(key: K, value: ApplicationSettings[K]) {
  const settings = applicationSnapshot.value?.settings
  if (!settings || settingBusy.value) return
  settingBusy.value = key
  try {
    await saveApplicationSettings({ ...settings, [key]: value })
  } catch (error) {
    toast.err(errorMessage(error))
  } finally {
    settingBusy.value = null
  }
}

async function checkNow() {
  if (updateBusy.value || applicationUpdateInstalling.value) return
  updateBusy.value = 'check'
  try {
    const status = await checkForApplicationUpdate()
    toast[status.available ? 'info' : 'ok'](status.available
      ? `HeyaClient ${status.version} is available.`
      : 'HeyaClient is up to date.')
  } catch (error) {
    toast.err(errorMessage(error))
  } finally {
    updateBusy.value = null
  }
}

async function installUpdate() {
  if (updateBusy.value || applicationUpdateInstalling.value) return
  updateBusy.value = 'install'
  try {
    await installApplicationUpdate()
    if (applicationSnapshot.value?.capabilities.platform === 'windows') {
      toast.ok('Update installed. Restart HeyaClient to finish.')
    }
  } catch (error) {
    toast.err(errorMessage(error))
  } finally {
    updateBusy.value = null
  }
}

async function installMpv() {
  mpvBusy.value = true
  try {
    await installNativePlaybackRuntime()
    toast.ok('Native video playback is ready.')
  } catch (error) {
    toast.err(errorMessage(error))
  } finally {
    mpvBusy.value = false
  }
}

async function resetSession() {
  const approved = await confirm({
    title: 'Reset this server session?',
    message: 'HeyaClient will clear the embedded browser session and return to the sign-in screen.',
    confirmLabel: 'Reset session',
    destructive: true,
  })
  if (!approved) return
  sessionBusy.value = true
  try {
    await resetApplicationServerSession()
  } catch (error) {
    sessionBusy.value = false
    toast.err(errorMessage(error))
  }
}

async function switchServer() {
  sessionBusy.value = true
  try {
    await openApplicationServerPicker()
  } catch (error) {
    sessionBusy.value = false
    toast.err(errorMessage(error))
  }
}

async function forgetServer() {
  const approved = await confirm({
    title: 'Forget this Heya server?',
    message: 'The saved server and embedded browser session will be removed. Your server data is not affected.',
    confirmLabel: 'Forget server',
    destructive: true,
  })
  if (!approved) return
  sessionBusy.value = true
  try {
    await forgetApplicationServer()
  } catch (error) {
    sessionBusy.value = false
    toast.err(errorMessage(error))
  }
}

onMounted(async () => {
  try {
    if (await ensureBridge()) await refreshApplicationSnapshot()
  } catch (error) {
    toast.err(errorMessage(error))
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <div>
    <SettingsContextHero
      title="Application"
      icon="settings"
      eyebrow="HeyaClient settings"
      tone="connected"
      description="Control the native app around Heya. These settings stay on this device and adapt to the capabilities of the current platform."
    >
      <div class="context-fact"><strong>{{ applicationSnapshot?.capabilities.appVersion ?? '—' }}</strong><span>Version</span></div>
      <div class="context-fact"><strong>{{ platformLabel }}</strong><span>Platform</span></div>
      <div class="context-fact"><strong>{{ applicationSnapshot?.profile?.name ?? '—' }}</strong><span>Server</span></div>
    </SettingsContextHero>

    <SettingsSection v-if="loading" title="Loading application settings" icon="refresh">
      <p class="app-note">Connecting to the native HeyaClient bridge…</p>
    </SettingsSection>

    <SettingsSection
      v-else-if="!applicationAvailable || !applicationSnapshot"
      title="Available in HeyaClient"
      icon="info"
      description="This page controls the native macOS, Windows, Linux, Android, or iOS app around Heya."
    >
      <p class="app-note">Open this Heya server in HeyaClient to manage application settings.</p>
      <NuxtLink class="sv2-btn ghost" to="/settings/device">Open browser device settings</NuxtLink>
    </SettingsSection>

    <template v-else>
      <div class="application-grid">
        <SettingsSection title="Application updates" icon="download"
          :description="applicationSnapshot.capabilities.updaterSupported
            ? 'HeyaClient checks automatically after the native bridge is ready.'
            : 'Updates are delivered by the platform app store or package manager.'">
          <SettingsField
            label="Installed version"
            :description="!applicationSnapshot.capabilities.updaterSupported
              ? 'Application updates are managed outside HeyaClient on this platform.'
              : update?.available
                ? `HeyaClient ${update.version} is ready to install.`
                : 'You are running the latest version found by the last check.'"
          >
            <div class="app-actions">
              <StatusBadge :state="!applicationSnapshot.capabilities.updaterSupported ? 'idle' : update?.available ? 'warn' : 'ok'">
                {{ !applicationSnapshot.capabilities.updaterSupported
                  ? `Version ${applicationSnapshot.capabilities.appVersion}`
                  : update?.available
                    ? `Update ${update.version}`
                    : `Version ${update?.currentVersion ?? applicationSnapshot.capabilities.appVersion}` }}
              </StatusBadge>
              <button v-if="applicationSnapshot.capabilities.updaterSupported" class="sv2-btn ghost" type="button" :disabled="!!updateBusy || applicationUpdateInstalling" @click="checkNow">
                {{ updateBusy === 'check' ? 'Checking…' : 'Check now' }}
              </button>
              <button v-if="update?.available" class="sv2-btn primary" type="button" :disabled="!!updateBusy || applicationUpdateInstalling" @click="installUpdate">
                {{ updateBusy === 'install' || applicationUpdateInstalling ? 'Installing…' : 'Install update' }}
              </button>
            </div>
          </SettingsField>
        </SettingsSection>

        <SettingsSection title="Launch behaviour" icon="refresh"
          description="Choose what HeyaClient restores when it starts.">
          <SettingsField label="Reconnect on launch" description="Open the last connected Heya server automatically." v-slot="{ fieldId }">
            <AppSwitch :id="fieldId" :model-value="applicationSnapshot.settings.reconnectOnLaunch"
              :disabled="!!settingBusy" size="md" aria-label="Reconnect on launch"
              @update:model-value="setSetting('reconnectOnLaunch', $event)" />
          </SettingsField>
        </SettingsSection>
      </div>

      <div class="application-grid">
        <SettingsSection title="Native video playback" icon="film"
          description="Use HeyaClient's MPV-backed renderer when the platform provides it.">
          <SettingsField label="Native playback"
            :description="nativePlayback?.available ? `${nativePlayback.backend} is ready.` : 'The native playback backend is not available.'"
            v-slot="{ fieldId }">
            <div class="app-actions">
              <AppSwitch :id="fieldId" :model-value="applicationSnapshot.settings.nativePlaybackEnabled"
                :disabled="!!settingBusy || !nativePlayback?.available" size="md" aria-label="Native playback"
                @update:model-value="setSetting('nativePlaybackEnabled', $event)" />
              <button v-if="!nativePlayback?.available && nativePlayback?.installation.supported"
                class="sv2-btn primary" type="button" :disabled="mpvBusy" @click="installMpv">
                {{ mpvBusy ? 'Installing…' : `Install MPV${nativePlayback.installation.downloadBytes ? ` (${fmtBytes(nativePlayback.installation.downloadBytes)})` : ''}` }}
              </button>
            </div>
          </SettingsField>
        </SettingsSection>

        <SettingsSection title="Native music audio" icon="vol"
          description="Use HeyaClient's gapless native audio engine for music playback.">
          <SettingsField label="Native audio"
            :description="nativeAudio?.available ? `${nativeAudio.backend} · ${nativeAudio.gapless ? 'gapless' : 'standard'} playback` : 'The native audio backend is unavailable.'"
            v-slot="{ fieldId }">
            <AppSwitch :id="fieldId" :model-value="applicationSnapshot.settings.nativeAudioEnabled"
              :disabled="!!settingBusy || !nativeAudio?.available" size="md" aria-label="Native audio"
              @update:model-value="setSetting('nativeAudioEnabled', $event)" />
          </SettingsField>
        </SettingsSection>
      </div>

      <SettingsSection title="System integration" icon="bell"
        description="Control operating-system features owned by the native application.">
        <SettingsField label="Song-change notifications"
          description="Show a native notification when the current song changes."
          v-slot="{ fieldId }">
          <AppSwitch :id="fieldId" :model-value="applicationSnapshot.settings.trackChangeNotifications"
            :disabled="!!settingBusy" size="md" aria-label="Song-change notifications"
            @update:model-value="setSetting('trackChangeNotifications', $event)" />
        </SettingsField>
      </SettingsSection>

      <SettingsSection title="Server & session" icon="globe"
        description="Manage the Heya server wrapped by this application installation.">
        <SettingsField label="Connected server"
          :description="applicationSnapshot.profile?.origin ?? 'No server is currently saved.'">
          <div class="app-actions">
            <button class="sv2-btn ghost" type="button" :disabled="sessionBusy" @click="switchServer">Switch server</button>
            <button class="sv2-btn danger" type="button" :disabled="sessionBusy" @click="resetSession">Reset session</button>
            <button class="sv2-btn danger" type="button" :disabled="sessionBusy" @click="forgetServer">Forget server</button>
          </div>
        </SettingsField>
      </SettingsSection>
    </template>
  </div>
</template>

<style scoped>
.application-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}
.application-grid :deep(.sv2-section) { min-width: 0; }
.app-actions { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
.app-note { margin: 0 0 14px; color: var(--fg-2); font-size: 13px; line-height: 1.55; }

@media (max-width: 900px) {
  .application-grid { grid-template-columns: 1fr; gap: 0; }
}
</style>
