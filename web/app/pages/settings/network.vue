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

const {
  available: remoteAvailable, cfg: remoteCfg, status: remoteStatus, message: remoteMessage,
  refresh: refreshRemote, saveConfig: saveRemote, recheck: recheckRemote,
  subscribeToEvents: subscribeRemote,
} = useRemoteAccess()

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
const { toast } = useToast()

let unsubscribe: (() => void) | null = null
let unsubscribeRemote: (() => void) | null = null

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
async function copyRaw() {
  try {
    await navigator.clipboard.writeText(rawJSON.value)
    toast.ok('Copied JSON to clipboard.')
  } catch {
    toast.err('Clipboard blocked — copy manually.')
  }
}

const stateDirHint = computed(() => cfg.value?.state_dir || 'data/tailscale/')

// ---- Remote access (UPnP + ACME + reachability) ----

const remoteSaving = ref(false)
const checking = ref(false)
// 'none' sentinel: AppSelect treats '' as no-value (reka), so the "no
// provider" row needs a real string; mapped back to '' on save.
const providerDraft = ref('none')
const domainDraft = ref('')
const subdomainDraft = ref('')
const tokenDraft = ref('')
const emailDraft = ref('')
const portDraft = ref('')

const providerOptions = [
  { value: 'none', label: 'None — bare IP, self-signed certificate' },
  { value: 'desec', label: 'deSEC (dedyn.io)', meta: 'free · LAN + WAN hostnames' },
  { value: 'duckdns', label: 'DuckDNS', meta: 'free · WAN hostname only' },
  { value: 'cloudflare', label: 'Cloudflare', meta: 'your own domain' },
]

const domainPlaceholder = computed(() => {
  switch (providerDraft.value) {
    case 'desec': return 'myname.dedyn.io'
    case 'duckdns': return 'myname.duckdns.org'
    case 'cloudflare': return 'example.com'
    default: return ''
  }
})

const remoteBadge = computed((): { state: 'ok' | 'warn' | 'error' | 'idle', label: string } => {
  switch (remoteStatus.value?.phase) {
    case 'reachable':   return { state: 'ok', label: 'Reachable' }
    case 'starting':    return { state: 'warn', label: 'Starting…' }
    case 'mapping':     return { state: 'warn', label: 'Mapping port…' }
    case 'probing':     return { state: 'warn', label: 'Checking…' }
    case 'unverified':  return { state: 'warn', label: 'Unverified' }
    case 'unreachable': return { state: 'error', label: 'Unreachable' }
    case 'error':       return { state: 'error', label: 'Error' }
    default:            return { state: 'idle', label: 'Off' }
  }
})

const certBadge = computed((): { state: 'ok' | 'warn' | 'idle', label: string } => {
  const c = remoteStatus.value?.cert
  if (!c) return { state: 'idle', label: 'no certificate' }
  if (c.issuing) return { state: 'warn', label: 'issuing…' }
  if (c.mode === 'acme') return { state: 'ok', label: 'Let’s Encrypt' }
  return { state: 'warn', label: 'self-signed' }
})

const remoteRows = computed(() => {
  const s = remoteStatus.value
  if (!s) return []
  const check = s.last_check
  return [
    { key: 'Port', value: s.port ? String(s.port) : '', mono: true, copy: true },
    { key: 'LAN IP', value: s.lan_ip ?? '', mono: true },
    { key: 'Router WAN IP', value: s.router_external_ip ?? '', mono: true },
    { key: 'Public IP (observed)', value: s.observed_ip ?? '', mono: true, copy: true },
    { key: 'UPnP', value: s.upnp?.available ? (s.upnp.error || 'mapped') : (s.upnp?.error || 'unavailable') },
    { key: 'Last check', value: s.last_check_at ? `${s.last_check_at}${check?.latency_ms ? ` · ${check.latency_ms}ms` : ''}` : 'never' },
  ]
})

const dnsDirty = computed(() => {
  const c = remoteCfg.value
  if (!c) return false
  const provider = providerDraft.value === 'none' ? '' : providerDraft.value
  return provider !== (c.dns_provider ?? '')
    || domainDraft.value !== (c.domain ?? '')
    || subdomainDraft.value !== (c.subdomain ?? '')
    || emailDraft.value !== (c.acme_email ?? '')
    || tokenDraft.value !== ''
})

function seedRemoteDrafts() {
  const c = remoteCfg.value
  if (!c) return
  providerDraft.value = c.dns_provider || 'none'
  domainDraft.value = c.domain ?? ''
  subdomainDraft.value = c.subdomain ?? ''
  emailDraft.value = c.acme_email ?? ''
  tokenDraft.value = ''
  portDraft.value = c.port ? String(c.port) : ''
}

