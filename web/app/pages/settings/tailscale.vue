<template>
  <div>
    <div class="page-header">
      <h2 class="page-title">Tailscale</h2>
      <p class="page-desc">
        Join your tailnet directly — no port forwarding, no reverse proxy.
        Flip the toggle below and you'll get a one-click login button.
      </p>
    </div>

    <section class="section">
      <div class="master-toggle">
        <div class="master-text">
          <div class="master-title">Tailscale integration</div>
          <div class="master-hint">
            {{ enabled ? 'Heya is exposed on your tailnet.' : 'Off — Heya only listens on the LAN.' }}
          </div>
        </div>
        <label class="switch lg" :title="lockTooltip('tailscale.enabled')">
          <input
            type="checkbox"
            :checked="enabled"
            :disabled="saving || isLocked('tailscale.enabled')"
            @change="onMasterToggle(($event.target as HTMLInputElement).checked)"
          >
          <span class="slider" />
        </label>
      </div>
    </section>

    <section v-if="enabled && status?.login_url" class="section">
      <a :href="status.login_url" target="_blank" rel="noopener" class="login-cta">
        <div class="login-cta-icon">
          <Icon name="cloud" :size="32" />
        </div>
        <div class="login-cta-text">
          <div class="login-cta-title">Authorize this device on your tailnet</div>
          <div class="login-cta-sub">Click to open Tailscale and approve the <code>{{ status.hostname }}</code> node. One time only.</div>
        </div>
        <Icon name="arrow-right" :size="20" />
      </a>
    </section>

    <template v-if="enabled">
      <section class="section">
        <h3 class="section-heading">
          <Icon name="pulse" :size="14" />
          Node status
        </h3>
        <div class="status-grid">
          <div class="status-card" :class="{ ok: status?.running, warn: status && !status.running }">
            <div class="status-label">Backend</div>
            <div class="status-value">{{ status?.backend_state || (saving ? 'Starting…' : 'Pending') }}</div>
          </div>
          <div class="status-card">
            <div class="status-label">Hostname</div>
            <div class="status-value mono">{{ status?.hostname || cfg?.hostname }}</div>
          </div>
          <div class="status-card">
            <div class="status-label">MagicDNS</div>
            <div class="status-value mono">{{ status?.magic_dns || '—' }}</div>
          </div>
          <div class="status-card">
            <div class="status-label">Tailnet IPv4</div>
            <div class="status-value mono">{{ status?.ipv4 || '—' }}</div>
          </div>
          <div class="status-card">
            <div class="status-label">Tailnet IPv6</div>
            <div class="status-value mono">{{ status?.ipv6 || '—' }}</div>
          </div>
          <div class="status-card">
            <div class="status-label">HTTPS cert</div>
            <div class="status-value mono">{{ status?.cert_domain || '—' }}</div>
          </div>
        </div>
      </section>

      <section v-if="status?.https_url || status?.funnel_url" class="section">
        <h3 class="section-heading">
          <Icon name="link" :size="14" />
          Access URLs
        </h3>
        <div class="url-grid">
          <a v-if="status?.https_url" :href="status.https_url" target="_blank" rel="noopener" class="url-card">
            <div class="url-label">
              HTTPS · tailnet only
              <span class="badge ok">active</span>
            </div>
            <div class="url-value mono">{{ status.https_url }}</div>
            <div class="url-hint">Reachable from any device on your tailnet.</div>
          </a>
          <a v-if="status?.funnel_url" :href="status.funnel_url" target="_blank" rel="noopener" class="url-card funnel">
            <div class="url-label">
              Funnel · public internet
              <span class="badge ok">active</span>
            </div>
            <div class="url-value mono">{{ status.funnel_url }}</div>
            <div class="url-hint">Reachable from anywhere — auth still applies.</div>
          </a>
        </div>
      </section>

      <section v-if="status?.last_error" class="section">
        <div class="error-card">
          <Icon name="warning" :size="20" />
          <div>
            <h3>Last error</h3>
            <pre class="code">{{ status.last_error }}</pre>
          </div>
        </div>
      </section>

      <section class="section">
        <h3 class="section-heading">
          <Icon name="settings" :size="14" />
          Settings
        </h3>

        <div class="form-row">
          <div class="form-text">
            <div class="form-title">Hostname</div>
            <div class="form-hint">Shown in the tailnet admin console. Changing this re-onboards the node.</div>
          </div>
          <input
            v-model="hostnameDraft"
            class="input"
            :disabled="saving || isLocked('tailscale.hostname')"
            :title="lockTooltip('tailscale.hostname')"
            @blur="saveHostname"
          >
        </div>

        <div class="toggle-row">
          <div class="toggle-text">
            <div class="toggle-title">
              HTTPS
              <span v-if="cfg?.https && status?.https_active" class="badge ok">active</span>
              <span v-else-if="cfg?.https" class="badge warn">requested · not active</span>
            </div>
            <div class="toggle-hint">
              Serve TLS on tailnet :443 using a Tailscale-issued cert.
              Requires HTTPS to be enabled for your tailnet in the
              <a href="https://login.tailscale.com/admin/dns/https" target="_blank" rel="noopener">admin console</a>.
            </div>
          </div>
          <label class="switch" :title="lockTooltip('tailscale.https')">
            <input
              type="checkbox"
              :checked="cfg?.https ?? true"
              :disabled="saving || isLocked('tailscale.https')"
              @change="saveHTTPS(($event.target as HTMLInputElement).checked)"
            >
            <span class="slider" />
          </label>
        </div>

        <div class="toggle-row">
          <div class="toggle-text">
            <div class="toggle-title">
              Funnel (public exposure)
              <span v-if="cfg?.funnel && status?.funnel_active" class="badge ok">active</span>
              <span v-else-if="cfg?.funnel" class="badge warn">requested · not active</span>
            </div>
            <div class="toggle-hint">
              Expose Heya on the public internet at
              <code v-if="status?.cert_domain">{{ status.cert_domain }}</code><code v-else>your MagicDNS name</code>.
              Heya's auth still applies. Requires Funnel to be
              <a href="https://login.tailscale.com/admin/settings/funnel" target="_blank" rel="noopener">allowed for your tailnet</a>
              and the node to be advertised with a permitted ACL tag.
              Flipping the toggle saves your preference — what tsnet
              actually does is up to your tailnet config.
            </div>
          </div>
          <label class="switch" :title="lockTooltip('tailscale.funnel')">
            <input
              type="checkbox"
              :checked="cfg?.funnel ?? false"
              :disabled="saving || isLocked('tailscale.funnel')"
              @change="saveFunnel(($event.target as HTMLInputElement).checked)"
            >
            <span class="slider" />
          </label>
        </div>
      </section>

      <section class="section">
        <h3 class="section-heading">
          <Icon name="logout" :size="14" />
          Identity
        </h3>
        <div class="actions">
          <button class="btn btn-secondary" :disabled="loggingOut" @click="onLogout">
            <Icon name="logout" :size="14" />
            {{ loggingOut ? 'Logging out…' : 'Log out of tailnet' }}
          </button>
        </div>
        <p class="hint">
          Clears the saved tailnet identity at <code>{{ stateDirHint }}</code> and disables Tailscale.
          Toggle Tailscale back on to re-onboard.
        </p>
      </section>

      <section class="section">
        <h3 class="section-heading">
          <Icon name="terminal" :size="14" />
          Raw tsnet status
          <button class="btn-link" @click="toggleRaw">
            {{ rawOpen ? 'Hide' : 'Show' }}
          </button>
        </h3>
        <div v-if="rawOpen" class="raw-panel">
          <div class="raw-toolbar">
            <button class="btn btn-secondary btn-sm" :disabled="rawLoading" @click="loadRaw">
              <Icon name="refresh" :size="12" />
              {{ rawLoading ? 'Loading…' : 'Refresh' }}
            </button>
            <button v-if="rawJSON" class="btn btn-secondary btn-sm" @click="copyRaw">
              <Icon name="copy" :size="12" />
              Copy JSON
            </button>
          </div>
          <div v-if="rawError" class="error-card">
            <Icon name="warning" :size="20" />
            <pre class="code">{{ rawError }}</pre>
          </div>
          <pre v-else-if="rawJSON" class="raw-json">{{ rawJSON }}</pre>
          <p v-else class="hint">Click Refresh to fetch the live status from tsnet's LocalClient — same blob <code>tailscale status --json</code> would print.</p>
        </div>
      </section>
    </template>

    <template v-else>
      <section class="section">
        <div class="empty-card">
          <Icon name="cloud" :size="32" />
          <p>
            Toggle Tailscale on above and you'll get a one-click sign-in button.
            Heya will join your tailnet as <code>heya.&lt;your-tailnet&gt;.ts.net</code> and serve
            the same UI you see now — accessible from any of your tailnet devices.
          </p>
        </div>
      </section>
    </template>
  </div>
