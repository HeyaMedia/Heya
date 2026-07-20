<script setup lang="ts">
// Personal credentials for client apps: the Jellyfin TV PIN and the Subsonic
// app password. Per-user, no admin gate — the admin Client APIs pages own the
// global on/off toggles and link here.
definePageMeta({ layout: 'settings' })

const { $heya } = useNuxtApp()
const { user } = useAuth()
import {
  jellyfinConfigQuery, jellyfinCredentialQuery,
  subsonicConfigQuery, subsonicCredentialQuery,
} from '~/queries/settings'

const jellyfinConfig = useQuery(jellyfinConfigQuery())
const subsonicConfig = useQuery(subsonicConfigQuery())
const jellyfinEnabled = computed(() => jellyfinConfig.data.value?.enabled ?? false)
const subsonicEnabled = computed(() => subsonicConfig.data.value?.enabled ?? false)

const serverAddress = computed(() =>
  import.meta.client ? window.location.origin : '')

const flash = ref<{ kind: 'ok' | 'err', text: string } | null>(null)

// ---- Jellyfin TV PIN ----

type PinCredential = {
  pin: string
  created_at: string
  rotated_at: string
  last_used_at?: string
}
const pinData = useQuery(jellyfinCredentialQuery())
const pin = ref<PinCredential | null>(null)
const pinBusy = ref(false)
const pinVisible = ref(false)

watch(() => pinData.data.value, value => {
  if (value) pin.value = value as PinCredential
}, { immediate: true })

async function rotatePin() {
  if (pin.value) {
    const ok = await useConfirm().confirm({
      title: 'Rotate PIN?',
      message: 'The current PIN stops working immediately. Devices already signed in stay signed in.',
      confirmLabel: 'Rotate',
    })
    if (!ok) return
  }
  pinBusy.value = true
  try {
    pin.value = await $heya('/api/me/jellyfin-credential', { method: 'POST' }) as PinCredential
    pinVisible.value = true
    flash.value = { kind: 'ok', text: 'PIN ready — sign in with your username and this PIN.' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail ?? e?.message ?? 'Could not create PIN.' }
  } finally {
    pinBusy.value = false
  }
}

async function revokePin() {
  const ok = await useConfirm().confirm({
    title: 'Revoke PIN?',
    message: 'The PIN stops working immediately. Your normal password keeps signing in; devices already signed in stay signed in.',
    confirmLabel: 'Revoke',
    destructive: true,
  })
  if (!ok) return
  pinBusy.value = true
  try {
    await $heya('/api/me/jellyfin-credential', { method: 'DELETE' })
    pin.value = null
    pinVisible.value = false
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail ?? e?.message ?? 'Revoke failed.' }
  } finally {
    pinBusy.value = false
  }
}

// ---- Subsonic app password ----

type AppPassword = {
  secret: string
  created_at: string
  rotated_at: string
  last_used_at?: string
}
const secretData = useQuery(subsonicCredentialQuery())
const secret = ref<AppPassword | null>(null)
const secretBusy = ref(false)
const secretVisible = ref(false)

watch(() => secretData.data.value, value => {
  if (value) secret.value = value as AppPassword
}, { immediate: true })

async function rotateSecret() {
  if (secret.value) {
    const ok = await useConfirm().confirm({
      title: 'Rotate app password?',
      message: 'Every Subsonic client signed in with the current app password stops working until you update it.',
      confirmLabel: 'Rotate',
    })
    if (!ok) return
  }
  secretBusy.value = true
  try {
    secret.value = await $heya('/api/me/subsonic-credential', { method: 'POST' }) as AppPassword
    secretVisible.value = true
    flash.value = { kind: 'ok', text: 'App password ready — paste it into your music client.' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail ?? e?.message ?? 'Could not create app password.' }
  } finally {
    secretBusy.value = false
  }
}

async function revokeSecret() {
  const ok = await useConfirm().confirm({
    title: 'Revoke app password?',
    message: 'All Subsonic clients are signed out immediately. You can generate a new password any time.',
    confirmLabel: 'Revoke',
    destructive: true,
  })
  if (!ok) return
  secretBusy.value = true
  try {
    await $heya('/api/me/subsonic-credential', { method: 'DELETE' })
    secret.value = null
    secretVisible.value = false
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail ?? e?.message ?? 'Revoke failed.' }
  } finally {
    secretBusy.value = false
  }
}