async function savePort() {
  const n = Number.parseInt(portDraft.value, 10)
  if (!Number.isFinite(n) || n === remoteCfg.value?.port) return
  if (n < 1024 || n > 65535) {
    flash.value = { kind: 'err', text: 'Port must be between 1024 and 65535.' }
    return
  }
  remoteSaving.value = true
  try {
    await saveRemote({ port: n })
    flash.value = { kind: 'ok', text: `Port ${n} saved — remapping and re-checking.` }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail ?? e?.message ?? 'Port save failed.' }
  } finally { remoteSaving.value = false }
}

async function onRemoteToggle(on: boolean) {
  remoteSaving.value = true
  try {
    await saveRemote({ enabled: on })
    flash.value = { kind: 'ok', text: on ? 'Remote access enabling — watch the status above.' : 'Remote access disabled.' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail ?? e?.message ?? 'Toggle failed.' }
  } finally { remoteSaving.value = false }
}

async function saveDNS() {
  remoteSaving.value = true
  try {
    await saveRemote({
      dns_provider: providerDraft.value === 'none' ? '' : providerDraft.value,
      domain: domainDraft.value.trim(),
      subdomain: subdomainDraft.value.trim(),
      acme_email: emailDraft.value.trim(),
      ...(tokenDraft.value ? { dns_token: tokenDraft.value } : {}),
    })
    tokenDraft.value = ''
    flash.value = { kind: 'ok', text: 'DNS settings saved — re-applying remote access.' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail ?? e?.message ?? 'Save failed.' }
  } finally { remoteSaving.value = false }
}

async function onRecheck() {
  checking.value = true
  try {
    await recheckRemote()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail ?? e?.message ?? 'Check failed.' }
  } finally { checking.value = false }
}

watch(remoteCfg, () => {
  if (!remoteSaving.value) seedRemoteDrafts()
})

function listenerIcon(kind: string): string {
  switch (kind) {
    case 'lan':       return 'network'
    case 'tailscale': return 'cloud'
    default:          return 'pulse'
  }
}

onMounted(async () => {
  await Promise.all([refreshTS(), refreshRemote(), loadListeners(), ensureSources()])
  hostnameDraft.value = cfg.value?.hostname ?? 'heya'
  seedRemoteDrafts()
  unsubscribe = subscribeToEvents()
  unsubscribeRemote = subscribeRemote()
})
onBeforeUnmount(() => { unsubscribe?.(); unsubscribeRemote?.() })

watch(cfg, (next) => {
  if (next && hostnameDraft.value !== next.hostname && !saving.value) {
    hostnameDraft.value = next.hostname
  }
})
</script>