</template>

<script setup lang="ts">
const { enabled, status, cfg, loading, refresh, saveConfig, setFunnel, logout, fetchRaw, subscribeToEvents } = useTailscale()
const { isLocked, lockTooltip, ensure: ensureSources } = useConfigSources()

const saving = ref(false)
const loggingOut = ref(false)
const hostnameDraft = ref('')
const rawOpen = ref(false)
const rawLoading = ref(false)
const rawJSON = ref<string>('')
const rawError = ref<string>('')

const stateDirHint = computed(() => cfg.value?.state_dir || 'data/tailscale/')

let unsubscribe: (() => void) | null = null

onMounted(async () => {
  await Promise.all([refresh(), ensureSources()])
  hostnameDraft.value = cfg.value?.hostname ?? 'heya'
  unsubscribe = subscribeToEvents()
})

onUnmounted(() => {
  unsubscribe?.()
})

watch(cfg, (next) => {
  if (next && hostnameDraft.value !== next.hostname && !saving.value) {
    hostnameDraft.value = next.hostname
  }
})

async function onMasterToggle(on: boolean) {
  saving.value = true
  try {
    await saveConfig({ enabled: on })
  } finally {
    saving.value = false
  }
}

async function saveHostname() {
  if (!cfg.value || hostnameDraft.value === cfg.value.hostname) return
  saving.value = true
  try {
    await saveConfig({ hostname: hostnameDraft.value })
  } finally {
    saving.value = false
  }
}

