<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

const { $heya } = useNuxtApp()
const { isLocked, lockTooltip, ensure: ensureSources } = useConfigSources()
import { subsonicConfigQuery, subsonicCredentialQuery } from '~/queries/settings'

const enabled = ref(false)
const configData = useQuery(subsonicConfigQuery())
const credentialData = useQuery(subsonicCredentialQuery())
const loading = computed(() => configData.isLoading.value)
const saving = ref(false)
const flash = ref<{ kind: 'ok' | 'err', text: string } | null>(null)

type Credential = {
  secret: string
  created_at: string
  rotated_at: string
  last_used_at?: string
}
const credential = ref<Credential | null>(null)
const credentialBusy = ref(false)
const secretVisible = ref(false)

const serverAddress = computed(() =>
  import.meta.client ? window.location.origin : '')

async function load() {
  try {
    const res = await configData.refetch()
    if (res.data) enabled.value = res.data.enabled
  } catch {}
  try {
    const res = await credentialData.refetch()
    credential.value = res.data as Credential
  } catch {
    credential.value = null // 404 — none minted yet
  }
}

watch(() => configData.data.value, value => {
  if (value) enabled.value = value.enabled
}, { immediate: true })
watch(() => credentialData.data.value, value => {
  if (value) credential.value = value as Credential
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

async function rotateCredential() {
  if (credential.value) {
    const ok = await useConfirm().confirm({
      title: 'Rotate app password?',
      message: 'Every Subsonic client signed in with the current app password stops working until you update it.',
      confirmLabel: 'Rotate',
    })
    if (!ok) return
  }
  credentialBusy.value = true
  try {
    credential.value = await $heya('/api/me/subsonic-credential', { method: 'POST' }) as Credential
    secretVisible.value = true
    flash.value = { kind: 'ok', text: credential.value ? 'App password ready — paste it into your client.' : '' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail ?? e?.message ?? 'Could not create app password.' }
  } finally {
    credentialBusy.value = false
  }
}

async function revokeCredential() {
  const ok = await useConfirm().confirm({
    title: 'Revoke app password?',
    message: 'All Subsonic clients are signed out immediately. You can generate a new password any time.',
    confirmLabel: 'Revoke',
    destructive: true,
  })
  if (!ok) return
  credentialBusy.value = true
  try {
    await $heya('/api/me/subsonic-credential', { method: 'DELETE' })
    credential.value = null
    secretVisible.value = false
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail ?? e?.message ?? 'Revoke failed.' }
  } finally {
    credentialBusy.value = false
  }
}

async function copySecret() {
  if (!credential.value) return
  try {
    await navigator.clipboard.writeText(credential.value.secret)
    flash.value = { kind: 'ok', text: 'App password copied to clipboard.' }
  } catch {
    flash.value = { kind: 'err', text: 'Clipboard blocked — reveal and copy manually.' }
  }
}

function formatWhen(value?: string): string {
  if (!value) return 'never'
  const d = new Date(value)
  return Number.isNaN(d.getTime())
    ? value
    : d.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
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
          { key: 'Password', value: 'The app password below — never your Heya login password' },
        ]" />
        <p class="ss-hint">
          Works with Symfonium, DSub, play:Sub, Tempo, Supersonic, Amperfy, and any
          other Subsonic/OpenSubsonic client: add a server with the address above,
          your Heya username, and the app password from the section below.
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

    <SettingsSection
      title="App password"
      icon="key"
      description="Subsonic's token login needs a secret the server can read back, so clients use a generated app password — your real Heya password is never involved and never at risk."
    >
      <template #actions>
        <button class="ss-btn" :disabled="credentialBusy" @click="rotateCredential">
          {{ credential ? 'Rotate' : 'Generate' }}
        </button>
        <button v-if="credential" class="ss-btn danger" :disabled="credentialBusy" @click="revokeCredential">
          Revoke
        </button>
      </template>

      <template v-if="credential">
        <div class="ss-secret-row">
          <code class="ss-secret">{{ secretVisible ? credential.secret : '••••••••••••••••••••' }}</code>
          <button class="ss-btn" @click="secretVisible = !secretVisible">
            {{ secretVisible ? 'Hide' : 'Reveal' }}
          </button>
          <button class="ss-btn" @click="copySecret">Copy</button>
        </div>
        <KVTable :rows="[
          { key: 'Created', value: formatWhen(credential.created_at) },
          { key: 'Last rotated', value: formatWhen(credential.rotated_at) },
          { key: 'Last used', value: formatWhen(credential.last_used_at) },
        ]" />
      </template>
      <p v-else class="ss-hint">
        No app password yet — generate one, then paste it into your Subsonic client
        together with your Heya username. Each user mints their own; rotating or
        revoking it never touches the Heya account password.
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
.ss-btn {
  padding: 6px 14px;
  border-radius: var(--r-sm);
  border: 1px solid var(--border);
  background: var(--bg-2);
  color: var(--fg-1);
  font-size: 12.5px;
  transition: color 0.12s, border-color 0.12s, background 0.12s;
}
.ss-btn:hover:not(:disabled) {
  color: var(--fg-0);
  border-color: var(--border-strong);
  background: rgb(var(--ink) / 0.05);
}
.ss-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.ss-btn.danger:hover:not(:disabled) {
  color: var(--bad);
  border-color: color-mix(in srgb, var(--bad) 35%, transparent);
  background: color-mix(in srgb, var(--bad) 8%, transparent);
}
.ss-secret-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
}
.ss-secret {
  flex: 1;
  min-width: 0;
  padding: 8px 12px;
  border-radius: var(--r-sm);
  border: 1px solid var(--border);
  background: var(--bg-3);
  font-family: var(--font-mono);
  font-size: 13px;
  letter-spacing: 0.04em;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (max-width: 520px) {
  .ss-secret-row { align-items: stretch; flex-wrap: wrap; }
  .ss-secret { flex-basis: 100%; }
  .ss-secret-row .ss-btn { flex: 1; }
}
</style>