<template>
  <div>
    <SettingsContextHero
      title="Network"
      icon="network"
      eyebrow="Server · Connectivity"
      description="Understand every route into Heya—from the local listener to Tailscale and optional Funnel access—with authentication preserved throughout."
    />

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

    <SettingsSection title="Remote access" icon="globe"
      :description="remoteCfg?.enabled ? 'Direct access from the internet — UPnP port mapping, verified from outside by heya.media.' : 'Off — map a router port via UPnP and reach Heya directly, no VPN required.'"
      :lockedBy="isLocked('remote.enabled') ? lockTooltip('remote.enabled') : undefined">
      <template #actions>
        <StatusBadge v-if="remoteAvailable && remoteCfg?.enabled" :state="remoteBadge.state">{{ remoteBadge.label }}</StatusBadge>
        <AppSwitch
          :model-value="remoteCfg?.enabled ?? false"
          size="md"
          aria-label="Enable remote access"
          :disabled="remoteSaving || !remoteAvailable || isLocked('remote.enabled')"
          @update:model-value="onRemoteToggle"
        />
      </template>

      <p v-if="!remoteAvailable" class="hint">{{ remoteMessage || 'Remote access is unavailable in this run mode.' }}</p>

      <template v-else-if="remoteCfg?.enabled && remoteStatus">
        <div v-if="remoteStatus.cgnat" class="cgnat-banner">
          <Icon name="warning" :size="14" />
          <div>
            <strong>Carrier-grade NAT detected.</strong> Your ISP shares one public IP across customers —
            port forwarding cannot work on this connection. Use <NuxtLink to="/settings/network">Tailscale</NuxtLink> below for remote access instead.
          </div>
        </div>
        <div v-else-if="remoteStatus.detail" class="remote-detail" :class="remoteBadge.state">{{ remoteStatus.detail }}</div>

        <KVTable :rows="remoteRows" />

        <div v-if="remoteStatus.remote_url || remoteStatus.lan_url" class="urls">
          <a v-if="remoteStatus.remote_url" :href="remoteStatus.remote_url" target="_blank" rel="noopener" class="url-card">
            <div class="url-head">
              <span class="url-label">Remote · internet</span>
              <StatusBadge :state="remoteStatus.phase === 'reachable' ? 'ok' : 'warn'">
                {{ remoteStatus.phase === 'reachable' ? 'verified' : remoteStatus.phase }}
              </StatusBadge>
            </div>
            <div class="url-val mono">{{ remoteStatus.remote_url }}</div>
            <div class="url-hint">Reachable from anywhere — auth still applies.</div>
          </a>
          <a v-if="remoteStatus.lan_url" :href="remoteStatus.lan_url" target="_blank" rel="noopener" class="url-card">
            <div class="url-head">
              <span class="url-label">LAN · HTTPS</span>
              <StatusBadge state="ok">local</StatusBadge>
            </div>
            <div class="url-val mono">{{ remoteStatus.lan_url }}</div>
            <div class="url-hint">Valid TLS on your own network, no port forwarding involved.</div>
          </a>
        </div>

        <SettingsField label="External port"
          description="The router port mapped to Heya — part of every remote URL, so changing it breaks existing bookmarks. Auto-generated on first enable."
          :lockedBy="isLocked('remote.port') ? lockTooltip('remote.port') : undefined"
          v-slot="{ fieldId }">
          <input :id="fieldId" v-model="portDraft" type="number" min="1024" max="65535" class="sv2-input mono port-input"
            :disabled="remoteSaving || isLocked('remote.port')" @blur="savePort" @keyup.enter="savePort" />
        </SettingsField>

        <div class="raw-bar" style="margin-top: 12px">
          <button class="sv2-btn ghost" :disabled="checking" @click="onRecheck">
            <Icon :name="checking ? 'spinner' : 'refresh'" :size="12" />
            {{ checking ? 'Checking…' : 'Check now' }}
          </button>
        </div>
      </template>
    </SettingsSection>

    <SettingsSection v-if="remoteAvailable && remoteCfg?.enabled" title="Hostnames & certificate" icon="shield"
      description="Point a DNS provider at this server to get stable hostnames and a real browser-trusted certificate (Let’s Encrypt, DNS-01 — no port 80/443 needed)."
      :lockedBy="isLocked('remote.dns_provider') ? lockTooltip('remote.dns_provider') : undefined">
      <template #actions>
        <StatusBadge :state="certBadge.state">{{ certBadge.label }}</StatusBadge>
      </template>

      <SettingsField label="DNS provider"
        description="deSEC and DuckDNS are free (create an account, paste the token). Cloudflare manages a domain you own."
        v-slot="{ fieldId }">
        <AppSelect :id="fieldId" v-model="providerDraft" :options="providerOptions"
          :disabled="remoteSaving || isLocked('remote.dns_provider')" />
      </SettingsField>

      <template v-if="providerDraft !== 'none'">
        <SettingsField label="Domain"
          :description="providerDraft === 'cloudflare' ? 'The zone as it appears in your Cloudflare dashboard.' : 'The domain you registered at the provider.'"
          :lockedBy="isLocked('remote.domain') ? lockTooltip('remote.domain') : undefined"
          v-slot="{ fieldId }">
          <input :id="fieldId" v-model="domainDraft" class="sv2-input mono" :placeholder="domainPlaceholder"
            :disabled="remoteSaving || isLocked('remote.domain')" />
        </SettingsField>

        <SettingsField v-if="providerDraft !== 'duckdns'" label="Subdomain (optional)"
          description="Nest Heya under a label — e.g. “heya” gives wan.heya.your-domain."
          :lockedBy="isLocked('remote.subdomain') ? lockTooltip('remote.subdomain') : undefined"
          v-slot="{ fieldId }">
          <input :id="fieldId" v-model="subdomainDraft" class="sv2-input mono" placeholder="heya"
            :disabled="remoteSaving || isLocked('remote.subdomain')" />
        </SettingsField>

        <SettingsField label="API token"
          :description="providerDraft === 'cloudflare' ? 'A scoped API token with Zone.DNS:Edit — never your global API key.' : 'The token from your provider dashboard. Stored server-side, never shown again.'"
          :lockedBy="isLocked('remote.dns_token') ? lockTooltip('remote.dns_token') : undefined"
          v-slot="{ fieldId }">
          <input :id="fieldId" v-model="tokenDraft" type="password" class="sv2-input mono" autocomplete="off"
            :placeholder="remoteCfg?.token_set ? '•••••• saved — paste to replace' : 'paste token'"
            :disabled="remoteSaving || isLocked('remote.dns_token')" />
        </SettingsField>

        <SettingsField label="ACME email (optional)"
          description="Let’s Encrypt expiry notices go here. Leave empty to skip."
          v-slot="{ fieldId }">
          <input :id="fieldId" v-model="emailDraft" type="email" class="sv2-input" placeholder="you@example.com"
            :disabled="remoteSaving" />
        </SettingsField>
      </template>

      <div class="raw-bar" style="margin-top: 4px">
        <button class="sv2-btn" :disabled="!dnsDirty || remoteSaving" @click="saveDNS">
          {{ remoteSaving ? 'Saving…' : 'Save & apply' }}
        </button>
      </div>

      <template v-if="remoteStatus?.cert && remoteStatus.cert.mode !== 'none'">
        <KVTable :rows="[
          { key: 'Certificate', value: certBadge.label },
          { key: 'Covers', value: remoteStatus.cert.sans?.join(', ') ?? '', mono: true },
          { key: 'Expires', value: remoteStatus.cert.expiry ?? '' },
          { key: 'Error', value: remoteStatus.cert.error ?? '' },
          { key: 'DNS error', value: remoteStatus.dns?.error ?? '' },
        ]" />
      </template>

      <p v-if="providerDraft === 'duckdns'" class="hint" style="margin-top: 10px">
        DuckDNS holds a single address per domain, so only the remote (WAN) hostname exists — there's no LAN hostname tier.
      </p>
      <p v-if="providerDraft === 'cloudflare'" class="hint" style="margin-top: 10px">
        Records are created DNS-only (grey cloud). Cloudflare's proxy can't forward Heya's high port, and streaming video through it is a fast way to get flagged — leave it off.
      </p>
    </SettingsSection>

    <SettingsSection title="Tailscale" icon="cloud"
      :description="enabled ? 'Joined to your tailnet — every tailnet device can reach this Heya at the address below.' : 'Off — Heya only answers on the LAN.'"
      :lockedBy="isLocked('tailscale.enabled') ? lockTooltip('tailscale.enabled') : undefined">
      <template #actions>
        <label class="ts-switch" :title="lockTooltip('tailscale.enabled')">
          <input
            type="checkbox"
            aria-label="Enable Tailscale"
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
        :lockedBy="isLocked('tailscale.hostname') ? lockTooltip('tailscale.hostname') : undefined"
        v-slot="{ fieldId }">
        <input
          :id="fieldId"
          v-model="hostnameDraft"
          class="sv2-input"
          :disabled="saving || isLocked('tailscale.hostname')"
          @blur="saveHostname"
        />
      </SettingsField>

      <SettingsField label="HTTPS on :443"
        description="Serve TLS on tailnet :443 using a Tailscale-issued cert. Requires HTTPS to be enabled for your tailnet."
        :lockedBy="isLocked('tailscale.https') ? lockTooltip('tailscale.https') : undefined"
        v-slot="{ fieldId }">
        <label class="ts-switch sm">
          <input
            :id="fieldId"
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
        :lockedBy="isLocked('tailscale.funnel') ? lockTooltip('tailscale.funnel') : undefined"
        v-slot="{ fieldId }">
        <label class="ts-switch sm">
          <input
            :id="fieldId"
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
.port-input { min-width: 120px; width: 120px; }
.sv2-input:disabled { opacity: 0.5; cursor: not-allowed; }