async function saveHTTPS(on: boolean) {
  saving.value = true
  try {
    await saveConfig({ https: on })
  } finally {
    saving.value = false
  }
}

async function saveFunnel(on: boolean) {
  saving.value = true
  try {
    await setFunnel(on)
  } finally {
    saving.value = false
  }
}

async function onLogout() {
  if (!confirm('Log out of the tailnet and disable Tailscale?')) return
  loggingOut.value = true
  try {
    await logout()
  } finally {
    loggingOut.value = false
  }
}

async function toggleRaw() {
  rawOpen.value = !rawOpen.value
  if (rawOpen.value && !rawJSON.value && !rawError.value) {
    await loadRaw()
  }
}

async function loadRaw() {
  rawLoading.value = true
  rawError.value = ''
  try {
    const data = await fetchRaw()
    rawJSON.value = JSON.stringify(data, null, 2)
  } catch (err: unknown) {
    rawError.value = err instanceof Error ? err.message : String(err)
    rawJSON.value = ''
  } finally {
    rawLoading.value = false
  }
}

async function copyRaw() {
  try {
    await navigator.clipboard.writeText(rawJSON.value)
  } catch {
    // best-effort; clipboard permissions can be denied
  }
}

// keep loading reactive
void loading
</script>

<style scoped>
.page-header {
  margin-bottom: 24px;
}

.page-title {
  font-size: 24px;
  font-weight: 600;
  margin: 0 0 4px;
}

.page-desc {
  color: var(--fg-3);
  margin: 0;
}

.section {
  margin-bottom: 24px;
}

.section-heading {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--fg-3);
  margin: 0 0 12px;
}

.master-toggle {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  padding: 20px 22px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}

.master-title {
  font-size: 16px;
  font-weight: 600;
  margin-bottom: 4px;
}

.master-hint {
  font-size: 13px;
  color: var(--fg-3);
}

.badge {
  margin-left: 8px;
  display: inline-block;
  padding: 1px 8px;
  border-radius: 999px;
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  font-family: var(--font-mono);
  vertical-align: middle;
}

.badge.ok {
  background: rgba(120, 200, 120, 0.18);
  color: rgb(150, 220, 150);
  border: 1px solid rgba(120, 200, 120, 0.4);
}

.badge.warn {
  background: rgba(230, 185, 74, 0.12);
  color: var(--gold-bright);
  border: 1px solid rgba(230, 185, 74, 0.4);
}

.url-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 12px;
}

.url-card {
  display: block;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 16px 18px;
  text-decoration: none;
  transition: border-color 0.15s ease;
}

.url-card:hover {
  border-color: var(--gold);
  text-decoration: none;
}

.url-card.funnel {
  border-color: rgba(120, 200, 120, 0.4);
  background: rgba(120, 200, 120, 0.06);
}

.url-card.funnel:hover {
  border-color: rgb(150, 220, 150);
}

.url-label {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 12px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--fg-1);
  margin-bottom: 8px;
}

.url-card.funnel .url-label {
  color: rgb(150, 220, 150);
}

.url-value {
  color: var(--fg-1);
  font-size: 15px;
  font-weight: 500;
  word-break: break-all;
  margin-bottom: 6px;
}

.url-hint {
  font-size: 13px;
  color: var(--fg-2);
  line-height: 1.4;
}

.btn-link {
  background: none;
  border: none;
  color: var(--gold-bright);
  font-size: 12px;
  margin-left: auto;
  cursor: pointer;
  text-transform: none;
  letter-spacing: 0;
  padding: 0;
}

.section-heading {
  display: flex;
  align-items: center;
}

.btn-sm {
  padding: 4px 10px;
  font-size: 12px;
}

