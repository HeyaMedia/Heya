<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

import { adminSecurityQuery } from '~/queries/settings'
import type { SecurityEvent } from '~~/shared/api/types.gen'

const { $heya } = useNuxtApp()
const securityData = useQuery(adminSecurityQuery())
const status = computed(() => securityData.data.value ?? null)
const loading = computed(() => securityData.isLoading.value && !status.value)
const events = computed(() => [...(status.value?.events.recent ?? [])].reverse())
const counters = computed(() => status.value?.events.counters ?? {
  login_failures: 0,
  login_throttled: 0,
  registration_throttled: 0,
  verifier_saturated: 0,
  waf_blocked: 0,
  waf_matches: 0,
})
const trustedDraft = ref('')
const trustedDirty = ref(false)
const trustedSaving = ref(false)
const { flash: trustedFlash } = useFlash()
const trustedLocked = computed(() => status.value?.trusted_networks.runtime_editable === false)

watch(() => status.value?.trusted_networks.networks, networks => {
  if (!networks || trustedDirty.value || trustedSaving.value) return
  trustedDraft.value = networks.join('\n')
}, { immediate: true })

let timer: ReturnType<typeof setInterval> | null = null
onMounted(() => { timer = setInterval(() => { void securityData.refetch() }, 5000) })
onBeforeUnmount(() => { if (timer) clearInterval(timer) })

const wafLabel = computed(() => {
  if (!status.value) return '—'
  if (status.value.waf.blocking) return 'Blocking'
  if (status.value.waf.enabled) return 'Detecting'
  return 'Off'
})

const wafBadge = computed((): 'ok' | 'warn' | 'error' | 'idle' => {
  if (!status.value?.waf.enabled) return 'idle'
  return status.value.waf.blocking ? 'ok' : 'warn'
})

const registrationLabel = computed(() => {
  switch (status.value?.registration.state) {
    case 'available': return 'Open for first user'
    case 'closed': return 'Closed after setup'
    case 'unknown': return 'State unavailable'
    default: return 'Disabled'
  }
})

const registrationBadge = computed((): 'ok' | 'warn' | 'error' | 'idle' => {
  switch (status.value?.registration.state) {
    case 'available': return 'warn'
    case 'unknown': return 'error'
    default: return 'ok'
  }
})

function configSource(source: string, envVar?: string) {
  return source === 'env' && envVar ? `${source} · ${envVar}` : source
}

function refillLabel(seconds: number) {
  if (seconds >= 60 && seconds % 60 === 0) return `1 token / ${seconds / 60} min`
  return `1 token / ${seconds} sec`
}

function formatBytes(bytes: number) {
  if (bytes >= 1024 * 1024) return `${bytes / (1024 * 1024)} MiB`
  return `${Math.round(bytes / 1024)} KiB`
}

function formatTime(value: string) {
  return new Date(value).toLocaleString([], {
    month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit',
  })
}

function runtimeAge(value?: string) {
  if (!value) return 'this process'
  const seconds = Math.max(0, Math.floor((Date.now() - new Date(value).getTime()) / 1000))
  if (seconds < 60) return `${seconds}s runtime`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m runtime`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h runtime`
  return `${Math.floor(seconds / 86400)}d runtime`
}

async function saveTrustedNetworks() {
  if (trustedSaving.value || trustedLocked.value) return
  const networks = trustedDraft.value
    .split(/[\s,;]+/)
    .map(value => value.trim())
    .filter(Boolean)
  trustedSaving.value = true
  trustedFlash.value = null
  try {
    const result = await $heya('/api/admin/security/trusted-networks', {
      method: 'PUT',
      body: { networks },
    })
    trustedDraft.value = result.networks?.join('\n') ?? ''
    trustedDirty.value = false
    trustedFlash.value = { kind: 'ok', text: 'Trusted networks applied live to the WAF and authentication limiters.' }
    await securityData.refetch()
  } catch (e: any) {
    trustedFlash.value = { kind: 'err', text: e?.data?.detail ?? e?.message ?? 'Failed to apply trusted networks.' }
  } finally {
    trustedSaving.value = false
  }
}

function eventTitle(event: SecurityEvent) {
  switch (event.kind) {
    case 'waf_match': return event.rule_id ? `OWASP CRS rule ${event.rule_id} matched` : 'OWASP CRS rule matched'
    case 'waf_block': return 'Request blocked by the WAF'
    case 'login_throttled': return 'Login attempt throttled'
    case 'registration_throttled': return 'Registration attempt throttled'
    case 'verifier_saturated': return 'Password verifier at capacity'
    case 'login_failed': return 'Invalid login rejected'
    default: return event.kind.replaceAll('_', ' ')
  }
}

function eventIcon(event: SecurityEvent) {
  if (event.kind.startsWith('waf_')) return 'shield'
  if (event.kind === 'login_failed') return 'key'
  return 'timer'
}