.hint { font-size: 12px; color: var(--fg-3); line-height: 1.5; margin: 0 0 10px; }
.hint code { font-family: var(--font-mono); color: var(--fg-1); }
.hint-warn { font-size: 11px; color: var(--gold); margin-left: 8px; }

.cgnat-banner {
  display: flex; align-items: flex-start; gap: 10px;
  padding: 12px 14px;
  margin-bottom: 14px;
  font-size: 12.5px; line-height: 1.5;
  color: var(--fg-1);
  background: color-mix(in srgb, var(--bad) 8%, transparent);
  border: 1px solid color-mix(in srgb, var(--bad) 30%, transparent);
  border-radius: var(--r-md);
}
.cgnat-banner :first-child { color: var(--bad); flex-shrink: 0; margin-top: 2px; }
.cgnat-banner strong { color: var(--bad); }

.remote-detail {
  padding: 10px 14px;
  margin-bottom: 14px;
  font-size: 12.5px; line-height: 1.5;
  border-radius: var(--r-md);
  border: 1px solid var(--border);
  background: var(--bg-2);
  color: var(--fg-2);
}
.remote-detail.error {
  background: color-mix(in srgb, var(--bad) 8%, transparent);
  border-color: color-mix(in srgb, var(--bad) 30%, transparent);
  color: var(--fg-1);
}
.remote-detail.warn {
  background: var(--gold-soft);
  border-color: color-mix(in srgb, var(--gold) 25%, transparent);
}

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
