<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

import { adminListenersQuery } from '~/queries/settings'

const { $heya } = useNuxtApp()
const { confirm } = useConfirm()
const { isLocked, lockTooltip, ensure: ensureSources } = useConfigSources()

const {
  enabled, status, cfg,
  refresh: refreshTS, saveConfig, setFunnel, logout,
  fetchRaw, subscribeToEvents,
} = useTailscale()

const listenersData = useQuery(adminListenersQuery())
const listeners = computed(() => listenersData.data.value ?? null)
const loadingListeners = computed(() => listenersData.isLoading.value)
const saving = ref(false)
const loggingOut = ref(false)
const hostnameDraft = ref('')
const rawOpen = ref(false)
const rawLoading = ref(false)
const rawJSON = ref('')
const rawError = ref('')
const { flash } = useFlash()

let unsubscribe: (() => void) | null = null

async function loadListeners() {
  try { await listenersData.refetch() } catch {}
}

async function onMasterToggle(on: boolean) {
  saving.value = true
  try {
    await saveConfig({ enabled: on })
    await loadListeners()
    flash.value = { kind: 'ok', text: on ? 'Tailscale enabled.' : 'Tailscale disabled.' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Toggle failed.' }
  } finally { saving.value = false }
}

async function saveHostname() {
  if (!cfg.value || hostnameDraft.value === cfg.value.hostname) return
  saving.value = true
  try {
    await saveConfig({ hostname: hostnameDraft.value })
    flash.value = { kind: 'ok', text: 'Hostname saved — re-onboarding the node.' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Save failed.' }
  } finally { saving.value = false }
}

async function saveHTTPS(on: boolean) {
  saving.value = true
  try {
    await saveConfig({ https: on })
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'HTTPS toggle failed.' }
  } finally { saving.value = false }
}

async function saveFunnel(on: boolean) {
  saving.value = true
  try {
    await setFunnel(on)
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Funnel toggle failed.' }
  } finally { saving.value = false }
}

async function onLogout() {
  const ok = await confirm({
    title: 'Log out of the tailnet?',
    message: 'Clears the saved tailnet identity and disables Tailscale. Re-enable to onboard again.',
    destructive: true,
    confirmLabel: 'Log out',
  })
  if (!ok) return
  loggingOut.value = true
  try {
    await logout()
    await loadListeners()
  } finally { loggingOut.value = false }
}

async function toggleRaw() {
  rawOpen.value = !rawOpen.value
  if (rawOpen.value && !rawJSON.value && !rawError.value) {
    await loadRaw()
  }
}
async function loadRaw() {
  rawLoading.value = true; rawError.value = ''
  try {
    rawJSON.value = JSON.stringify(await fetchRaw(), null, 2)
  } catch (err: any) {
    rawError.value = err?.message ?? String(err)
    rawJSON.value = ''
  } finally { rawLoading.value = false }
}
async function copyRaw() { try { await navigator.clipboard.writeText(rawJSON.value) } catch {} }

const stateDirHint = computed(() => cfg.value?.state_dir || 'data/tailscale/')

function listenerIcon(kind: string): string {
  switch (kind) {
    case 'lan':       return 'network'
    case 'tailscale': return 'cloud'
    default:          return 'pulse'
  }
}

onMounted(async () => {
  await Promise.all([refreshTS(), loadListeners(), ensureSources()])
  hostnameDraft.value = cfg.value?.hostname ?? 'heya'
  unsubscribe = subscribeToEvents()
})
onBeforeUnmount(() => { unsubscribe?.() })

watch(cfg, (next) => {
  if (next && hostnameDraft.value !== next.hostname && !saving.value) {
    hostnameDraft.value = next.hostname
  }
})
</script>

<template>
  <div>
    <header class="sv2-page-head">
      <h2 class="sv2-page-title">Network</h2>
      <p class="sv2-page-desc">
        How the world reaches Heya. LAN listener is always on; Tailscale
        joins your tailnet without port forwarding; Funnel optionally
        publishes to the open internet (auth still applies).
      </p>
    </header>

    <SettingsSection title="Active listeners" icon="network">
      <template #actions>
        <LiveDot connected :label="`${listeners?.ws_subscribers ?? 0} WS clients`" />
      </template>

      <div v-if="loadingListeners" class="loading-state"><Icon name="spinner" :size="14" /> Loading…</div>
      <div v-else-if="listeners?.listeners?.length" class="lst-list">
        <div v-for="l in listeners.listeners" :key="l.kind + l.address" class="lst-card" :class="l.kind">
          <div class="lst-icon" :class="l.kind">
            <Icon :name="listenerIcon(l.kind)" :size="16" />
          </div>
          <div class="lst-body">
            <div class="lst-row">
              <span class="lst-addr mono">{{ l.address }}</span>
              <StatusBadge :state="l.public ? 'warn' : 'ok'">
                {{ l.public ? 'public' : (l.kind === 'tailscale' ? 'tailnet' : 'lan') }}
              </StatusBadge>
              <StatusBadge v-if="l.tls" state="ok">TLS</StatusBadge>
            </div>
            <div class="lst-desc">{{ l.description }}</div>
          </div>
        </div>
      </div>
    </SettingsSection>

    <SettingsSection title="Tailscale" icon="cloud"
      :description="enabled ? 'Joined to your tailnet — every tailnet device can reach this Heya at the address below.' : 'Off — Heya only answers on the LAN.'"
      :lockedBy="isLocked('tailscale.enabled') ? lockTooltip('tailscale.enabled') : undefined">
      <template #actions>
        <label class="ts-switch" :title="lockTooltip('tailscale.enabled')">
          <input
            type="checkbox"
            :checked="enabled"
            :disabled="saving || isLocked('tailscale.enabled')"
            @change="onMasterToggle(($event.target as HTMLInputElement).checked)"
          />
          <span class="ts-slider" />
        </label>
      </template>

      <a v-if="enabled && status?.login_url" :href="status.login_url" target="_blank" rel="noopener" class="login-cta">
        <div class="login-icon"><Icon name="cloud" :size="22" /></div>
        <div class="login-body">
          <div class="login-title">Authorize this device on your tailnet</div>
          <div class="login-sub">Click to open Tailscale and approve <code>{{ status.hostname }}</code>. One time only.</div>
        </div>
        <Icon name="chevright" :size="16" />
      </a>

      <template v-if="enabled && status">
        <KVTable :rows="[
          { key: 'Backend',     value: status.backend_state || (saving ? 'Starting…' : 'Pending') },
          { key: 'Hostname',    value: status.hostname || cfg?.hostname || '—', mono: true, copy: true },
          { key: 'MagicDNS',    value: status.magic_dns ?? '', mono: true, copy: true },
          { key: 'Tailnet IPv4', value: status.ipv4 ?? '', mono: true, copy: true },
          { key: 'Tailnet IPv6', value: status.ipv6 ?? '', mono: true, copy: true },
          { key: 'HTTPS cert',  value: status.cert_domain ?? '' },
          { key: 'Last error',  value: status.last_error ?? '' },
        ]" />

        <div v-if="status.https_url || status.funnel_url" class="urls">
          <a v-if="status.https_url" :href="status.https_url" target="_blank" rel="noopener" class="url-card">
            <div class="url-head">
              <span class="url-label">HTTPS · tailnet only</span>
              <StatusBadge state="ok">active</StatusBadge>
            </div>
            <div class="url-val mono">{{ status.https_url }}</div>
            <div class="url-hint">Reachable from any device on your tailnet.</div>
          </a>
          <a v-if="status.funnel_url" :href="status.funnel_url" target="_blank" rel="noopener" class="url-card funnel">
            <div class="url-head">
              <span class="url-label">Funnel · public internet</span>
              <StatusBadge state="warn">active</StatusBadge>
            </div>
            <div class="url-val mono">{{ status.funnel_url }}</div>
            <div class="url-hint">Reachable from anywhere — auth still applies.</div>
          </a>
        </div>
      </template>
    </SettingsSection>

    <SettingsSection v-if="enabled" title="Tailscale settings" icon="settings">
      <SettingsField label="Hostname"
        description="The name your node shows up as in the Tailscale admin console. Changing this re-onboards."
        :lockedBy="isLocked('tailscale.hostname') ? lockTooltip('tailscale.hostname') : undefined">
        <input
          v-model="hostnameDraft"
          class="sv2-input"
          :disabled="saving || isLocked('tailscale.hostname')"
          @blur="saveHostname"
        />
      </SettingsField>

      <SettingsField label="HTTPS on :443"
        description="Serve TLS on tailnet :443 using a Tailscale-issued cert. Requires HTTPS to be enabled for your tailnet."
        :lockedBy="isLocked('tailscale.https') ? lockTooltip('tailscale.https') : undefined">
        <label class="ts-switch sm">
          <input
            type="checkbox"
            :checked="cfg?.https ?? true"
            :disabled="saving || isLocked('tailscale.https')"
            @change="saveHTTPS(($event.target as HTMLInputElement).checked)"
          />
          <span class="ts-slider" />
        </label>
        <span v-if="cfg?.https && !status?.https_active" class="hint-warn">requested · not yet active</span>
      </SettingsField>

      <SettingsField label="Funnel (public exposure)"
        description="Publish Heya to the open internet via Tailscale Funnel. Requires Funnel to be allowed for your tailnet."
        :lockedBy="isLocked('tailscale.funnel') ? lockTooltip('tailscale.funnel') : undefined">
        <label class="ts-switch sm">
          <input
            type="checkbox"
            :checked="cfg?.funnel ?? false"
            :disabled="saving || isLocked('tailscale.funnel')"
            @change="saveFunnel(($event.target as HTMLInputElement).checked)"
          />
          <span class="ts-slider" />
        </label>
        <span v-if="cfg?.funnel && !status?.funnel_active" class="hint-warn">requested · not yet active</span>
      </SettingsField>
    </SettingsSection>

    <SettingsSection v-if="enabled" title="Identity" icon="key">
      <p class="hint">Clears the saved tailnet identity at <code>{{ stateDirHint }}</code> and disables Tailscale. Re-enable to onboard.</p>
      <button class="sv2-btn danger" :disabled="loggingOut" @click="onLogout">
        <Icon name="key" :size="12" />
        {{ loggingOut ? 'Logging out…' : 'Log out of tailnet' }}
      </button>
    </SettingsSection>

    <SettingsSection v-if="enabled" title="Raw tsnet status" icon="terminal">
      <template #actions>
        <button class="sv2-btn ghost" @click="toggleRaw">
          {{ rawOpen ? 'Hide' : 'Show' }}
        </button>
      </template>
      <div v-if="rawOpen">
        <div class="raw-bar">
          <button class="sv2-btn ghost" :disabled="rawLoading" @click="loadRaw">
            <Icon name="refresh" :size="12" /> {{ rawLoading ? 'Loading…' : 'Refresh' }}
          </button>
          <button v-if="rawJSON" class="sv2-btn ghost" @click="copyRaw">
            <Icon name="clipboard" :size="12" /> Copy JSON
          </button>
        </div>
        <pre v-if="rawError" class="raw-err">{{ rawError }}</pre>
        <pre v-else-if="rawJSON" class="raw-json">{{ rawJSON }}</pre>
        <p v-else class="hint">Click Refresh to fetch the live status from tsnet's LocalClient (same payload as <code>tailscale status --json</code>).</p>
      </div>
    </SettingsSection>

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