function eventTone(event: SecurityEvent): 'ok' | 'warn' | 'error' | 'idle' {
  if (event.kind === 'waf_block') return 'ok'
  if (event.kind === 'login_failed' || event.kind.endsWith('_throttled')) return 'warn'
  if (event.kind === 'verifier_saturated') return 'error'
  return 'idle'
}

function eventDetail(event: SecurityEvent) {
  const parts: string[] = []
  if (event.message) parts.push(event.message)
  if (event.path) parts.push(event.path)
  if (event.client_ip) parts.push(`source ${event.client_ip}`)
  if (event.account_key) parts.push(`account ${event.account_key}`)
  return parts.join(' · ')
}
</script>

<template>
  <div>
    <SettingsContextHero
      title="Security"
      icon="shield"
      eyebrow="Server · Public boundary"
      description="Inspect Heya's effective registration, WAF, authentication, password, and HTTP safeguards. Runtime counters reset when the API process restarts."
    >
      <div class="context-fact"><strong>{{ wafLabel }}</strong><span>OWASP WAF</span></div>
      <div class="context-fact"><strong>{{ registrationLabel }}</strong><span>Registration</span></div>
      <div class="context-fact"><strong>{{ runtimeAge(status?.started_at) }}</strong><span>Counter window</span></div>
    </SettingsContextHero>

    <div v-if="loading" class="loading-state"><Icon name="spinner" :size="15" /> Reading security posture…</div>
    <template v-else-if="status">
      <div class="tiles">
        <MetricTile label="WAF matches" :value="counters.waf_matches" icon="shield"
          :tone="counters.waf_matches ? 'warn' : 'good'" sub="CRS rule matches" />
        <MetricTile label="WAF blocked" :value="counters.waf_blocked" icon="shield"
          :tone="counters.waf_blocked ? 'warn' : 'good'" sub="requests stopped" />
        <MetricTile label="Login failures" :value="counters.login_failures" icon="key"
          :tone="counters.login_failures ? 'warn' : 'good'" sub="invalid credentials" />
        <MetricTile label="Throttled" :value="counters.login_throttled + counters.registration_throttled" icon="timer"
          :tone="(counters.login_throttled + counters.registration_throttled) ? 'warn' : 'good'" sub="rate-limit rejections" />
      </div>

      <div class="posture-grid">
        <SettingsSection title="Public boundary" icon="shield" description="WAF mode and registration are boot-time controls; trusted networks are applied live below.">
          <div class="posture-list">
            <div class="posture-row">
              <div><strong>OWASP Core Rule Set</strong><span>Coraza inspects every Heya ingress before the application handler.</span></div>
              <div class="posture-value">
                <StatusBadge :state="wafBadge">{{ wafLabel }}</StatusBadge>
                <code>{{ status.waf.value }}</code>
              </div>
            </div>
            <div class="posture-row">
              <div><strong>Rule bundle</strong><span>Pinned into the Heya binary and updated through reviewed dependency releases.</span></div>
              <div class="posture-value"><StatusBadge state="ok">Bundled</StatusBadge><code>{{ status.waf.crs_version }}</code></div>
            </div>
            <div class="posture-row">
              <div><strong>User registration</strong><span>Even when enabled, only the atomic first-user setup path can register.</span></div>
              <div class="posture-value"><StatusBadge :state="registrationBadge">{{ registrationLabel }}</StatusBadge></div>
            </div>
            <div class="posture-row">
              <div><strong>Trusted direct peers</strong><span>Explicit CIDRs bypass CRS inspection and authentication attempt buckets.</span></div>
              <div class="posture-value"><StatusBadge state="ok">{{ status.trusted_networks.networks?.length ?? 0 }} networks</StatusBadge></div>
            </div>
          </div>
          <div class="config-note">
            <Icon name="key" :size="13" />
            <span><code>security.waf_mode</code> from {{ configSource(status.waf.source, status.waf.env_var) }}</span>
            <span><code>security.enable_registration</code> from {{ configSource(status.registration.source, status.registration.env_var) }}</span>
            <NuxtLink to="/settings/configuration">Configuration details</NuxtLink>
          </div>
        </SettingsSection>

        <SettingsSection title="Login protection" icon="timer" description="Independent buckets stop both source-based stuffing and distributed attacks against one account.">
          <KVTable :rows="[
            { key: 'Per source IP', value: `${status.login.by_ip.burst} attempt burst · ${refillLabel(status.login.by_ip.refill_seconds)}` },
            { key: 'Per account', value: `${status.login.by_account.burst} attempt burst · ${refillLabel(status.login.by_account.refill_seconds)}` },
            { key: 'Password verifier', value: `${status.login.stats.password_checks_active} active / ${status.login.stats.password_check_capacity} concurrent` },
            { key: 'Tracked sources', value: `${status.login.stats.active_ip_buckets.toLocaleString()} IP · ${status.login.stats.active_account_buckets.toLocaleString()} account` },
            { key: 'Admitted attempts', value: status.login.stats.allowed_total.toLocaleString() },
            { key: 'Limiter rejections', value: status.login.stats.throttled_total.toLocaleString() },
            { key: 'Verifier saturation', value: status.login.stats.saturated_total.toLocaleString() },
            { key: 'Memory bound', value: `${status.login.tracked_key_capacity.toLocaleString()} tracked keys` },
          ]" />
        </SettingsSection>
      </div>

      <SettingsSection
        title="Trusted networks"
        icon="network"
        description="Direct-peer addresses in these CIDRs bypass OWASP CRS inspection and login/registration attempt buckets. Changes are applied immediately."
      >
        <template #actions>
          <StatusBadge :state="trustedLocked ? 'idle' : 'ok'">{{ trustedLocked ? 'Env locked' : 'Live editable' }}</StatusBadge>
        </template>
        <div class="trusted-editor">
          <SettingsField
            label="Allowed IP addresses and CIDRs"
            description="One per line. Individual IP addresses are accepted and normalized to /32 or /128. An empty list trusts nobody."
            :lockedBy="trustedLocked ? `Locked by ${status.trusted_networks.env_var}` : undefined"
            v-slot="{ fieldId }"
          >
            <textarea
              :id="fieldId"
              v-model="trustedDraft"
              class="sv2-textarea trusted-textarea"
              rows="4"
              spellcheck="false"
              autocomplete="off"
              placeholder="100.64.0.0/10&#10;192.168.0.0/16"
              :disabled="trustedLocked || trustedSaving"
              @input="trustedDirty = true"
            />
          </SettingsField>
          <div class="trusted-warning">
            <Icon name="shield" :size="13" />
            <span>Trust is based only on the accepted connection's direct peer—not <code>X-Forwarded-For</code>. Credentials, permissions, CSRF protection, request-size limits, and password-verifier capacity still apply.</span>
          </div>
          <div class="trusted-save">
            <code>{{ configSource(status.trusted_networks.source, status.trusted_networks.env_var) }}</code>
            <button class="sv2-btn primary" :disabled="!trustedDirty || trustedSaving || trustedLocked" @click="saveTrustedNetworks">
              <Icon v-if="trustedSaving" name="spinner" :size="13" />
              {{ trustedSaving ? 'Applying…' : 'Apply trusted networks' }}
            </button>
          </div>
          <SettingsFlash :flash="trustedFlash" />
        </div>
      </SettingsSection>

      <div class="posture-grid">
        <SettingsSection title="Passwords & credentials" icon="key">
          <KVTable :rows="[
            { key: 'New password length', value: `${status.password.minimum_length}–${status.password.maximum_length} characters` },
            { key: 'Password hashing', value: status.password.hash_algorithm },
            { key: 'Legacy account hashes', value: status.password.legacy_hashes_upgraded ? 'Upgraded after successful login' : 'Not upgraded automatically' },
            { key: 'Unknown usernames', value: status.password.unknown_user_timing_defense ? 'Dummy hash check prevents a cheap timing oracle' : 'No timing equalization' },
            { key: 'Password changes', value: status.password.password_change_revokes_other_credentials ? 'Revoke other sessions and API tokens' : 'Credentials remain active' },
          ]" />
        </SettingsSection>

        <SettingsSection title="HTTP safeguards" icon="network">
          <KVTable :rows="[
            { key: 'Security headers', value: status.http.security_headers ? 'Enforced' : 'Off' },
            { key: 'Content Security Policy', value: status.http.csp_mode },
            { key: 'Cookie-session CSRF', value: status.http.same_origin_csrf_gate ? 'Same-origin gate enforced' : 'Off' },
            { key: 'Public HTTPS', value: status.http.hsts_on_public_ingress ? 'HSTS enabled on remote and Funnel ingress' : 'No HSTS policy' },
            { key: 'Application body limit', value: formatBytes(status.http.application_body_limit_bytes) },
            { key: 'Forwarded client headers', value: status.http.trusted_forwarded_headers ? 'Trusted proxy configured' : 'Ignored; direct peer only' },
          ]" />
        </SettingsSection>
      </div>

      <SettingsSection title="Recent security events" icon="pulse"
        :description="`Newest first · bounded to ${status.events.capacity} events · no credentials, headers, request bodies, query strings, or WAF match data retained.`">
        <template #actions>
          <StatusBadge :state="events.length ? 'warn' : 'ok'">{{ events.length }} retained</StatusBadge>
        </template>
        <div v-if="events.length === 0" class="empty-state">
          <Icon name="shield" :size="15" /> No rejected authentication attempts or WAF signals in this process yet.
        </div>
        <div v-else class="event-list">
          <div v-for="event in events" :key="event.id" class="event-row">
            <div class="event-icon"><Icon :name="eventIcon(event)" :size="15" /></div>
            <div class="event-body">
              <div class="event-head">
                <strong>{{ eventTitle(event) }}</strong>
                <StatusBadge :state="eventTone(event)">{{ event.action || event.severity || 'observed' }}</StatusBadge>
              </div>
              <p v-if="eventDetail(event)">{{ eventDetail(event) }}</p>
              <div class="event-meta">
                <span>{{ formatTime(event.time) }}</span>
                <span v-if="event.transaction_id">transaction {{ event.transaction_id }}</span>
              </div>
            </div>
          </div>
        </div>
      </SettingsSection>
    </template>
  </div>