async function copyText(value: string, label: string) {
  try {
    await navigator.clipboard.writeText(value)
    flash.value = { kind: 'ok', text: `${label} copied to clipboard.` }
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
</script>

<template>
  <div class="settings-page">
    <SettingsContextHero
      title="Client apps"
      icon="link"
      eyebrow="Personal · Devices"
      description="Your sign-in credentials for TV, mobile, and music apps that connect through the Jellyfin and Subsonic APIs — separate from your Heya password, rotatable any time."
    />

    <div v-if="flash" class="cl-flash" :class="flash.kind" :role="flash.kind === 'err' ? 'alert' : 'status'" aria-live="polite">{{ flash.text }}</div>

    <SettingsSection
      title="TV sign-in PIN"
      icon="cast"
      description="A short numeric PIN that signs in on Jellyfin clients (Infuse, Streamyfin, Findroid, TV apps…) in place of your password — made for TV remotes. It works only on the Jellyfin API; your real password is never involved and keeps working everywhere."
    >
      <template #actions>
        <button class="cl-btn" :disabled="pinBusy" @click="rotatePin">
          {{ pin ? 'Rotate' : 'Generate' }}
        </button>
        <button v-if="pin" class="cl-btn danger" :disabled="pinBusy" @click="revokePin">
          Revoke
        </button>
      </template>

      <p v-if="!jellyfinEnabled" class="cl-hint cl-warn">
        The Jellyfin-compatible API is currently turned off on this server, so the
        PIN has nothing to sign in to. An admin can enable it under
        Settings → Client APIs.
      </p>

      <template v-if="pin">
        <div class="cl-secret-row">
          <code class="cl-secret cl-pin">{{ pinVisible ? pin.pin : '••••••' }}</code>
          <button class="cl-btn" @click="pinVisible = !pinVisible">
            {{ pinVisible ? 'Hide' : 'Reveal' }}
          </button>
          <button class="cl-btn" @click="copyText(pin.pin, 'PIN')">Copy</button>
        </div>
        <KVTable :rows="[
          { key: 'Server address', value: serverAddress, mono: true, copy: true },
          { key: 'Username', value: user?.username ?? '', mono: true },
          { key: 'Last rotated', value: formatWhen(pin.rotated_at) },
          { key: 'Last used', value: formatWhen(pin.last_used_at) },
        ]" />
      </template>
      <p v-else class="cl-hint">
        No PIN yet — generate one, then sign in on your TV with your Heya username
        and the 6-digit PIN instead of your password. Rotating or revoking it never
        touches your account password.
      </p>
    </SettingsSection>

    <SettingsSection
      title="Music app password"
      icon="music"
      description="Subsonic clients (Symfonium, DSub, play:Sub, Tempo, Supersonic, Amperfy…) use a generated app password — the protocol needs a secret the server can read back, so your real Heya password is never involved and never at risk."
    >
      <template #actions>
        <button class="cl-btn" :disabled="secretBusy" @click="rotateSecret">
          {{ secret ? 'Rotate' : 'Generate' }}
        </button>
        <button v-if="secret" class="cl-btn danger" :disabled="secretBusy" @click="revokeSecret">
          Revoke
        </button>
      </template>

      <p v-if="!subsonicEnabled" class="cl-hint cl-warn">
        The Subsonic-compatible API is currently turned off on this server, so the
        app password has nothing to sign in to. An admin can enable it under
        Settings → Client APIs.
      </p>

      <template v-if="secret">
        <div class="cl-secret-row">
          <code class="cl-secret">{{ secretVisible ? secret.secret : '••••••••••••••••••••' }}</code>
          <button class="cl-btn" @click="secretVisible = !secretVisible">
            {{ secretVisible ? 'Hide' : 'Reveal' }}
          </button>
          <button class="cl-btn" @click="copyText(secret.secret, 'App password')">Copy</button>
        </div>
        <KVTable :rows="[
          { key: 'Server address', value: serverAddress, mono: true, copy: true },
          { key: 'Username', value: user?.username ?? '', mono: true },
          { key: 'Last rotated', value: formatWhen(secret.rotated_at) },
          { key: 'Last used', value: formatWhen(secret.last_used_at) },
        ]" />
      </template>
      <p v-else class="cl-hint">
        No app password yet — generate one, then paste it into your Subsonic client
        together with your Heya username. Rotating or revoking it never touches
        your account password.
      </p>
    </SettingsSection>
  </div>
</template>

<style scoped>
.cl-flash {
  margin: 0 0 12px;
  padding: 8px 12px;
  border-radius: 8px;
  font-size: 13px;
}
.cl-flash.ok {
  background: color-mix(in srgb, var(--good) 14%, transparent);
}
.cl-flash.err {
  background: color-mix(in srgb, var(--bad) 16%, transparent);
}
.cl-hint {
  margin-top: 12px;
  font-size: 13px;
  color: var(--fg-2);
  line-height: 1.55;
}
.cl-hint.cl-warn {
  margin: 0 0 12px;
  padding: 8px 12px;
  border-radius: 8px;
  background: color-mix(in srgb, var(--warn, var(--bad)) 10%, transparent);
  color: var(--fg-1);
}
.cl-btn {
  padding: 6px 14px;
  border-radius: var(--r-sm);
  border: 1px solid var(--border);
  background: var(--bg-2);
  color: var(--fg-1);
  font-size: 12.5px;
  transition: color 0.12s, border-color 0.12s, background 0.12s;
}
.cl-btn:hover:not(:disabled) {
  color: var(--fg-0);
  border-color: var(--border-strong);
  background: rgb(var(--ink) / 0.05);
}
.cl-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.cl-btn.danger:hover:not(:disabled) {
  color: var(--bad);
  border-color: color-mix(in srgb, var(--bad) 35%, transparent);
  background: color-mix(in srgb, var(--bad) 8%, transparent);
}
.cl-secret-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
}
.cl-secret {
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
.cl-secret.cl-pin {
  font-size: 15px;
  letter-spacing: 0.35em;
}

@media (max-width: 520px) {
  .cl-secret-row { align-items: stretch; flex-wrap: wrap; }
  .cl-secret { flex-basis: 100%; }
  .cl-secret-row .cl-btn { flex: 1; }
}
</style>