.lst-list { display: flex; flex-direction: column; gap: 8px; }
.lst-card {
  display: flex; align-items: flex-start; gap: 14px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.lst-card.tailscale { border-color: rgba(140, 160, 255, 0.30); background: rgba(140, 160, 255, 0.04); }
.lst-icon {
  width: 36px; height: 36px;
  border-radius: var(--r-sm);
  background: var(--bg-0);
  color: var(--good);
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}
.lst-icon.tailscale { color: rgb(140, 160, 255); }
.lst-body { flex: 1; min-width: 0; }
.lst-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.lst-addr { font-size: 14px; font-weight: 600; color: var(--fg-0); }
.lst-desc { font-size: 12px; color: var(--fg-3); margin-top: 2px; }

.ts-switch {
  position: relative;
  width: 44px; height: 24px;
  cursor: pointer;
  flex-shrink: 0;
}
.ts-switch.sm { width: 36px; height: 20px; }
.ts-switch input { opacity: 0; width: 0; height: 0; }
.ts-slider {
  position: absolute; inset: 0;
  background: rgb(var(--ink) / 0.08);
  border-radius: 12px;
  transition: background 0.2s;
}
.ts-slider::before {
  content: '';
  position: absolute;
  top: 3px; left: 3px;
  width: 18px; height: 18px;
  border-radius: 50%;
  background: #fff;
  transition: transform 0.2s;
  box-shadow: 0 1px 3px rgb(var(--shade) / 0.4);
}
.ts-switch.sm .ts-slider::before { top: 3px; left: 3px; width: 14px; height: 14px; }
.ts-switch input:checked + .ts-slider { background: var(--good); }
.ts-switch input:checked + .ts-slider::before { transform: translateX(20px); }
.ts-switch.sm input:checked + .ts-slider::before { transform: translateX(16px); }

.login-cta {
  display: flex; align-items: center; gap: 14px;
  padding: 16px 18px;
  background: var(--gold-soft);
  border: 1px solid color-mix(in srgb, var(--gold) 30%, transparent);
  border-radius: var(--r-md);
  text-decoration: none;
  color: inherit;
  margin-bottom: 14px;
  transition: background 0.12s;
}
.login-cta:hover { background: color-mix(in srgb, var(--gold) 18%, transparent); }
.login-icon {
  width: 40px; height: 40px;
  border-radius: var(--r-md);
  background: color-mix(in srgb, var(--gold) 18%, transparent);
  color: var(--gold);
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}
.login-body { flex: 1; }
.login-title { font-size: 14px; font-weight: 600; color: var(--gold); }
.login-sub { font-size: 12px; color: var(--fg-2); margin-top: 2px; }
.login-sub code { font-family: var(--font-mono); color: var(--gold); }

.urls {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 10px;
  margin-top: 14px;
}
.url-card {
  display: flex; flex-direction: column; gap: 6px;
  padding: 14px 16px;
  background: var(--bg-1);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  text-decoration: none;
  color: inherit;
  transition: border-color 0.12s;
}
.url-card:hover { border-color: var(--gold); }
.url-card.funnel { border-color: color-mix(in srgb, var(--gold) 20%, transparent); background: var(--gold-soft); }
.url-head { display: flex; align-items: center; justify-content: space-between; gap: 6px; }
.url-label {
  font-family: var(--font-mono);
  font-size: 10.5px; font-weight: 700;
  text-transform: uppercase; letter-spacing: 0.06em;
  color: var(--fg-3);
}
.url-val { font-size: 13px; color: var(--fg-0); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.url-hint { font-size: 11px; color: var(--fg-3); }

.sv2-input {
  background: var(--bg-0);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  color: var(--fg-0);
  font-size: 13px;
  padding: 8px 12px;
  outline: none;
  min-width: 240px;
  transition: border-color 0.12s;
}
.sv2-input:focus { border-color: var(--gold); }
.sv2-input:disabled { opacity: 0.5; cursor: not-allowed; }

.hint { font-size: 12px; color: var(--fg-3); line-height: 1.5; margin: 0 0 10px; }
.hint code { font-family: var(--font-mono); color: var(--fg-1); }
.hint-warn { font-size: 11px; color: var(--gold); margin-left: 8px; }

.raw-bar { display: flex; gap: 6px; margin-bottom: 10px; }
.raw-json, .raw-err {
  font-family: var(--font-mono);
  font-size: 11px;
  background: var(--bg-0);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  padding: 12px 14px;
  margin: 0;
  overflow-x: auto;
  white-space: pre;
  max-height: 360px;
  overflow-y: auto;
}
.raw-err { color: var(--bad); }

.mono { font-family: var(--font-mono); }

@media (max-width: 720px) {
  .sv2-input { min-width: 0; width: 100%; }
}
</style>
