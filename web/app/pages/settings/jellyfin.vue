<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

const { $heya } = useNuxtApp()
const { isLocked, lockTooltip, ensure: ensureSources } = useConfigSources()

const enabled = ref(false)
const loading = ref(true)
const saving = ref(false)
const flash = ref<{ kind: 'ok' | 'err', text: string } | null>(null)

const serverAddress = computed(() =>
  import.meta.client ? window.location.origin : '')

async function load() {
  try {
    const res = await $heya('/api/jellyfin/config')
    enabled.value = res.enabled
  } catch {} finally {
    loading.value = false
  }
}

async function onToggle(on: boolean) {
  saving.value = true
  flash.value = null
  try {
    const res = await $heya('/api/jellyfin/config', {
      method: 'PUT',
      body: { enabled: on },
    })
    enabled.value = res.enabled
    flash.value = {
      kind: 'ok',
      text: on
        ? 'Jellyfin-compatible API enabled — point a client at the address below.'
        : 'Jellyfin-compatible API disabled.',
    }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail ?? e?.message ?? 'Toggle failed.' }
    await load()
  } finally {
    saving.value = false
  }
}

onMounted(() => { ensureSources(); load() })
</script>

<template>
  <div class="settings-page">
    <SettingsSection
      title="Jellyfin-compatible API"
      icon="cast"
      :description="enabled
        ? 'On — stock Jellyfin apps can sign in to this Heya with their normal server-address flow.'
        : 'Off — Jellyfin client apps can\'t see this server.'"
      :lockedBy="isLocked('jellyfin.enabled') ? lockTooltip('jellyfin.enabled') : undefined"
    >
      <template #actions>
        <label class="jf-switch" :title="lockTooltip('jellyfin.enabled')">
          <input
            type="checkbox"
            :checked="enabled"
            :disabled="loading || saving || isLocked('jellyfin.enabled')"
            @change="onToggle(($event.target as HTMLInputElement).checked)"
          />
          <span class="jf-slider" />
        </label>
      </template>

      <div v-if="flash" class="jf-flash" :class="flash.kind">{{ flash.text }}</div>

      <template v-if="enabled">
        <KVTable :rows="[
          { key: 'Server address', value: serverAddress, mono: true, copy: true },
          { key: 'Advertises as', value: 'Jellyfin Server 10.11.11' },
          { key: 'Sign in with', value: 'Your normal Heya username & password' },
        ]" />
        <p class="jf-hint">
          In any Jellyfin app (Infuse, Streamyfin, Finamp, Findroid, jellyfin-web…),
          add a server with the address above and log in with your Heya account.
          Sessions created by Jellyfin apps show up under
          <NuxtLink to="/settings/sessions">Settings → Sessions</NuxtLink> and can be
          revoked like any other device.
        </p>
      </template>
      <p v-else class="jf-hint">
        When enabled, Heya answers the Jellyfin client protocol alongside its own API
        — nothing about the normal web app changes. You can also force this on with
        <code>HEYA_JELLYFIN_API_ENABLED=true</code>, which locks this toggle.
      </p>
    </SettingsSection>
  </div>
</template>

<style scoped>
.jf-switch {
  position: relative;
  display: inline-block;
  width: 42px;
  height: 24px;
  flex: none;
}
.jf-switch input {
  opacity: 0;
  width: 0;
  height: 0;
}
.jf-slider {
  position: absolute;
  inset: 0;
  border-radius: 999px;
  background: color-mix(in oklab, var(--text) 18%, transparent);
  transition: background 0.15s ease;
  cursor: pointer;
}
.jf-slider::before {
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
.jf-switch input:checked + .jf-slider {
  background: var(--accent, #7c5cff);
}
.jf-switch input:checked + .jf-slider::before {
  transform: translateX(18px);
}
.jf-switch input:disabled + .jf-slider {
  opacity: 0.5;
  cursor: not-allowed;
}
.jf-flash {
  margin: 0 0 12px;
  padding: 8px 12px;
  border-radius: 8px;
  font-size: 13px;
}
.jf-flash.ok {
  background: color-mix(in oklab, #22c55e 14%, transparent);
}
.jf-flash.err {
  background: color-mix(in oklab, #ef4444 16%, transparent);
}
.jf-hint {
  margin-top: 12px;
  font-size: 13px;
  opacity: 0.75;
  line-height: 1.55;
}
.jf-hint code {
  font-size: 12px;
}
</style>