</template>

<style scoped>
.loading-state, .empty-state {
  display: flex; align-items: center; gap: 8px;
  padding: 14px; border: 1px solid var(--border); border-radius: var(--r-md);
  color: var(--fg-3); background: var(--bg-2); font-size: 12px;
}
.tiles { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 8px; margin-bottom: 20px; }
.posture-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.posture-grid :deep(.sv2-section) { height: calc(100% - 16px); }
.posture-list { border: 1px solid var(--border); border-radius: var(--r-md); overflow: hidden; background: var(--bg-2); }
.posture-row {
  display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 16px; align-items: center;
  padding: 12px 14px; border-bottom: 1px solid var(--border);
}
.posture-row:last-child { border-bottom: 0; }
.posture-row > div:first-child { display: flex; flex-direction: column; gap: 3px; }
.posture-row strong { color: var(--fg-1); font-size: 12px; }
.posture-row span { color: var(--fg-3); font-size: 11px; line-height: 1.45; }
.posture-value { display: flex; align-items: flex-end; flex-direction: column; gap: 5px; }
.posture-value code, .config-note code { color: var(--fg-2); font-family: var(--font-mono); font-size: 10.5px; }
.config-note {
  display: flex; flex-wrap: wrap; gap: 8px 14px; align-items: center;
  margin-top: 10px; padding: 9px 11px; color: var(--fg-3); font-size: 10.5px;
  border: 1px dashed var(--border); border-radius: var(--r-sm);
}
.config-note > svg { color: var(--gold); }
.config-note a { margin-left: auto; color: var(--gold); text-decoration: none; }
.event-list { border: 1px solid var(--border); border-radius: var(--r-md); overflow: hidden; background: var(--bg-2); }
.event-row { display: grid; grid-template-columns: 34px minmax(0, 1fr); gap: 10px; padding: 11px 13px; border-bottom: 1px solid var(--border); }
.event-row:last-child { border-bottom: 0; }
.event-icon { width: 30px; height: 30px; display: grid; place-items: center; border-radius: var(--r-sm); background: rgb(var(--ink) / .04); color: var(--gold); }
.event-body { min-width: 0; }
.event-head { display: flex; align-items: center; gap: 8px; }
.event-head strong { color: var(--fg-1); font-size: 12px; }
.event-head :deep(.sv2-badge) { margin-left: auto; }
.event-body p { margin: 5px 0 0; color: var(--fg-2); font-family: var(--font-mono); font-size: 10.5px; overflow-wrap: anywhere; }
.event-meta { display: flex; flex-wrap: wrap; gap: 5px 12px; margin-top: 5px; color: var(--fg-4); font-family: var(--font-mono); font-size: 9.5px; }
.trusted-editor { display: flex; flex-direction: column; gap: 10px; }
.trusted-textarea { width: 100%; min-height: 104px; resize: vertical; font-family: var(--font-mono); font-size: 11px; line-height: 1.6; }
.trusted-warning { display: flex; align-items: flex-start; gap: 8px; padding: 10px 12px; border: 1px dashed var(--border); border-radius: var(--r-sm); color: var(--fg-3); font-size: 10.5px; line-height: 1.5; }
.trusted-warning > svg { flex: 0 0 auto; margin-top: 1px; color: var(--gold); }
.trusted-warning code, .trusted-save code { font-family: var(--font-mono); color: var(--fg-2); }
.trusted-save { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.trusted-save > code { font-size: 10.5px; }
@media (max-width: 1000px) { .tiles { grid-template-columns: repeat(2, minmax(0, 1fr)); } .posture-grid { grid-template-columns: 1fr; } }
@media (max-width: 600px) {
  .tiles { grid-template-columns: 1fr; }
  .posture-row { grid-template-columns: 1fr; }
  .posture-value { align-items: flex-start; }
  .config-note a { margin-left: 0; width: 100%; }
  .event-head { align-items: flex-start; }
  .trusted-save { align-items: stretch; flex-direction: column; }
}
</style>
