<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

const { $heya } = useNuxtApp()
const { isLocked, lockTooltip, ensure: ensureSources } = useConfigSources()
import { subsonicConfigQuery } from '~/queries/settings'

const enabled = ref(false)
const configData = useQuery(subsonicConfigQuery())
const loading = computed(() => configData.isLoading.value)
const saving = ref(false)
const flash = ref<{ kind: 'ok' | 'err', text: string } | null>(null)

const serverAddress = computed(() =>
  import.meta.client ? window.location.origin : '')

async function load() {
  try {
    const res = await configData.refetch()
    if (res.data) enabled.value = res.data.enabled
  } catch {}
}

watch(() => configData.data.value, value => {
  if (value) enabled.value = value.enabled
}, { immediate: true })

async function onToggle(on: boolean) {
  saving.value = true
  flash.value = null
  try {
    const res = await $heya('/api/subsonic/config', {
      method: 'PUT',
      body: { enabled: on },
    })
    enabled.value = res.enabled
    flash.value = {
      kind: 'ok',
      text: on
        ? 'Subsonic-compatible API enabled — point a music client at the address below.'
        : 'Subsonic-compatible API disabled.',
    }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail ?? e?.message ?? 'Toggle failed.' }
    await load()
  } finally {
    saving.value = false
  }
}

onMounted(() => { ensureSources() })
</script>

<template>
  <div class="settings-page">
    <SettingsContextHero
      title="Subsonic API"
      icon="music"
      eyebrow="Server · Music clients"
      description="Connect dedicated music apps with a separate revocable credential, without exposing your normal account password."
    />

    <SettingsSection
      title="Subsonic-compatible API"
      icon="music"
      :description="enabled
        ? 'On — Subsonic/OpenSubsonic music apps can browse, search and stream this library.'
        : 'Off — Subsonic client apps can\'t see this server.'"
      :lockedBy="isLocked('subsonic.enabled') ? lockTooltip('subsonic.enabled') : undefined"
    >
      <template #actions>
        <label class="ss-switch" :title="lockTooltip('subsonic.enabled')">
          <input
            type="checkbox"
            aria-label="Enable Subsonic-compatible API"
            :checked="enabled"
            :disabled="loading || saving || isLocked('subsonic.enabled')"
            @change="onToggle(($event.target as HTMLInputElement).checked)"
          />
          <span class="ss-slider" />
        </label>
      </template>

      <div v-if="flash" class="ss-flash" :class="flash.kind" :role="flash.kind === 'err' ? 'alert' : 'status'" aria-live="polite">{{ flash.text }}</div>

      <template v-if="enabled">
        <KVTable :rows="[
          { key: 'Server address', value: serverAddress, mono: true, copy: true },
          { key: 'Protocol', value: 'Subsonic 1.16.1 + OpenSubsonic' },
          { key: 'Username', value: 'Your Heya username' },
          { key: 'Password', value: 'A personal app password — never your Heya login password' },
        ]" />
        <p class="ss-hint">
          Works with Symfonium, DSub, play:Sub, Tempo, Supersonic, Amperfy, and any
          other Subsonic/OpenSubsonic client: add a server with the address above,
          your Heya username, and your personal app password from
          <NuxtLink to="/settings/clients">Settings → Client apps</NuxtLink>.
          Plays scrobble into your normal Heya listening history; hearts and
          ratings sync both ways.
        </p>
      </template>
      <p v-else class="ss-hint">
        When enabled, Heya answers the Subsonic client protocol alongside its own API
        — nothing about the normal web app changes. You can also force this on with
        <code>HEYA_SUBSONIC_API_ENABLED=true</code>, which locks this toggle.
      </p>
    </SettingsSection>
  </div>
</template>

<style scoped>
.ss-switch {
  position: relative;
  display: inline-block;
  width: 42px;
  height: 24px;
  flex: none;
}
.ss-switch input {
  opacity: 0;
  width: 0;
  height: 0;
}
.ss-slider {
  position: absolute;
  inset: 0;
  border-radius: 999px;
  background: color-mix(in oklab, var(--text) 18%, transparent);
  transition: background 0.15s ease;
  cursor: pointer;
}
.ss-slider::before {
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
.ss-switch input:checked + .ss-slider {
  background: var(--accent);
}
.ss-switch input:checked + .ss-slider::before {
  transform: translateX(18px);
}
.ss-switch input:disabled + .ss-slider {
  opacity: 0.5;
  cursor: not-allowed;
}
.ss-flash {
  margin: 0 0 12px;
  padding: 8px 12px;
  border-radius: 8px;
  font-size: 13px;
}
.ss-flash.ok {
  background: color-mix(in srgb, var(--good) 14%, transparent);
}
.ss-flash.err {
  background: color-mix(in srgb, var(--bad) 16%, transparent);
}
.ss-hint {
  margin-top: 12px;
  font-size: 13px;
  color: var(--fg-2);
  line-height: 1.55;
}
.ss-hint code {
  font-size: 12px;
}
</style>