.raw-panel {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 14px 16px;
}

.raw-toolbar {
  display: flex;
  gap: 8px;
  margin-bottom: 12px;
}

.raw-json {
  background: var(--bg-3);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  padding: 12px 14px;
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--fg-1);
  margin: 0;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 600px;
  overflow: auto;
}

.empty-card {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 32px;
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  color: var(--fg-2);
  gap: 12px;
}

.empty-card p {
  max-width: 540px;
  margin: 0;
  line-height: 1.5;
}

.login-cta {
  display: flex;
  align-items: center;
  gap: 18px;
  padding: 20px 22px;
  background: var(--gold-soft);
  border: 1px solid var(--gold);
  border-radius: var(--r-md);
  text-decoration: none;
  color: inherit;
  transition: background 0.15s ease;
}

.login-cta:hover {
  background: rgba(230, 185, 74, 0.18);
}

.login-cta-icon {
  width: 56px;
  height: 56px;
  border-radius: var(--r-md);
  background: rgba(230, 185, 74, 0.18);
  color: var(--gold-bright);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.login-cta-text {
  flex: 1;
  min-width: 0;
}

.login-cta-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--gold-bright);
  margin-bottom: 4px;
}

.login-cta-sub {
  font-size: 13px;
  color: var(--fg-2);
  line-height: 1.4;
}

.error-card {
  background: rgba(220, 80, 80, 0.08);
  border: 1px solid rgba(220, 80, 80, 0.4);
  border-radius: var(--r-md);
  padding: 16px 20px;
  display: flex;
  gap: 16px;
  align-items: flex-start;
}

.error-card h3 {
  margin: 0 0 6px;
  font-size: 14px;
  color: var(--fg-1);
}

.status-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 12px;
}

.status-card {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 14px 16px;
}

.status-card.ok {
  border-color: rgba(120, 200, 120, 0.4);
}

.status-card.warn {
  border-color: rgba(230, 185, 74, 0.4);
}

.status-label {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--fg-4);
  margin-bottom: 4px;
}

.status-value {
  font-size: 14px;
  color: var(--fg-1);
  font-weight: 500;
}

.status-value.mono {
  font-family: var(--font-mono);
  font-size: 13px;
  word-break: break-all;
}

.form-row,
.toggle-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  padding: 16px 18px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  margin-bottom: 10px;
}

.form-text,
.toggle-text {
  flex: 1;
  min-width: 0;
}

.form-title,
.toggle-title {
  font-size: 14px;
  font-weight: 500;
  margin-bottom: 4px;
}

.form-hint,
.toggle-hint {
  font-size: 12px;
  color: var(--fg-3);
  line-height: 1.5;
}

.input {
  background: var(--bg-3);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  padding: 8px 12px;
  color: var(--fg-1);
  font-family: var(--font-mono);
  font-size: 13px;
  width: 200px;
}

.input:focus {
  outline: none;
  border-color: var(--gold);
}

.switch {
  position: relative;
  display: inline-block;
  width: 44px;
  height: 24px;
  flex-shrink: 0;
}

.switch.lg {
  width: 52px;
  height: 28px;
}

.switch input {
  opacity: 0;
  width: 0;
  height: 0;
}

.slider {
  position: absolute;
  inset: 0;
  background: var(--bg-3);
  border-radius: 14px;
  cursor: pointer;
  transition: background 0.15s ease;
}

.slider::before {
  content: '';
  position: absolute;
  top: 2px;
  left: 2px;
  width: 20px;
  height: 20px;
  border-radius: 50%;
  background: var(--fg-1);
  transition: transform 0.15s ease;
}

.switch.lg .slider::before {
  width: 24px;
  height: 24px;
}

.switch input:checked + .slider {
  background: var(--gold);
}

.switch input:checked + .slider::before {
  transform: translateX(20px);
}

.switch.lg input:checked + .slider::before {
  transform: translateX(24px);
}

.switch input:disabled + .slider {
  opacity: 0.5;
  cursor: not-allowed;
}

.code {
  background: var(--bg-3);
  border-radius: var(--r-sm);
  padding: 12px 14px;
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--fg-1);
  margin: 0;
  white-space: pre-wrap;
  word-break: break-word;
  text-align: left;
  border: 1px solid var(--border);
}

.actions {
  display: flex;
  gap: 12px;
}

.hint {
  margin-top: 8px;
  font-size: 12px;
  color: var(--fg-3);
}

code {
  font-family: var(--font-mono);
  font-size: 12px;
  background: var(--bg-3);
  padding: 1px 6px;
  border-radius: 4px;
}

a {
  color: var(--gold-bright);
  text-decoration: none;
}

a:hover {
  text-decoration: underline;
}
</style>
